package opentile

import (
	"math"
	"os"
	"runtime/debug"
	"strconv"
)

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

// defaultReadMemoryBudget is the default per-Slide live-memory target
// for the ScaledStrips read path (the decoded-tile cache, C1). ~2 GB
// peak under GOGC=100. Override with WithMemoryBudget or the
// OPENTILE_READ_MEMORY_BUDGET env var (bytes).
const defaultReadMemoryBudget int64 = 1 << 30 // 1 GiB

// minReadMemoryBudget is the floor below which budget derivation never
// goes (roughly one worker's worth of in-flight tiles plus a strip
// buffer). Keeps GOMEMLIMIT-driven shrinking from starving the cache.
const minReadMemoryBudget int64 = 128 << 20 // 128 MiB

// Option mutates the opentile configuration before Open returns a Tiler.
type Option func(*config)

// config is the aggregate of all Option values applied at Open time.
type config struct {
	tileSize        Size
	hasTileSize     bool
	corruptTile     CorruptTilePolicy
	ndpiSynthLabel  bool // default true
	backing         Backing
	hasBacking      bool
	memoryBudget    int64
	hasMemoryBudget bool
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

// WithMemoryBudget sets the per-Slide live-memory budget (bytes) for
// the ScaledStrips read path. Governs the decoded-tile cache so peak
// memory stays flat regardless of slide width / DZI tile size. Default
// 1 GiB; also settable via OPENTILE_READ_MEMORY_BUDGET (option wins).
// Values < 1 are ignored (default used).
func WithMemoryBudget(bytes int64) Option {
	return func(c *config) {
		if bytes >= 1 {
			c.memoryBudget = bytes
			c.hasMemoryBudget = true
		}
	}
}

// resolveMemoryBudget returns the effective budget: option > env >
// default. A non-positive or unparseable env value is ignored.
func (c *config) resolveMemoryBudget() int64 {
	if c.hasMemoryBudget {
		return c.memoryBudget
	}
	if v := os.Getenv("OPENTILE_READ_MEMORY_BUDGET"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 1 {
			return n
		}
	}
	// No explicit option/env budget: start from the default, but if the
	// process has a GOMEMLIMIT set, shrink to <= half of it so our live
	// set + GC headroom + the NDPI caches + the caller's own buffers fit
	// under the runtime ceiling. SetMemoryLimit(-1) reads the current
	// limit without changing it; math.MaxInt64 means "unset".
	budget := defaultReadMemoryBudget
	if limit := debug.SetMemoryLimit(-1); limit != math.MaxInt64 {
		if half := limit / 2; half < budget {
			budget = half
		}
	}
	if budget < minReadMemoryBudget {
		budget = minReadMemoryBudget
	}
	return budget
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
// Deprecated: use opentile/opentiletest.NewConfig. Retained for
// compatibility; slated for removal in the coordinated v1.0 API pass.
func NewTestConfig(tileSize Size, policy CorruptTilePolicy) *Config {
	has := tileSize.W != 0 || tileSize.H != 0
	return &Config{c: &config{tileSize: tileSize, hasTileSize: has, corruptTile: policy}}
}
