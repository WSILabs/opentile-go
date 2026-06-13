package opentile

import (
	"github.com/wsilabs/opentile-go/decoder"
)

// Level is value-type pyramid-level metadata. All fields are
// inspection-only (read at *Slide.Open time). Tile reads use
// *Slide.RawTile / *Slide.DecodedTile (takes level index).
type Level struct {
	// Index is the 0-based index of this level within the parent
	// Image's Levels slice. Pass to *Slide.RawTile(level, tx, ty).
	Index int

	// PyramidIndex is the pyramid-group index for multi-image formats.
	// Single-image formats always have PyramidIndex = 0. OME-TIFF
	// multi-image files preserve the per-image series identifier here.
	PyramidIndex int

	// Size is the pixel dimensions of this level (Width × Height).
	Size Size

	// TileSize is the tile dimensions used by this level.
	TileSize Size

	// Grid is the tile grid dimensions: ceil(Size / TileSize) per axis.
	// Pre-computed for convenience.
	Grid Size

	// TileOverlap is the per-tile overlap (BIF / NDPI in overlapping
	// modes). Zero for non-overlapping tile formats.
	TileOverlap Point

	// Compression identifies the codec for tile bytes at this level.
	// Used by *Slide.DecodedTile to dispatch to the right decoder.
	Compression Compression

	// MPP is microns-per-pixel at this level (X and Y; usually equal).
	// Zero value (MPP.IsZero() == true) when the slide doesn't carry
	// per-level MPP metadata. Values are in microns, not millimeters.
	//
	// Changed from SizeMm (millimeters) to MPP (microns) in v1.0.
	MPP MPP

	// FocalPlane is the z-position in microns for multi-focal-plane
	// sources. Zero value for 2D slides.
	FocalPlane float64

	// Downsample is the resolution factor from L0. 1.0 at level 0,
	// 2.0 at half-resolution, 4.0 at quarter, etc. Computed at Open
	// time from the level's Size relative to the image's L0 Size.
	//
	// Used by *Slide.BestLevelForDownsample and *Slide.ReadRegionScaled
	// to translate L0 coords into level coords.
	//
	// Added in v0.25 alongside the ReadRegion family.
	Downsample float64
}

// AssociatedType is the type of an associated image (label, overview,
// thumbnail, etc.). The underlying value is the lowercase string
// surfaced by AssociatedImage.Type(). Use the AssociatedLabel /
// AssociatedOverview / … constants for comparisons.
type AssociatedType string

// AssociatedImage is a non-pyramidal slide-level image (label, overview,
// thumbnail).
//
// Standard Type() values used across opentile-go's format readers.
// The choice of names follows DICOM PS3.3 / Supplement 145
// (Whole Slide Microscopic Image IOD), where the Image Type
// attribute (0008,0008) value 3 enumerates: VOLUME / LABEL /
// OVERVIEW / THUMBNAIL. opentile-go uses the lowercase form,
// extended with format-specific types where the underlying file
// surfaces them:
//
//	"label"       — slide label / barcode
//	"overview"    — wide-field image of the slide. The DICOM-canonical
//	                term, also used by upstream Python opentile and by
//	                six of opentile-go's eight format readers. The
//	                seventh (Iris IFE) intentionally distinguishes
//	                "overview" from "macro" per the IFE spec.
//	"thumbnail"   — full-slide downsample (typically square, JPEG)
//	"map"         — NDPI / IFE: low-magnification map / overview-of-
//	                pyramid; semantically distinct from a wide-field
//	                slide image
//	"probability" — Ventana BIF / IFE: confidence / classification map
//	"macro"       — Iris IFE only. The IFE spec defines LABEL_MACRO
//	                as a type distinct from LABEL_OVERVIEW. Other
//	                formats' wide-field slide images surface as
//	                "overview", not "macro".
//	"associated"  — generic-TIFF heuristic-fallback (v0.10+) when the
//	                classifier can't confidently match a type above
//
// Format readers use the exported AssociatedType constants; the values
// above are stable and part of the public API contract from v0.15 onward.
type AssociatedImage interface {
	Type() AssociatedType
	Size() Size
	Compression() Compression
	Bytes() ([]byte, error)
	// Decode returns the faithfully-decoded pixels for this associated
	// image, honoring opts (Format RGB/RGBA; Scale on codec-backed images
	// only). Unlike Bytes() — which is a re-encoded, predictor-dropping
	// stream for LZW labels — Decode owns all codec / TIFF-strip / predictor
	// handling. Returns decoder.ErrCodecUnavailable when the codec isn't
	// compiled in (e.g. a JPEG 2000 image under a nojp2k build).
	Decode(opts decoder.DecodeOptions) (*decoder.Image, error)

	// Encoding returns the on-disk encoded form (source strips + TIFF tags)
	// for byte-identical re-emission into a new standalone single-IFD TIFF
	// without re-encoding. ok=false for non-TIFF, non-strip, or synthesized
	// associated images (DICOM frames, IFE, SZI, NDPI synthesized label,
	// OME-TIFF planar pages, Leica SCN tiled).
	Encoding() (AssociatedEncoding, bool)

	// TIFFTags returns the parsed TIFF tags of this associated image's
	// backing IFD. ok=false for non-TIFF formats (DICOM, IFE, SZI) and
	// for implementations that don't retain a page reference.
	TIFFTags() (TIFFTags, bool)

	// IFDOffset returns the byte offset of the IFD backing this associated
	// image, for in-place TIFF editing (e.g. raw IFD splice/replace).
	// ok=false for non-TIFF formats, non-strip associated images, and
	// formats where the implementation doesn't record the offset
	// (currently only SVS and generic-TIFF return ok=true).
	IFDOffset() (int64, bool)
}

// Standard AssociatedImage.Type() values returned by Type() across all
// format readers (documented on AssociatedImage above). Exported so
// consumers can switch/compare against named constants instead of
// hardcoding the literals. The set is open — a format reader may surface
// an additional value — so this is a naming convention, not a closed enum.
const (
	AssociatedLabel       AssociatedType = "label"       // slide label / barcode
	AssociatedOverview    AssociatedType = "overview"    // wide-field image of the slide
	AssociatedThumbnail   AssociatedType = "thumbnail"   // full-slide downsample
	AssociatedMap         AssociatedType = "map"         // NDPI / IFE low-magnification map
	AssociatedProbability AssociatedType = "probability" // BIF / IFE confidence map
	AssociatedMacro       AssociatedType = "macro"       // Iris IFE only (distinct from overview)
	AssociatedGeneric     AssociatedType = "associated"  // generic-TIFF heuristic fallback
)

// Pyramid identifies one multi-resolution image within a slide.
// Single-image formats carry a single Pyramid. OME-TIFF can carry multiple.
type Pyramid struct {
	// Name identifies this pyramid. For OME-TIFF, the <Image Name="...">
	// attribute. Empty for single-image formats.
	Name string

	// Index is the 0-based document-order index of this Pyramid within
	// the parent Slide. Pass to *Slide.ImageRawTile(image, level, tx, ty).
	Index int

	// Levels is the pyramid for this image, finest-to-coarsest. Level 0
	// is the full-resolution base; subsequent indices are progressively
	// downsampled.
	Levels []Level
}

// TileResult carries the yield from RangeTiles.
type TileResult struct {
	Bytes []byte
	Err   error
}
