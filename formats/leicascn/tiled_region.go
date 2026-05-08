package leicascn

import (
	"fmt"
	"io"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/jpeg"
	"github.com/cornish/opentile-go/internal/tiff"
)

// tiledRegion represents one main scan's data at a single pyramid
// level. It carries per-channel TIFF-IFD information: tile offsets,
// counts, JPEGTables, and the precomputed splice prefix for the
// v0.9 zero-alloc path. Tile reads dispatch by channel index.
//
// SCN main scans are tiled JPEG (verified across all 3 fixtures: comp=7,
// tile size 512×512). Each level + channel combination is one TIFF
// IFD; tiledRegion bundles all channels at one level into a single
// region-level slot for the compositeLevel to dispatch into.
type tiledRegion struct {
	// Level-wide invariants (all channels share these).
	tileSize  opentile.Size // tile pixel dimensions
	grid      opentile.Size // region-local tile grid (cols, rows)
	pixelSize opentile.Size // region's pixel extent at this level

	// Per-channel data; len == compositeLevel.SizeC. For brightfield
	// SCN files with SizeC=1, perChannel has length 1.
	perChannel []channelData

	// Source reader (shared across channels — all IFDs come from the
	// same TIFF file).
	reader io.ReaderAt
}

// channelData holds the TIFF-IFD-derived state for one channel at
// one pyramid level: tile offsets/counts plus the cached JPEG splice
// prefix used by Tile / TileInto.
type channelData struct {
	offsets      []uint64
	counts       []uint64
	jpegTables   []byte // TIFF tag 347; nil if absent
	splicePrefix []byte // BuildSplicePrefix output; nil if no JPEGTables
	maxTileSize  int    // max(counts) + len(splicePrefix)
	// bodyMaxSize is max(counts) — strictly less than maxTileSize when
	// splicePrefix is non-nil. Used by tiledRegion.bodyMaxSize().
	bodyMaxSize int
}

// newTiledRegion constructs a tiledRegion from a RegionLevel slot
// plus the underlying *tiff.File. ifdPerChannel maps channel index →
// IFD index in the TIFF file; the constructor pulls each IFD's
// tile offsets/counts/JPEGTables and precomputes the splice prefix.
func newTiledRegion(rl RegionLevel, file *tiff.File, r io.ReaderAt) (*tiledRegion, error) {
	if len(rl.IFDPerChannel) == 0 {
		return nil, fmt.Errorf("leicascn: region has no channels")
	}
	pages := file.Pages()

	// Use the first channel's IFD as the canonical source for tile
	// size / grid (all channels at a given level share these per Q5
	// invariants).
	canonical := pages[rl.IFDPerChannel[0]]
	tw, ok := canonical.TileWidth()
	if !ok || tw == 0 {
		return nil, fmt.Errorf("leicascn: region IFD %d not tiled", rl.IFDPerChannel[0])
	}
	tl, ok := canonical.TileLength()
	if !ok || tl == 0 {
		return nil, fmt.Errorf("leicascn: region IFD %d missing TileLength", rl.IFDPerChannel[0])
	}
	gx, gy, err := canonical.TileGrid()
	if err != nil {
		return nil, fmt.Errorf("leicascn: region IFD %d TileGrid: %w", rl.IFDPerChannel[0], err)
	}

	tr := &tiledRegion{
		tileSize:   opentile.Size{W: int(tw), H: int(tl)},
		grid:       opentile.Size{W: gx, H: gy},
		pixelSize:  opentile.Size{W: rl.PixelSizeX, H: rl.PixelSizeY},
		perChannel: make([]channelData, len(rl.IFDPerChannel)),
		reader:     r,
	}

	for ch, ifdIdx := range rl.IFDPerChannel {
		if ifdIdx < 0 || ifdIdx >= len(pages) {
			return nil, fmt.Errorf("leicascn: region channel %d IFD %d out of range",
				ch, ifdIdx)
		}
		page := pages[ifdIdx]

		// Verify per-channel tile size + grid match canonical (Q5
		// invariant for multi-channel; cheap sanity check).
		if cw, _ := page.TileWidth(); cw != tw {
			return nil, fmt.Errorf("leicascn: region channel %d IFD %d TileWidth %d != canonical %d",
				ch, ifdIdx, cw, tw)
		}
		if cl, _ := page.TileLength(); cl != tl {
			return nil, fmt.Errorf("leicascn: region channel %d IFD %d TileLength %d != canonical %d",
				ch, ifdIdx, cl, tl)
		}

		offsets, err := page.TileOffsets64()
		if err != nil {
			return nil, fmt.Errorf("leicascn: region channel %d IFD %d TileOffsets: %w",
				ch, ifdIdx, err)
		}
		counts, err := page.TileByteCounts64()
		if err != nil {
			return nil, fmt.Errorf("leicascn: region channel %d IFD %d TileByteCounts: %w",
				ch, ifdIdx, err)
		}
		if len(offsets) != gx*gy || len(counts) != gx*gy {
			return nil, fmt.Errorf("leicascn: region channel %d IFD %d tile-table mismatch (%d offsets, %d counts, %dx%d grid)",
				ch, ifdIdx, len(offsets), len(counts), gx, gy)
		}

		var jpegTables []byte
		if tb, ok := page.JPEGTables(); ok {
			jpegTables = tb
		}

		var maxCount uint64
		for _, c := range counts {
			if c > maxCount {
				maxCount = c
			}
		}
		bodyMax := int(maxCount)
		maxTile := bodyMax

		var splicePrefix []byte
		if len(jpegTables) > 0 {
			sp, err := jpeg.BuildSplicePrefix(jpegTables, false)
			if err != nil {
				return nil, fmt.Errorf("leicascn: region channel %d IFD %d build splice prefix: %w",
					ch, ifdIdx, err)
			}
			splicePrefix = sp
			maxTile += len(splicePrefix)
		}

		tr.perChannel[ch] = channelData{
			offsets:      offsets,
			counts:       counts,
			jpegTables:   jpegTables,
			splicePrefix: splicePrefix,
			maxTileSize:  maxTile,
			bodyMaxSize:  bodyMax,
		}
	}
	return tr, nil
}

