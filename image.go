package opentile

import (
	"context"
	"image"
	"io"
	"iter"
)

// Level is a single resolution in a pyramidal WSI.
//
// Concurrency: Tile, TileInto, TileAt, TileReader, and the Tiles
// iterator are safe to call concurrently from multiple goroutines on
// SVS / Philips / OME tiled / BIF / IFE — those formats have no
// internal locks on the tile hot path. NDPI's striped Level holds a
// per-page mutex around the assembled-frame cache: concurrent reads
// of *different* pages run in parallel; concurrent reads of the
// *same* page serialize. OME OneFrame uses a similar per-level
// extended-frame cache. The underlying [io.ReaderAt] supplied to
// [Open] (or constructed by [OpenFile]) must itself be safe for
// concurrent use; stdlib *os.File and the v0.9 mmap-backed
// io.ReaderAt both satisfy this.
//
// Bytes returned by Tile / TileAt are caller-owned. Bytes written
// by TileInto into the caller-provided dst remain caller-owned.
// opentile-go never reads them after return.
//
// Under [BackingMmap] (the v0.9 default), the underlying file must
// not be truncated or rewritten while a Tiler is open — doing so
// raises SIGBUS in any thread that subsequently reads through the
// mapping. WSI files don't get truncated under normal use; if your
// storage allows it, opt out via [WithBacking](BackingPread).
type Level interface {
	Index() int
	PyramidIndex() int
	Size() Size
	TileSize() Size
	Grid() Size

	// TileOverlap returns the pixel overlap between adjacent tiles at this level.
	// Tile (c, r) is positioned in image-space at
	//   (c · (TileSize().X - TileOverlap().X),
	//    r · (TileSize().Y - TileOverlap().Y)).
	// In the overlap region, tiles further along the row/column overwrite earlier
	// tiles (no blending). Returns image.Point{} for non-overlapping levels and
	// non-BIF formats.
	TileOverlap() image.Point

	Compression() Compression
	MPP() SizeMm
	FocalPlane() float64

	// Tile returns the raw compressed tile bytes at (x, y) as stored in the
	// source TIFF. Equivalent to TileAt(TileCoord{X: x, Y: y}) — the
	// nominal-plane / first-channel / T=0 tile.
	//
	// Allocates a fresh []byte for each call. For high-RPS callers,
	// prefer [Level.TileInto] with a caller-provided buffer (typically
	// from a [sync.Pool]).
	Tile(x, y int) ([]byte, error)

	// TileInto writes the raw compressed tile bytes at (x, y) into dst
	// and returns the number of bytes written. Returns
	// [io.ErrShortBuffer] if len(dst) < [Level.TileMaxSize] for this
	// level (no I/O is performed in that case).
	//
	// Tile(x, y) is shorthand for:
	//
	//	buf := make([]byte, l.TileMaxSize())
	//	n, err := l.TileInto(x, y, buf)
	//	return buf[:n], err
	//
	// dst is caller-allocated and caller-owned; opentile-go writes into
	// it and never reads it after return. Pool-friendly callers should
	// keep a sync.Pool of []byte buffers sized at TileMaxSize() and
	// reset to that capacity before each call.
	//
	// For 2D-only Levels: non-zero Z/C/T arguments to TileAt are not
	// applicable here; use [Level.TileAt] directly when multi-dim
	// addressing is needed. TileInto is the (x, y) hot path.
	//
	// Added in v0.9 as the pool-friendly tile-read entry point.
	TileInto(x, y int, dst []byte) (int, error)

	// TileMaxSize returns the maximum byte length any tile on this
	// level may produce through [Level.Tile] or [Level.TileInto].
	// Computed at level-open time and cached; safe to call repeatedly.
	//
	// For TIFF formats: max(TileByteCounts) + len(JPEGTables splice
	// prefix), where the prefix is the (typically per-level) header
	// inserted by the format reader. For IFE: max(TileEntry.Size).
	// For NDPI striped + OME OneFrame: the assembled-frame tile size
	// (always the level's TileSize().W * TileSize().H bytes for the
	// uncompressed assembly path; the compressed output equals the
	// decoded tile region).
	//
	// Sizing a sync.Pool bucket: round up to the next power-of-two of
	// TileMaxSize() and reuse across many tiles on the same level.
	// Adjacent levels typically have similar TileMaxSize values; one
	// pool per Tiler (sized by max across levels) is usually enough.
	//
	// Added in v0.9 alongside [Level.TileInto].
	TileMaxSize() int

	// TilePrefix returns the constant per-level JPEG splice prefix bytes.
	// When non-nil, callers can ship the prefix once per level + per-tile
	// TileBodyInto output, then reconstitute the full JPEG on the client
	// side via opentile.SpliceJPEGTile. When nil, no splice prefix exists
	// for this level — TileBodyInto returns the same bytes as TileInto.
	//
	// Use case: bandwidth-efficient client-server tile transfer. SVS /
	// Philips / OME / leicascn / generictiff levels with shared JPEGTables
	// typically have ~1 KB of prefix per level applied to 100k+ tiles per
	// slide; deduplicating saves ~100 MB bandwidth per slide.
	//
	// Added in v0.13.
	TilePrefix() []byte

	// TileBodyInto writes the on-disk tile bytes (the "body" — what gets
	// spliced with TilePrefix to form a complete JPEG) into dst. Returns
	// the number of bytes written.
	//
	// For levels where TilePrefix() returns nil (non-splice path),
	// TileBodyInto is functionally equivalent to TileInto.
	//
	// Caller must size dst to at least TileBodyMaxSize().
	//
	// Added in v0.13.
	TileBodyInto(x, y int, dst []byte) (int, error)

	// TileBodyMaxSize returns the upper bound on TileBodyInto output size
	// across all tiles in this level. For levels with shared JPEGTables
	// (TilePrefix() != nil), this is strictly less than TileMaxSize() (no
	// splice prefix added). For levels without shared JPEGTables
	// (TilePrefix() == nil), equal to TileMaxSize().
	//
	// Added in v0.13.
	TileBodyMaxSize() int

	// TileAt returns the raw compressed tile bytes at the multi-dimensional
	// coord. Tile(x, y) is shorthand for TileAt(TileCoord{X: x, Y: y}).
	//
	// For 2D-only Levels (the parent Image's SizeZ/SizeC/SizeT all == 1),
	// any non-zero Z, C, or T value yields *TileError wrapping
	// ErrDimensionUnavailable. For multi-dim Levels (BIF level-0 with
	// IMAGE_DEPTH > 1; future OME multi-Z), the multi-dim coord is
	// resolved by the format's reader.
	//
	// Added in v0.7 alongside TileCoord and the Image dimension accessors.
	TileAt(coord TileCoord) ([]byte, error)

	// TileReader returns a streaming reader for the tile at (x, y). Callers
	// should Close the returned ReadCloser.
	TileReader(x, y int) (io.ReadCloser, error)

	// Tiles iterates every tile position in row-major order. Callers that need
	// parallelism goroutine on top of Tile(x, y); Tiles itself is serial.
	// Z=C=T=0 only — multi-dim iteration is consumer-driven via nested
	// loops over Image.SizeZ/SizeC/SizeT.
	Tiles(ctx context.Context) iter.Seq2[TilePos, TileResult]
}

