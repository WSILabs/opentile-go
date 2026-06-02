package dicom

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
	"github.com/wsilabs/opentile-go/internal/format"
)

// Compile-time assertion: *Tiler satisfies format.Reader.
var _ format.Reader = (*Tiler)(nil)

// instanceBytes returns the (mmapped) bytes of an instance file plus a
// closer. Production passes an mmap-backed opener; tests inject blobs.
type instanceBytes func(path string) (data []byte, closeFn func() error, err error)

type levelEngine struct {
	info    levelInfo
	data    []byte
	spans   []span
	tileMap map[tileKey]int
	grid    opentile.Size
	closeFn func() error
}

// Tiler is the formats/dicom reader. It owns one mmap per instance and
// closes them all in Close().
type Tiler struct {
	img        opentile.Image
	levels     []*levelEngine
	associated []opentile.AssociatedImage
	meta       opentile.Metadata
	dmeta      Metadata // format-specific (Task 8)
	icc        []byte
	closers    []func() error
}

func openSeriesFromInstances(insts []idicom.Instance, open instanceBytes) (*Tiler, error) {
	s, err := assembleSeries(insts)
	if err != nil {
		return nil, err
	}
	t := &Tiler{}
	l0 := s.levels[0].inst
	for i, li := range s.levels {
		data, closeFn, err := open(li.inst.Path)
		if err != nil {
			t.Close()
			return nil, err
		}
		t.closers = append(t.closers, closeFn)
		spans, err := walkEncapsulatedFrames(data)
		if err != nil {
			t.Close()
			return nil, fmt.Errorf("dicom: level %d: %w", i, err)
		}
		across := ceilDiv(li.inst.TotalCols, li.inst.TileCols)
		down := ceilDiv(li.inst.TotalRows, li.inst.TileRows)
		eng := &levelEngine{
			info:    li,
			data:    data,
			spans:   spans,
			tileMap: buildTileMap(li.inst.DimOrg, across, down, li.inst.TileCols, li.inst.FramePositions, li.inst.NumFrames),
			grid:    opentile.Size{W: across, H: down},
			closeFn: closeFn,
		}
		t.levels = append(t.levels, eng)
	}
	t.img = opentile.Image{Name: "", Index: 0, Levels: t.buildLevels()}
	t.meta, t.dmeta = buildMetadata(l0, s)  // Task 8
	t.associated = buildAssociated(s, open) // Task 7
	return t, nil
}

func (t *Tiler) buildLevels() []opentile.Level {
	out := make([]opentile.Level, len(t.levels))
	for i, e := range t.levels {
		out[i] = opentile.Level{
			Index:        i,
			PyramidIndex: 0,
			Size:         opentile.Size{W: e.info.inst.TotalCols, H: e.info.inst.TotalRows},
			TileSize:     opentile.Size{W: e.info.inst.TileCols, H: e.info.inst.TileRows},
			Grid:         e.grid,
			TileOverlap:  image.Point{},
			Compression:  opentile.CompressionJPEG,
			Downsample:   e.info.downsample,
		}
	}
	return out
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

// --- format.Reader methods ---

func (t *Tiler) Format() opentile.Format                { return opentile.FormatDICOM }
func (t *Tiler) Images() []opentile.Image               { return []opentile.Image{t.img} }
func (t *Tiler) Associated() []opentile.AssociatedImage { return t.associated }
func (t *Tiler) Metadata() opentile.Metadata            { return t.meta }
func (t *Tiler) ICCProfile() []byte                     { return t.icc }

func (t *Tiler) Level(imageIdx, level int) (opentile.Level, error) {
	if imageIdx != 0 || level < 0 || level >= len(t.levels) {
		return opentile.Level{}, fmt.Errorf("dicom: level (%d,%d) out of range", imageIdx, level)
	}
	return t.img.Levels[level], nil
}

func (t *Tiler) ImageRawTile(imageIdx, level, tx, ty int) ([]byte, error) {
	e, err := t.engine(imageIdx, level)
	if err != nil {
		return nil, err
	}
	idx, ok := e.tileMap[tileKey{tx, ty}]
	if !ok {
		return nil, &opentile.TileError{Level: level, X: tx, Y: ty, Err: fmt.Errorf("dicom: tile (%d,%d) absent", tx, ty)}
	}
	if idx < 0 || idx >= len(e.spans) {
		return nil, &opentile.TileError{Level: level, X: tx, Y: ty, Err: fmt.Errorf("dicom: frame %d out of range", idx)}
	}
	sp := e.spans[idx]
	out := make([]byte, sp.length)
	copy(out, e.data[sp.off:sp.off+sp.length])
	return out, nil
}

func (t *Tiler) ImageRawTileInto(imageIdx, level, tx, ty int, dst []byte) (int, error) {
	b, err := t.ImageRawTile(imageIdx, level, tx, ty)
	if err != nil {
		return 0, err
	}
	if len(dst) < len(b) {
		return 0, fmt.Errorf("dicom: dst too small (%d < %d)", len(dst), len(b))
	}
	return copy(dst, b), nil
}

func (t *Tiler) engine(imageIdx, level int) (*levelEngine, error) {
	if imageIdx != 0 || level < 0 || level >= len(t.levels) {
		return nil, fmt.Errorf("dicom: level (%d,%d) out of range", imageIdx, level)
	}
	return t.levels[level], nil
}

// ImageTileMaxSize returns the largest frame length for the level (bound for buffers).
func (t *Tiler) ImageTileMaxSize(imageIdx, level int) int {
	e, err := t.engine(imageIdx, level)
	if err != nil {
		return 0
	}
	max := 0
	for _, sp := range e.spans {
		if sp.length > max {
			max = sp.length
		}
	}
	return max
}

// DICOM frames are self-contained JPEGs: no shared prefix.
func (t *Tiler) ImageTilePrefix(imageIdx, level int) []byte { return nil }
func (t *Tiler) ImageTileBodyMaxSize(imageIdx, level int) int {
	return t.ImageTileMaxSize(imageIdx, level)
}
func (t *Tiler) ImageTileBodyInto(imageIdx, level, tx, ty int, dst []byte) (int, error) {
	return t.ImageRawTileInto(imageIdx, level, tx, ty, dst)
}

func (t *Tiler) ImageTileReader(imageIdx, level, tx, ty int) (io.ReadCloser, error) {
	b, err := t.ImageRawTile(imageIdx, level, tx, ty)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (t *Tiler) WarmLevel(imageIdx, level int) error {
	_, err := t.engine(imageIdx, level)
	return err // bytes already mapped; warming is a no-op
}

func (t *Tiler) ImageRangeTiles(ctx context.Context, imageIdx, level int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
		e, err := t.engine(imageIdx, level)
		if err != nil {
			return
		}
		for ty := 0; ty < e.grid.H; ty++ {
			for tx := 0; tx < e.grid.W; tx++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				b, err := t.ImageRawTile(imageIdx, level, tx, ty)
				if !yield(opentile.TilePos{X: tx, Y: ty}, opentile.TileResult{Bytes: b, Err: err}) {
					return
				}
			}
		}
	}
}

func (t *Tiler) Close() error {
	var firstErr error
	for _, c := range t.closers {
		if c != nil {
			if err := c(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
