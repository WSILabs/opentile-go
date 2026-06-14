package svs

import (
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
	"github.com/wsilabs/opentile-go/internal/tiffstrip"
)

// newAssociatedImage dispatches construction by the IFD's actual compression,
// not by image role. Canonical Aperio stores thumbnail/overview as striped
// JPEG (assembled via ConcatenateScans) and the label as LZW; but ImageScope
// can re-export any associated image as LZW or uncompressed to match the
// pyramid's tile codec (GH #29 — an LZW thumbnail must not be routed to the
// JPEG-reassembly path). JPEG (tag 7) goes through the striped-JPEG assembler;
// every other codec (LZW / uncompressed / Deflate) goes through the
// compression-aware strip path, which reverses the predictor and stacks strips.
func newAssociatedImage(imageType opentile.AssociatedType, p *tiff.Page, r io.ReaderAt) (opentile.AssociatedImage, error) {
	comp, _ := p.Compression()
	if comp == 7 { // JPEG
		return newStripedJPEGAssociated(imageType, p, r)
	}
	return newStripedAssociated(imageType, p, r)
}

// stripedJPEGAssociated is the SVS AssociatedImage implementation for
// thumbnail and overview pages. Bytes() assembles a standalone JPEG from the
// TIFF strips via internal/jpeg.ConcatenateScans, injecting the page's
// JPEGTables and an APP14 "Adobe" marker to signal RGB colorspace (Aperio
// stores non-standard RGB JPEG).
//
// mcuW/mcuH are the JPEG MCU pixel dimensions detected once at construction
// time via jpeg.MCUSizeOf on the first strip (with JPEGTables prepended when
// the strips themselves don't carry SOF tables). Used for DRI / restart-
// interval computation in Bytes().
type stripedJPEGAssociated struct {
	imageType    opentile.AssociatedType
	size         opentile.Size
	stripOffsets []uint64
	stripCounts  []uint64
	jpegTables   []byte
	mcuW, mcuH   int
	rowsPerStrip int
	samples      int
	photometric  int
	reader       io.ReaderAt
	ifdOffset    int64
	tiffTags     opentile.TIFFTags
}

func (a *stripedJPEGAssociated) Type() opentile.AssociatedType     { return a.imageType }
func (a *stripedJPEGAssociated) Size() opentile.Size               { return a.size }
func (a *stripedJPEGAssociated) Compression() opentile.Compression { return opentile.CompressionJPEG }

// Decode returns the decoded pixels. It first tries the assembled standalone
// JPEG (Bytes() via ConcatenateScans); if libjpeg rejects it (some Aperio
// strip layouts produce "extraneous bytes before marker"), it falls back to
// decoding each strip's abbreviated JPEG independently and stacking them.
func (a *stripedJPEGAssociated) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	if data, err := a.Bytes(); err == nil {
		if img, derr := assocdecode.ViaCodec(opentile.CompressionJPEG, data, opts); derr == nil {
			return img, nil
		}
	}
	return a.decodeStripStack(opts)
}

// decodeStripStack decodes each strip's abbreviated JPEG (tables in
// JPEGTables, RGB via APP14) and stacks them vertically by decoded height.
func (a *stripedJPEGAssociated) decodeStripStack(opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale > 1 {
		return nil, decoder.ErrUnsupportedScale
	}
	fac, ok := decoder.GetByCompressionTag(7) // JPEG
	if !ok {
		return nil, fmt.Errorf("svs: no JPEG decoder: %w", decoder.ErrCodecUnavailable)
	}
	dec := fac.New()
	defer dec.Close()
	full := decoder.NewImageFormat(a.size.W, a.size.H, opts.Format)
	bpp := 3
	if opts.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	y0 := 0
	for i := range a.stripOffsets {
		buf := make([]byte, a.stripCounts[i])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
			return nil, fmt.Errorf("svs: read associated strip %d: %w", i, err)
		}
		data := buf
		if len(a.jpegTables) > 0 {
			d, err := jpeg.InsertTablesAndAPP14(buf, a.jpegTables)
			if err != nil {
				return nil, fmt.Errorf("svs: splice tables strip %d: %w", i, err)
			}
			data = d
		}
		sub, err := dec.Decode(data, decoder.DecodeOptions{Format: opts.Format})
		if err != nil {
			return nil, fmt.Errorf("svs: decode associated strip %d: %w", i, err)
		}
		rows := sub.Height
		if y0+rows > full.Height {
			rows = full.Height - y0
		}
		w := sub.Width
		if w > full.Width {
			w = full.Width
		}
		for y := 0; y < rows; y++ {
			copy(full.Pix[(y0+y)*full.Stride:(y0+y)*full.Stride+w*bpp], sub.Pix[y*sub.Stride:y*sub.Stride+w*bpp])
		}
		y0 += sub.Height
	}
	return full, nil
}

