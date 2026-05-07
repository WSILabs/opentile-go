package ndpi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"
	"sync"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/jpegturbo"
	"github.com/cornish/opentile-go/internal/tiff"
)

// stripedImage is an NDPI pyramid level whose source is a single giant JPEG
// strip subdivided by restart (RSTn) markers into "native stripes" (one
// DRI interval per stripe). Output tiles are assembled by concatenating
// the relevant stripe scan fragments with a patched JPEG header, then
// lossless-cropping the assembly to the output tile size via
// libjpeg-turbo.
//
// This is a direct port of opentile's NdpiStripedImage (see
// opentile/formats/ndpi/ndpi_image.py:408-580). The frame-caching shape
// matches upstream: multiple tiles sharing a frame position reuse the
// assembled frame; edge tiles use a smaller frame so we never crop past
// the image bounds.
type stripedImage struct {
	index       int
	pyrIndex    int
	size        opentile.Size // image pixel size
	tileSize    opentile.Size // output tile size
	grid        opentile.Size // output tile grid
	stripes     *StripInfo
	compression opentile.Compression
	mpp         opentile.SizeMm
	reader      io.ReaderAt

	// frameSize = max(tileSize, stripeSize) — the default frame geometry
	// for non-edge tiles. Stored so we don't recompute on every Tile call.
	frameSize opentile.Size

	// dcBackground is the post-quantisation luma DC coefficient to plant
	// in OOB DCT blocks during edge-tile CropWithBackground calls. Derived
	// once at construction from the level's shared JPEGHeader DQT (the DC
	// quant doesn't vary across stripes/tiles of the same level), then
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
	//
	// Note: v0.2 does not bound the cache size. A worst-case pyramid-level
	// pass iterates tiles in row-major order, so each frame is assembled
	// once and then every tile-inside-frame reads from the same entry
	// until the next frame is needed. Memory cost per entry is ~frame
	// JPEG size (~100s of KB for a 512x512 tile-equivalent frame).
	frameMu     sync.Mutex
	framesByKey map[frameKey][]byte
}

type frameKey struct {
	posX, posY, w, h int
}

func newStripedImage(
	index int,
	p *tiff.Page,
	tileSize opentile.Size,
	stripes *StripInfo,
	r io.ReaderAt,
) (*stripedImage, error) {
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
	dc, err := jpeg.LuminanceToDCCoefficient(stripes.JPEGHeader, float64(jpegturbo.DefaultBackgroundLuminance))
	if err != nil {
		return nil, fmt.Errorf("ndpi: derive luma DC for level %d: %w", index, err)
	}
	return &stripedImage{
		index:              index,
		size:               size,
		tileSize:           tileSize,
		grid:               opentile.Size{W: gridW, H: gridH},
		stripes:            stripes,
		compression:        opentile.CompressionJPEG,
		reader:             r,
		frameSize:          maxSize(tileSize, opentile.Size{W: stripes.StripW, H: stripes.StripH}),
		dcBackground:       dc,
		headersByFrameSize: make(map[opentile.Size][]byte),
		framesByKey:        make(map[frameKey][]byte),
	}, nil
}

func (l *stripedImage) Index() int                        { return l.index }
func (l *stripedImage) PyramidIndex() int                 { return l.pyrIndex }
func (l *stripedImage) Size() opentile.Size               { return l.size }
func (l *stripedImage) TileSize() opentile.Size           { return l.tileSize }
func (l *stripedImage) Grid() opentile.Size               { return l.grid }
func (l *stripedImage) Compression() opentile.Compression { return l.compression }
func (l *stripedImage) MPP() opentile.SizeMm              { return l.mpp }
func (l *stripedImage) FocalPlane() float64               { return 0 }
func (l *stripedImage) TileOverlap() image.Point          { return image.Point{} }

// TileAt is the multi-dim entry point. NDPI is 2D-only.
func (l *stripedImage) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.C != 0 || coord.T != 0 {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: opentile.ErrDimensionUnavailable}
	}
	return l.Tile(coord.X, coord.Y)
}

