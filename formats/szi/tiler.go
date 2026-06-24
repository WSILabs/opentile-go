package szi

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"path"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/dzi"
	"github.com/wsilabs/opentile-go/internal/format"
)

// ErrCorruptArchive is returned when an SZI archive violates the
// spec — currently only on missing tiles in the addressable grid
// (the SZI spec forbids sparse images, so a missing tile is a
// corruption signal, not a sparse-data convention).
//
// Per Q2 of the v0.16 spec: a future additive ErrTileMissing
// sentinel + opt-in lenient mode could distinguish "spec-
// noncompliant but recoverable" from "archive structurally bad"
// once a real sparse-SZI fixture surfaces.
var ErrCorruptArchive = errors.New("szi: corrupt archive")

// Tiler is the SZI-format implementation of format.Reader.
type Tiler struct {
	r    io.ReaderAt
	size int64
	zipR *zip.Reader

	// rootDir is the single top-level directory inside the ZIP
	// (e.g., "CMU-1" or "scan_618_"). All other paths are relative
	// to it.
	rootDir string

	// manifest is the parsed DZI XML manifest from <rootDir>/<name>.dzi.
	manifest dzi.Manifest

	// filesDir is the tile-pyramid root, "<rootDir>/<rootName>_files".
	filesDir string

	// scanPropertiesXML holds the raw bytes of scan-properties.xml.
	// T4 parses these into szi.Metadata.
	scanPropertiesXML []byte

	// entries indexes ZIP central-directory entries by full path
	// for fast tile lookup.
	entries map[string]*zip.File

	// sziImage holds the single value-type opentile.Pyramid built in
	// buildLevels. SZI spec mandates exactly one image per archive.
	sziImage opentile.Pyramid

	// levelEngines holds the per-level tile-read engines, parallel to
	// sziImage.Levels. Used for (image, level, tx, ty) dispatch.
	levelEngines []*level

	// associated holds the optional associated_images/ entries
	// (label / overview / thumbnail) populated by buildAssociated.
	// nil/empty when no associated_images/ folder is present.
	associated []opentile.AssociatedImage

	// cross is the parsed cross-format metadata (canonical fields
	// shared with all other formats). Populated from
	// scan-properties.xml at Open() time.
	cross opentile.Metadata
	// szim is the SZI-specific metadata exposed via szi.MetadataOf.
	szim Metadata

	cfg *format.Config
}

// openSZIWithFormatConfig constructs a Tiler using the format.Config path.
// This is the shared construction path used by both openSZIFormat (new
// format.Register path) and Factory.OpenRaw (legacy FormatFactory path).
func openSZIWithFormatConfig(r io.ReaderAt, size int64, cfg *format.Config) (*Tiler, error) {
	return openSZI(r, size, cfg)
}

// openSZI is the core constructor. Called via openSZIWithFormatConfig.
func openSZI(r io.ReaderAt, size int64, cfg *format.Config) (*Tiler, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("szi: open zip: %w", err)
	}

	t := &Tiler{
		r:       r,
		size:    size,
		zipR:    zr,
		entries: make(map[string]*zip.File, len(zr.File)),
		cfg:     cfg,
	}
	for _, f := range zr.File {
		t.entries[f.Name] = f
	}

	if err := t.discoverRoot(); err != nil {
		return nil, err
	}
	if err := t.loadManifest(); err != nil {
		return nil, err
	}
	if err := t.loadScanProperties(); err != nil {
		return nil, err
	}
	cross, szim, err := parseScanProperties(t.scanPropertiesXML)
	if err != nil {
		return nil, fmt.Errorf("szi: parse scan-properties.xml: %w", err)
	}
	t.cross = cross
	t.szim = szim
	if err := t.checkTileEntriesStored(); err != nil {
		return nil, err
	}
	if err := t.buildLevels(); err != nil {
		return nil, err
	}
	t.buildAssociated()
	return t, nil
}

