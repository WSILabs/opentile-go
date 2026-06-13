package ndpi

import (
	"bytes"
	"context"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/fastpath"
	"github.com/wsilabs/opentile-go/internal/format"
)

// Compile-time assertion: *tiler satisfies format.Reader.
var _ format.Reader = (*tiler)(nil)

// ndpiLevel is the internal interface for NDPI pyramid-level tile access.
// Both strippedImage and oneframe.Image satisfy it.
type ndpiLevel interface {
	Tile(x, y int) ([]byte, error)
	TileInto(x, y int, dst []byte) (int, error)
	TileMaxSize() int
	TilePrefix() []byte
	TileBodyMaxSize() int
	TileBodyInto(x, y int, dst []byte) (int, error)
	TileReader(x, y int) (io.ReadCloser, error)
	Tiles(ctx context.Context) iter.Seq2[opentile.Point, opentile.TileResult]
}

// ndpiWarmer is optionally implemented by ndpiLevel types that support page
// pre-warming. warm() is unexported, so only same-package types (strippedImage)
// can satisfy this; oneframe.Image (external package) is handled via type
// assertion in WarmLevel.
type ndpiWarmer interface {
	warm() error
}

// tiler is the NDPI implementation of format.Reader.
type tiler struct {
	md          Metadata
	images      []opentile.Pyramid
	levelImpls  []ndpiLevel // parallel to images[0].Levels
	associated  []opentile.AssociatedImage
	icc         []byte
	dirSpecs    []ndpiDirSpec // page→role mapping captured at Open; used by TIFFDirectories
}

func (t *tiler) Format() opentile.Format              { return opentile.FormatNDPI }
func (t *tiler) Pyramids() []opentile.Pyramid         { return t.images }
func (t *tiler) AssociatedImages() []opentile.AssociatedImage { return t.associated }
func (t *tiler) Metadata() opentile.Metadata            { return t.md.Metadata }
func (t *tiler) ICCProfile() []byte                     { return t.icc }
func (t *tiler) Close() error {
	// v0.27: release each strippedImage's long-lived decoder handle.
	var firstErr error
	for _, lvl := range t.levelImpls {
		if si, ok := lvl.(*strippedImage); ok {
			if err := si.closeResources(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ImageDecodedTile is the v0.27 fast pixel-path dispatch method. The
// opentile root's imageDecodedTile type-asserts the underlying
// reader against the unexported decodedTiler interface and calls
// this method when it matches.
//
// For striped levels (the common case), delegates to
// strippedImage.DecodedTile. For non-striped levels (oneframe,
// associated images), returns fastpath.ErrUnsupported to signal the
// caller to fall back to the slow path.
//
// Added in v0.27.
func (t *tiler) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	si, ok := t.levelImpls[level].(*strippedImage)
	if !ok {
		return nil, fastpath.ErrUnsupported
	}
	return si.DecodedTile(tx, ty, opts)
}

func (t *tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 || image >= len(t.images) {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.images[image].Levels) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.images[image].Levels[level], nil
}

func (t *tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return opentile.ErrLevelOutOfRange
	}
	if w, ok := t.levelImpls[level].(ndpiWarmer); ok {
		return w.warm()
	}
	return nil
}

func (t *tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].Tile(tx, ty)
}

func (t *tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileInto(tx, ty, dst)
}

func (t *tiler) ImageTileMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileMaxSize()
}

func (t *tiler) ImageTilePrefix(image, level int) []byte {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return nil
	}
	return t.levelImpls[level].TilePrefix()
}

func (t *tiler) ImageTileBodyMaxSize(image, level int) int {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return 0
	}
	return t.levelImpls[level].TileBodyMaxSize()
}

func (t *tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	if image != 0 {
		return 0, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return 0, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileBodyInto(tx, ty, dst)
}

func (t *tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelImpls) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelImpls[level].TileReader(tx, ty)
}

func (t *tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.Point, opentile.TileResult] {
	if image != 0 || level < 0 || level >= len(t.levelImpls) {
		return func(yield func(opentile.Point, opentile.TileResult) bool) {}
	}
	return t.levelImpls[level].Tiles(ctx)
}

// levelAsValueType converts an ndpiLevel to its opentile.Level metadata.
// Called at Open time to build the value-type slice; after that, metadata
// is read from t.images[0].Levels. l0Width is the base level's pixel width,
// used to compute the Downsample ratio.
func levelAsValueType(idx int, l ndpiLevel, l0Width int) opentile.Level {
	// Use a type switch to extract metadata fields from the concrete types.
	type inspector interface {
		Index() int
		PyramidIndex() int
		Size() opentile.Size
		TileSize() opentile.Size
		Grid() opentile.Size
		Compression() opentile.Compression
		MPP() opentile.MPP
		FocalPlane() float64
	}
	if ins, ok := l.(inspector); ok {
		sz := ins.Size()
		return opentile.Level{
			Index:        ins.Index(),
			PyramidIndex: ins.PyramidIndex(),
			Size:         sz,
			TileSize:     ins.TileSize(),
			Grid:         ins.Grid(),
			Compression:  ins.Compression(),
			MPP:          ins.MPP(),
			FocalPlane:   ins.FocalPlane(),
			Downsample:   float64(l0Width) / float64(sz.W),
		}
	}
	// Fallback — should not happen; concrete types always implement inspector.
	return opentile.Level{Index: idx, Downsample: 1.0}
}

// levelToValueSlice builds the value-type []opentile.Level from ndpiLevels.
func levelToValueSlice(impls []ndpiLevel) []opentile.Level {
	out := make([]opentile.Level, len(impls))
	// Determine L0 width from the first level's inspector interface.
	var l0Width int
	type sizer interface{ Size() opentile.Size }
	if len(impls) > 0 {
		if s, ok := impls[0].(sizer); ok {
			l0Width = s.Size().W
		}
	}
	for i, l := range impls {
		out[i] = levelAsValueType(i, l, l0Width)
	}
	return out
}

// ndpiTileReader wraps a []byte as an io.ReadCloser. Used when an ndpiLevel
// doesn't implement TileReader.
func ndpiTileReader(b []byte, err error) (io.ReadCloser, error) {
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