func (a *stripedJPEGAssociated) Bytes() ([]byte, error) {
	fragments := make([][]byte, len(a.stripOffsets))
	for i := range a.stripOffsets {
		buf := make([]byte, a.stripCounts[i])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
			return nil, fmt.Errorf("svs: read associated strip %d: %w", i, err)
		}
		fragments[i] = buf
	}
	if a.size.W > 0xFFFF || a.size.H > 0xFFFF {
		return nil, fmt.Errorf("svs: associated %s %dx%d exceeds SOF uint16", a.imageType, a.size.W, a.size.H)
	}

	// RestartInterval matches Python opentile's Jpeg.concatenate_scans:
	// scan_size.area // mcu_area, where scan_size is the FIRST strip's own
	// SOF dimensions (width × RowsPerStrip, pre-padding) and mcu_area comes
	// from the strip's sampling factors. A value of 0 (single-strip thumb)
	// produces no DRI marker — matching what Python's _manipulate_header
	// emits when restart_interval is 0 (the find-existing path updates the
	// payload; the insert path creates `FF DD 00 04 00 00`, which on decode
	// means "no restart"). We intentionally propagate 0 through as-is to
	// avoid emitting a useless DRI.
	ri, err := computeRestartInterval(fragments, a.mcuW, a.mcuH)
	if err != nil {
		return nil, fmt.Errorf("svs: associated %s restart interval: %w", a.imageType, err)
	}

	return jpeg.ConcatenateScans(fragments, jpeg.ConcatOpts{
		Width:           uint16(a.size.W),
		Height:          uint16(a.size.H),
		JPEGTables:      a.jpegTables,
		ColorspaceFix:   true,
		RestartInterval: ri,
	})
}

// computeRestartInterval matches Python opentile's Jpeg.concatenate_scans
// computation:
//
//	restart_interval = scan_size.area // mcu_area
//
// where scan_size is the first strip's JPEG SOF dimensions (W × H from
// the strip's own SOF, NOT the TIFF ImageWidth/RowsPerStrip, which
// differ when the encoder pads) and mcu_area is mcuW * mcuH (derived from
// the strip's luma sampling factors, computed once at construction time
// via jpeg.MCUSizeOf and threaded through here).
//
// This deviates from the original Go implementation which used
// TIFF ImageWidth and a hard-coded 16×16 MCU (Aperio 4:2:0 assumption);
// CMU-1-Small-Region.svs uses 4:4:4 (subsample=0, MCU 8×8), so the
// hardcoded value produced an incorrect DRI payload and a single-byte
// divergence from Python.
func computeRestartInterval(fragments [][]byte, mcuW, mcuH int) (int, error) {
	if len(fragments) == 0 {
		return 0, nil
	}
	if mcuW <= 0 || mcuH <= 0 {
		return 0, fmt.Errorf("invalid MCU size %dx%d", mcuW, mcuH)
	}
	if len(fragments) < 2 {
		// Single strip: Python emits restart_interval = scan_size.area /
		// mcu_size. For a correctly-sized single-strip image this is
		// effectively redundant but Python does emit it. We match
		// unconditionally to avoid byte divergence on edge cases where
		// the page has >1 strip but the caller never gets multi-strip.
		// If it's a single fragment AND only one strip total, Python's
		// behaviour still writes the DRI. Return the computed value, not 0.
	}
	sof, err := parseFirstSOF(fragments[0])
	if err != nil {
		return 0, fmt.Errorf("parse first strip SOF: %w", err)
	}
	// scan_size = (sof.Width, sof.Height) — the first strip's own SOF.
	return (int(sof.Width) * int(sof.Height)) / (mcuW * mcuH), nil
}

