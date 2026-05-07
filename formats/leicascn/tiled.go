package leicascn

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"iter"

	opentile "github.com/cornish/opentile-go"
)

// compositeLevel is the public Level impl for Leica SCN. Wraps N
// tiledRegion slots (one per main scan) plus a synthesized blank
// tile for inter-region gaps.
//
// Per sealed Q4 + Q6: the composite presents the slide as one
// coherent canvas; consumers never see the discontinuous-scanning
// detail. Tile dispatch translates (x, y) to slide-local coords,
// finds the region containing that point, and reads from that
// region's IFD. Coords outside any region return the cached blank
// JPEG.
type compositeLevel struct {
	index       int
	pyrIndex    int
	size        opentile.Size  // composite/union pixel extent
	tileSize    opentile.Size  // uniform across all regions per Q5
	grid        opentile.Size  // composite tile grid (ceil(size / tileSize))
	compression opentile.Compression
	sizeC       int
	maxTile     int

	regions      []*tiledRegion
	regionBounds []regionBound // composite-pixel-space bounds for each region
}

// newCompositeLevel constructs a compositeLevel from a CompositeLevel
// (the value-typed result of ComposePyramid) plus the corresponding
// per-region tiledRegion slots.
//
// The tile size is taken from the first region (Q5 invariant: all
// regions share tile size at any given level). The composite grid
// is ceil(size / tileSize).
func newCompositeLevel(idx, pyrIdx int, cl CompositeLevel, regions []*tiledRegion) (*compositeLevel, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("leicascn: compositeLevel %d has no regions", idx)
	}
	tileSize := regions[0].tileSize
	for i, r := range regions[1:] {
		if r.tileSize != tileSize {
			return nil, fmt.Errorf("leicascn: composite L%d region %d tileSize %v != region 0 %v",
				idx, i+1, r.tileSize, tileSize)
		}
	}

	gx := ceilDiv(cl.PixelSizeX, tileSize.W)
	gy := ceilDiv(cl.PixelSizeY, tileSize.H)

	maxTile := 0
	for _, r := range regions {
		if mt := r.maxTileSize(); mt > maxTile {
			maxTile = mt
		}
	}
	// blankTile bytes can also be returned via Tile/TileInto; size dst
	// budget for the worst case across regions + the blank tile.
	bt, err := blankTile(tileSize.W, tileSize.H)
	if err != nil {
		return nil, err
	}
	if len(bt) > maxTile {
		maxTile = len(bt)
	}

	// Tile-snap region offsets to the nearest tile boundary (rounded
	// down). Necessary because SCN's <view offsetX/Y> values are in
	// nm and don't generally align to our composite-pixel-space tile
	// grid (e.g., Leica-2 region 0 lands at composite px Y=24427 vs
	// nearest tile boundary Y=24064 = 363 px misalignment).
	//
	// Trade-off: the region appears in the composite at most
	// (tile_size - 1) pixels earlier than its true slide-physical
	// position. For 512×512 tiles at typical 250 nm/px scanner
	// resolution, that's ≤ 128 µm worst-case position error —
	// pathology-rendering-acceptable.
	//
	// Consumers needing pixel-perfect slide positioning can use
	// Metadata.Regions to read the original nm-space (offset, size)
	// and composite themselves. Documented in docs/formats/leicascn.md.
	bounds := make([]regionBound, len(cl.Regions))
	for i, rl := range cl.Regions {
		bounds[i] = regionBound{
			OffsetX:    (rl.OffsetX / tileSize.W) * tileSize.W,
			OffsetY:    (rl.OffsetY / tileSize.H) * tileSize.H,
			PixelSizeX: rl.PixelSizeX,
			PixelSizeY: rl.PixelSizeY,
		}
	}

	return &compositeLevel{
		index:        idx,
		pyrIndex:     pyrIdx,
		size:         opentile.Size{W: cl.PixelSizeX, H: cl.PixelSizeY},
		tileSize:     tileSize,
		grid:         opentile.Size{W: gx, H: gy},
		compression:  opentile.CompressionJPEG, // SCN tiles + blank tiles are always JPEG
		sizeC:        cl.SizeC,
		maxTile:      maxTile,
		regions:      regions,
		regionBounds: bounds,
	}, nil
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

func (l *compositeLevel) Index() int                        { return l.index }
func (l *compositeLevel) PyramidIndex() int                 { return l.pyrIndex }
func (l *compositeLevel) Size() opentile.Size               { return l.size }
func (l *compositeLevel) TileSize() opentile.Size           { return l.tileSize }
func (l *compositeLevel) Grid() opentile.Size               { return l.grid }
func (l *compositeLevel) Compression() opentile.Compression { return l.compression }
func (l *compositeLevel) MPP() opentile.SizeMm              { return opentile.SizeMm{} }
func (l *compositeLevel) FocalPlane() float64               { return 0 }
func (l *compositeLevel) TileOverlap() image.Point          { return image.Point{} }
func (l *compositeLevel) TileMaxSize() int                  { return l.maxTile }

// regionBound is the per-region positioning info inside the
// composite level, kept alongside the tiledRegion slice for
// O(N)-per-tile dispatch (N ≤ 4 on our fixtures; quad-tree lookup
// is YAGNI until N grows).
type regionBound struct {
	OffsetX, OffsetY       int
	PixelSizeX, PixelSizeY int
}

