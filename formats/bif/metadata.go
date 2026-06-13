package bif

import (
	"strconv"
	"strings"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/bifxml"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Metadata is the BIF-specific slide metadata. It embeds
// opentile.Metadata so the common fields (Magnification, scanner
// identity, AcquisitionDateTime) are populated via the embedded
// struct; BIF-specific fields (Generation, ScanRes, ScanWhitePoint,
// AOIs, ImageDescription) live on the outer struct.
//
// Consumers read the common fields via opentile.Slide.Metadata() as
// usual; to read the BIF-specific fields, pass the Tiler to
// bif.MetadataOf.
type Metadata struct {
	opentile.Metadata

	// Generation is the routing decision: "spec-compliant" for
	// VENTANA DP scanners (200, 600, future); "legacy-iscan" for
	// pre-DP iScan slides and any unrecognised iScan ScannerModel.
	Generation string

	// ScanRes is the base-level microns/pixel from <iScan>/@ScanRes.
	// Same value applies to X and Y per spec — BIF doesn't carry
	// anisotropic pixels.
	ScanRes float64

	// ScanWhitePoint is the white-fill luminance for empty tiles
	// (`<iScan>/@ScanWhitePoint`). Only populated when
	// ScanWhitePointPresent is true; otherwise the consumer should
	// default to 255 (matches T9's blankTile fallback).
	ScanWhitePoint        uint8
	ScanWhitePointPresent bool

	// ZLayers is `<iScan>/@Z-layers`. As of v0.7 multi-dim closeout,
	// volumetric BIF slides expose every Z plane via the public
	// Image.SizeZ() / Level.TileAt(coord{Z: ...}) API; this field is
	// kept on the format-specific metadata for parity with the XMP
	// attribute name. Should equal the level-0 IFD's IMAGE_DEPTH tag.
	ZLayers int

	// ZSpacing is `<iScan>/@Z-spacing` in microns per focal plane
	// step. 0 if absent. Used by computeZPlaneFocusTable to translate
	// Z storage indices to physical focal offsets exposed via
	// Image.ZPlaneFocus(z).
	ZSpacing float64

	// ZPlaneFoci is the per-Z focal offset (microns from nominal),
	// matching what Image.ZPlaneFocus(z) returns. Index 0 is always
	// the nominal plane (offset 0). Length == SizeZ() (= 1 on
	// non-volumetric slides). Convenience: callers wanting to walk
	// the entire stack can range over this slice instead of looping
	// 0..SizeZ-1 + calling ZPlaneFocus.
	ZPlaneFoci []float64

	// AOIs is the list of areas-of-interest declared in the
	// <iScan> XMP (one entry per AOI<N> sub-element). For
	// single-AOI slides (both our local fixtures), this has one
	// entry. The bounding rectangles are in the slide's physical
	// coordinate system (origin at lower-left, Y up).
	AOIs []bifxml.AOI

	// AOIOrigins is the list of AOI origins from the level-0 IFD's
	// <EncodeInfo>/<AoiOrigin> elements (one per AOI). Origins are
	// in image-space pixel coordinates (top-left origin), always
	// multiples of the tile size per spec. Empty for legacy iScan
	// slides that don't carry EncodeInfo, or when EncodeInfo failed
	// to parse.
	AOIOrigins []bifxml.AoiOrigin

	// EncodeInfoVer is the level-0 EncodeInfo @Ver attribute (must
	// be ≥ 2 per spec; the parser exposes the raw value here).
	EncodeInfoVer int
}

const maxUnwrapHops = 16

// MetadataOf returns the BIF-specific metadata if v is (or wraps) a BIF
// reader, otherwise (nil, false). Accepts *opentile.Slide, format.Reader
// implementations, and any type implementing UnwrapReader() any.
//
//	if md, ok := bif.MetadataOf(slide); ok {
//	    use md.Generation, md.ScanRes, ...
//	}
func MetadataOf(v any) (*Metadata, bool) {
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if bt, ok := v.(*Tiler); ok {
			return bt.metadata(), true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}

// metadata builds the Metadata struct from this Tiler's parsed
// IScan + EncodeInfo. Called once at Open time and cached on the
// Tiler.
func (t *Tiler) metadata() *Metadata {
	if t.cachedMetadata != nil {
		return t.cachedMetadata
	}
	md := &Metadata{
		Generation: t.gen.String(),
	}
	if t.iscan != nil {
		md.Magnification = t.iscan.Magnification
		md.ScanRes = t.iscan.ScanRes
		md.ScanWhitePoint = t.iscan.ScanWhitePoint
		md.ScanWhitePointPresent = t.iscan.ScanWhitePointPresent
		md.ZLayers = t.iscan.ZLayers
		md.ZSpacing = t.iscan.ZSpacing
		md.AOIs = append([]bifxml.AOI(nil), t.iscan.AOIs...)

		// ScannerManufacturer: every iScan-tagged slide is from
		// Roche / VENTANA Tissue Diagnostics regardless of model.
		md.ScannerManufacturer = "Roche"
		md.ScannerModel = t.iscan.ScannerModel
		if md.ScannerModel == "" {
			md.ScannerModel = "VENTANA iScan" // best-effort label for legacy slides
		}
		if t.iscan.BuildVersion != "" {
			md.ScannerSoftware = []string{t.iscan.BuildVersion}
			md.Writer = t.iscan.BuildVersion // v0.20
		}
		md.ScannerSerial = t.iscan.UnitNumber

		// AcquisitionDateTime: the iScan node's BuildDate is the
		// scanner-software build, NOT the scan-time. Real
		// scan-time lives in the IFD's TIFF tag DateTime
		// (yyyy:mm:dd HH:MM:SS) — populated below.

		// Cross-format MPP (v0.17): BIF carries a single ScanRes
		// applied to both axes — the spec doesn't allow anisotropic pixels.
		if t.iscan.ScanRes > 0 {
			md.MPP = opentile.MPP{X: t.iscan.ScanRes, Y: t.iscan.ScanRes}
		}

		// Cross-format canonical: scan operator. The iScan element's
		// UserName attribute is rare in real fixtures (neither
		// Ventana-1 nor OS-1 carry one) but the bifxml parser
		// surfaces it when present.
		if t.iscan.UserName != "" {
			md.SetProperty(opentile.PropertyUserName, t.iscan.UserName)
		}

		// Cross-format vendor passthrough: surface every iScan XML
		// attribute under the "ventana." namespace so consumers can
		// reach format-specific fields without reparsing the raw
		// XMP. Includes attributes that ARE typed elsewhere on the
		// outer Metadata struct (e.g. ScannerModel, ScanRes,
		// BuildVersion) so the namespaced Properties view is a
		// faithful complete mirror of the source XML.
		if t.iscan.ScannerModel != "" {
			md.SetProperty("ventana.ScannerModel", t.iscan.ScannerModel)
		}
		if t.iscan.Magnification != 0 {
			md.SetProperty("ventana.Magnification", strconv.FormatFloat(t.iscan.Magnification, 'f', -1, 64))
		}
		if t.iscan.ScanRes != 0 {
			md.SetProperty("ventana.ScanRes", strconv.FormatFloat(t.iscan.ScanRes, 'f', -1, 64))
		}
		if t.iscan.ScanWhitePointPresent {
			md.SetProperty("ventana.ScanWhitePoint", strconv.FormatUint(uint64(t.iscan.ScanWhitePoint), 10))
		}
		if t.iscan.ZLayers != 0 {
			md.SetProperty("ventana.Z-layers", strconv.Itoa(t.iscan.ZLayers))
		}
		if t.iscan.ZSpacing != 0 {
			md.SetProperty("ventana.Z-spacing", strconv.FormatFloat(t.iscan.ZSpacing, 'f', -1, 64))
		}
		if t.iscan.BuildVersion != "" {
			md.SetProperty("ventana.BuildVersion", t.iscan.BuildVersion)
		}
		if t.iscan.BuildDate != "" {
			md.SetProperty("ventana.BuildDate", t.iscan.BuildDate)
		}
		if t.iscan.UnitNumber != "" {
			md.SetProperty("ventana.UnitNumber", t.iscan.UnitNumber)
		}
		if t.iscan.UserName != "" {
			md.SetProperty("ventana.UserName", t.iscan.UserName)
		}
		// RawAttributes covers any iScan attribute the typed parser
		// didn't pull into a struct field (e.g. "Mode" — consumed but
		// not typed — and any future attribute names a spec revision
		// might introduce).
		for k, v := range t.iscan.RawAttributes {
			md.SetProperty("ventana."+k, v)
		}
	}

	// Pull TIFF tag DateTime / Software / ImageDescription from the
	// level-0 IFD when available. ImageDescription populates the
	// cross-format opentile.Metadata.ImageDescription via field
	// promotion (v0.17: Q4 Option B removed the duplicate outer
	// field).
	if len(t.levelIFDs) > 0 {
		p := t.levelIFDs[0].Page
		if v, ok := p.ImageDescription(); ok {
			md.ImageDescription = strings.TrimSpace(v)
		}
		if v, ok := p.ASCII(tiff.TagDateTime); ok {
			if ts, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(v)); err == nil {
				md.AcquisitionDateTime = ts
			}
		}
	}

	if t.encodeInfo != nil {
		md.EncodeInfoVer = t.encodeInfo.Ver
		md.AOIOrigins = append([]bifxml.AoiOrigin(nil), t.encodeInfo.AoiOrigins...)
	}

	// ZPlaneFoci mirrors the bifImage's per-Z focal offset table
	// — the same data Image.ZPlaneFocus(z) reads. Always at least
	// length 1 (Z=0 nominal at offset 0) on every slide.
	if t.image != nil {
		md.ZPlaneFoci = append([]float64(nil), t.image.zPlaneFocus...)
	}

	t.cachedMetadata = md
	return md
}
