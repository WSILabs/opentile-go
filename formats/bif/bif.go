// Package bif implements opentile-go format support for Ventana BIF
// (BioImagene Image File) — a BigTIFF dialect produced by Roche's
// VENTANA DP scanner family (DP 200, DP 600) and predecessor iScan
// scanners (Coreo, HT). The format is publicly specified by Roche
// (Roche-Digital-Pathology-BIF-Whitepaper.pdf v1.0, 2020) but only
// the DP 200 generation is documented in detail; legacy iScan slides
// require openslide's permissive interpretation.
//
// Detection is a single substring match (`<iScan` in any IFD's XMP)
// shared by both spec-compliant DP slides and legacy iScan slides;
// internal classification then routes each open file to a
// spec-compliant or legacy behavioural path. See spec §4 for the
// branching rationale and `docs/deferred.md §1a` for the v0.7
// deviations from upstream Python opentile.
//
// Not affiliated with or endorsed by Roche.
package bif

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/bifxml"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *Tiler satisfies format.Reader.
var _ format.Reader = (*Tiler)(nil)

func init() {
	format.Register("bif", matchBIF, openBIF)
}

// matchBIF returns nil iff r is a BIF file (BigTIFF with at least one
// IFD whose XMP contains the "<iScan" marker).
func matchBIF(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("bif: not a TIFF: %w", err)
	}
	if !Detect(file) {
		return fmt.Errorf("bif: no IFD XMP contains %q marker", iScanMarker)
	}
	return nil
}

// openBIF constructs a format.Reader from a raw reader.
func openBIF(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("bif: %w", err)
	}
	return openFromTIFFFile(file, cfg)
}

