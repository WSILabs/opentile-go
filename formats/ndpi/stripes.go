package ndpi

import (
	"fmt"
	"io"

	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/tiff"
)

// NDPI vendor-private tag IDs for per-strip restart-marker offsets.
// These are added to `dataoffsets` + `databytecounts` by tifffile at IFD
// parse time so the page behaves as if it had N stripes rather than 1 —
// see tifffile.py:8239 (_gettags near `mcustarts = tags.valueof(65426)`).
const (
	tagMcuStarts          uint16 = 65426 // LONG[] byte offsets within strip
	tagMcuStartsHighBytes uint16 = 65432 // LONG[] high 32 bits of mcustarts (>4GB strips)
)

// StripInfo describes the native strip layout of an NDPI pyramid level.
// NDPI stores each level as ONE giant JPEG (tag 273 has one entry); the
// restart (RSTn) markers inside that JPEG delimit native strips, and
// NDPI private tag 65426 gives their byte offsets within the strip.
//
// Constructed once at Open() time; safe for concurrent read.
type StripInfo struct {
	StripW, StripH  int      // pixel dimensions of a native strip
	GridW, GridH    int      // grid dimensions (ceil(imageW/StripW), ceil(imageH/StripH))
	StripOffsets    []uint64 // per-strip absolute file offset
	StripByteCounts []uint64 // per-strip length
	JPEGHeader      []byte   // patched JPEG header (SOF rewritten to strip size)
}

// readStripes parses NDPI pyramid-level strip metadata from page p.
//
// If the page does NOT carry tag 65426 (McuStarts), readStripes returns
// (nil, nil) — signalling that the level is a true one-frame image and
// the caller should fall back to the oneFrameImage path.
//
// If the tag IS present, this function:
//
//  1. Reads McuStarts (and optionally McuStartsHighBytes for >4 GiB levels)
//     into a []uint64.
//  2. Computes per-strip absolute offsets and byte counts from the strip's
//     single (StripOffset, StripByteCount).
//  3. Reads mcuStarts[0] bytes of JPEG-header prefix from the file and
//     parses it via jpeg.NDPIStripeJPEGHeader to derive strip pixel
//     dimensions and a patched header with SOF dims set to strip size.
//  4. Returns a StripInfo the caller can embed in a stripedImage.
//
// Direct port of tifffile.TiffPage._gettags (tifffile.py:8239-8268) — the
// NDPI-specific block that rewrites `dataoffsets`/`databytecounts`.
func readStripes(p *tiff.Page, r io.ReaderAt) (*StripInfo, error) {
	if !p.HasTag(tagMcuStarts) {
		return nil, nil
	}
	iw, ok := p.ImageWidth()
	if !ok {
		return nil, fmt.Errorf("ndpi: striped page missing ImageWidth")
	}
	il, ok := p.ImageLength()
	if !ok {
		return nil, fmt.Errorf("ndpi: striped page missing ImageLength")
	}
	stripOff, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil {
		return nil, fmt.Errorf("ndpi: StripOffsets: %w", err)
	}
	stripLen, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil {
		return nil, fmt.Errorf("ndpi: StripByteCounts: %w", err)
	}
	if len(stripOff) != 1 || len(stripLen) != 1 {
		return nil, fmt.Errorf("ndpi: striped page expected 1 StripOffset/Count, got %d/%d", len(stripOff), len(stripLen))
	}

	mcuStartsLo, err := p.ScalarArrayU32(tagMcuStarts)
	if err != nil {
		return nil, fmt.Errorf("ndpi: McuStarts: %w", err)
	}
	if len(mcuStartsLo) == 0 {
		return nil, fmt.Errorf("ndpi: McuStarts is empty")
	}
	mcuStarts := make([]uint64, len(mcuStartsLo))
	for i, v := range mcuStartsLo {
		mcuStarts[i] = uint64(v)
	}
	if p.HasTag(tagMcuStartsHighBytes) {
		high, err := p.ScalarArrayU32(tagMcuStartsHighBytes)
		if err != nil {
			return nil, fmt.Errorf("ndpi: McuStartsHighBytes: %w", err)
		}
		if len(high) != len(mcuStartsLo) {
			return nil, fmt.Errorf("ndpi: McuStartsHighBytes length %d != McuStarts length %d", len(high), len(mcuStartsLo))
		}
		for i, h := range high {
			mcuStarts[i] |= uint64(h) << 32
		}
	}

	// Read the JPEG header prefix: the bytes before the first MCU.
	hdrLen := int(mcuStarts[0])
	if hdrLen <= 0 {
		return nil, fmt.Errorf("ndpi: invalid McuStarts[0]=%d", mcuStarts[0])
	}
	prefix := make([]byte, hdrLen)
	if err := tiff.ReadAtFull(r, prefix, int64(stripOff[0])); err != nil {
		return nil, fmt.Errorf("ndpi: read JPEG header prefix: %w", err)
	}
	stripeW, stripeH, patched, err := jpeg.NDPIStripeJPEGHeader(prefix)
	if err != nil {
		return nil, fmt.Errorf("ndpi: parse JPEG header prefix: %w", err)
	}
	if stripeW <= 0 || stripeH <= 0 {
		return nil, fmt.Errorf("ndpi: non-positive stripe size %dx%d", stripeW, stripeH)
	}

	// Compute per-stripe absolute offsets and byte counts.
	n := len(mcuStarts)
	offsets := make([]uint64, n)
	counts := make([]uint64, n)
	for i := 0; i < n; i++ {
		offsets[i] = stripOff[0] + mcuStarts[i]
		if i+1 < n {
			counts[i] = mcuStarts[i+1] - mcuStarts[i]
		} else {
			// Last stripe: from mcuStarts[i] to end of strip payload.
			counts[i] = stripLen[0] - mcuStarts[i]
		}
	}

	stripedW := (int(iw) + stripeW - 1) / stripeW
	stripedH := (int(il) + stripeH - 1) / stripeH
	if stripedW*stripedH != n {
		// Don't hard-fail — NDPI levels with irregular stripe counts exist,
		// but warn via error so callers can decide. All CMU-1 levels match
		// exactly, so a mismatch here points at a format quirk we haven't
		// seen yet.
		return nil, fmt.Errorf("ndpi: stripe count %d != stripedW*stripedH %d (W=%d H=%d stripe=%dx%d image=%dx%d)",
			n, stripedW*stripedH, stripedW, stripedH, stripeW, stripeH, iw, il)
	}

	return &StripInfo{
		StripW:          stripeW,
		StripH:          stripeH,
		GridW:           stripedW,
		GridH:           stripedH,
		StripOffsets:    offsets,
		StripByteCounts: counts,
		JPEGHeader:      patched,
	}, nil
}