// warm pre-faults the page-cache pages backing every native stripe
// on this level. NDPI's striped path packs the level's compressed
// data into one TIFF strip subdivided by JPEG restart markers; the
// per-strip StripOffsets/StripByteCounts are the byte ranges that
// matter for warming.
func (l *stripedImage) warm() error {
	for i, off := range l.stripes.StripOffsets {
		if err := tiff.TouchPages(l.reader, int64(off), int64(l.stripes.StripByteCounts[i])); err != nil {
			return err
		}
	}
	return nil
}

// TileMaxSize returns a generous upper bound for compressed tile
// output. NDPI's stripedImage.Tile produces a freshly-encoded JPEG
// via libjpeg-turbo whose exact size depends on entropy coding; we
// return tileSize.W * tileSize.H as a worst-case bound (one byte
// per pixel — JPEG output rarely exceeds that on real photographic
// data). Callers using TileInto with a dst sized to TileMaxSize
// have ample headroom; the actual output is typically ~5–10% of
// this bound.
func (l *stripedImage) TileMaxSize() int { return l.tileSize.W * l.tileSize.H }

// TileInto writes the tile bytes into dst. NDPI's striped path
// internally allocates (frame assembly + libjpeg-turbo crop output);
// dst receives the final copy. Pool savings at the boundary still
// apply, but per-tile allocation isn't fully eliminated for this
// format.
func (l *stripedImage) TileInto(x, y int, dst []byte) (int, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return 0, err
	}
	if len(dst) < len(b) {
		return 0, io.ErrShortBuffer
	}
	return copy(dst, b), nil
}

