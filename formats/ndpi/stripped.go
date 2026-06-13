package ndpi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"
	"runtime"
	"sync"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/decoderhandle"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/jpegturbo"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// strippedImage is an NDPI pyramid level whose source is a single giant JPEG
// strip subdivided by restart (RSTn) markers into "native strips" (one
// DRI interval per strip). Output tiles are assembled by concatenating
// the relevant strip scan fragments with a patched JPEG header, then
// lossless-cropping the assembly to the output tile size via
// libjpeg-turbo.
//
// This is a direct port of opentile's NdpiStripedImage (see
// opentile/formats/ndpi/ndpi_image.py:408-580). The frame-caching shape
// matches upstream: multiple tiles sharing a frame position reuse the
// assembled frame; edge tiles use a smaller frame so we never crop past
// the image bounds.
type strippedImage struct {
	index       int
	pyrIndex    int
	size        opentile.Size // image pixel size
	tileSize    opentile.Size // output tile size
	grid        opentile.Size // output tile grid
	strips      *StripInfo
	compression opentile.Compression
	mpp         opentile.MPP
	reader      io.ReaderAt

	// frameSize = max(tileSize, stripSize) — the default frame geometry
	// for non-edge tiles. Stored so we don't recompute on every Tile call.
	frameSize opentile.Size

	// dcBackground is the post-quantisation luma DC coefficient to plant
	// in OOB DCT blocks during edge-tile CropWithBackground calls. Derived
	// once at construction from the level's shared JPEGHeader DQT (the DC
	// quant doesn't vary across strips/tiles of the same level), then
	// passed via jpegturbo.CropOpts on every edge-tile call. Saves a
	// per-call DQT byte-scan inside libjpeg-turbo's wrapper.
	//
	// Always uses luminance=1.0 (the white-fill default that matches
	// Python opentile's PyTurboJPEG.crop_multiple). If a future caller
	// needs a different luminance per tile they must compute the DC
	// themselves and pass it via CropOpts; the legacy
	// CropWithBackground / CropWithBackgroundLuminance APIs still derive
	// per-call.
	dcBackground int

	// Patched-header cache. Keyed by frame size (the crop-safe frame
	// geometry varies at the image edges). Populated lazily; an entry
	// for each unique frame size is built once and reused thereafter.
	headerMu           sync.Mutex
	headersByFrameSize map[opentile.Size][]byte

	// Assembled-frame cache. Keyed by (framePos, frameSize); the value is
	// a complete JPEG that covers the frame. Populated lazily per-call.
	// Byte-bounded LRU (128 MiB default) — replaces the v0.2 unbounded
	// map. On a single-pass DZI traversal each frame is assembled once
	// and read by all tiles-inside-frame before eviction; the bound
	// keeps OS-2's ~0.6 GB C3 in check for random-access Tile() callers.
	frames *frameByteLRU

	// Pixel-frame cache. Decoded RGB frames keyed by (framePos,
	// frameSize). Bounded LRU; max(NumCPU, 16) entries. Populated
	// lazily by strippedImage.DecodedTile (v0.27).
	pixelCache *pixelFrameCache

	// Reusable decoder handle for the fast pixel path. Lazy-init on
	// first DecodedTile call via decHandleOnce so non-DecodedTile
	// users pay no decoder-creation cost.
	decHandle     *decoderhandle.Pool
	decHandleOnce sync.Once
}

type frameKey struct {
	posX, posY, w, h int
}

func newStrippedImage(
	index int,
	p *tiff.Page,
	tileSize opentile.Size,
	strips *StripInfo,
	r io.ReaderAt,
) (*strippedImage, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ndpi: ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ndpi: ImageLength missing")
	}
	size := opentile.Size{W: int(iw), H: int(il)}
	gridW := (size.W + tileSize.W - 1) / tileSize.W
	gridH := (size.H + tileSize.H - 1) / tileSize.H
	// Pre-compute the OOB-fill DC coefficient from the level's shared
	// DQT so edge-tile CropWithBackground calls can skip the per-call
	// DQT parse. The header carries the DQT verbatim — no need to
	// assemble a frame to look it up.
	dc, err := jpeg.LuminanceToDCCoefficient(strips.JPEGHeader, float64(jpegturbo.DefaultBackgroundLuminance))
	if err != nil {
		return nil, fmt.Errorf("ndpi: derive luma DC for level %d: %w", index, err)
	}
	return &strippedImage{
		index:              index,
		size:               size,
		tileSize:           tileSize,
		grid:               opentile.Size{W: gridW, H: gridH},
		strips:             strips,
		compression:        opentile.CompressionJPEG,
		reader:             r,
		frameSize:          maxSize(tileSize, opentile.Size{W: strips.StripW, H: strips.StripH}),
		dcBackground:       dc,
		headersByFrameSize: make(map[opentile.Size][]byte),
		frames:             newFrameByteLRU(128 << 20),
		pixelCache:         newPixelFrameCache(maxInt(runtime.NumCPU(), 16)),
	}, nil
}