// parseFirstSOF returns the first SOF0 payload from frag, parsed.
func parseFirstSOF(frag []byte) (*jpeg.SOF, error) {
	return jpeg.FirstFragmentSOF(frag)
}

// Encoding returns the abbreviated-JPEG strips + JPEGTables for
// faithful standalone re-emission (GH #22). A consumer writes the strips +
// tag 347 (JPEGTables) verbatim into a fresh IFD.
func (a *stripedJPEGAssociated) Encoding() (opentile.AssociatedEncoding, bool) {
	strips, err := readStrips(a.reader, a.stripOffsets, a.stripCounts)
	if err != nil {
		return opentile.AssociatedEncoding{}, false
	}
	return opentile.AssociatedEncoding{
		Strips:       strips,
		Compression:  opentile.CompressionJPEG,
		JPEGTables:   a.jpegTables,
		RowsPerStrip: a.rowsPerStrip,
		Samples:      a.samples,
		Photometric:  a.photometric,
	}, true
}

// TIFFTags returns the parsed TIFF tags of this associated image's backing IFD.
func (a *stripedJPEGAssociated) TIFFTags() (opentile.TIFFTags, bool) {
	if a.tiffTags == nil {
		return nil, false
	}
	return a.tiffTags, true
}

// IFDOffset returns the byte offset of this associated image's backing IFD.
func (a *stripedJPEGAssociated) IFDOffset() (int64, bool) {
	if a.ifdOffset <= 0 {
		return 0, false
	}
	return a.ifdOffset, true
}

func newStripedJPEGAssociated(imageType opentile.AssociatedType, p *tiff.Page, r io.ReaderAt) (*stripedJPEGAssociated, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("svs: associated %s ImageWidth missing", imageType)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("svs: associated %s ImageLength missing", imageType)
	}
	offsets, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("svs: associated %s strip offsets: %w", imageType, err)
	}
	counts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("svs: associated %s strip counts: %w", imageType, err)
	}
	if len(offsets) != len(counts) {
		return nil, fmt.Errorf("svs: associated %s strip tag mismatch: offsets=%d counts=%d", imageType, len(offsets), len(counts))
	}
	if len(offsets) == 0 {
		return nil, fmt.Errorf("svs: associated %s has no strips", imageType)
	}
	tables, _ := p.JPEGTables()

	// Derive MCU size once at construction time from the first strip's SOF.
	// Aperio SVS strips embed their own SOF0 segment, so the strip bytes are
	// sufficient on their own (no need to splice JPEGTables first). If the
	// strip happens to lack a SOF (unusual but possible if a vendor strips
	// per-strip headers) we fall back to 16x16 — the Aperio 4:2:0 default —
	// which preserves v0.2 behavior on those inputs.
	firstStripe := make([]byte, counts[0])
	if err := tiff.ReadAtFull(r, firstStripe, int64(offsets[0])); err != nil {
		return nil, fmt.Errorf("svs: read first stripe for MCU detection: %w", err)
	}
	mcuW, mcuH := 16, 16
	if w, h, err := jpeg.MCUSizeOf(firstStripe); err == nil {
		mcuW, mcuH = w, h
	} else if len(tables) > 4 {
		// Fallback: some encoders may not embed a SOF in each strip; in that
		// case build a minimal valid JPEG header by splicing JPEGTables'
		// inner segments around the strip.
		header := []byte{0xFF, 0xD8}
		header = append(header, tables[2:len(tables)-2]...)
		assembled := append(header, firstStripe...)
		assembled = append(assembled, 0xFF, 0xD9)
		if w, h, err2 := jpeg.MCUSizeOf(assembled); err2 == nil {
			mcuW, mcuH = w, h
		}
	}

	rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)
	spp, _ := p.SamplesPerPixel()
	photo, _ := p.Photometric()
	return &stripedJPEGAssociated{
		imageType:    imageType,
		size:         opentile.Size{W: int(iw), H: int(il)},
		stripOffsets: offsets,
		stripCounts:  counts,
		jpegTables:   tables,
		mcuW:         mcuW,
		mcuH:         mcuH,
		rowsPerStrip: int(rps),
		samples:      int(spp),
		photometric:  int(photo),
		reader:       r,
		ifdOffset:    p.IFDOffset(),
		tiffTags:     opentile.TIFFTagsFromPage(p),
	}, nil
}

