package opentile

// CorruptTilePolicy controls how corrupt-edge tiles (currently: Aperio SVS) are
// reported. v0.1 supports only CorruptTileError.
type CorruptTilePolicy uint8

const (
	CorruptTileError CorruptTilePolicy = iota // return ErrCorruptTile (default, v0.1)
	CorruptTileBlank                          // v0.3: return a typed blank tile
	CorruptTileFix                            // v1.0: reconstruct from parent level
)

// Backing identifies the I/O backend used to read tile bytes from a
// slide file. Selectable via [WithBacking]; defaults to [BackingMmap]
// since v0.9.
//
// BackingMmap memory-maps the file read-only and reads tiles via
// userspace memcpy from the mapped region. No syscall per Tile()
// call once the kernel has paged in the relevant region. Recommended
// for high-RPS serving and warm-cache desktop use. Caveat: SIGBUS
// on file truncation; not suitable for storage that gets rewritten
// underneath open Tilers.
//
// BackingPread keeps the v0.8 (and earlier) [os.File]-based path:
// pread(2) syscall per [Level.Tile] call. Use this on filesystems
// that don't support mmap (some FUSE / network mounts), or when
// you specifically need the os.File semantics around truncation.
type Backing uint8

const (
	// BackingMmap memory-maps the slide file. Default since v0.9.
	BackingMmap Backing = iota
	// BackingPread uses os.File + pread(2) per Tile().
	BackingPread
)

// Option mutates the opentile configuration before Open returns a Tiler.
type Option func(*config)

// config is the aggregate of all Option values applied at Open time.
type config struct {
	tileSize       Size
	hasTileSize    bool
	corruptTile    CorruptTilePolicy
	ndpiSynthLabel bool // default true
	backing        Backing
	hasBacking     bool
}

func newConfig(opts []Option) *config {
	c := &config{
		tileSize:       Size{},
		corruptTile:    CorruptTileError,
		ndpiSynthLabel: true, // v0.2 behavior; opt-out via WithNDPISynthesizedLabel(false)
		backing:        BackingMmap,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithTileSize requests output tile dimensions in pixels. If unset, the format
// default is used (SVS: native tile size from the TIFF). Required for formats
// that have no native rectangular tiles (NDPI, v0.2+).
func WithTileSize(w, h int) Option {
	return func(c *config) {
		c.tileSize = Size{W: w, H: h}
		c.hasTileSize = true
	}
}

// WithCorruptTilePolicy sets the behavior for corrupt-edge tiles. v0.1 supports
// only CorruptTileError.
func WithCorruptTilePolicy(p CorruptTilePolicy) Option {
	return func(c *config) { c.corruptTile = p }
}

// WithNDPISynthesizedLabel controls whether NDPI Tiler.Associated() includes
// a synthesized "label" image, which Go produces by cropping the left 30%
// of the overview page. Python opentile 0.20.0 does not expose NDPI labels;
// this is a Go-side extension. Default: true (matches v0.2 behavior).
func WithNDPISynthesizedLabel(enable bool) Option {
	return func(c *config) {
		c.ndpiSynthLabel = enable
	}
}

// WithBacking selects the I/O backend used by [OpenFile]. The default
// since v0.9 is [BackingMmap]; pass WithBacking(BackingPread) on the
// rare filesystem that doesn't support mmap or when you need os.File
// truncation semantics.
//
// Has no effect on [Open] (which already takes a caller-provided
// [io.ReaderAt]); only the path-resolving [OpenFile] honors this.
//
// When set to BackingMmap and the underlying mmap call fails (FUSE
// mount that doesn't support mapping, etc.), OpenFile returns
// [ErrMmapUnavailable] wrapping the underlying error rather than
// silently falling back. Callers that want auto-fallback should
// retry with WithBacking(BackingPread).
func WithBacking(b Backing) Option {
	return func(c *config) {
		c.backing = b
		c.hasBacking = true
	}
}

// Config is an opaque, read-only view of the configuration passed to a
// FormatFactory. Format packages import opentile.Config rather than the
// unexported config struct.
type Config struct {
	c *config
}

// TileSize returns the requested output tile size and whether the caller
// set one.
//
//   - (Size{}, false): caller did not pass WithTileSize. Format packages
//     should use their format default (e.g. SVS reads the native tile size
//     from the TIFF; NDPI uses 512).
//   - (Size{}, true): caller explicitly passed WithTileSize(0, 0). Format
//     packages MUST reject this as malformed input. The zero Size is
//     distinct from "unset" because the API contract is that an explicit
//     option overrides the default.
//   - (non-zero, true): caller's requested tile size; format honors it
//     (NDPI may snap to a stripe-multiple, SVS rejects when it doesn't
//     match the native tile dimensions).
func (c *Config) TileSize() (Size, bool) {
	if c == nil || c.c == nil {
		return Size{}, false
	}
	return c.c.tileSize, c.c.hasTileSize
}

// CorruptTilePolicy returns the configured policy.
func (c *Config) CorruptTilePolicy() CorruptTilePolicy {
	if c == nil || c.c == nil {
		return CorruptTilePolicy(0)
	}
	return c.c.corruptTile
}

// NDPISynthesizedLabel reports whether NDPI Tiler.Associated() should
// include a synthesized label cropped from the overview. Default true.
func (c *Config) NDPISynthesizedLabel() bool {
	if c == nil || c.c == nil {
		return false
	}
	return c.c.ndpiSynthLabel
}

// Backing reports the I/O backing the caller selected via
// [WithBacking]. Defaults to [BackingMmap] since v0.9 if no option
// was passed. Format packages typically don't need this — Open is
// path-agnostic — but it's exposed for diagnostic accessors.
func (c *Config) Backing() Backing {
	if c == nil || c.c == nil {
		return BackingMmap
	}
	return c.c.backing
}

// NewTestConfig constructs a Config for use in tests.
//
// Deprecated: use opentile/opentiletest.NewConfig. This wrapper remains
// for one release to keep external callers compiling; it will be removed
// in v0.4.
func NewTestConfig(tileSize Size, policy CorruptTilePolicy) *Config {
	has := tileSize.W != 0 || tileSize.H != 0
	return &Config{c: &config{tileSize: tileSize, hasTileSize: has, corruptTile: policy}}
}