func (l *strippedImage) Index() int                        { return l.index }
func (l *strippedImage) PyramidIndex() int                 { return l.pyrIndex }
func (l *strippedImage) Size() opentile.Size               { return l.size }
func (l *strippedImage) TileSize() opentile.Size           { return l.tileSize }
func (l *strippedImage) Grid() opentile.Size               { return l.grid }
func (l *strippedImage) Compression() opentile.Compression { return l.compression }
func (l *strippedImage) MPP() opentile.MPP                  { return l.mpp }
func (l *strippedImage) FocalPlane() float64               { return 0 }
func (l *strippedImage) TileOverlap() image.Point          { return image.Point{} }

// TileAt is the multi-dim entry point. NDPI is 2D-only.
func (l *strippedImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	return l.Tile(coord.X, coord.Y)
}

// warm pre-faults the page-cache pages backing every native strip
// on this level. NDPI's stripped path packs the level's compressed
// data into one TIFF strip subdivided by JPEG restart markers; the
// per-strip StripOffsets/StripByteCounts are the byte ranges that
// matter for warming.
func (l *strippedImage) warm() error {
	for i, off := range l.strips.StripOffsets {
		if err := tiff.TouchPages(l.reader, int64(off), int64(l.strips.StripByteCounts[i])); err != nil {
			return err
		}
	}
	return nil
}

// TileMaxSize returns a generous upper bound for compressed tile
// output. NDPI's strippedImage.Tile produces a freshly-encoded JPEG
// via libjpeg-turbo whose exact size depends on entropy coding; we
// return tileSize.W * tileSize.H as a worst-case bound (one byte
// per pixel — JPEG output rarely exceeds that on real photographic
// data). Callers using TileInto with a dst sized to TileMaxSize
// have ample headroom; the actual output is typically ~5–10% of
// this bound.
func (l *strippedImage) TileMaxSize() int { return l.tileSize.W * l.tileSize.H }

// TilePrefix returns nil — this Level type doesn't expose a separable
// per-level splice prefix in v0.13. T2-T4 specializations override
// for the splice-format levels.
//
// Added in v0.13.
func (l *strippedImage) TilePrefix() []byte { return nil }

// TileBodyInto delegates to TileInto (no separation between body
// bytes and full tile output for non-splice levels). T2-T4
// specializations override for the splice-format levels.
//
// Added in v0.13.
func (l *strippedImage) TileBodyInto(x, y int, dst []byte) (int, error) {
	return l.TileInto(x, y, dst)
}

// TileBodyMaxSize equals TileMaxSize for non-splice levels (the body
// IS the full tile output). T2-T4 specializations override.
//
// Added in v0.13.
func (l *strippedImage) TileBodyMaxSize() int { return l.TileMaxSize() }

// TileInto writes the tile bytes into dst. NDPI's stripped path
// internally allocates (frame assembly + libjpeg-turbo crop output);
// dst receives the final copy. Pool savings at the boundary still
// apply, but per-tile allocation isn't fully eliminated for this
// format.
func (l *strippedImage) TileInto(x, y int, dst []byte) (int, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return 0, err
	}
	if len(dst) < len(b) {
		return 0, io.ErrShortBuffer
	}
	return copy(dst, b), nil
}

