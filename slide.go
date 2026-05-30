package opentile

import (
	"context"
	"io"
	"iter"
	"sync"

	"github.com/wsilabs/opentile-go/internal/decoderhandle"
)

// slideReader is the unexported interface with the same method set as
// internal/format.Reader. Slide uses this type so that the opentile package
// does not need to import internal/format (which imports opentile, creating a
// cycle). Any format.Reader automatically satisfies slideReader because the
// method sets are identical.
type slideReader interface {
	Format() Format
	Images() []Image
	Level(image, level int) (Level, error)
	Associated() []AssociatedImage
	Metadata() Metadata
	ICCProfile() []byte
	WarmLevel(image, level int) error

	// Raw tile access.
	ImageRawTile(image, level, tx, ty int) ([]byte, error)
	ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error)

	// Splice-prefix optimization family.
	ImageTileMaxSize(image, level int) int
	ImageTilePrefix(image, level int) []byte
	ImageTileBodyMaxSize(image, level int) int
	ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error)
	ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error)

	// Range-over-function iterator.
	ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[TilePos, TileResult]

	Close() error
}

// Slide is the canonical handle for an open whole-slide image.
// Constructed via OpenFile (path) or Open (io.ReaderAt). Replaces the
// pre-v0.23 Tiler interface as the public return type of Open and
// OpenFile.
//
// Concurrency contract: all accessor methods (Format, Images, Levels,
// Level, Associated, Metadata, ICCProfile) are safe to call concurrently.
// Tile reads via RawTile / RawTileInto are safe concurrently.
// Close must not race with in-flight tile reads.
type Slide struct {
	r slideReader

	// v0.28: per-codec decoder pool cache. Lazy: nil until first
	// decoderFor() call. Drained by Close. Keyed by TIFF compression
	// tag (the same tag space CompressionToTIFFTag emits).
	handlesMu sync.Mutex
	handles   map[uint16]*decoderhandle.Pool

	// v0.30: per-Slide read-path memory budget (bytes), resolved at
	// Open from WithMemoryBudget / OPENTILE_READ_MEMORY_BUDGET /
	// default. Read by newStripIterator to size the C1 tile cache.
	readBudget int64
}

// Format returns the canonical format identifier.
func (s *Slide) Format() Format { return s.r.Format() }

// Images returns the main pyramids carried by this file. Always
// returns at least one Image; multi-image OME-TIFF exposes multiple.
// Index 0 is the legacy Levels() / Level(i) shortcut target.
func (s *Slide) Images() []Image { return s.r.Images() }

// Levels is a shortcut for s.Images()[0].Levels.
func (s *Slide) Levels() []Level {
	imgs := s.r.Images()
	if len(imgs) == 0 {
		return nil
	}
	return imgs[0].Levels
}

// Level is a shortcut for s.Images()[0].Levels[i].
func (s *Slide) Level(i int) (Level, error) { return s.r.Level(0, i) }

// Associated returns the auxiliary images (label, macro, thumbnail,
// overview, ...) embedded in this slide.
func (s *Slide) Associated() []AssociatedImage { return s.r.Associated() }

// Metadata returns the cross-format metadata view.
func (s *Slide) Metadata() Metadata { return s.r.Metadata() }

// ICCProfile returns the embedded color profile bytes, or nil if the
// slide has none.
func (s *Slide) ICCProfile() []byte { return s.r.ICCProfile() }

// WarmLevel pre-warms the page cache for level i within image 0. Hint-only.
func (s *Slide) WarmLevel(i int) error { return s.r.WarmLevel(0, i) }

