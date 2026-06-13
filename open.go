package opentile

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/fastpath"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// openAnyHook is set by internal/format's bridge to avoid an import cycle.
// internal/format imports opentile; opentile cannot import internal/format.
// Returns an any value that satisfies slideReader (a format.Reader).
// dispatchOpen type-asserts the result to slideReader.
var openAnyHook func(
	r io.ReaderAt,
	size int64,
	tileSize Size,
	hasTileSize bool,
	corruptTilePolicy CorruptTilePolicy,
	ndpiSynthLabel bool,
	backing Backing,
) (any, error)

// dicomPathOpenHook is set by formats/dicom's init() via SetDICOMPathOpenHook
// to avoid an import cycle: formats/dicom imports opentile; opentile cannot
// import formats/dicom. The hook receives the path and returns a slideReader
// wrapped in an any (type-asserted in OpenFile). Returns ErrUnsupportedFormat
// if the path is not a DICOM series; OpenFile falls through to normal dispatch.
var dicomPathOpenHook func(path string) (any, error)

// SetDICOMPathOpenHook registers the DICOM path-open function. Called once
// from formats/dicom's init() via factory.go. The hook receives a path (file
// or directory) and returns a slideReader (as any) or an error. If it returns
// ErrUnsupportedFormat the path is not DICOM and OpenFile falls through to
// the normal single-file dispatch. Not safe for concurrent use during setup.
//
// This indirection avoids an import cycle: formats/dicom imports the root
// opentile package (for Level, Image, etc.); the root cannot import
// formats/dicom. The same pattern is used by internal/format (SetOpenAnyHook).
func SetDICOMPathOpenHook(fn func(path string) (any, error)) {
	dicomPathOpenHook = fn
}

// SetOpenAnyHook registers the format dispatch function. Called once from
// internal/format's init() via a bridge file. Must be called before any
// Open/OpenFile call. Not safe for concurrent use during setup.
func SetOpenAnyHook(fn func(
	r io.ReaderAt,
	size int64,
	tileSize Size,
	hasTileSize bool,
	corruptTilePolicy CorruptTilePolicy,
	ndpiSynthLabel bool,
	backing Backing,
) (any, error)) {
	openAnyHook = fn
}

func dispatchOpen(r io.ReaderAt, size int64, cfg *config) (slideReader, error) {
	if openAnyHook == nil {
		return nil, fmt.Errorf("opentile: no format registered — import a formats package (e.g. _ \"github.com/wsilabs/opentile-go/formats/all\")")
	}
	result, err := openAnyHook(r, size, cfg.tileSize, cfg.hasTileSize, cfg.corruptTile, cfg.ndpiSynthLabel, cfg.backing)
	if err != nil {
		return nil, err
	}
	sr, ok := result.(slideReader)
	if !ok {
		return nil, fmt.Errorf("opentile: internal error: format returned unexpected type %T", result)
	}
	return sr, nil
}

// Open parses r as a WSI file and returns a *Slide for the matching format.
// size is the total file size in bytes.
//
// Dispatch probes each registered format in registration order; the first
// whose match function returns nil wins. Returns an error if no format
// claims the input (requires a format package to be imported, e.g.
// _ "github.com/wsilabs/opentile-go/formats/all").
func Open(r io.ReaderAt, size int64, opts ...Option) (*Slide, error) {
	cfg := newConfig(opts)
	rdr, err := dispatchOpen(r, size, cfg)
	if err != nil {
		return nil, err
	}
	return &Slide{r: rdr, readBudget: cfg.resolveMemoryBudget()}, nil
}

// OpenFile opens path for reading and delegates to [Open]. The
// returned [*Slide] owns the underlying file handle (or memory map);
// Close releases it.
//
// # DICOM series (Contract 1 & 2)
//
// DICOM VL Whole Slide Microscopy is the only format reachable via
// OpenFile but NOT via Open(io.ReaderAt, size). A DICOM series is
// multi-file; [Open] cannot express it. When the formats/dicom package
// is imported (e.g. via _ "github.com/wsilabs/opentile-go/formats/all"),
// OpenFile detects DICOM before the normal single-file dispatch:
//
//   - Directory path: opens all WSM .dcm files in the directory.
//   - Single .dcm path (Contract 2): performs a bounded sibling-scan —
//     same directory only, same SeriesUID only, WSM-filtered — so that
//     passing any one instance opens the full series.
//
// The [WithBacking] option is accepted but ignored for DICOM (the Tiler
// owns its own per-instance mmaps).
//
// Default backing since v0.9 is [BackingMmap]: the file is
// memory-mapped read-only and tile reads become userspace memcpys
// from the mapped region — no pread(2) syscall per [Level.Tile]
// call. The kernel page cache handles paging in tile-data regions
// on first access; warm-cache reads hit RAM at memory-bandwidth
// speed.
//
// Pass [WithBacking](BackingPread) to opt out and use the v0.8 (and
// earlier) os.File + pread path. Required for filesystems that
// don't support mmap (some FUSE / network mounts) or when the
// caller specifically needs os.File truncation semantics.
//
// Failure modes:
//   - mmap unavailable for this file (filesystem doesn't support it,
//     or some platform-specific failure): returns
//     [ErrMmapUnavailable] wrapping the underlying error. Callers
//     wanting automatic fallback should retry with
//     WithBacking(BackingPread).
//   - file truncated underneath an open mmap-backed *Slide: subsequent
//     Tile() calls into the truncated region raise SIGBUS in the
//     calling thread. WSI files don't get truncated under normal
//     use; if your storage allows it, use BackingPread.
func OpenFile(path string, opts ...Option) (*Slide, error) {
	cfg := newConfig(opts)

	// DICOM path-aware branch — must come before the single-file dispatch
	// because DICOM is multi-file (a directory or a single instance that
	// expands to its sibling series). The hook is nil when formats/dicom is
	// not imported. ErrUnsupportedFormat from the hook means "not DICOM —
	// fall through to normal dispatch."
	if dicomPathOpenHook != nil {
		result, err := dicomPathOpenHook(path)
		if err == nil {
			sr, ok := result.(slideReader)
			if !ok {
				return nil, fmt.Errorf("opentile: dicom hook returned unexpected type %T", result)
			}
			return &Slide{r: sr, readBudget: cfg.resolveMemoryBudget()}, nil
		}
		if !errors.Is(err, ErrUnsupportedFormat) {
			return nil, err
		}
		// ErrUnsupportedFormat: not DICOM — fall through to normal dispatch.
	}

	switch cfg.backing {
	case BackingMmap:
		return openFileMmap(path, opts)
	case BackingPread:
		return openFilePread(path, opts)
	default:
		return nil, fmt.Errorf("opentile: unknown backing %d", cfg.backing)
	}
}