func (l *strippedImage) Tile(x, y int) ([]byte, error) {
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	frameSize := l.frameSizeForTile(x, y)
	framePos := l.framePosition(x, y, frameSize)
	frame, err := l.getFrame(framePos, frameSize)
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	// Position of the tile's top-left inside the frame.
	denomX := maxInt(frameSize.W, l.tileSize.W)
	denomY := maxInt(frameSize.H, l.tileSize.H)
	left := (x * l.tileSize.W) % denomX
	top := (y * l.tileSize.H) % denomY

	// Always go through libjpeg-turbo, even when the crop is an identity
	// (frame == tile at origin). Upstream Python opentile calls
	// PyTurboJPEG.crop_multiple unconditionally; its tjTransform pass
	// rewrites the output JPEG's marker sequence to a canonical order
	// (SOF before DHT). An identity-region fast path that returns the
	// assembled frame as-is preserves input marker order (DHT before
	// SOF, as in the NDPI file) and diverges from upstream byte-for-byte
	// on interior tiles at smaller pyramid levels.
	region := jpegturbo.Region{X: left, Y: top, Width: l.tileSize.W, Height: l.tileSize.H}

	// Dispatch matches Python's __need_fill_background gate at
	// turbojpeg.py:839-863: route through CropWithBackground iff the
	// tile crosses the image edge in IMAGE coordinates. The pre-v0.4
	// "try Crop first, fall through on error" pattern silently
	// returned mid-gray OOB fills (libjpeg-turbo's default DC=0) on
	// tiles where Crop succeeded despite extending past the image —
	// the cached white-fill DC was never planted because the fall-
	// through path never fired. See L12 in docs/deferred.md and v0.4
	// Task 9 for the full reproduction. The v0.3 acc2282 commit's
	// "geometry-first inversion is unsafe" claim was wrong: the
	// inversion is safe — the previous attempt used assembled-frame
	// size as the comparator instead of image size, and the v0.3
	// fixtures had already encoded the buggy mid-gray output.
	tileXOrigin := x * l.tileSize.W
	tileYOrigin := y * l.tileSize.H
	extendsBeyond := tileXOrigin+l.tileSize.W > l.size.W || tileYOrigin+l.tileSize.H > l.size.H
	var out []byte
	if extendsBeyond {
		out, err = jpegturbo.CropWithBackgroundLuminanceOpts(
			frame, region, jpegturbo.DefaultBackgroundLuminance,
			jpegturbo.CropOpts{DCBackground: l.dcBackground},
		)
	} else {
		out, err = jpegturbo.Crop(frame, region)
	}
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return out, nil
}

// DecodedTile is the v0.27 fast pixel path. Returns the decoded
// pixels for tile (tx, ty) by looking up (or building) the assembled
// strip frame, decoding it once, and blitting the tile region out.
//
// Interior tiles take the cache+blit path. Edge tiles (extending
// past image bounds) fall back to the existing CropWithBackground +
// decode path via Tile() to preserve pixel-parity with Python
// opentile's white-fill behavior.
//
// Concurrency: safe for concurrent invocation. The pixel cache uses
// a promise pattern (one decode per cache miss regardless of fanout);
// the decoder handle serializes its internal libjpeg-turbo
// invocation under a mutex.
//
// opts.Scale != 1 falls through to the existing Tile()+decode path —
// the cache holds full-resolution frames only.
//
// Added in v0.27.
func (l *strippedImage) DecodedTile(tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if tx < 0 || ty < 0 || tx >= l.grid.W || ty >= l.grid.H {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: opentile.ErrTileOutOfBounds}
	}
	// Slow-path triggers: WithScale > 1 OR edge tile. Both share the
	// fallthrough to Tile() + handle-decode.
	tileXOrigin := tx * l.tileSize.W
	tileYOrigin := ty * l.tileSize.H
	extendsBeyond := tileXOrigin+l.tileSize.W > l.size.W ||
		tileYOrigin+l.tileSize.H > l.size.H
	if opts.Scale > 1 || extendsBeyond {
		return l.decodedTileViaCrop(tx, ty, opts)
	}

	// Fast path. Compute frame geometry (mirrors Tile()).
	frameSize := l.frameSizeForTile(tx, ty)
	framePos := l.framePosition(tx, ty, frameSize)
	key := frameKey{posX: framePos.W, posY: framePos.H, w: frameSize.W, h: frameSize.H}

	pixFrame, err := l.pixelCache.getOrLoad(key, func() (*decoder.Image, error) {
		jpegFrame, err := l.getFrame(framePos, frameSize)
		if err != nil {
			return nil, err
		}
		l.ensureDecHandle()
		if l.decHandle == nil {
			return nil, fmt.Errorf("ndpi: no decoder registered for %s", l.compression)
		}
		dec, err := l.decHandle.Borrow()
		if err != nil {
			return nil, err
		}
		defer l.decHandle.Return(dec)
		return dec.Decode(jpegFrame, decoder.DecodeOptions{
			Format: decoder.PixelFormatRGB,
		})
	})
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: err}
	}

	// Blit the tile region out of the cached pixel frame.
	denomX := maxInt(frameSize.W, l.tileSize.W)
	denomY := maxInt(frameSize.H, l.tileSize.H)
	left := (tx * l.tileSize.W) % denomX
	top := (ty * l.tileSize.H) % denomY

	outFormat := opts.Format
	if outFormat == 0 {
		outFormat = decoder.PixelFormatRGB
	}
	// v0.29 Layer 2 prereq: honor opts.Dst when caller provides a
	// buffer of matching dimensions+format. Falls back to allocation
	// otherwise (defensive — callers passing arbitrary Dst don't
	// panic).
	var out *decoder.Image
	if opts.Dst != nil &&
		opts.Dst.Width == l.tileSize.W &&
		opts.Dst.Height == l.tileSize.H &&
		opts.Dst.Format == outFormat {
		out = opts.Dst
	} else {
		out = decoder.NewImageFormat(l.tileSize.W, l.tileSize.H, outFormat)
	}
	blitFromFrame(pixFrame, left, top, l.tileSize.W, l.tileSize.H, out)
	return out, nil
}

