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
	Pyramids() []Pyramid
	Level(image, level int) (Level, error)
	AssociatedImages() []AssociatedImage
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
	ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[Point, TileResult]

	Close() error
}

// Slide is the canonical handle for an open whole-slide image.
// Constructed via OpenFile (path) or Open (io.ReaderAt). Replaces the
// pre-v0.23 Tiler interface as the public return type of Open and
// OpenFile.
//
// Concurrency contract: all accessor methods (Format, Pyramids, Levels,
// Level, AssociatedImages, Metadata, ICCProfile) are safe to call concurrently.
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

	// v1.0: lazily-materialized navigation cache backing the
	// receiver-method read API. Populated exactly once by
	// ensurePyramids (sync.Once), then read-only — so navigation
	// (Pyramids/Pyramid/Levels/Level and the *Pyramid/*Level methods)
	// returns stable pointers into this slice across calls, satisfying
	// both pointer-identity and the lock-free hot-path contract.
	pyramidsOnce sync.Once
	pyramids     []Pyramid
}

// ensurePyramids materializes the navigation cache exactly once. After
// the sync.Once fires, s.pyramids is immutable and every Level/Pyramid
// carries its back-reference to s, so the receiver-method read API can
// delegate. PyramidIndex / Index are normalized from loop indices when a
// reader leaves them zero, guaranteeing l.PyramidIndex + l.Index are the
// correct delegation coordinates.
func (s *Slide) ensurePyramids() {
	s.pyramidsOnce.Do(func() {
		s.pyramids = s.r.Pyramids()
		for pi := range s.pyramids {
			p := &s.pyramids[pi]
			p.slide = s
			// Normalize the pyramid index from document order if the
			// reader didn't set it (only pyramid 0 is ambiguous).
			if p.Index == 0 {
				p.Index = pi
			}
			for li := range p.Levels {
				l := &p.Levels[li]
				l.slide = s
				// Normalize level index + pyramid index from loop
				// position when the reader left them at the zero value.
				if l.Index == 0 {
					l.Index = li
				}
				l.PyramidIndex = p.Index
			}
		}
	})
}

// Format returns the canonical format identifier.
func (s *Slide) Format() Format { return s.r.Format() }

// Pyramids returns the main pyramids carried by this file. Always
// returns at least one Pyramid; multi-image OME-TIFF exposes multiple.
// Index 0 is the legacy Levels() / Level(i) shortcut target.
//
// v1.0: returns []*Pyramid — stable pointers into the Slide's internal
// navigation cache. The same *Pyramid is returned across calls (pointer
// identity is preserved), and each *Pyramid / *Level carries the
// receiver-method read API.
func (s *Slide) Pyramids() []*Pyramid {
	s.ensurePyramids()
	out := make([]*Pyramid, len(s.pyramids))
	for i := range s.pyramids {
		out[i] = &s.pyramids[i]
	}
	return out
}

// Pyramid returns the i-th pyramid, or nil if i is out of range.
//
// v1.0: returns a stable *Pyramid into the navigation cache.
func (s *Slide) Pyramid(i int) *Pyramid {
	s.ensurePyramids()
	if i < 0 || i >= len(s.pyramids) {
		return nil
	}
	return &s.pyramids[i]
}

// Levels is a shortcut for s.Pyramid(0).Levels(). Returns nil if the
// slide carries no pyramids.
//
// v1.0: returns []*Level — stable pointers into the navigation cache.
func (s *Slide) Levels() []*Level {
	s.ensurePyramids()
	if len(s.pyramids) == 0 {
		return nil
	}
	levels := s.pyramids[0].Levels
	out := make([]*Level, len(levels))
	for i := range levels {
		out[i] = &levels[i]
	}
	return out
}

// Level is a shortcut for s.Pyramid(0).Level(i). Returns
// ErrLevelOutOfRange if i is out of range (or the slide carries no
// pyramids).
//
// v1.0: returns a stable *Level into the navigation cache.
func (s *Slide) Level(i int) (*Level, error) {
	s.ensurePyramids()
	if len(s.pyramids) == 0 {
		return nil, ErrLevelOutOfRange
	}
	levels := s.pyramids[0].Levels
	if i < 0 || i >= len(levels) {
		return nil, ErrLevelOutOfRange
	}
	return &levels[i], nil
}

// AssociatedImages returns the auxiliary images (label, macro, thumbnail,
// overview, ...) embedded in this slide.
func (s *Slide) AssociatedImages() []AssociatedImage { return s.r.AssociatedImages() }

// Metadata returns the cross-format metadata view.
func (s *Slide) Metadata() Metadata { return s.r.Metadata() }

// ICCProfile returns the embedded color profile bytes, or nil if the
// slide has none.
func (s *Slide) ICCProfile() []byte { return s.r.ICCProfile() }

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