// Close releases resources held by the slide.
// Close releases the Slide's resources: drains every cached decoder
// pool, then delegates to the underlying reader (which closes the
// mmap or file handle and tears down format-specific state).
//
// v0.27 contract: Close must not race with in-flight tile reads. v0.28
// preserves that. A racing Borrow gets ErrClosed; a racing Decode
// completes against an already-borrowed Decoder, then Return closes
// it directly via the pool's closed-pool branch.
func (s *Slide) Close() error {
	s.handlesMu.Lock()
	handles := s.handles
	s.handles = nil
	s.handlesMu.Unlock()

	var firstErr error
	for _, pool := range handles {
		if err := pool.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.r.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// UnwrapReader returns the underlying format-specific reader.
// Format packages use this to chain-walk through *Slide to their
// own concrete reader type (e.g., bif.MetadataOf(slide) walks the
// chain to find the BIF *Tiler).
func (s *Slide) UnwrapReader() any { return s.r }

// RawTile returns the compressed tile bytes at the given level and
// tile coordinates within image 0.
func (s *Slide) RawTile(level, tx, ty int) ([]byte, error) {
	return s.r.ImageRawTile(0, level, tx, ty)
}

// RawTileInto fills dst with the compressed tile bytes at the given level
// and tile coordinates within image 0. Returns the byte count written.
func (s *Slide) RawTileInto(level, tx, ty int, dst []byte) (int, error) {
	return s.r.ImageRawTileInto(0, level, tx, ty, dst)
}

// TileMaxSize returns the upper bound on tile byte size at the given
// level within image 0. Used for sizing caller-provided destination
// buffers passed to RawTileInto.
func (s *Slide) TileMaxSize(level int) int {
	return s.r.ImageTileMaxSize(0, level)
}

// ImageRawTile returns the compressed tile bytes at the given image,
// level, and tile coordinates. Required for multi-image formats
// (OME-TIFF); single-image formats accept only image=0.
func (s *Slide) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	return s.r.ImageRawTile(image, level, tx, ty)
}

// ImageRawTileInto fills dst with the compressed tile bytes at the
// given image, level, and tile coordinates. Returns the byte count written.
func (s *Slide) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	return s.r.ImageRawTileInto(image, level, tx, ty, dst)
}

// ImageWarmLevel pre-warms the page cache for the given image + level.
func (s *Slide) ImageWarmLevel(image, level int) error {
	return s.r.WarmLevel(image, level)
}

// TilePrefix returns the JPEG-tables prefix bytes shared across all
// tiles at the given level within image 0. nil if the codec has no prefix.
func (s *Slide) TilePrefix(level int) []byte {
	return s.r.ImageTilePrefix(0, level)
}

// ImageTilePrefix is the multi-image variant of TilePrefix.
func (s *Slide) ImageTilePrefix(image, level int) []byte {
	return s.r.ImageTilePrefix(image, level)
}

// ImageTileMaxSize is the multi-image variant of TileMaxSize.
func (s *Slide) ImageTileMaxSize(image, level int) int {
	return s.r.ImageTileMaxSize(image, level)
}

// TileBodyMaxSize returns the upper bound on tile-body (no prefix) bytes
// at the given level within image 0.
func (s *Slide) TileBodyMaxSize(level int) int {
	return s.r.ImageTileBodyMaxSize(0, level)
}

// ImageTileBodyMaxSize is the multi-image variant of TileBodyMaxSize.
func (s *Slide) ImageTileBodyMaxSize(image, level int) int {
	return s.r.ImageTileBodyMaxSize(image, level)
}

// TileBodyInto fills dst with the tile body bytes at the given level and
// tile coordinates within image 0.
func (s *Slide) TileBodyInto(level, tx, ty int, dst []byte) (int, error) {
	return s.r.ImageTileBodyInto(0, level, tx, ty, dst)
}

// ImageTileBodyInto is the multi-image variant of TileBodyInto.
func (s *Slide) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return s.r.ImageTileBodyInto(image, level, tx, ty, dst)
}

// TileReader returns a streaming io.ReadCloser for the compressed tile
// at the given level and tile coordinates within image 0.
func (s *Slide) TileReader(level, tx, ty int) (io.ReadCloser, error) {
	return s.r.ImageTileReader(0, level, tx, ty)
}

// ImageTileReader is the multi-image variant of TileReader.
func (s *Slide) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	return s.r.ImageTileReader(image, level, tx, ty)
}

// RangeTiles returns a range-over-function iterator over all tiles in
// the given level within image 0.
func (s *Slide) RangeTiles(ctx context.Context, level int) iter.Seq2[TilePos, TileResult] {
	return s.r.ImageRangeTiles(ctx, 0, level)
}

// ImageRangeTiles is the multi-image variant of RangeTiles.
func (s *Slide) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[TilePos, TileResult] {
	return s.r.ImageRangeTiles(ctx, image, level)
}