// maxTileSize returns the largest tile output size across all
// channels, used by compositeLevel to size dst buffers.
func (r *tiledRegion) maxTileSize() int {
	max := 0
	for _, c := range r.perChannel {
		if c.maxTileSize > max {
			max = c.maxTileSize
		}
	}
	return max
}

// bodyMaxSize returns the largest on-disk tile byte count across all
// channels: max(counts). Strictly less than maxTileSize() when channels
// carry shared JPEGTables (splice path). Used by compositeLevel.TileBodyMaxSize.
func (r *tiledRegion) bodyMaxSize() int {
	max := 0
	for _, c := range r.perChannel {
		if c.bodyMaxSize > max {
			max = c.bodyMaxSize
		}
	}
	return max
}

// tileBodyInto reads on-disk tile bytes at region-local (x, y) channel
// c into dst WITHOUT applying the splice prefix. This is an internal
// helper; compositeLevel.TileBodyInto delegates to it for in-region
// tiles. Returns io.ErrShortBuffer if dst is too small.
func (r *tiledRegion) tileBodyInto(c, x, y int, dst []byte) (int, error) {
	if c < 0 || c >= len(r.perChannel) {
		return 0, opentile.ErrDimensionUnavailable
	}
	idx, err := r.indexOf(x, y)
	if err != nil {
		return 0, err
	}
	cd := &r.perChannel[c]
	count := int(cd.counts[idx])
	if len(dst) < count {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(r.reader, dst[:count], int64(cd.offsets[idx])); err != nil {
		return 0, err
	}
	return count, nil
}

// indexOf computes the row-major tile index for region-local (x, y)
// and validates the entry. Returns ErrTileOutOfBounds for out-of-grid;
// ErrCorruptTile for zero-length entries.
func (r *tiledRegion) indexOf(x, y int) (int, error) {
	if x < 0 || y < 0 || x >= r.grid.W || y >= r.grid.H {
		return 0, opentile.ErrTileOutOfBounds
	}
	idx := y*r.grid.W + x
	// Pick channel 0 for the count check; all channels at a level
	// share the same grid, but per-channel zero-counts can differ.
	if r.perChannel[0].counts[idx] == 0 {
		return 0, opentile.ErrCorruptTile
	}
	return idx, nil
}

// Tile returns the tile bytes at region-local (x, y) for channel c.
// Splice JPEGTables on the fly if present.
func (r *tiledRegion) Tile(c, x, y int) ([]byte, error) {
	if c < 0 || c >= len(r.perChannel) {
		return nil, opentile.ErrDimensionUnavailable
	}
	idx, err := r.indexOf(x, y)
	if err != nil {
		return nil, err
	}
	cd := &r.perChannel[c]
	length := cd.counts[idx]
	off := int64(cd.offsets[idx])
	buf := make([]byte, length)
	if err := tiff.ReadAtFull(r.reader, buf, off); err != nil {
		return nil, err
	}
	if len(cd.jpegTables) > 0 {
		out, err := jpeg.InsertTables(buf, cd.jpegTables)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	return buf, nil
}

// TileInto writes tile bytes at region-local (x, y) channel c
// directly into dst. Zero-alloc on the splice path via
// jpeg.InsertPrefixInPlace; zero-alloc on the no-splice path via a
// single ReadAt. Returns io.ErrShortBuffer if dst is too small.
func (r *tiledRegion) TileInto(c, x, y int, dst []byte) (int, error) {
	if c < 0 || c >= len(r.perChannel) {
		return 0, opentile.ErrDimensionUnavailable
	}
	idx, err := r.indexOf(x, y)
	if err != nil {
		return 0, err
	}
	cd := &r.perChannel[c]
	length := int(cd.counts[idx])
	off := int64(cd.offsets[idx])

	if cd.splicePrefix == nil {
		if len(dst) < length {
			return 0, io.ErrShortBuffer
		}
		if err := tiff.ReadAtFull(r.reader, dst[:length], off); err != nil {
			return 0, err
		}
		return length, nil
	}

	prefixLen := len(cd.splicePrefix)
	if len(dst) < length+prefixLen {
		return 0, io.ErrShortBuffer
	}
	if err := tiff.ReadAtFull(r.reader, dst[prefixLen:prefixLen+length], off); err != nil {
		return 0, err
	}
	n, err := jpeg.InsertPrefixInPlace(dst, length, cd.splicePrefix)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// warm pre-faults the page-cache pages backing every tile across
// every channel on this region. Called via compositeLevel.warm().
func (r *tiledRegion) warm() error {
	for _, cd := range r.perChannel {
		for i, off := range cd.offsets {
			if err := tiff.TouchPages(r.reader, int64(off), int64(cd.counts[i])); err != nil {
				return err
			}
		}
	}
	return nil
}