// decodedTileViaCrop is the slow-path fallback used for edge tiles
// and WithScale != 1 calls. Equivalent to the v0.26 decode path
// (Tile() returns a tile-shaped JPEG, then decode), reusing the
// strippedImage's long-lived decoder handle.
func (l *strippedImage) decodedTileViaCrop(tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	jpegBytes, err := l.Tile(tx, ty)
	if err != nil {
		return nil, err
	}
	l.ensureDecHandle()
	if l.decHandle == nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty,
			Err: fmt.Errorf("ndpi: no decoder registered for %s", l.compression)}
	}
	dec, err := l.decHandle.Borrow()
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: err}
	}
	defer l.decHandle.Return(dec)
	out, err := dec.Decode(jpegBytes, opts)
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: tx, Y: ty, Err: err}
	}
	return out, nil
}

func (l *strippedImage) ensureDecHandle() {
	l.decHandleOnce.Do(func() {
		tag := opentile.CompressionToTIFFTag(l.compression)
		fac, ok := decoder.GetByCompressionTag(tag)
		if !ok {
			return
		}
		capacity := runtime.NumCPU()
		if capacity > 8 {
			capacity = 8
		}
		l.decHandle = decoderhandle.New(fac, capacity)
	})
}

// closeResources releases the long-lived decoder handle. Called from
// the parent tiler.Close. Safe to call multiple times.
func (l *strippedImage) closeResources() error {
	if l.decHandle == nil {
		return nil
	}
	return l.decHandle.Close()
}

// blitFromFrame copies a srcW × srcH region starting at (srcX, srcY)
// from src into dst at (0,0). Both images are RGB or RGBA; widening
// from RGB src → RGBA dst pads alpha=0xFF, narrowing from RGBA src →
// RGB dst drops alpha. dst's bounds determine the blit extent.
func blitFromFrame(src *decoder.Image, srcX, srcY, srcW, srcH int, dst *decoder.Image) {
	srcBpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		srcBpp = 4
	}
	dstBpp := 3
	if dst.Format == decoder.PixelFormatRGBA {
		dstBpp = 4
	}
	rows := srcH
	if rows > dst.Height {
		rows = dst.Height
	}
	cols := srcW
	if cols > dst.Width {
		cols = dst.Width
	}
	for r := 0; r < rows; r++ {
		so := (srcY+r)*src.Stride + srcX*srcBpp
		do := r * dst.Stride
		if srcBpp == dstBpp {
			copy(dst.Pix[do:do+cols*dstBpp], src.Pix[so:so+cols*srcBpp])
			continue
		}
		for c := 0; c < cols; c++ {
			dst.Pix[do+0] = src.Pix[so+0]
			dst.Pix[do+1] = src.Pix[so+1]
			dst.Pix[do+2] = src.Pix[so+2]
			if dstBpp == 4 {
				dst.Pix[do+3] = 0xFF
			}
			so += srcBpp
			do += dstBpp
		}
	}
}

