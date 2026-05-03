package opentile

// Format identifies the source file format.
type Format string

const (
	FormatSVS     Format = "svs"
	FormatNDPI    Format = "ndpi"
	FormatPhilips Format = "philips"
	FormatOME     Format = "ome"
	FormatBIF     Format = "bif"
	FormatIFE     Format = "ife"
	// FormatGeneric is the catch-all reader for tiled pyramidal TIFF
	// without vendor metadata (added in v0.10). Activates when no
	// vendor format factory claims the file. Reports as "generic-tiff"
	// to differentiate from possible future generic-non-TIFF readers.
	FormatGeneric Format = "generic-tiff"
)

// Tiler is the top-level handle returned by [Open] / [OpenFile].
//
// Concurrency contract:
//
//   - All accessor methods (Format, Images, Levels, Level, Associated,
//     Metadata, ICCProfile) are safe to call concurrently from any
//     number of goroutines after [Open]. They return immutable views
//     of state populated at Open time.
//
//   - Tile reads via [Level.Tile] and [Level.TileInto] are safe to
//     call concurrently. SVS / Philips / OME tiled / BIF / IFE have
//     no internal locks on the tile hot path — concurrency is bounded
//     only by the OS page cache (under [BackingMmap], the v0.9 default)
//     or by file-descriptor syscall throughput (under [BackingPread]).
//     NDPI's striped reader takes a per-page mutex on the assembled-
//     frame cache: concurrent reads of *different* pages run in
//     parallel; concurrent reads of the *same* page serialize.
//     OME OneFrame uses a similar per-level extended-frame cache.
//
//   - Bytes returned by Tile() are caller-owned: opentile-go does not
//     retain a reference, and callers may modify them (typical pattern
//     is to write them straight to a network response). Bytes written
//     by TileInto into the caller-provided dst remain caller-owned.
//
//   - WarmLevel is a hint — safe under any concurrency, returns an
//     error only if the underlying I/O fails or if i is out of bounds.
//
//   - Close() must not race with in-flight tile reads. Under
//     [BackingMmap] this is non-negotiable: closing unmaps the file,
//     and subsequent reads through the mapping raise SIGBUS. Callers
//     that need lifecycle management beyond a synchronous read loop
//     should hold a reader lock around tile reads or sequence Close
//     after a wait group on outstanding readers.
type Tiler interface {
	Format() Format
	// Images returns the main pyramids carried by this file. Always
	// returns at least one Image; multi-image OME TIFF files expose
	// multiple. Index 0 corresponds to the legacy Levels() / Level(i)
	// shortcut accessors.
	//
	// Added in v0.6. Single-image formats (SVS, NDPI, Philips) return a
	// one-element slice wrapping their existing pyramid.
	Images() []Image
	// Levels is a shortcut for Images()[0].Levels(). Preserved from
	// pre-v0.6 callers; behaves identically on single-image formats.
	Levels() []Level
	// Level is a shortcut for Images()[0].Level(i).
	Level(i int) (Level, error)
	Associated() []AssociatedImage
	Metadata() Metadata
	ICCProfile() []byte

	// WarmLevel pre-warms the page cache for level i by touching one
	// byte per OS page covering this level's tile-data ranges. Under
	// mmap-backed [OpenFile] (the v0.9 default), this forces the
	// kernel to populate the page cache lazily on first call —
	// subsequent [Level.Tile] / [Level.TileInto] reads on level i hit
	// RAM at memory-bandwidth speed regardless of access pattern.
	//
	// Under [BackingPread], WarmLevel does effectively the same work
	// via a pread(1) per page. Slower, but the warm-up effect
	// (kernel page cache population) is the same.
	//
	// Returns [ErrLevelOutOfRange] if i is out of bounds. Returns
	// the first non-EOF read error encountered while touching pages
	// (typically I/O errors on the underlying file). nil on success.
	//
	// Best-effort: callers that want to ignore errors (it's a hint,
	// after all) can discard the result. Concurrent calls on
	// different levels are safe; concurrent calls on the same level
	// are safe but redundant.
	//
	// Added in v0.9 alongside the mmap-default [OpenFile] change.
	WarmLevel(i int) error

	Close() error
}