// openFromTIFFFile is the shared construction path used by both openBIF and
// Factory.Open.
func openFromTIFFFile(file *tiff.File, cfg *format.Config) (format.Reader, error) {
	if !Detect(file) {
		return nil, opentile.ErrUnsupportedFormat
	}
	iscan, err := loadIScan(file)
	if err != nil {
		return nil, err
	}
	levelIFDs, associatedIFDs, unknownIFDs, err := inventory(file)
	if err != nil {
		return nil, err
	}
	encodeInfo, err := loadEncodeInfo(levelIFDs)
	if err != nil {
		return nil, err
	}
	scanWhite := scanWhitePointFor(iscan)
	var dirSpecs []bifDirSpec
	levelImpls := make([]*levelImpl, 0, len(levelIFDs))
	valueLevels := make([]opentile.Level, 0, len(levelIFDs))
	var levelZeroDepth int
	var l0Width int
	var l0Hull opentile.Size
	var l0Layout *Layout
	gen := classifyGeneration(iscan)
	for i, c := range levelIFDs {
		l, err := newLevelImpl(i, c, iscan.ScanRes, scanWhite, gen, encodeInfo, file.ReaderAt())
		if err != nil {
			return nil, err
		}
		if i == 0 {
			levelZeroDepth = l.imageDepth
			l0Width = l.size.W
			l0Hull = l.size
			l0Layout = l.layout
		} else if gen == GenerationSpecCompliant && l0Layout != nil {
			// #78/#83: DP (spec-compliant / DP 200) reduced levels are stored as
			// the raw (un-compacted) frame grid downsampled, so their pixels carry
			// a *residual* frame overlap (~overlap/2^i, concentrated at the sparse
			// frame-join seams). Two coupled corrections:
			//   1. Size = the L0 stitched hull floor-halved (true content extent,
			//      == bio-formats), so the pyramid's inter-level scale is exactly 2×.
			//   2. Layout = the L0 compacted layout downsampled (reduced tile
			//      (col,row) inherits L0 frame (col<<i,row<<i)'s compacted origin
			//      scaled by 1/2^i), with Overlapping flagged — so ReadRegion /
			//      StitchedTile composite the reduced level stitch-aligned with L0
			//      via the existing regionLayout path (zero compositor change).
			//
			// LEGACY iScan is deliberately EXCLUDED (#80 reverted in v0.55.1).
			// Legacy overlap is DENSE (every gap). Legacy reduced tiles are clean
			// and overlap at their BOUNDARIES (~49px at L1, ~25px at L2), so they
			// ARE stitchable by placement — but downsampleLayout derives placement
			// from the L0 FRAME positions, so a reduced tile (spanning 2^i L0
			// frames) removes both the internal and boundary L0 overlaps and
			// over-compacts (L1: (121+121)/2 = 121px/gap vs the real ~49px → ~8%
			// squish → "very broken on zoom"). DP escapes it because its overlap is
			// sparse (internal term 0). The correct fix is this same placement
			// mechanism with the reduced level's ACTUAL per-level overlap — see #80.
			// Until then legacy reduced levels stay naive (Size = raw IFD grid,
			// Overlapping=false).
			l.size = floorHalveSize(l0Hull, i)
			l.layout = downsampleLayout(l0Layout, uint(i), l.grid.W, l.grid.H, l.tileSize.W, l.tileSize.H)
			l.overlapping = l.layout.Width < l.grid.W*l.tileSize.W || l.layout.Height < l.grid.H*l.tileSize.H
		}
		levelImpls = append(levelImpls, l)
		valueLevels = append(valueLevels, opentile.Level{
			Index:        l.index,
			PyramidIndex: l.pyrIndex,
			Size:         l.size,
			TileSize:     l.tileSize,
			Grid:         l.grid,
			Compression:  l.compression,
			MPP:          l.mpp,
			TileOverlap:  l.tileOverlap,
			Overlapping:  l.overlapping,
			FocalPlane:   0,
			// l0Width is the level-0 STITCHED width (the compacted hull, #60);
			// lower levels report their own IFD extent (pre-stitched, no joints
			// → naive layout → size = IFD ImageWidth×ImageLength). So the ratio
			// is the true pyramid downsample.
			Downsample: float64(l0Width) / float64(l.size.W),
		})
		dirSpecs = append(dirSpecs, bifDirSpec{page: c.Page, typ: opentile.DirLevel, level: i})
	}
	if levelZeroDepth < 1 {
		levelZeroDepth = 1
	}
	associated := make([]opentile.AssociatedImage, 0, len(associatedIFDs))
	for _, c := range associatedIFDs {
		imageType := typeFromIFDRole(c.Role)
		if imageType == "" {
			dirSpecs = append(dirSpecs, bifDirSpec{page: c.Page, typ: opentile.DirOther})
			continue
		}
		a, err := newAssociatedImage(imageType, c.Page, file.ReaderAt())
		if err != nil {
			return nil, err
		}
		associated = append(associated, a)
		dirSpecs = append(dirSpecs, bifDirSpec{page: c.Page, typ: opentile.DirAssociated, assoc: imageType})

		// GH #19: BIF's Label_Image (IFD 0, surfaced as "overview") carries the
		// printed label as its top band. Synthesize a "label" associated image
		// (top 1/3 crop) so consumers can locate the label — parity with NDPI,
		// which exposes a macro-crop label. No dirSpec: the label is synthesized,
		// with no backing IFD.
		if c.Role == ifdRoleLabel {
			if lbl := newSynthesizedLabel(a); lbl != nil {
				associated = append(associated, lbl)
			}
		}
	}
	// Capture orphan pages (IFDs not surfaced as a level or associated image).
	for _, c := range unknownIFDs {
		dirSpecs = append(dirSpecs, bifDirSpec{page: c.Page, typ: opentile.DirOther})
	}
	zSpacing := 0.0
	if iscan != nil {
		zSpacing = iscan.ZSpacing
	}
	images := []opentile.Pyramid{{
		Name:   "",
		Index:  0,
		Levels: valueLevels,
	}}
	return &Tiler{
		file:          file,
		cfg:           nil, // format.Config not stored; cfg param reserved for future knobs
		iscan:         iscan,
		gen:           gen,
		encodeInfo:    encodeInfo,
		levelIFDs:     levelIFDs,
		associatedIFD: associatedIFDs,
		levelImpls:    levelImpls,
		image:         newBifImage(levelZeroDepth, zSpacing),
		images:        images,
		associated:    associated,
		dirSpecs:      dirSpecs,
	}, nil
}