// findRegion returns the index in l.regions of the region containing
// composite-pixel-space coords (px, py), or -1 if no region covers
// that point.
func (l *compositeLevel) findRegion(px, py int) int {
	for i, b := range l.regionBounds {
		if px >= b.OffsetX && px < b.OffsetX+b.PixelSizeX &&
			py >= b.OffsetY && py < b.OffsetY+b.PixelSizeY {
			return i
		}
	}
	return -1
}

// Tile returns the tile bytes at composite-space (x, y) for channel 0.
// Convenience wrapper over TileAt for the 2D entry point.
func (l *compositeLevel) Tile(x, y int) ([]byte, error) {
	return l.TileAt(opentile.TileCoord{X: x, Y: y})
}

// TileInto writes tile bytes at composite (x, y) channel 0 into dst.
func (l *compositeLevel) TileInto(x, y int, dst []byte) (int, error) {
	return l.tileIntoChannel(0, x, y, dst)
}

// TileAt is the multi-dim entry point. Validates Z=T=0; routes by
// channel index. SCN supports SizeC > 1 on fluorescence files.
func (l *compositeLevel) TileAt(coord opentile.TileCoord) ([]byte, error) {
	if coord.Z != 0 || coord.T != 0 {
		return nil, &opentile.TileError{
			Level: l.index, X: coord.X, Y: coord.Y,
			Err: opentile.ErrDimensionUnavailable,
		}
	}
	if coord.C < 0 || coord.C >= l.sizeC {
		return nil, &opentile.TileError{
			Level: l.index, X: coord.X, Y: coord.Y,
			Err: opentile.ErrDimensionUnavailable,
		}
	}
	if coord.X < 0 || coord.Y < 0 || coord.X >= l.grid.W || coord.Y >= l.grid.H {
		return nil, &opentile.TileError{
			Level: l.index, X: coord.X, Y: coord.Y,
			Err: opentile.ErrTileOutOfBounds,
		}
	}

	px := coord.X * l.tileSize.W
	py := coord.Y * l.tileSize.H

	regionIdx := l.findRegion(px, py)
	if regionIdx < 0 {
		// Inter-region gap: return a defensive copy of the cached
		// blank JPEG. Cheap because the cache stores the bytes once;
		// the copy is one allocation per gap-tile read.
		bt, err := blankTile(l.tileSize.W, l.tileSize.H)
		if err != nil {
			return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: err}
		}
		out := make([]byte, len(bt))
		copy(out, bt)
		return out, nil
	}

	rb := l.regionBounds[regionIdx]
	rx := (px - rb.OffsetX) / l.tileSize.W
	ry := (py - rb.OffsetY) / l.tileSize.H
	b, err := l.regions[regionIdx].Tile(coord.C, rx, ry)
	if err != nil {
		return nil, &opentile.TileError{Level: l.index, X: coord.X, Y: coord.Y, Err: err}
	}
	return b, nil
}

func (l *compositeLevel) tileIntoChannel(c, x, y int, dst []byte) (int, error) {
	if c < 0 || c >= l.sizeC {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrDimensionUnavailable}
	}
	if x < 0 || y < 0 || x >= l.grid.W || y >= l.grid.H {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	px := x * l.tileSize.W
	py := y * l.tileSize.H
	regionIdx := l.findRegion(px, py)
	if regionIdx < 0 {
		bt, err := blankTile(l.tileSize.W, l.tileSize.H)
		if err != nil {
			return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
		}
		if len(dst) < len(bt) {
			return 0, io.ErrShortBuffer
		}
		copy(dst, bt)
		return len(bt), nil
	}
	rb := l.regionBounds[regionIdx]
	rx := (px - rb.OffsetX) / l.tileSize.W
	ry := (py - rb.OffsetY) / l.tileSize.H
	n, err := l.regions[regionIdx].TileInto(c, rx, ry, dst)
	if err != nil {
		return 0, &opentile.TileError{Level: l.index, X: x, Y: y, Err: err}
	}
	return n, nil
}

// TileReader returns an io.ReadCloser over the tile bytes. Materialises
// through Tile() (multi-region splice + blank-fill paths can't be
// expressed as contiguous file regions in general).
func (l *compositeLevel) TileReader(x, y int) (io.ReadCloser, error) {
	b, err := l.Tile(x, y)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// Tiles iterates all (x, y) in row-major order at channel 0.
func (l *compositeLevel) Tiles(ctx context.Context) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return func(yield func(opentile.TilePos, opentile.TileResult) bool) {
		for y := 0; y < l.grid.H; y++ {
			for x := 0; x < l.grid.W; x++ {
				if err := ctx.Err(); err != nil {
					yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Err: err})
					return
				}
				b, err := l.Tile(x, y)
				if !yield(opentile.TilePos{X: x, Y: y}, opentile.TileResult{Bytes: b, Err: err}) {
					return
				}
			}
		}
	}
}

// warm pre-faults the page-cache pages backing every region's tile data.
func (l *compositeLevel) warm() error {
	for _, r := range l.regions {
		if err := r.warm(); err != nil {
			return err
		}
	}
	return nil
}
