package dzi

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
	"github.com/wsilabs/opentile-go/internal/fastpath"
	"github.com/wsilabs/opentile-go/internal/format"
)

// Compile-time assertion: *Tiler satisfies format.Reader (a superset of the
// root's slideReader interface, so the OpenFile hook's type-assertion succeeds).
var _ format.Reader = (*Tiler)(nil)

// Tiler is the bare-DZI reader. It holds the parsed manifest and the absolute
// path to the <base>_files tile tree; tiles are read from the filesystem on
// demand. There is no scan-properties.xml and no associated_images/ in a bare
// DZI, so Metadata is empty and AssociatedImages is nil.
type Tiler struct {
	filesDir string // absolute path to <base>_files
	manifest idzi.Manifest

	dziImage     opentile.Pyramid // the single pyramid (DZI has exactly one image)
	levelEngines []*level
}

// openBareDZI parses the manifest at dziPath and builds the pyramid.
// filesDir is <dir(dziPath)>/<base(dziPath) without .ext>_files.
// Overlap>0 is now accepted; the regionLayout/subtileLayout methods
// composite the border-cropped content cells at read time.
func openBareDZI(dziPath string, manifestBytes []byte, filesDir string) (*Tiler, error) {
	m, err := idzi.ParseManifest(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("dzi: parse manifest %s: %w", dziPath, err)
	}
	t := &Tiler{filesDir: filesDir, manifest: m}
	t.buildLevels()
	return t, nil
}

// buildLevels populates dziImage + levelEngines, one entry per DZI pyramid
// level. opentile L_i = DZI (MaxLevel - i); index 0 = highest resolution.
func (t *Tiler) buildLevels() {
	maxLevel := idzi.MaxLevel(t.manifest.Width, t.manifest.Height)

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
	l0W, _ := idzi.LevelDims(t.manifest.Width, t.manifest.Height, maxLevel)
	mode := opentile.OverlapNone
	tov := opentile.Point{}
	if t.manifest.Overlap > 0 {
		mode = opentile.OverlapBordered
		tov = opentile.Point{X: t.manifest.Overlap, Y: t.manifest.Overlap}
	}
	for i := 0; i <= maxLevel; i++ {
		dziL := maxLevel - i
		w, h := idzi.LevelDims(t.manifest.Width, t.manifest.Height, dziL)
		cols, rows := idzi.GridDims(w, h, t.manifest.TileSize)
		engines[i] = &level{
			filesDir:    t.filesDir,
			format:      t.manifest.Format,
			dziLevel:    dziL,
			openTileIdx: i,
			width:       w,
			height:      h,
			cols:        cols,
			rows:        rows,
			tileSize:    t.manifest.TileSize,
			overlap:     t.manifest.Overlap,
		}
		valueLevels[i] = opentile.Level{
			Index:        i,
			PyramidIndex: i,
			Size:         opentile.Size{W: w, H: h},
			TileSize:     opentile.Size{W: t.manifest.TileSize, H: t.manifest.TileSize},
			Grid:         opentile.Size{W: cols, H: rows},
			Compression:  comp,
			Downsample:   float64(l0W) / float64(w),
			OverlapMode:  mode,
			Overlapping:  t.manifest.Overlap > 0,
			TileOverlap:  tov,
		}
	}
	t.dziImage = opentile.Pyramid{Name: "", Index: 0, Levels: valueLevels}
	t.levelEngines = engines
}

// Format returns opentile.FormatDZI.
func (t *Tiler) Format() opentile.Format { return opentile.FormatDZI }

// Close is a no-op: bare DZI holds no open handles (tiles are read per call).
func (t *Tiler) Close() error { return nil }

// Pyramids returns the single Pyramid carried by the bare DZI.
func (t *Tiler) Pyramids() []opentile.Pyramid {
	if t.levelEngines == nil {
		return nil
	}
	return []opentile.Pyramid{t.dziImage}
}

// Level returns the value-type Level for (image, level).
func (t *Tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelEngines) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.dziImage.Levels[level], nil
}