// Tiler is the BIF implementation of format.Reader. Built up across
// v0.7 batches: T10 establishes the skeleton (factory wiring + Open
// gate); T11 adds generation classification; T12 builds the IFD
// inventory + pyramid level slice; T13 wires per-Level reads with
// row-major tile addressing; T14 wires the empty-tile blank-fill path; T15
// composes JPEGTables into per-tile bytes when the IFD has a shared
// header. T16+ surface associated images, metadata, and ICC profile.
// v0.24: restructured to satisfy format.Reader with (image, level)
// dispatch; value-type []opentile.Pyramid populated eagerly at Open time.
type Tiler struct {
	file *tiff.File
	cfg  *format.Config // reserved for future format-level knobs; currently unused

	gen        Generation         // routing decision (T11)
	iscan      *bifxml.IScan      // parsed IFD-0 metadata block; non-nil after Open
	encodeInfo *bifxml.EncodeInfo // parsed level-0 IFD's XMP; nil if absent / parse failed

	// IFD inventory (T12); built once at Open time.
	levelIFDs     []classifiedIFD // pyramid IFDs sorted by parsed level=N
	associatedIFD []classifiedIFD // label / probability / thumbnail IFDs

	// levelImpls are the internal per-level tile-read helpers.
	// Parallel to images[0].Levels; images carries value-type metadata,
	// levelImpls carries the tile-read logic.
	levelImpls []*levelImpl

	// images is the value-type pyramid slice returned by Pyramids().
	// BIF is single-image; always len == 1.
	images []opentile.Pyramid

	// image holds BIF-specific Z-stack metadata (SizeZ, ZPlaneFocus).
	// Not the opentile.Pyramid — that's in images[0].
	image *bifImage

	// Associated images built from the associatedIFD inventory.
	// Populated at Open time (T16); typically 2 entries (overview +
	// {probability | thumbnail}).
	associated []opentile.AssociatedImage

	// cachedMetadata is built lazily on the first Metadata() /
	// MetadataOf call (T17). Subsequent calls return the cached
	// pointer; the struct itself is never mutated.
	cachedMetadata *Metadata

	// dirSpecs captures the page→role mapping for every IFD, recorded
	// at Open time so TIFFDirectories() can build the public view lazily.
	dirSpecs []bifDirSpec
}

// bifImage holds BIF-specific multi-Z metadata for the level-0 IFD.
// The opentile.Pyramid value-type struct doesn't carry SizeZ or
// ZPlaneFocus; bifImage keeps those fields internally so that the
// Tiler and its tests can access BIF-specific Z-stack metadata without
// going through the public opentile.Pyramid.
type bifImage struct {
	imageDepth  int
	zPlaneFocus []float64 // index z → microns from nominal; len == imageDepth
}

func newBifImage(imageDepth int, zSpacing float64) *bifImage {
	return &bifImage{
		imageDepth:  imageDepth,
		zPlaneFocus: computeZPlaneFocusTable(imageDepth, zSpacing),
	}
}

func (i *bifImage) SizeZ() int { return i.imageDepth }
func (i *bifImage) SizeC() int { return 1 }
func (i *bifImage) SizeT() int { return 1 }

func (i *bifImage) ZPlaneFocus(z int) float64 {
	if z < 0 || z >= len(i.zPlaneFocus) {
		return 0
	}
	return i.zPlaneFocus[z]
}

// computeZPlaneFocusTable maps a Z storage index (0..imageDepth-1)
// to the corresponding focal offset in microns from the nominal
// plane, following BIF whitepaper §"Whole slide imaging process":
//
//	Z=0                          → nominal (offset 0)
//	Z=1..nNear                   → near focus  (offsets -1·spacing, -2·spacing, ..., -nNear·spacing)
//	Z=nNear+1..nNear+nFar        → far focus   (offsets +1·spacing, +2·spacing, ..., +nFar·spacing)
//
// where nNear = (imageDepth - 1) / 2, nFar = imageDepth - 1 - nNear.
// Spec mandates an odd imageDepth (so nNear == nFar always); we
// tolerate even values defensively (one fewer near plane than far).
//
// imageDepth == 1 yields a single-element table {0}; zSpacing == 0
// yields a table of zeros (every plane reports nominal, matching
// the "no Z-spacing recorded" case).
func computeZPlaneFocusTable(imageDepth int, zSpacing float64) []float64 {
	if imageDepth < 1 {
		imageDepth = 1
	}
	t := make([]float64, imageDepth)
	if imageDepth == 1 {
		return t
	}
	nNear := (imageDepth - 1) / 2
	nFar := imageDepth - 1 - nNear
	t[0] = 0
	for i := 1; i <= nNear; i++ {
		t[i] = -float64(i) * zSpacing
	}
	for i := 1; i <= nFar; i++ {
		t[nNear+i] = float64(i) * zSpacing
	}
	return t
}