// stripedAssociated is the SVS AssociatedImage implementation for any
// non-JPEG associated image (label, and — for ImageScope re-exports —
// thumbnail/overview too). Compression varies (LZW=5 in all three CMU
// labels, but can be uncompressed or Deflate); upstream SvsLabelImage
// returns the raw first strip bytes and advertises whatever Compression the
// TIFF carries. imageType records the page's role so Type() reports it
// faithfully regardless of which codec the strips use.
type stripedAssociated struct {
	imageType    opentile.AssociatedType
	size         opentile.Size
	compression  opentile.Compression
	stripOffsets []uint64
	stripCounts  []uint64
	rowsPerStrip int
	samples      int
	predictor    int
	photometric  int
	reader       io.ReaderAt
	ifdOffset    int64
	tiffTags     opentile.TIFFTags
}

func (a *stripedAssociated) Type() opentile.AssociatedType     { return a.imageType }
func (a *stripedAssociated) Size() opentile.Size               { return a.size }
func (a *stripedAssociated) Compression() opentile.Compression { return a.compression }

// Decode returns the faithfully-decoded label pixels. Unlike Bytes() — which
// re-encodes LZW and drops the predictor — this decompresses the strips,
// reverses Predictor=2 horizontal differencing, and interprets the samples
// as RGB(A) (GH #20). Strip-based codecs only (LZW / Deflate / none); the
// CMU Aperio labels are LZW + Predictor=2.
func (a *stripedAssociated) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	strips := make([][]byte, len(a.stripOffsets))
	for i := range a.stripOffsets {
		buf := make([]byte, a.stripCounts[i])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
			return nil, fmt.Errorf("svs: read %s strip %d: %w", a.imageType, i, err)
		}
		strips[i] = buf
	}
	return tiffstrip.Decode(tiffstrip.Params{
		Width:        a.size.W,
		Height:       a.size.H,
		Samples:      a.samples,
		Photometric:  a.photometric,
		Predictor:    a.predictor,
		Compression:  int(opentile.CompressionToTIFFTag(a.compression)),
		RowsPerStrip: a.rowsPerStrip,
		Strips:       strips,
	}, opts)
}

// Bytes returns the full label as a single compressed bytestream.
//
// Single-strip labels are returned as-is. Multi-strip LZW labels (typical
// for CMU fixtures) are decoded strip-by-strip, the decoded raster is
// concatenated row-major, and the result is re-encoded as a single LZW
// stream covering the full image height. This deviates from the upstream
// Python opentile 0.20.0 SvsLabelImage.get_tile((0,0)) which returns only
// strip 0 — a long-standing upstream bug; we'll file a PR there separately
// so parity can re-engage once Python lands the same fix.
func (a *stripedAssociated) Bytes() ([]byte, error) {
	if len(a.stripOffsets) == 0 || len(a.stripCounts) == 0 {
		return nil, fmt.Errorf("svs: %s has no strips", a.imageType)
	}
	if len(a.stripOffsets) == 1 {
		// Single-strip image: decode-restitch is a no-op; return as-is.
		buf := make([]byte, a.stripCounts[0])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[0])); err != nil {
			return nil, fmt.Errorf("svs: read %s strip 0: %w", a.imageType, err)
		}
		return buf, nil
	}
	strips := make([][]byte, len(a.stripOffsets))
	for i := range a.stripOffsets {
		buf := make([]byte, a.stripCounts[i])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
			return nil, fmt.Errorf("svs: read %s strip %d: %w", a.imageType, i, err)
		}
		strips[i] = buf
	}
	switch a.compression {
	case opentile.CompressionLZW:
		return reconstructLZWLabel(strips, a.rowsPerStrip, a.size.H, a.size.W, a.samples)
	case opentile.CompressionNone:
		// Uncompressed strips are raw pixel rows; concatenating them row-major
		// yields the full-height raster (still a valid single uncompressed
		// bytestream).
		var total int
		for _, s := range strips {
			total += len(s)
		}
		out := make([]byte, 0, total)
		for _, s := range strips {
			out = append(out, s...)
		}
		return out, nil
	}
	// Other multi-strip codecs (Deflate/JPEG) would each need their own
	// restitch path; we haven't seen one in the wild yet.
	return nil, fmt.Errorf("svs: multi-strip %s compression %s unsupported", a.imageType, a.compression)
}