func (l *strippedImage) TileReader(x, y int) (io.ReadCloser, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (l *strippedImage) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
		for y := 0; y < l.grid.H; y++ {
			for x := 0; x < l.grid.W; x++ {
				if err := ctx.Err(); err != nil {
					yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Err: err})
					return
				}
				b, err := l.Tile(x, y)
				if !yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Bytes: b, Err: err}) {
					return
				}
			}
		}
	}
}

// frameSizeForTile mirrors NdpiStripedImage._get_frame_size_for_tile. The
// narrowing conditions (sw < tileSize.W / sh < tileSize.H) fire only when
// a native strip is smaller than the output tile — the upstream-original
// case. When the native strip is wider/taller than the tile (the more
// common NDPI layout), this returns the default frame size and any resulting
// crop that extends past image bounds falls through to CropWithBackground
// in Tile(); see docs/deferred.md L12 for the OOB fill parity story.
func (l *strippedImage) frameSizeForTile(x, y int) opentile.Size {
	w := l.frameSize.W
	h := l.frameSize.H
	sw := l.strips.StripW
	sh := l.strips.StripH
	if x == l.grid.W-1 && sw < l.tileSize.W {
		w = sw*l.strips.GridW - x*l.tileSize.W
	}
	if y == l.grid.H-1 && sh < l.tileSize.H {
		h = sh*l.strips.GridH - y*l.tileSize.H
	}
	return opentile.Size{W: w, H: h}
}

// framePosition computes the top-left tile coordinate of the frame that
// covers tile (x, y). Mirrors NdpiTile's frame_position math: group tiles
// by "tiles per frame", multiply by tile size → pixel top-left of frame
// divided by tile size = tile-coord top-left of frame.
func (l *strippedImage) framePosition(x, y int, frameSize opentile.Size) opentile.Size {
	tpfX := maxInt(frameSize.W/l.tileSize.W, 1)
	tpfY := maxInt(frameSize.H/l.tileSize.H, 1)
	return opentile.Size{
		W: (x / tpfX) * tpfX,
		H: (y / tpfY) * tpfY,
	}
}

// getFrame returns (and caches) the assembled JPEG covering framePos at
// frameSize. Cache key uses tile-coord position and pixel size so distinct
// edge-tile frames don't collide with the interior-frame key.
//
// Unlike the v0.2 double-checked-lock map this replaced, concurrent misses
// for the same key may each run assembleFrame independently; that is safe
// because assembleFrame is deterministic and byte-identical per key, so the
// redundant work (only on the slow random-access Tile() path — the
// ScaledStrips fast path serializes per-key assembly inside
// pixelCache.getOrLoad) merely costs a repeat read, never a wrong result.
func (l *strippedImage) getFrame(framePos, frameSize opentile.Size) ([]byte, error) {
	key := frameKey{posX: framePos.W, posY: framePos.H, w: frameSize.W, h: frameSize.H}
	if b, ok := l.frames.get(key); ok {
		return b, nil
	}
	frame, err := l.assembleFrame(framePos, frameSize)
	if err != nil {
		return nil, err
	}
	l.frames.put(key, frame)
	return frame, nil
}