// scanWhitePointFor returns the empty-tile fill value for this
// slide. Per spec §"AOI Positions" empty tiles take the
// `<iScan>/@ScanWhitePoint` luminance; if the attribute is absent
// (every legacy iScan slide we've seen) we fall back to 255 (true
// white), matching openslide's implicit default.
func scanWhitePointFor(iscan *bifxml.IScan) uint8 {
	if iscan != nil && iscan.ScanWhitePointPresent {
		return iscan.ScanWhitePoint
	}
	return 255
}

// loadEncodeInfo parses the level-0 IFD's XMP into an EncodeInfo
// struct. The level-0 IFD is the first entry in levels (sorted
// ascending).
//
// Returns (nil, nil) when the level-0 IFD has no XMP tag at all —
// legitimate for some legacy iScan slides; downstream code
// (TileOverlap) treats nil as "no overlap data".
//
// Returns (nil, error) when the XMP IS present but the parser
// rejects it (currently only Ver < 2; bifxml may grow more
// guards in future). Callers propagate the error so Open fails
// loudly on a spec-violating slide.
func loadEncodeInfo(levels []classifiedIFD) (*bifxml.EncodeInfo, error) {
	if len(levels) == 0 {
		return nil, nil
	}
	xmp, ok := levels[0].Page.XMP()
	if !ok {
		return nil, nil
	}
	ei, err := bifxml.ParseEncodeInfo(xmp)
	if err != nil {
		return nil, fmt.Errorf("bif: parse EncodeInfo XMP from level-0 IFD: %w", err)
	}
	return ei, nil
}

// loadIScan locates the IFD whose XMP carries the `<iScan>` element
// and parses it. Both spec-compliant and legacy iScan slides put the
// `<iScan>` block in IFD 0's XMP, so we walk pages in order and
// parse the first match. Returns a nil *IScan only if no IFD's XMP
// contains the marker — Detect guarantees at least one does.
func loadIScan(file *tiff.File) (*bifxml.IScan, error) {
	marker := []byte(iScanMarker)
	for _, p := range file.Pages() {
		xmp, ok := p.XMP()
		if !ok {
			continue
		}
		if !bytes.Contains(xmp, marker) {
			continue
		}
		iscan, err := bifxml.ParseIScan(xmp)
		if err != nil {
			return nil, fmt.Errorf("bif: parse iScan XMP: %w", err)
		}
		return iscan, nil
	}
	return nil, fmt.Errorf("bif: no IFD carries an `%s` XMP block (Detect should have rejected)", iScanMarker)
}

// Format reports the BIF format identifier.
func (t *Tiler) Format() opentile.Format { return opentile.FormatBIF }

// Pyramids returns the main pyramids carried by this file. BIF is a
// single-image format — always one Pyramid regardless of AOI count.
func (t *Tiler) Pyramids() []opentile.Pyramid { return t.images }

func (t *Tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 || image >= len(t.images) {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.images[image].Levels[level], nil
}

func (t *Tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].warm()
}

func (t *Tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].Tile(tx, ty)
}

func (t *Tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileInto(tx, ty, dst)
}

func (t *Tiler) ImageTileMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileMaxSize()
}

func (t *Tiler) ImageTilePrefix(image, level int) []byte {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return nil
	}
	return t.levelImpls[level].TilePrefix()
}

func (t *Tiler) ImageTileBodyMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileBodyMaxSize()
}

func (t *Tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileBodyInto(tx, ty, dst)
}

func (t *Tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileReader(tx, ty)
}

func (t *Tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.Point, opentile.TileResult] {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return func(yield func(opentile.Point, opentile.TileResult) bool) {}
	}
	return t.levelImpls[level].Tiles(ctx)
}