func (l *stripedImage) Tile(x, y int) ([]byte, error) {
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

func (l *stripedImage) TileReader(x, y int) (io.ReadCloser, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (l *stripedImage) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
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
// a native stripe is smaller than the output tile — the upstream-original
// case. When the native stripe is wider/taller than the tile (the more
// common NDPI layout), this returns the default frame size and any resulting
// crop that extends past image bounds falls through to CropWithBackground
// in Tile(); see docs/deferred.md L12 for the OOB fill parity story.
func (l *stripedImage) frameSizeForTile(x, y int) opentile.Size {
	w := l.frameSize.W
	h := l.frameSize.H
	sw := l.stripes.StripW
	sh := l.stripes.StripH
	if x == l.grid.W-1 && sw < l.tileSize.W {
		w = sw*l.stripes.GridW - x*l.tileSize.W
	}
	if y == l.grid.H-1 && sh < l.tileSize.H {
		h = sh*l.stripes.GridH - y*l.tileSize.H
	}
	return opentile.Size{W: w, H: h}
}

// framePosition computes the top-left tile coordinate of the frame that
// covers tile (x, y). Mirrors NdpiTile's frame_position math: group tiles
// by "tiles per frame", multiply by tile size → pixel top-left of frame
// divided by tile size = tile-coord top-left of frame.
func (l *stripedImage) framePosition(x, y int, frameSize opentile.Size) opentile.Size {
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
func (l *stripedImage) getFrame(framePos, frameSize opentile.Size) ([]byte, error) {
	key := frameKey{posX: framePos.W, posY: framePos.H, w: frameSize.W, h: frameSize.H}
	l.frameMu.Lock()
	if b, ok := l.framesByKey[key]; ok {
		l.frameMu.Unlock()
		return b, nil
	}
	l.frameMu.Unlock()

	frame, err := l.assembleFrame(framePos, frameSize)
	if err != nil {
		return nil, err
	}

	l.frameMu.Lock()
	if existing, ok := l.framesByKey[key]; ok {
		l.frameMu.Unlock()
		return existing, nil
	}
	l.framesByKey[key] = frame
	l.frameMu.Unlock()
	return frame, nil
}

// assembleFrame reads the stripe fragments covering (framePos, frameSize)
// and concatenates them into a single JPEG, inserting restart markers at
// fragment boundaries and prefixing a size-patched header.
//
// Direct port of NdpiStripedImage._read_extended_frame (ndpi_image.py:527-563)
// plus Jpeg.concatenate_fragments (jpeg/jpeg.py:78-102).
func (l *stripedImage) assembleFrame(framePos, frameSize opentile.Size) ([]byte, error) {
	header, err := l.getPatchedHeader(frameSize)
	if err != nil {
		return nil, err
	}

	// Region of native stripes that covers the frame.
	stripeStartX := (framePos.W * l.tileSize.W) / l.stripes.StripW
	stripeStartY := (framePos.H * l.tileSize.H) / l.stripes.StripH
	stripeCountX := maxInt(frameSize.W/l.stripes.StripW, 1)
	stripeCountY := maxInt(frameSize.H/l.stripes.StripH, 1)

	// Clip at the right/bottom edge of the native stripe grid — NDPI
	// images that are not a multiple of stripe width end at stripedW etc.
	if stripeStartX+stripeCountX > l.stripes.GridW {
		stripeCountX = l.stripes.GridW - stripeStartX
	}
	if stripeStartY+stripeCountY > l.stripes.GridH {
		stripeCountY = l.stripes.GridH - stripeStartY
	}
	if stripeCountX <= 0 || stripeCountY <= 0 {
		return nil, fmt.Errorf("ndpi: empty stripe region for frame pos %v size %v", framePos, frameSize)
	}

	// Pre-size the output buffer. Header + stripes + trailing EOI.
	estSize := len(header) + 2
	for sy := stripeStartY; sy < stripeStartY+stripeCountY; sy++ {
		for sx := stripeStartX; sx < stripeStartX+stripeCountX; sx++ {
			idx := sy*l.stripes.GridW + sx
			estSize += int(l.stripes.StripByteCounts[idx])
		}
	}
	out := make([]byte, 0, estSize)
	out = append(out, header...)

	fragIdx := 0
	for sy := stripeStartY; sy < stripeStartY+stripeCountY; sy++ {
		for sx := stripeStartX; sx < stripeStartX+stripeCountX; sx++ {
			idx := sy*l.stripes.GridW + sx
			count := int(l.stripes.StripByteCounts[idx])
			off := int64(l.stripes.StripOffsets[idx])
			buf := make([]byte, count)
			if err := tiff.ReadAtFull(l.reader, buf, off); err != nil {
				return nil, fmt.Errorf("ndpi: read stripe (%d,%d) idx=%d: %w", sx, sy, idx, err)
			}
			// Each stripe ends with FF RSTn — or FF D9 (EOI) on the very
			// last stripe of the level, which upstream opentile
			// (Jpeg.concatenate_fragments) silently treats the same way:
			// drop the trailing byte and append a global RSTn. Validate
			// the penultimate byte is 0xFF and the trailing byte falls
			// in the expected marker range.
			if count < 2 {
				return nil, fmt.Errorf("ndpi: stripe idx=%d too short (%d bytes)", idx, count)
			}
			if buf[count-2] != 0xFF {
				return nil, fmt.Errorf("ndpi: stripe idx=%d does not end with FF marker (got %02X %02X)",
					idx, buf[count-2], buf[count-1])
			}
			last := buf[count-1]
			if !(last >= 0xD0 && last <= 0xD7) && last != 0xD9 {
				return nil, fmt.Errorf("ndpi: stripe idx=%d trailing marker %02X outside [D0..D7, D9]",
					idx, last)
			}
			// Drop the stripe's own trailing marker byte and append the
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
func (l *stripedImage) getPatchedHeader(frameSize opentile.Size) ([]byte, error) {
	l.headerMu.Lock()
	if b, ok := l.headersByFrameSize[frameSize]; ok {
		l.headerMu.Unlock()
		return b, nil
	}
	l.headerMu.Unlock()

	if frameSize.H < 0 || frameSize.W < 0 || frameSize.H > 0xFFFF || frameSize.W > 0xFFFF {
		return nil, fmt.Errorf("ndpi: SOF size %dx%d out of uint16 range", frameSize.W, frameSize.H)
	}
	patched, err := jpeg.ReplaceSOFDimensions(l.stripes.JPEGHeader, uint16(frameSize.W), uint16(frameSize.H))
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
