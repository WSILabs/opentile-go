package generictiff

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

// errUnsupportedAssociatedShape is returned by [newAssociatedImage]
// for IFD shapes we don't read in v0.10:
//
//   - Multi-strip Deflate (the re-encode side isn't implemented in
//     v0.10 — flate writers exist in stdlib but compose differently
//     from the LZW reuse pattern).
//   - Tiled associated images (rare; OME emits these but its own
//     format reader handles them).
//
// Tiler's Associated() builder filters these out silently: the IFD
// is recognised but not exposed.
var errUnsupportedAssociatedShape = errors.New("generic: associated image shape unsupported in v0.10")

// associatedImage is the generic-TIFF AssociatedImage. Bytes are
// read eagerly at construction time and cached — associated images
// are typically <2 MB so the memory cost is fine, and it lets the
// constructor enforce the supported-shape check up front.
type associatedImage struct {
	imageType   string
	size        opentile.Size
	compression opentile.Compression
	bytes       []byte
}

func (a *associatedImage) Type() string                      { return a.imageType }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }
func (a *associatedImage) Bytes() ([]byte, error) {
	out := make([]byte, len(a.bytes))
	copy(out, a.bytes)
	return out, nil
}

// newAssociatedImage dispatches on the IFD's shape (single-strip
// vs multi-strip × compression) and reads the associated-image
// bytes. Returns errUnsupportedAssociatedShape for the v0.10
// out-of-scope variants (multi-strip JPEG / Deflate, tiled).
func newAssociatedImage(imageType string, info associatedSourceInfo, r io.ReaderAt) (*associatedImage, error) {
	if info.tiled {
		// Tiled associated images out of scope for v0.10.
		return nil, fmt.Errorf("%w: tiled associated image", errUnsupportedAssociatedShape)
	}
	if len(info.stripOffsets) == 0 {
		return nil, fmt.Errorf("generic: associated %s has no strips", imageType)
	}
	if len(info.stripOffsets) != len(info.stripCounts) {
		return nil, fmt.Errorf("generic: associated %s strip-table mismatch (%d offsets, %d counts)",
			imageType, len(info.stripOffsets), len(info.stripCounts))
	}

	// Read every strip into memory. Limit total to a sanity ceiling
	// (32 MB) so a malicious / malformed page can't OOM us.
	const maxAssocBytes = 32 << 20
	var total uint64
	for _, c := range info.stripCounts {
		total += c
	}
	if total > maxAssocBytes {
		return nil, fmt.Errorf("generic: associated %s strips total %d bytes > %d max",
			imageType, total, maxAssocBytes)
	}

	stripBytes := make([][]byte, len(info.stripOffsets))
	for i := range info.stripOffsets {
		buf := make([]byte, info.stripCounts[i])
		if _, err := r.ReadAt(buf, int64(info.stripOffsets[i])); err != nil {
			return nil, fmt.Errorf("generic: associated %s read strip %d: %w", imageType, i, err)
		}
		stripBytes[i] = buf
	}

	out, ocomp, err := assembleAssociated(imageType, info, stripBytes)
	if err != nil {
		return nil, err
	}
	return &associatedImage{
		imageType:   imageType,
		size:        opentile.Size{W: int(info.width), H: int(info.height)},
		compression: ocomp,
		bytes:       out,
	}, nil
}

// associatedSourceInfo carries the IFD-level metadata the
// associated reader needs. Built by the Tiler from the *tiff.Page
// at Open time and passed in as a value.
type associatedSourceInfo struct {
	tiled        bool   // true if TileWidth + TileLength tags are present
	width        uint32 // ImageWidth
	height       uint32 // ImageLength
	rowsPerStrip uint32 // RowsPerStrip; 0 means "all rows in one strip"
	samples      uint32 // SamplesPerPixel
	compression  uint32 // TIFF tag 259 raw value
	stripOffsets []uint64
	stripCounts  []uint64
}

