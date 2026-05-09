package svs

import (
	"fmt"
	"io"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/tiff"
)

// newAssociatedImage dispatches construction by kind. Thumbnail and overview
// are striped JPEG assembled via ConcatenateScans; label is raw strip
// passthrough (codec as advertised by the TIFF Compression tag).
func newAssociatedImage(kind string, p *tiff.Page, r io.ReaderAt) (opentile.AssociatedImage, error) {
	switch kind {
	case "thumbnail", "overview":
		return newStripedJPEGAssociated(kind, p, r)
	case "label":
		return newStripedLabel(p, r)
	}
	return nil, fmt.Errorf("svs: unknown associated kind %q", kind)
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
	kind         string
	size         opentile.Size
	stripOffsets []uint64
	stripCounts  []uint64
	jpegTables   []byte
	mcuW, mcuH   int
	reader       io.ReaderAt
}

func (a *stripedJPEGAssociated) Type() string                      { return a.kind }
func (a *stripedJPEGAssociated) Size() opentile.Size               { return a.size }
func (a *stripedJPEGAssociated) Compression() opentile.Compression { return opentile.CompressionJPEG }

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
		return nil, fmt.Errorf("svs: associated %s %dx%d exceeds SOF uint16", a.kind, a.size.W, a.size.H)
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
		return nil, fmt.Errorf("svs: associated %s restart interval: %w", a.kind, err)
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

func newStripedJPEGAssociated(kind string, p *tiff.Page, r io.ReaderAt) (*stripedJPEGAssociated, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("svs: associated %s ImageWidth missing", kind)
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("svs: associated %s ImageLength missing", kind)
	}
	offsets, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("svs: associated %s strip offsets: %w", kind, err)
	}
	counts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("svs: associated %s strip counts: %w", kind, err)
	}
	if len(offsets) != len(counts) {
		return nil, fmt.Errorf("svs: associated %s strip tag mismatch: offsets=%d counts=%d", kind, len(offsets), len(counts))
	}
	if len(offsets) == 0 {
		return nil, fmt.Errorf("svs: associated %s has no strips", kind)
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

	return &stripedJPEGAssociated{
		kind:         kind,
		size:         opentile.Size{W: int(iw), H: int(il)},
		stripOffsets: offsets,
		stripCounts:  counts,
		jpegTables:   tables,
		mcuW:         mcuW,
		mcuH:         mcuH,
		reader:       r,
	}, nil
}

// stripedLabel is the SVS AssociatedImage implementation for the label page.
// Label compression varies (LZW=5 in all three CMU fixtures, but can be JPEG
// or uncompressed); upstream SvsLabelImage returns the raw first strip bytes
// and advertises whatever Compression the TIFF carries. Callers are expected
// to decode with an external codec if needed.
type stripedLabel struct {
	size         opentile.Size
	compression  opentile.Compression
	stripOffsets []uint64
	stripCounts  []uint64
	rowsPerStrip int
	samples      int
	reader       io.ReaderAt
}

func (a *stripedLabel) Type() string                      { return "label" }
func (a *stripedLabel) Size() opentile.Size               { return a.size }
func (a *stripedLabel) Compression() opentile.Compression { return a.compression }

// Bytes returns the full label as a single compressed bytestream.
//
// Single-strip labels are returned as-is. Multi-strip LZW labels (typical
// for CMU fixtures) are decoded strip-by-strip, the decoded raster is
// concatenated row-major, and the result is re-encoded as a single LZW
// stream covering the full image height. This deviates from the upstream
// Python opentile 0.20.0 SvsLabelImage.get_tile((0,0)) which returns only
// strip 0 — a long-standing upstream bug; we'll file a PR there separately
// so parity can re-engage once Python lands the same fix.
func (a *stripedLabel) Bytes() ([]byte, error) {
	if len(a.stripOffsets) == 0 || len(a.stripCounts) == 0 {
		return nil, fmt.Errorf("svs: label has no strips")
	}
	if len(a.stripOffsets) == 1 {
		// Single-strip label: decode-restitch is a no-op; return as-is.
		buf := make([]byte, a.stripCounts[0])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[0])); err != nil {
			return nil, fmt.Errorf("svs: read label strip 0: %w", err)
		}
		return buf, nil
	}
	// Multi-strip label: only LZW is supported in v0.3. JPEG/uncompressed
	// multi-strip labels would need their own restitch path; we haven't seen
	// one in the wild yet (all three CMU fixtures are LZW=5).
	if a.compression != opentile.CompressionLZW {
		return nil, fmt.Errorf("svs: multi-strip label compression %s unsupported (LZW only in v0.3)", a.compression)
	}
	strips := make([][]byte, len(a.stripOffsets))
	for i := range a.stripOffsets {
		buf := make([]byte, a.stripCounts[i])
		if err := tiff.ReadAtFull(a.reader, buf, int64(a.stripOffsets[i])); err != nil {
			return nil, fmt.Errorf("svs: read label strip %d: %w", i, err)
		}
		strips[i] = buf
	}
	return reconstructLZWLabel(strips, a.rowsPerStrip, a.size.H, a.size.W, a.samples)
}

func newStripedLabel(p *tiff.Page, r io.ReaderAt) (*stripedLabel, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("svs: label ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("svs: label ImageLength missing")
	}
	offsets, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("svs: label strip offsets: %w", err)
	}
	counts, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("svs: label strip counts: %w", err)
	}
	comp, _ := p.Compression()
	rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)
	spp, _ := p.SamplesPerPixel()
	return &stripedLabel{
		size:         opentile.Size{W: int(iw), H: int(il)},
		compression:  tiffCompressionToOpentile(comp),
		stripOffsets: offsets,
		stripCounts:  counts,
		rowsPerStrip: int(rps),
		samples:      int(spp),
		reader:       r,
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