// Encoding returns the LZW label's source strips + tags for faithful
// standalone re-emission (GH #22). The label keeps Predictor (typically 2);
// a consumer MUST emit tag 317 or the differencing isn't reversed.
func (a *stripedAssociated) Encoding() (opentile.AssociatedEncoding, bool) {
	strips, err := readStrips(a.reader, a.stripOffsets, a.stripCounts)
	if err != nil {
		return opentile.AssociatedEncoding{}, false
	}
	return opentile.AssociatedEncoding{
		Strips:       strips,
		Compression:  a.compression,
		Predictor:    a.predictor,
		RowsPerStrip: a.rowsPerStrip,
		Samples:      a.samples,
		Photometric:  a.photometric,
	}, true
}

// TIFFTags returns the parsed TIFF tags of this associated image's backing IFD.
func (a *stripedAssociated) TIFFTags() (opentile.TIFFTags, bool) {
	if a.tiffTags == nil {
		return nil, false
	}
	return a.tiffTags, true
}

// IFDOffset returns the byte offset of this associated image's backing IFD.
func (a *stripedAssociated) IFDOffset() (int64, bool) {
	if a.ifdOffset <= 0 {
		return 0, false
	}
	return a.ifdOffset, true
}

func newStripedAssociated(imageType opentile.AssociatedType, p *tiff.Page, r io.ReaderAt) (*stripedAssociated, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("svs: %s ImageWidth missing", imageType)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("svs: %s ImageLength missing", imageType)
	}
	offsets, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("svs: %s strip offsets: %w", imageType, err)
	}
	counts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("svs: %s strip counts: %w", imageType, err)
	}
	comp, _ := p.Compression()
	rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)
	spp, _ := p.SamplesPerPixel()
	pred, _ := p.Predictor()
	photo, _ := p.Photometric()
	return &stripedAssociated{
		imageType:    imageType,
		size:         opentile.Size{W: int(iw), H: int(il)},
		compression:  tiffCompressionToOpentile(comp),
		stripOffsets: offsets,
		stripCounts:  counts,
		rowsPerStrip: int(rps),
		predictor:    int(pred),
		photometric:  int(photo),
		samples:      int(spp),
		reader:       r,
		ifdOffset:    p.IFDOffset(),
		tiffTags:     opentile.TIFFTagsFromPage(p),
	}, nil
}

// tiffCompressionToOpentile maps TIFF tag 259 numeric values to the
// opentile.Compression enum. Unknown codes (including vendor-specific ones)
// become CompressionUnknown so consumers can still get the raw bytes but
// know the codec is not advertised.
//
// Shared by both the tiled level path (tiled.go) and the associated-image
// path (this file) — kept in one place so adding a new code (e.g. LZW) is a
// single-line change covering every SVS consumer.
func tiffCompressionToOpentile(tiffCode uint32) opentile.Compression {
	switch tiffCode {
	case 1:
		return opentile.CompressionNone
	case 5:
		return opentile.CompressionLZW
	case 7:
		return opentile.CompressionJPEG
	case 33003, 33005: // APERIO_JP2000_YCBC / APERIO_JP2000_RGB
		return opentile.CompressionJP2K
	}
	return opentile.CompressionUnknown
}

// readStrips reads the strips at the given offsets/counts into memory.
func readStrips(r io.ReaderAt, offsets, counts []uint64) ([][]byte, error) {
	strips := make([][]byte, len(offsets))
	for i := range offsets {
		buf := make([]byte, counts[i])
		if err := tiff.ReadAtFull(r, buf, int64(offsets[i])); err != nil {
			return nil, fmt.Errorf("svs: read associated strip %d: %w", i, err)
		}
		strips[i] = buf
	}
	return strips, nil
}
