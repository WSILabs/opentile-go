package opentile

// slideReader is an unexported interface with the same method set as
// format.Reader. Slide uses this type for its field so that opentile
// does not import internal/format (which imports opentile, creating a
// cycle). Every format.Reader automatically satisfies slideReader
// because the method sets are identical.
type slideReader interface {
	Format() Format
	Images() []Image
	Levels() []Level
	Level(i int) (Level, error)
	Associated() []AssociatedImage
	Metadata() Metadata
	ICCProfile() []byte
	WarmLevel(i int) error
	Close() error
}

// Slide is the canonical handle for an open whole-slide image.
// Constructed via OpenFile (path) or Open (io.ReaderAt). Replaces the
// pre-v0.23 Tiler interface as the public return type of Open and
// OpenFile.
//
// Concurrency contract: see Tiler for per-method threading promises.
// Slide preserves them verbatim; it is a thin forwarder over the
// format-specific reader.
type Slide struct {
	r slideReader
}

// Format returns the canonical format identifier.
func (s *Slide) Format() Format { return s.r.Format() }

// Images returns the main pyramids carried by this file. Always
// returns at least one Image; multi-image OME-TIFF exposes multiple.
// Index 0 is the legacy Levels() / Level(i) shortcut target.
func (s *Slide) Images() []Image { return s.r.Images() }

// Levels is a shortcut for s.Images()[0].Levels().
func (s *Slide) Levels() []Level { return s.r.Levels() }

// Level is a shortcut for s.Images()[0].Level(i).
func (s *Slide) Level(i int) (Level, error) { return s.r.Level(i) }

// Associated returns the auxiliary images (label, macro, thumbnail,
// overview, ...) embedded in this slide.
func (s *Slide) Associated() []AssociatedImage { return s.r.Associated() }

// Metadata returns the cross-format metadata view.
func (s *Slide) Metadata() Metadata { return s.r.Metadata() }

// ICCProfile returns the embedded color profile bytes, or nil if the
// slide has none.
func (s *Slide) ICCProfile() []byte { return s.r.ICCProfile() }

// WarmLevel pre-warms the page cache for level i. Hint-only; see
// Tiler.WarmLevel for semantics.
func (s *Slide) WarmLevel(i int) error { return s.r.WarmLevel(i) }

// Close releases resources held by the slide.
func (s *Slide) Close() error { return s.r.Close() }

// tilerAsReader converts an opentile.Tiler to a slideReader. Used
// during the v0.23 transition where the old factory dispatch returns
// Tiler but Slide wraps slideReader. Method sets are identical so
// the type assertion always succeeds for every format package that has
// been dual-registered in Phase 2.
func tilerAsReader(t Tiler) slideReader {
	return t
}