// AssociatedImage is a non-pyramidal slide-level image (label, overview,
// thumbnail).
//
// Kind() values used across the format readers:
//
//	"label"       — slide label / barcode (typical: small, often LZW)
//	"macro"       — wide overview of the slide (typical: aspect ≥ 2)
//	"overview"    — SVS-only synonym for macro (kept for back-compat
//	                with v0.1 SVS-first conventions)
//	"thumbnail"   — full-slide downsample (typical: square, JPEG)
//	"associated"  — generic-TIFF (v0.10+) catch-all when the heuristic
//	                classifier can't confidently match a kind above
//
// Format readers use the string literals directly; the values above
// are stable and part of the public API contract from v0.10 onward.
type AssociatedImage interface {
	Kind() string
	Size() Size
	Compression() Compression
	Bytes() ([]byte, error)
}

// Image represents one main pyramid in a Tiler. Single-image formats
// (SVS, NDPI, Philips) expose exactly one Image; OME-TIFF can expose
// multiple — one per OME <Image> element that isn't classified as
// macro / label / thumbnail.
//
// Within an Image the Levels are ordered from highest resolution
// (Index 0 = baseline) downwards.
//
// Added in v0.6 alongside Tiler.Images(). The legacy Tiler.Levels() /
// Tiler.Level(i) shortcut accessors continue to work, delegating to
// Images()[0].
type Image interface {
	// Index is the 0-based document-order index of this Image within
	// the Tiler. Always 0 for single-image formats; 0..N-1 for
	// multi-image OME.
	Index() int
	// Name is the format-specific name for this Image — for OME TIFF,
	// the <Image Name="..."> attribute (typically empty for main
	// pyramids; macro / label / thumbnail are routed to AssociatedImage
	// rather than Image). Empty string for non-OME formats.
	Name() string
	// Levels returns the pyramid levels from highest to lowest
	// resolution. Always returns a fresh slice; callers may mutate the
	// slice header without affecting the Image's internal state.
	Levels() []Level
	// Level returns the level at the given index, or
	// ErrLevelOutOfRange if i is out of bounds.
	Level(i int) (Level, error)
	// MPP returns the base-level microns/pixel for this Image. Zero
	// SizeMm when unknown.
	MPP() SizeMm

	// SizeZ returns the count of focal planes carried by this Image.
	// Returns 1 for non-Z-stack slides (every existing 2D format,
	// every BIF slide whose IMAGE_DEPTH tag is absent or 1, every
	// 2D OME slide). Added in v0.7.
	SizeZ() int

	// SizeC returns the count of separately-stored fluorescence /
	// spectral channels. Returns 1 for brightfield slides; > 1 for
	// fluorescence imaging where each channel is its own grayscale
	// image. Added in v0.7.
	//
	// IMPORTANT: this is the count of separately-stored channels,
	// NOT the per-pixel sample count. A brightfield RGB slide has
	// SizeC == 1 (one composite RGB tile per call), even though
	// each pixel decodes to 3 colour samples.
	SizeC() int

	// SizeT returns the count of time points. Returns 1 for
	// non-time-series slides. Added in v0.7.
	SizeT() int

	// ChannelName returns the human-readable name of channel c —
	// e.g., "DAPI", "FITC", "TRITC" for fluorescence; "" for
	// brightfield slides where the single channel is implicit RGB.
	//
	// c must be in [0, SizeC()); panics with index-out-of-range
	// otherwise (matching slice-access conventions). Added in v0.7.
	ChannelName(c int) string

	// ZPlaneFocus returns the focal distance (microns) of plane z
	// from the nominal focal plane. ZPlaneFocus(0) is always 0
	// (Z=0 is by convention the nominal plane). Negative values
	// indicate planes below the nominal plane (near focus); positive
	// values indicate planes above (far focus).
	//
	// z must be in [0, SizeZ()); panics with index-out-of-range
	// otherwise (matching slice-access conventions). Added in v0.7.
	ZPlaneFocus(z int) float64
}