// AssociatedImages returns nil — a bare DZI has no associated images.
func (t *Tiler) AssociatedImages() []opentile.AssociatedImage { return nil }

// Metadata returns an empty cross-format metadata view — a bare DZI manifest
// carries no scan/resolution metadata.
func (t *Tiler) Metadata() opentile.Metadata { return opentile.Metadata{} }

// ICCProfile returns nil — bare DZI surfaces no ICC profile.
func (t *Tiler) ICCProfile() []byte { return nil }

// WarmLevel validates bounds; tile reads are direct file reads, so warming is a
// no-op hint.
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

// ImageTilePrefix returns nil — DZI tiles carry no shared prefix.
func (t *Tiler) ImageTilePrefix(image, level int) []byte { return nil }

// ImageTileBodyMaxSize equals ImageTileMaxSize (no prefix).
func (t *Tiler) ImageTileBodyMaxSize(image, level int) int { return t.ImageTileMaxSize(image, level) }

// ImageTileBodyInto equals ImageRawTileInto (TilePrefix is nil).
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

// ImageDecodedTile implements the decodedTiler fast-path for DZI tiles.
// JPEG tiles go through the codec registry (libjpeg-turbo cgo, with IDCT
// scale support). PNG tiles are decoded via the standard library (pure Go,
// works under nocgo). For other compression values it returns
// fastpath.ErrUnsupported, causing the caller to fall back to the generic
// codec-registry path (which will return ErrCodecNotRegistered for
// unrecognised tile formats).
func (t *Tiler) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	lvl, err := t.Level(image, level)
	if err != nil {
		return nil, err
	}
	if lvl.Compression != opentile.CompressionPNG && lvl.Compression != opentile.CompressionJPEG {
		return nil, fmt.Errorf("dzi: ImageDecodedTile: %w", fastpath.ErrUnsupported)
	}
	raw, err := t.ImageRawTile(image, level, tx, ty)
	if err != nil {
		return nil, err
	}
	return assocdecode.ViaCodec(lvl.Compression, raw, opts)
}

// TileOrigin returns the content cell's top-left in level pixels for (level, col, row).
// This implements the regionLayout interface for Overlap>0 composite reads.
func (t *Tiler) TileOrigin(level, col, row int) (x, y int, ok bool) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0, false
	}
	return eng.tileOrigin(col, row)
}

// TilesIntersecting returns the content cells overlapping [x,y,x+w,y+h) at the given level.
// This implements the regionLayout interface for Overlap>0 composite reads.
func (t *Tiler) TilesIntersecting(level, x, y, w, h int) []struct{ Col, Row int } {
	eng, err := t.engine(0, level)
	if err != nil {
		return nil
	}
	return eng.tilesIntersecting(x, y, w, h)
}

// StitchedSize returns the level's content dimensions and ok=true only when
// Overlap>0. ok=false keeps Overlap=0 levels on the clean-grid fast path.
// This implements the regionLayout interface.
func (t *Tiler) StitchedSize(level int) (w, h int, ok bool) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0, false
	}
	return eng.stitchedSize()
}

// UnitSize returns the content cell size (TileSize × TileSize) for the given level.
// This implements the subtileLayout interface.
func (t *Tiler) UnitSize(level int) (w, h int) {
	eng, err := t.engine(0, level)
	if err != nil {
		return 0, 0
	}
	return eng.unitSize()
}

// SubtileSource maps a content cell (col, row) to the same stored tile plus
// its crop origin. This implements the subtileLayout interface.
func (t *Tiler) SubtileSource(level, col, row int) (srcCol, srcRow, cropX, cropY int) {
	eng, err := t.engine(0, level)
	if err != nil {
		return col, row, 0, 0
	}
	return eng.subtileSource(col, row)
}

// ImageRangeTiles returns a row-major iterator over all tiles at (image, level).
func (t *Tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.Point, opentile.TileResult] {
	eng, err := t.engine(image, level)
	if err != nil {
		return func(yield func(opentile.Point, opentile.TileResult) bool) {}
	}
	return eng.Tiles(ctx)
}