// buildLevels populates t.sziImage and t.levelEngines with one entry
// per DZI pyramid level. opentile-go's index 0 = highest resolution;
// DZI's MaxLevel = highest resolution; so opentile L_i = DZI (MaxLevel - i).
func (t *Tiler) buildLevels() error {
	maxLevel := dzi.MaxLevel(t.manifest.Width, t.manifest.Height)

	var comp opentile.Compression
	switch strings.ToLower(t.manifest.Format) {
	case "jpeg", "jpg":
		comp = opentile.CompressionJPEG
	case "png":
		comp = opentile.CompressionPNG
	default:
		comp = opentile.CompressionUnknown
	}

	valueLevels := make([]opentile.Level, maxLevel+1)
	engines := make([]*level, maxLevel+1)
	// L0 (i==0) maps to DZI MaxLevel; compute its width up front for Downsample.
	l0W, _ := dzi.LevelDims(t.manifest.Width, t.manifest.Height, maxLevel)
	for i := 0; i <= maxLevel; i++ {
		dziL := maxLevel - i
		w, h := dzi.LevelDims(t.manifest.Width, t.manifest.Height, dziL)
		cols, rows := dzi.GridDims(w, h, t.manifest.TileSize)
		eng := &level{
			t:           t,
			dziLevel:    dziL,
			openTileIdx: i,
			pyrIndex:    i,
			width:       w,
			height:      h,
			cols:        cols,
			rows:        rows,
			tileSize:    t.manifest.TileSize,
			compression: comp,
		}
		engines[i] = eng
		valueLevels[i] = opentile.Level{
			Index:        i,
			PyramidIndex: i,
			Size:         opentile.Size{W: w, H: h},
			TileSize:     opentile.Size{W: t.manifest.TileSize, H: t.manifest.TileSize},
			Grid:         opentile.Size{W: cols, H: rows},
			Compression:  comp,
			Downsample:   float64(l0W) / float64(w),
		}
	}
	t.sziImage = opentile.Pyramid{
		Name:   "",
		Index:  0,
		Levels: valueLevels,
	}
	t.levelEngines = engines
	return nil
}

// discoverRoot identifies the single top-level directory inside the
// ZIP archive. SZI spec mandates exactly one root folder named
// after the image.
func (t *Tiler) discoverRoot() error {
	roots := make(map[string]struct{})
	for _, f := range t.zipR.File {
		// Take the first path component.
		name := f.Name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			roots[name[:i]] = struct{}{}
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("szi: expected exactly 1 root folder, got %d", len(roots))
	}
	for name := range roots {
		t.rootDir = name
	}
	return nil
}

// loadManifest finds and parses <rootDir>/<rootDir>.dzi (case-
// insensitive on the .dzi extension per spec).
func (t *Tiler) loadManifest() error {
	// Try canonical name first: <root>/<root>.dzi (lowercase) and
	// <root>/<root>.DZI (uppercase, per spec page 5).
	candidates := []string{
		t.rootDir + "/" + t.rootDir + ".dzi",
		t.rootDir + "/" + t.rootDir + ".DZI",
	}
	var manifestEntry *zip.File
	for _, p := range candidates {
		if e, ok := t.entries[p]; ok {
			manifestEntry = e
			break
		}
	}
	// Fallback: any file ending in .dzi/.DZI directly under rootDir.
	if manifestEntry == nil {
		for _, f := range t.zipR.File {
			lower := strings.ToLower(f.Name)
			if !strings.HasSuffix(lower, ".dzi") {
				continue
			}
			if path.Dir(f.Name) != t.rootDir {
				continue
			}
			manifestEntry = f
			break
		}
	}
	if manifestEntry == nil {
		return errors.New("szi: no .dzi manifest found in archive")
	}

	data, err := readZipEntry(manifestEntry)
	if err != nil {
		return fmt.Errorf("szi: read manifest %s: %w", manifestEntry.Name, err)
	}
	m, err := dzi.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("szi: parse manifest %s: %w", manifestEntry.Name, err)
	}
	if m.Overlap > 0 {
		return fmt.Errorf("szi: %s: Overlap=%d: %w", manifestEntry.Name, m.Overlap, dzi.ErrOverlapNotSupported)
	}
	t.manifest = m

	// _files folder: same name as the .dzi without the extension,
	// plus "_files".
	base := strings.TrimSuffix(path.Base(manifestEntry.Name), path.Ext(manifestEntry.Name))
	t.filesDir = t.rootDir + "/" + base + "_files"
	return nil
}

// loadScanProperties locates and reads <rootDir>/scan-properties.xml.
// The spec marks this file mandatory; T4 parses it into Metadata.
func (t *Tiler) loadScanProperties() error {
	p := t.rootDir + "/scan-properties.xml"
	entry, ok := t.entries[p]
	if !ok {
		return fmt.Errorf("szi: missing %s", p)
	}
	data, err := readZipEntry(entry)
	if err != nil {
		return fmt.Errorf("szi: read %s: %w", p, err)
	}
	t.scanPropertiesXML = data
	return nil
}