// SingleImage is the one-element Image wrapper used by single-pyramid
// formats (SVS, NDPI, Philips) to satisfy Tiler.Images(). It holds a
// fixed level list; Index() is always 0, Name() always empty, and
// MPP() returns the base level's MPP() (or the zero SizeMm when the
// level list is empty).
//
// Multi-image formats (OME-TIFF) implement opentile.Image directly
// rather than reusing SingleImage.
type SingleImage struct {
	levels []Level
}

// NewSingleImage returns an Image wrapping the supplied level slice.
// The slice header is copied; underlying Level pointers are shared.
func NewSingleImage(levels []Level) *SingleImage {
	cp := make([]Level, len(levels))
	copy(cp, levels)
	return &SingleImage{levels: cp}
}

// Index always returns 0 for SingleImage.
func (s *SingleImage) Index() int { return 0 }

// Name always returns "" for SingleImage.
func (s *SingleImage) Name() string { return "" }

// Levels returns a fresh copy of the wrapped level slice.
func (s *SingleImage) Levels() []Level {
	out := make([]Level, len(s.levels))
	copy(out, s.levels)
	return out
}

// Level returns the level at i, or ErrLevelOutOfRange if i is out
// of bounds.
func (s *SingleImage) Level(i int) (Level, error) {
	if i < 0 || i >= len(s.levels) {
		return nil, ErrLevelOutOfRange
	}
	return s.levels[i], nil
}

// MPP returns the base level's MPP, or the zero SizeMm if there are
// no levels.
func (s *SingleImage) MPP() SizeMm {
	if len(s.levels) == 0 {
		return SizeMm{}
	}
	return s.levels[0].MPP()
}

// SizeZ always returns 1 for SingleImage — no Z-stack support on
// single-pyramid 2D formats. Multi-Z formats (BIF with IMAGE_DEPTH
// > 1; future OME multi-Z) implement Image directly or wrap
// SingleImage and override SizeZ.
func (s *SingleImage) SizeZ() int { return 1 }

// SizeC always returns 1 for SingleImage — no fluorescence /
// multi-channel support on 2D pathology formats. Brightfield RGB
// slides return 1 (single composite RGB channel per pixel).
func (s *SingleImage) SizeC() int { return 1 }

// SizeT always returns 1 for SingleImage — no time-series support
// on pathology formats.
func (s *SingleImage) SizeT() int { return 1 }

// ChannelName always returns "" for SingleImage — the single
// channel is implicit RGB on brightfield slides; consumers don't
// need a name to interpret it.
func (s *SingleImage) ChannelName(c int) string { return "" }

// ZPlaneFocus always returns 0 for SingleImage — single Z-plane
// at nominal focus. Multi-Z Image impls override.
func (s *SingleImage) ZPlaneFocus(z int) float64 { return 0 }

// TilePos is a (column, row) pair returned by Level.Tiles.
type TilePos struct{ X, Y int }

// TileResult carries the yield from Level.Tiles.
type TileResult struct {
	Bytes []byte
	Err   error
}