// openFilePread is the v0.8 (and earlier) os.File + pread(2) path.
// Active when WithBacking(BackingPread) is passed.
func openFilePread(path string, opts []Option) (*Slide, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opentile: open %q: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("opentile: stat %q: %w", path, err)
	}
	cfg := newConfig(opts)
	rdr, err := dispatchOpen(f, info.Size(), cfg)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("opentile: %s: %w", path, err)
	}
	fc := &fileCloser{slideReader: rdr, f: f}
	return &Slide{r: fc, readBudget: cfg.resolveMemoryBudget()}, nil
}

// openFileMmap is the v0.9 default path. Memory-maps the file and
// passes the resulting *tiff.MmapFile (which implements io.ReaderAt
// + io.Closer) to dispatchOpen. The returned *Slide owns the mapping;
// Close unmaps and releases the underlying file.
func openFileMmap(path string, opts []Option) (*Slide, error) {
	m, err := tiff.OpenMmap(path)
	if err != nil {
		return nil, fmt.Errorf("opentile: %s: %w: %v", path, ErrMmapUnavailable, err)
	}
	cfg := newConfig(opts)
	rdr, err := dispatchOpen(m, m.Size(), cfg)
	if err != nil {
		m.Close()
		return nil, fmt.Errorf("opentile: %s: %w", path, err)
	}
	mc := &mmapCloser{slideReader: rdr, m: m}
	return &Slide{r: mc, readBudget: cfg.resolveMemoryBudget()}, nil
}

// fileCloser wraps a slideReader and closes the underlying os.File on Close.
type fileCloser struct {
	slideReader
	f *os.File
}

func (fc *fileCloser) Close() error {
	return errors.Join(fc.slideReader.Close(), fc.f.Close())
}

// UnwrapReader exposes the inner reader so format-specific MetadataOf
// helpers can walk through wrapper chains.
func (fc *fileCloser) UnwrapReader() any { return fc.slideReader }

// ImageDecodedTile delegates to the wrapped reader's fast-path method
// if it implements decodedTiler; otherwise returns fastpath.ErrUnsupported
// so the dispatcher in imageDecodedTile falls through to the slow
// path. Without this delegation, the type assertion s.r.(decodedTiler)
// would fail on the wrapper even when the wrapped reader supports the
// fast path. Added in v0.27.
func (fc *fileCloser) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if dr, ok := fc.slideReader.(decodedTiler); ok {
		return dr.ImageDecodedTile(image, level, tx, ty, opts)
	}
	return nil, fastpath.ErrUnsupported
}

// mmapCloser wraps a slideReader and releases the mmap on Close.
type mmapCloser struct {
	slideReader
	m *tiff.MmapFile
}

func (mc *mmapCloser) Close() error {
	return errors.Join(mc.slideReader.Close(), mc.m.Close())
}

// UnwrapReader exposes the inner reader so format-specific MetadataOf
// helpers can walk through wrapper chains.
func (mc *mmapCloser) UnwrapReader() any { return mc.slideReader }

// ImageDecodedTile delegates to the wrapped reader's fast-path method
// if it implements decodedTiler; otherwise returns fastpath.ErrUnsupported
// so the dispatcher in imageDecodedTile falls through to the slow
// path. Without this delegation, the type assertion s.r.(decodedTiler)
// would fail on the wrapper even when the wrapped reader supports the
// fast path. Added in v0.27.
func (mc *mmapCloser) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if dr, ok := mc.slideReader.(decodedTiler); ok {
		return dr.ImageDecodedTile(image, level, tx, ty, opts)
	}
	return nil, fastpath.ErrUnsupported
}