// assembleAssociated builds the final byte buffer for the
// associated image based on its compression + strip layout.
//
// Per spec §6 reader-path table:
//
//	Single-strip uncompressed/JPEG/LZW/Deflate → trivial passthrough
//	Multi-strip uncompressed                   → concat strip bytes
//	Multi-strip JPEG (libtiff RST layout)      → concat strip bytes
//	Multi-strip LZW                            → decode each strip,
//	                                              concat raw, re-encode
//	                                              as single LZW
//	Multi-strip Deflate                        → unsupported in v0.10
func assembleAssociated(imageType string, info associatedSourceInfo, strips [][]byte) ([]byte, opentile.Compression, error) {
	if len(strips) == 1 {
		// Single-strip path: the strip bytes ARE the associated
		// image bytes for any compression we support.
		out := make([]byte, len(strips[0]))
		copy(out, strips[0])
		return out, mapCompression(info.compression), nil
	}

	// Multi-strip path: dispatch by compression.
	switch info.compression {
	case 1: // None
		return concatStrips(strips), opentile.CompressionNone, nil
	case 7: // JPEG
		// libtiff's default multi-strip JPEG layout splits a single
		// JPEG at byte boundaries between restart markers (RST0..7).
		// Concatenating the strip bytes reproduces the original JPEG —
		// SVS, Philips, OME, and generic libtiff outputs all do this.
		// The PlanarConfiguration=2 case where each strip is its own
		// JPEG is already excluded by the spec (OME-specific quirk).
		// Verified against CMU-1.svs's stripped thumbnail (IFD 1):
		// 46 strips concatenate to a valid JPEG (SOI...EOI, 143,874 bytes).
		return concatStrips(strips), opentile.CompressionJPEG, nil
	case 5: // LZW
		out, err := reconstructMultiStripLZW(strips, info)
		if err != nil {
			return nil, opentile.CompressionUnknown, fmt.Errorf("generic: associated %s LZW: %w", imageType, err)
		}
		return out, opentile.CompressionLZW, nil
	case 8, 32946: // Deflate / Adobe Deflate (multi-strip)
		return nil, opentile.CompressionUnknown,
			fmt.Errorf("%w: multi-strip Deflate (associated %s)", errUnsupportedAssociatedShape, imageType)
	default:
		return nil, opentile.CompressionUnknown,
			fmt.Errorf("%w: multi-strip compression %d (associated %s)",
				errUnsupportedAssociatedShape, info.compression, imageType)
	}
}

// concatStrips returns a single buffer holding every strip's bytes
// concatenated in offset order. For multi-strip uncompressed and
// JPEG layouts (libtiff default), this reproduces the original
// image bytes verbatim.
func concatStrips(strips [][]byte) []byte {
	var size int
	for _, s := range strips {
		size += len(s)
	}
	out := make([]byte, 0, size)
	for _, s := range strips {
		out = append(out, s...)
	}
	return out
}

// reconstructMultiStripLZW decodes each TIFF strip's LZW-compressed
// payload, concatenates the decoded raster row-major, and re-encodes
// as a single LZW stream covering the full image. Same algorithm as
// formats/svs/lzwlabel.go's reconstructLZWLabel — duplicated here
// to avoid pulling SVS into the generic package's import graph;
// future refactor could promote to internal/tiffstrip.
//
// Returns an LZW bytestream that the consumer can decode via any
// TIFF-LZW reader using the page's standard tag values
// (BitsPerSample=8, SamplesPerPixel from info, ImageWidth/Length,
// MSB bit ordering).
func reconstructMultiStripLZW(strips [][]byte, info associatedSourceInfo) ([]byte, error) {
	rps := int(info.rowsPerStrip)
	if rps == 0 {
		// TIFF spec default: one strip = whole image. But we already
		// handled single-strip above; reaching here with rps=0 means
		// the IFD has multiple strips with no RowsPerStrip — invalid.
		return nil, fmt.Errorf("multi-strip LZW with no RowsPerStrip (got %d strips)", len(strips))
	}
	imgW, imgH := int(info.width), int(info.height)
	samples := int(info.samples)
	if samples == 0 {
		samples = 1
	}
	expectedTotal := imgH * imgW * samples
	raster := make([]byte, 0, expectedTotal)

	for i, strip := range strips {
		dr := tifflzw.NewReader(bytes.NewReader(strip), tifflzw.MSB, 8)
		decoded, err := io.ReadAll(dr)
		dr.Close()
		if err != nil {
			return nil, fmt.Errorf("strip %d decode: %w", i, err)
		}
		// Last strip may have fewer rows.
		rowsThisStrip := rps
		if start := i * rps; start+rowsThisStrip > imgH {
			rowsThisStrip = imgH - start
		}
		if rowsThisStrip <= 0 {
			continue // extra strips beyond image height — ignore
		}
		expectedThisStrip := rowsThisStrip * imgW * samples
		if len(decoded) > expectedThisStrip {
			decoded = decoded[:expectedThisStrip]
		}
		if len(decoded) < expectedThisStrip {
			return nil, fmt.Errorf("strip %d short: got %d bytes, want %d (rows=%d w=%d samples=%d)",
				i, len(decoded), expectedThisStrip, rowsThisStrip, imgW, samples)
		}
		raster = append(raster, decoded...)
	}
	if len(raster) != expectedTotal {
		return nil, fmt.Errorf("raster size %d != expected %d (h=%d w=%d samples=%d)",
			len(raster), expectedTotal, imgH, imgW, samples)
	}

	var out bytes.Buffer
	w := tifflzw.NewWriter(&out, tifflzw.MSB, 8)
	if _, err := w.Write(raster); err != nil {
		return nil, fmt.Errorf("re-encode: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("re-encode close: %w", err)
	}
	return out.Bytes(), nil
}

// mapCompression mirrors tiledImage.tiffCompressionToOpentile so the
// associated image reports the same enum for the same TIFF tag value.
func mapCompression(comp uint32) opentile.Compression {
	return tiffCompressionToOpentile(comp)
}