// AssociatedImages returns the slide's associated images: every BIF has
// an "overview" entry; spec-compliant slides additionally expose
// "probability"; legacy iScan slides expose "thumbnail" instead.
// Returns a fresh slice; callers may mutate the slice header
// without affecting Tiler internal state.
func (t *Tiler) AssociatedImages() []opentile.AssociatedImage {
	out := make([]opentile.AssociatedImage, len(t.associated))
	copy(out, t.associated)
	return out
}

// Metadata returns the common opentile.Metadata fields populated
// from the BIF <iScan> XMP block: Magnification, ScannerManufacturer,
// ScannerModel, ScannerSoftware, ScannerSerial, AcquisitionDateTime.
// For BIF-specific fields (Generation, ScanRes, AOIs, ...) call
// bif.MetadataOf(tiler).
func (t *Tiler) Metadata() opentile.Metadata { return t.metadata().Metadata }

// ICCProfile returns the level-0 IFD's InterColorProfile bytes
// (TIFF tag 34675), or nil if the IFD doesn't carry an ICC profile.
// Per spec §"IFD 2: High resolution scan", the profile lives only on
// the level-0 (high-resolution) IFD; pyramid IFDs 3+ inherit it
// implicitly. The profile applies to every pyramid level, the
// overview/probability associated images excluded — those are sRGB
// (overview) or grayscale (probability), no ICC needed.
//
// Returned bytes are the raw ICC profile blob, including the
// 128-byte profile header. Consumers that want to verify the
// magic should check `bytes[36:40] == "acsp"`.
func (t *Tiler) ICCProfile() []byte {
	if len(t.levelIFDs) == 0 {
		return nil
	}
	prof, ok := t.levelIFDs[0].Page.ICCProfile()
	if !ok || len(prof) == 0 {
		return nil
	}
	return prof
}

// bifDirSpec captures the physical page pointer and semantic role of one IFD,
// recorded at Open time so TIFFDirectories() can build the public view lazily.
// BIF associated images carry direct *tiff.Page references (via classifiedIFD.Page),
// so we store the page pointer directly rather than a page index.
type bifDirSpec struct {
	page  *tiff.Page
	typ   opentile.DirectoryType
	level int                     // valid when typ==DirLevel
	assoc opentile.AssociatedType // valid when typ==DirAssociated; matches AssociatedImage.Type()
}

// TIFFDirectories exposes the raw TIFF tags per IFD, lazily decoded.
// Implements opentile's (unexported) tiffTagProvider.
func (t *Tiler) TIFFDirectories() []opentile.TIFFDirectory {
	out := make([]opentile.TIFFDirectory, 0, len(t.dirSpecs))
	for _, ds := range t.dirSpecs {
		if ds.page == nil {
			continue
		}
		out = append(out, opentile.TIFFDirectory{
			Type:           ds.typ,
			Image:          0, // BIF is single-image
			Level:          ds.level,
			AssociatedType: ds.assoc,
			Tags:           opentile.TIFFTagsFromPage(ds.page),
		})
	}
	return out
}

// TileOrigin reports the stitched-space top-left of image-grid tile (col,row)
// at the given level. Implements opentile's regionLayout (#60).
func (t *Tiler) TileOrigin(level, col, row int) (x, y int, ok bool) {
	if level < 0 || level >= len(t.levelImpls) {
		return 0, 0, false
	}
	return t.levelImpls[level].TileOrigin(col, row)
}

// TilesIntersecting reports image-grid tiles whose stitched extent touches
// [x,y,x+w,y+h) at the given level, in row-major order. Implements regionLayout.
func (t *Tiler) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	if level < 0 || level >= len(t.levelImpls) {
		return nil
	}
	tps := t.levelImpls[level].TilesIntersecting(x, y, w, h)
	out := make([]struct{ Col, Row int }, len(tps))
	for i, p := range tps {
		out[i] = struct{ Col, Row int }{p.Col, p.Row}
	}
	return out
}

// StitchedSize reports the level's stitched dimensions. Implements regionLayout.
func (t *Tiler) StitchedSize(level int) (w, h int, ok bool) {
	if level < 0 || level >= len(t.levelImpls) {
		return 0, 0, false
	}
	return t.levelImpls[level].StitchedSize()
}

// Close releases any resources held by the Tiler. Currently a no-op:
// the underlying *tiff.File is owned by the caller.
func (t *Tiler) Close() error { return nil }