// checkTileEntriesStored enforces the SZI spec requirement that
// tile-pyramid entries under <filesDir>/ use compression method 0
// (zip.Store / uncompressed). The spec mandates this so that tile
// fetches can be a zero-copy SectionReader on the underlying file
// (no zlib inflate on the hot path). Manifest, scan-properties,
// and associated_images entries are unconstrained.
func (t *Tiler) checkTileEntriesStored() error {
	prefix := t.filesDir + "/"
	for _, f := range t.zipR.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		if f.Method != zip.Store {
			return fmt.Errorf("szi: tile entry %s uses compression method %d, want 0 (Store) per spec", f.Name, f.Method)
		}
	}
	return nil
}

// readZipEntry reads a complete ZIP entry into memory. Used for
// small metadata blobs (manifest, scan-properties.xml) — NOT for
// tiles, which use a SectionReader fast path.
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Format returns opentile.FormatSZI.
func (t *Tiler) Format() opentile.Format { return opentile.FormatSZI }

// Close releases resources held by the Tiler. The underlying
// ReaderAt remains the caller's responsibility.
func (t *Tiler) Close() error {
	t.r = nil
	t.zipR = nil
	t.entries = nil
	return nil
}

// Pyramids returns the single Pyramid carried by the SZI file. The
// returned slice has exactly one element per the SZI spec (no
// DZC collections in SZI).
func (t *Tiler) Pyramids() []opentile.Pyramid {
	if t.levelEngines == nil {
		return nil
	}
	return []opentile.Pyramid{t.sziImage}
}

// Level returns the value-type Level for the given (image, level) pair.
func (t *Tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelEngines) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.sziImage.Levels[level], nil
}

// AssociatedImages returns the optional associated_images/ entries
// (macro.jpg → "overview", label.jpg → "label", thumbnail.jpg →
// "thumbnail" per the v0.15 alignment). Returns a fresh slice;
// callers may mutate the slice header without affecting the
// Tiler's internal state.
func (t *Tiler) AssociatedImages() []opentile.AssociatedImage {
	return append([]opentile.AssociatedImage(nil), t.associated...)
}

// Metadata returns the cross-format metadata populated from
// scan-properties.xml. SZI-specific fields (per-axis MPP, scan-area
// dimensions, vendor-prefixed properties, etc.) are accessible via
// szi.MetadataOf.
func (t *Tiler) Metadata() opentile.Metadata { return t.cross }

// ICCProfile returns nil — SZI does not surface ICC profiles in v0.16.
func (t *Tiler) ICCProfile() []byte { return nil }

// WarmLevel pre-warms the page cache for the given (image, level).
//
// SZI tile lookup is via stored ZIP entries (no inflate); this is
// a no-op hint that validates bounds. Warming is a hint, not a
// correctness requirement (per Q-decisions in the v0.16 spec).
func (t *Tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelEngines) {
		return opentile.ErrLevelOutOfRange
	}
	return nil
}

// engine returns the *level engine for (image, level), validating bounds.
func (t *Tiler) engine(image, level int) (*level, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelEngines) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelEngines[level], nil
}

// ImageRawTile returns the raw tile bytes at (image, level, tx, ty).
func (t *Tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return nil, err
	}
	return eng.Tile(tx, ty)
}

// ImageRawTileInto fills dst with raw tile bytes.
func (t *Tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0, err
	}
	return eng.TileInto(tx, ty, dst)
}

// ImageTileMaxSize returns the upper bound on tile byte size.
func (t *Tiler) ImageTileMaxSize(image, level int) int {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0
	}
	return eng.TileMaxSize()
}

// ImageTilePrefix returns nil — SZI tiles carry no shared prefix.
func (t *Tiler) ImageTilePrefix(image, level int) []byte {
	_, err := t.engine(image, level)
	if err != nil {
		return nil
	}
	return nil
}

// ImageTileBodyMaxSize returns the upper bound on tile body bytes.
func (t *Tiler) ImageTileBodyMaxSize(image, level int) int {
	return t.ImageTileMaxSize(image, level)
}

// ImageTileBodyInto fills dst with tile body bytes (identical to
// ImageRawTileInto for SZI since TilePrefix is nil).
func (t *Tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return t.ImageRawTileInto(image, level, tx, ty, dst)
}

// ImageTileReader returns a streaming reader for the tile at (image, level, tx, ty).
func (t *Tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return nil, err
	}
	return eng.TileReader(tx, ty)
}

// ImageRangeTiles returns a range-over-function iterator for all tiles
// at (image, level) in raster order.
func (t *Tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.Point, opentile.TileResult] {
	eng, err := t.engine(image, level)
	if err != nil {
		return func(yield func(opentile.Point, opentile.TileResult) bool) {}
	}
	return eng.Tiles(ctx)
}