// assembleFrame reads the strip fragments covering (framePos, frameSize)
// and concatenates them into a single JPEG, inserting restart markers at
// fragment boundaries and prefixing a size-patched header.
//
// Direct port of NdpiStripedImage._read_extended_frame (ndpi_image.py:527-563)
// plus Jpeg.concatenate_fragments (jpeg/jpeg.py:78-102).
func (l *strippedImage) assembleFrame(framePos, frameSize opentile.Size) ([]byte, error) {
	header, err := l.getPatchedHeader(frameSize)
	if err != nil {
		return nil, err
	}

	// Region of native strips that covers the frame.
	stripStartX := (framePos.W * l.tileSize.W) / l.strips.StripW
	stripStartY := (framePos.H * l.tileSize.H) / l.strips.StripH
	stripCountX := maxInt(frameSize.W/l.strips.StripW, 1)
	stripCountY := maxInt(frameSize.H/l.strips.StripH, 1)

	// Clip at the right/bottom edge of the native strip grid — NDPI
	// images that are not a multiple of strip width end at strippedW etc.
	if stripStartX+stripCountX > l.strips.GridW {
		stripCountX = l.strips.GridW - stripStartX
	}
	if stripStartY+stripCountY > l.strips.GridH {
		stripCountY = l.strips.GridH - stripStartY
	}
	if stripCountX <= 0 || stripCountY <= 0 {
		return nil, fmt.Errorf("ndpi: empty strip region for frame pos %v size %v", framePos, frameSize)
	}

	// Pre-size the output buffer. Header + strips + trailing EOI.
	estSize := len(header) + 2
	for sy := stripStartY; sy < stripStartY+stripCountY; sy++ {
		for sx := stripStartX; sx < stripStartX+stripCountX; sx++ {
			idx := sy*l.strips.GridW + sx
			estSize += int(l.strips.StripByteCounts[idx])
		}
	}
	out := make([]byte, 0, estSize)
	out = append(out, header...)

	fragIdx := 0
	for sy := stripStartY; sy < stripStartY+stripCountY; sy++ {
		for sx := stripStartX; sx < stripStartX+stripCountX; sx++ {
			idx := sy*l.strips.GridW + sx
			count := int(l.strips.StripByteCounts[idx])
			off := int64(l.strips.StripOffsets[idx])
			buf := make([]byte, count)
			if err := tiff.ReadAtFull(l.reader, buf, off); err != nil {
				return nil, fmt.Errorf("ndpi: read strip (%d,%d) idx=%d: %w", sx, sy, idx, err)
			}
			// Each strip ends with FF RSTn — or FF D9 (EOI) on the very
			// last strip of the level, which upstream opentile
			// (Jpeg.concatenate_fragments) silently treats the same way:
			// drop the trailing byte and append a global RSTn. Validate
			// the penultimate byte is 0xFF and the trailing byte falls
			// in the expected marker range.
			if count < 2 {
				return nil, fmt.Errorf("ndpi: strip idx=%d too short (%d bytes)", idx, count)
			}
			if buf[count-2] != 0xFF {
				return nil, fmt.Errorf("ndpi: strip idx=%d does not end with FF marker (got %02X %02X)",
					idx, buf[count-2], buf[count-1])
			}
			last := buf[count-1]
			if !(last >= 0xD0 && last <= 0xD7) && last != 0xD9 {
				return nil, fmt.Errorf("ndpi: strip idx=%d trailing marker %02X outside [D0..D7, D9]",
					idx, last)
			}
			// Drop the strip's own trailing marker byte and append the
			// globally-indexed RSTn so cycle counts line up across the
			// assembled frame. The leading 0xFF is the penultimate byte of
			// buf and is retained; we overwrite just the trailing marker.
			out = append(out, buf[:count-1]...)
			out = append(out, byte(0xD0+(fragIdx%8)))
			fragIdx++
		}
	}
	out = append(out, 0xFF, 0xD9) // EOI
	return out, nil
}

// getPatchedHeader returns (and caches) the JPEG header prefix patched so
// its SOF advertises the given frame size. Mirrors
// Jpeg._manipulate_header with size=frame_size.
func (l *strippedImage) getPatchedHeader(frameSize opentile.Size) ([]byte, error) {
	l.headerMu.Lock()
	if b, ok := l.headersByFrameSize[frameSize]; ok {
		l.headerMu.Unlock()
		return b, nil
	}
	l.headerMu.Unlock()

	if frameSize.H < 0 || frameSize.W < 0 || frameSize.H > 0xFFFF || frameSize.W > 0xFFFF {
		return nil, fmt.Errorf("ndpi: SOF size %dx%d out of uint16 range", frameSize.W, frameSize.H)
	}
	patched, err := jpeg.ReplaceSOFDimensions(l.strips.JPEGHeader, uint16(frameSize.W), uint16(frameSize.H))
	if err != nil {
		return nil, err
	}

	l.headerMu.Lock()
	if existing, ok := l.headersByFrameSize[frameSize]; ok {
		l.headerMu.Unlock()
		return existing, nil
	}
	l.headersByFrameSize[frameSize] = patched
	l.headerMu.Unlock()
	return patched, nil
}

func maxSize(a, b opentile.Size) opentile.Size {
	return opentile.Size{W: maxInt(a.W, b.W), H: maxInt(a.H, b.H)}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
