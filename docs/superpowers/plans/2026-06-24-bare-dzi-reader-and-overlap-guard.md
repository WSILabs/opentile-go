# Bare DZI Reader + Overlap>0 Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a filesystem-backed bare Deep Zoom Image reader (`formats/dzi`, `Overlap=0`, 12th format) opened via a DICOM-style path hook, and a shared `internal/dzi.ErrOverlapNotSupported` guard that makes both DZI and SZI reject `Overlap>0` loudly instead of silently mis-rendering.

**Architecture:** The bare-DZI reader ports `formats/szi`'s `Tiler`+`level` with a filesystem tile source (`os.ReadFile` from `<dir>/<base>_files/<level>/<col>_<row>.<ext>`) instead of ZIP entries, dropping ZIP/scan-properties/associated-image machinery. It reuses `internal/dzi` for all pyramid math (`MaxLevel`/`LevelDims`/`GridDims`/`TilePath`/`ParseManifest`). Opening mirrors DICOM: a `dziPathOpenHook` consulted in `OpenFile` before single-file dispatch, accepting a `.dzi` file path or a directory containing exactly one `.dzi`. `Open(io.ReaderAt)` doesn't support bare DZI (no path). The `Overlap>0` guard is a shared sentinel applied right after `ParseManifest` in both readers.

**Tech Stack:** Go 1.23+, root `opentile` package, `internal/dzi` (pure manifest/coord math), `internal/format` (compile-time `format.Reader` assertion), stdlib `os`/`path/filepath`/`image/jpeg` (test tiles).

**Reference (read before starting):**
- `formats/szi/tiler.go`, `formats/szi/level.go` — the reader being ported (FS source instead of ZIP; no scan-properties/associated).
- `internal/dzi/manifest.go`, `internal/dzi/coords.go` — `ParseManifest`, `MaxLevel(w,h)`, `LevelDims(w,h,level)`, `GridDims(w,h,tileSize)`, `TilePath(rootDir, level, col, row, format)`.
- `open.go:28-45,136-167` — the `dicomPathOpenHook` var/setter/consult pattern to mirror.
- `formats/dicom/factory.go:7-8` — `opentile.SetDICOMPathOpenHook(openForHook)` install pattern.
- `format.go:1-51` — the `Format` enum to extend.
- Design spec: `docs/superpowers/specs/2026-06-24-bare-dzi-reader-and-overlap-guard-design.md`.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `internal/dzi/errors.go` | `ErrOverlapNotSupported` sentinel (shared by both readers). | Create |
| `internal/dzi/errors_test.go` | Sentinel is non-nil + stable message. | Create |
| `formats/szi/tiler.go` | Apply the `Overlap>0` guard after `ParseManifest`. | Modify `loadManifest` |
| `formats/szi/factory_test.go` | SZI `Overlap=1` → `ErrOverlapNotSupported`; `Overlap=0` still opens. | Modify (append) |
| `format.go` | `FormatDZI = "dzi"`. | Modify |
| `open.go` | `dziPathOpenHook` var + `SetDZIPathOpenHook` + `OpenFile` consult. | Modify |
| `formats/dzi/doc.go` | Package doc. | Create |
| `formats/dzi/level.go` | `level` engine: FS tile fetch (`Tile`/`TileInto`/`TileReader`/`Tiles`/`TileMaxSize`). | Create |
| `formats/dzi/tiler.go` | `Tiler`: manifest+guard, `buildLevels` (reuse `internal/dzi`), slideReader method set. | Create |
| `formats/dzi/factory.go` | `init()` installs `dziPathOpenHook`; `openForHook` path detection. | Create |
| `formats/dzi/level_test.go` | (package `dzi`, internal) level engine reads a tile file; OOB; missing file. | Create |
| `formats/dzi/dzi_test.go` | (package `dzi_test`, external) synthetic temp-dir DZI via `opentile.OpenFile`: open (file + dir), levels/Size/Grid, Tile bytes, DecodedTile dims, OOB, missing tile, Overlap guard, empty-dir fall-through. | Create |
| `formats/all/all.go` | Blank-import `formats/dzi` (for its hook-install `init`). | Modify |
| `docs/formats/dzi.md` | Format doc. | Create |
| `README.md`, `docs/deferred.md`, `CHANGELOG.md` | Format list; retire R19; changelog. | Modify |

---

## Task 1: Shared Overlap>0 guard + apply to SZI

**Files:**
- Create: `internal/dzi/errors.go`, `internal/dzi/errors_test.go`
- Modify: `formats/szi/tiler.go` (`loadManifest`)
- Test: `formats/szi/factory_test.go`

- [ ] **Step 1: Write the sentinel test**

Create `internal/dzi/errors_test.go`:

```go
package dzi

import "testing"

func TestErrOverlapNotSupported(t *testing.T) {
	if ErrOverlapNotSupported == nil {
		t.Fatal("ErrOverlapNotSupported must be a non-nil sentinel")
	}
	if got := ErrOverlapNotSupported.Error(); got == "" {
		t.Fatal("ErrOverlapNotSupported must have a message")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/dzi/ -run TestErrOverlapNotSupported -count=1`
Expected: FAIL — `undefined: ErrOverlapNotSupported`.

- [ ] **Step 3: Create the sentinel**

Create `internal/dzi/errors.go`:

```go
package dzi

import "errors"

// ErrOverlapNotSupported is returned at open time when a DZI manifest declares
// Overlap > 0. Only Overlap=0 is implemented; tile-border cropping for
// Overlap > 0 is deferred. Both formats/dzi and formats/szi enforce this so an
// Overlap>0 slide fails loudly instead of being silently mis-placed.
var ErrOverlapNotSupported = errors.New("dzi: tile overlap > 0 not supported")
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/dzi/ -run TestErrOverlapNotSupported -count=1`
Expected: PASS.

- [ ] **Step 5: Apply the guard in SZI**

In `formats/szi/tiler.go`, in `loadManifest()`, immediately after the `t.manifest = m` assignment that follows the successful `dzi.ParseManifest` call, insert the guard. The current code reads:

```go
	m, err := dzi.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("szi: parse manifest %s: %w", manifestEntry.Name, err)
	}
	t.manifest = m
```

Change it to:

```go
	m, err := dzi.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("szi: parse manifest %s: %w", manifestEntry.Name, err)
	}
	if m.Overlap > 0 {
		return fmt.Errorf("szi: %s: Overlap=%d: %w", manifestEntry.Name, m.Overlap, dzi.ErrOverlapNotSupported)
	}
	t.manifest = m
```

- [ ] **Step 6: Write the SZI guard test**

The test below calls the unexported `openSZI`, so it must be in an **internal** `package szi` test file. Check `formats/szi/factory_test.go`'s package clause: if it is `package szi`, append there; if it is `package szi_test` (external), instead create a new `formats/szi/overlap_guard_test.go` with `package szi`. Reuse any existing in-memory-SZI-ZIP helper in the package if one exists; otherwise build the archive inline with `archive/zip` + `zip.Store` as shown. The test builds a minimal SZI with `Overlap="1"` and asserts the open fails with `ErrOverlapNotSupported`, and that an otherwise-identical `Overlap="0"` archive is not rejected by the guard:

```go
func TestSZIOverlapGuard(t *testing.T) {
	// Minimal SZI: <root>/<name>.dzi + <root>/<name>_files/ (no tiles needed —
	// the guard fires at manifest load, before tile access).
	manifest := func(overlap int) string {
		return fmt.Sprintf(`<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" `+
			`Format="jpeg" Overlap="%d" TileSize="256"><Size Width="256" Height="256"/></Image>`, overlap)
	}
	build := func(overlap int) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: "s/s.dzi", Method: zip.Store})
		w.Write([]byte(manifest(overlap)))
		// an empty _files marker entry so discoverRoot/loadManifest succeed
		zw.CreateHeader(&zip.FileHeader{Name: "s/s_files/", Method: zip.Store})
		zw.Close()
		return buf.Bytes()
	}

	bad := build(1)
	if _, err := openSZI(bytes.NewReader(bad), int64(len(bad)), nil); !errors.Is(err, dzi.ErrOverlapNotSupported) {
		t.Fatalf("Overlap=1 err = %v, want ErrOverlapNotSupported", err)
	}
	good := build(0)
	if _, err := openSZI(bytes.NewReader(good), int64(len(good)), nil); err != nil {
		// Overlap=0 must pass the guard; later stages may still object to the
		// minimal archive, but it must NOT be ErrOverlapNotSupported.
		if errors.Is(err, dzi.ErrOverlapNotSupported) {
			t.Fatalf("Overlap=0 wrongly rejected by overlap guard: %v", err)
		}
	}
}
```

Ensure `formats/szi/factory_test.go` imports `bytes`, `archive/zip`, `errors`, `fmt`, and `github.com/wsilabs/opentile-go/internal/dzi` (add any missing). Verify the `openSZI` signature matches `formats/szi/tiler.go` (it is `openSZI(r io.ReaderAt, size int64, cfg *format.Config) (*Tiler, error)`; pass `nil` cfg). If the minimal `Overlap=0` archive cannot reach the guard because an earlier stage (e.g. `discoverRoot`) rejects it, adjust the archive contents so it does — the assertion that matters is the `Overlap=1` case returning `ErrOverlapNotSupported`.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/dzi/ ./formats/szi/ -race -count=1`
Expected: PASS (the new guard test + all existing SZI tests; the `CMU-1.szi` Overlap=0 fixtures are unaffected).

- [ ] **Step 8: Commit**

```bash
# add the szi guard test file you used (factory_test.go or the new overlap_guard_test.go)
git add internal/dzi/errors.go internal/dzi/errors_test.go formats/szi/tiler.go formats/szi/*_test.go
git commit -m "feat(dzi): shared ErrOverlapNotSupported guard; SZI rejects Overlap>0 at open"
```

---

## Task 2: FormatDZI enum + dziPathOpenHook plumbing

**Files:**
- Modify: `format.go`, `open.go`

- [ ] **Step 1: Add the Format constant**

In `format.go`, inside the `const (...)` block (after `FormatDICOM`), add:

```go
	// FormatDZI identifies a bare Deep Zoom Image — a filesystem .dzi XML
	// manifest plus a sibling <name>_files/<level>/<col>_<row>.<ext> tile tree
	// (the OpenSeadragon / Microsoft Deep Zoom layout). Unlike SZI (a ZIP
	// wrapper), bare DZI is opened from a path via OpenFile (the .dzi file or a
	// directory containing exactly one), like DICOM. Overlap=0 only. Added in v0.52.
	FormatDZI Format = "dzi"
```

- [ ] **Step 2: Add the path-hook var + setter in open.go**

In `open.go`, immediately after the `dicomPathOpenHook` block (the `var dicomPathOpenHook ...` + `func SetDICOMPathOpenHook(...)`), add:

```go
// dziPathOpenHook is set by formats/dzi's init() via SetDZIPathOpenHook. It is
// consulted by OpenFile after the DICOM hook and before single-file dispatch,
// because bare DZI is path-based (a .dzi manifest + a sibling _files/ tile tree
// that an io.ReaderAt alone cannot locate). nil when formats/dzi is not imported.
var dziPathOpenHook func(path string) (any, error)

// SetDZIPathOpenHook registers the bare-DZI path-open function. Called once from
// formats/dzi's init().
func SetDZIPathOpenHook(fn func(path string) (any, error)) {
	dziPathOpenHook = fn
}
```

- [ ] **Step 3: Consult the hook in OpenFile**

In `open.go` `OpenFile`, immediately after the DICOM hook block (after its closing `}` and the `// ErrUnsupportedFormat: not DICOM — fall through...` comment, before the `switch cfg.backing`), add the parallel DZI block:

```go
	// Bare-DZI path-aware branch — same rationale as DICOM: bare DZI is a .dzi
	// manifest + sibling _files/ tile tree, so it needs the path, not just an
	// io.ReaderAt. ErrUnsupportedFormat means "not a bare DZI — fall through."
	if dziPathOpenHook != nil {
		result, err := dziPathOpenHook(path)
		if err == nil {
			sr, ok := result.(slideReader)
			if !ok {
				return nil, fmt.Errorf("opentile: dzi hook returned unexpected type %T", result)
			}
			return &Slide{r: sr, size: 0, readBudget: cfg.resolveMemoryBudget()}, nil
		}
		if !errors.Is(err, ErrUnsupportedFormat) {
			return nil, err
		}
		// ErrUnsupportedFormat: not a bare DZI — fall through to normal dispatch.
	}
```

- [ ] **Step 4: Verify it builds**

Run: `go build ./...`
Expected: builds clean (the hook is `nil` until `formats/dzi` registers it; nothing consults it yet).

Run: `go vet .`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add format.go open.go
git commit -m "feat(opentile): FormatDZI + dziPathOpenHook plumbing (mirrors DICOM)"
```

---

## Task 3: formats/dzi level engine (filesystem tile source)

**Files:**
- Create: `formats/dzi/doc.go`, `formats/dzi/level.go`
- Test: `formats/dzi/level_test.go` (package `dzi`, internal — exercises the unexported `level`; full integration is in Task 5's external test)

- [ ] **Step 1: Write the failing level test**

Create `formats/dzi/level_test.go` (note: `package dzi` — internal, so it can build the unexported `level`):

```go
package dzi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLevelTileReadsFile(t *testing.T) {
	dir := t.TempDir()
	filesDir := filepath.Join(dir, "img_files")
	// dziLevel 5, tile (1,0) → <filesDir>/5/1_0.jpeg
	tileDir := filepath.Join(filesDir, "5")
	if err := os.MkdirAll(tileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xff\xd8\xff\xe0FAKEJPEG")
	if err := os.WriteFile(filepath.Join(tileDir, "1_0.jpeg"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &level{filesDir: filesDir, format: "jpeg", dziLevel: 5,
		openTileIdx: 0, width: 512, height: 256, cols: 2, rows: 1, tileSize: 256}

	got, err := l.Tile(1, 0)
	if err != nil {
		t.Fatalf("Tile(1,0): %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("Tile bytes = %q, want %q", got, want)
	}
	// Out of grid.
	if _, err := l.Tile(5, 5); err == nil {
		t.Fatal("Tile(5,5) want out-of-bounds error")
	}
	// In-grid but file absent.
	if _, err := l.Tile(0, 0); err == nil {
		t.Fatal("Tile(0,0) want missing-tile error (no file written)")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./formats/dzi/ -run TestLevelTileReadsFile -count=1`
Expected: FAIL — `undefined: level` (package doesn't compile yet).

- [ ] **Step 3: Create the package doc**

Create `formats/dzi/doc.go`:

```go
// Package dzi reads bare Deep Zoom Image (DZI) slides: a filesystem .dzi XML
// manifest plus a sibling <name>_files/<level>/<col>_<row>.<ext> tile tree (the
// OpenSeadragon / Microsoft Deep Zoom layout). It is the filesystem sibling of
// formats/szi (the ZIP-wrapped variant) and shares internal/dzi for all pyramid
// and tile-coordinate math.
//
// Bare DZI is opened from a path via opentile.OpenFile — either the .dzi file or
// a directory containing exactly one .dzi — through a path-aware hook installed
// in init() (the same mechanism DICOM uses). Open(io.ReaderAt) does not support
// bare DZI because tiles live in sibling files that an io.ReaderAt cannot locate.
//
// Only Overlap=0 manifests are supported; Overlap>0 is rejected at open with
// internal/dzi.ErrOverlapNotSupported.
package dzi
```

- [ ] **Step 4: Create the level engine**

Create `formats/dzi/level.go`:

```go
package dzi

import (
	"context"
	"io"
	"iter"
	"os"

	opentile "github.com/wsilabs/opentile-go"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

// level is the per-level tile-read engine for a bare DZI pyramid. Tiles are
// individual files under <filesDir>/<dziLevel>/<col>_<row>.<format>, read via
// os.ReadFile. Overlap is always 0 (Overlap>0 is rejected at open), so each
// stored tile is exactly its grid cell.
type level struct {
	filesDir string // absolute path to <base>_files
	format   string // manifest Format ("jpeg" / "png"), the tile file extension

	dziLevel    int // DZI-side level index (MaxLevel = full resolution)
	openTileIdx int // opentile-side level index (0 = full resolution)

	width    int
	height   int
	cols     int
	rows     int
	tileSize int
}

// tilePath resolves (x, y) to the on-disk tile file path.
func (l *level) tilePath(x, y int) string {
	return idzi.TilePath(l.filesDir, l.dziLevel, x, y, l.format)
}

// inBounds reports whether (x, y) is within the level's tile grid.
func (l *level) inBounds(x, y int) bool {
	return x >= 0 && x < l.cols && y >= 0 && y < l.rows
}

// Tile returns the raw on-disk tile bytes (a complete JPEG / PNG file).
func (l *level) Tile(x, y int) ([]byte, error) {
	if !l.inBounds(x, y) {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	b, err := os.ReadFile(l.tilePath(x, y))
	if err != nil {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return b, nil
}

// TileInto reads tile (x, y) into dst and returns the byte count. dst must be
// at least the tile's on-disk size; otherwise returns 0, io.ErrShortBuffer.
func (l *level) TileInto(x, y int, dst []byte) (int, error) {
	if !l.inBounds(x, y) {
		return 0, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	f, err := os.Open(l.tilePath(x, y))
	if err != nil {
		return 0, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	size := int(info.Size())
	if len(dst) < size {
		return 0, io.ErrShortBuffer
	}
	n, err := io.ReadFull(f, dst[:size])
	if err != nil {
		return n, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return n, nil
}

// TileReader returns a streaming reader over the tile file. The caller closes it.
func (l *level) TileReader(x, y int) (io.ReadCloser, error) {
	if !l.inBounds(x, y) {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: opentile.ErrTileOutOfBounds}
	}
	f, err := os.Open(l.tilePath(x, y))
	if err != nil {
		return nil, &opentile.TileError{Level: l.openTileIdx, X: x, Y: y, Err: err}
	}
	return f, nil
}

// TileMaxSize returns a conservative upper bound on tile bytes: tileSize² × 4
// (an uncompressed RGBA tile). Compressed JPEG/PNG tiles are far smaller.
func (l *level) TileMaxSize() int { return l.tileSize * l.tileSize * 4 }

// Tiles iterates all tile positions in row-major order.
func (l *level) Tiles(ctx context.Context) iter.Seq2[opentile.Point, opentile.TileResult] {
	return func(yield func(opentile.Point, opentile.TileResult) bool) {
		for y := 0; y < l.rows; y++ {
			for x := 0; x < l.cols; x++ {
				if err := ctx.Err(); err != nil {
					yield(opentile.Point{X: x, Y: y}, opentile.TileResult{Err: err})
					return
				}
				b, err := l.Tile(x, y)
				if !yield(opentile.Point{X: x, Y: y}, opentile.TileResult{Bytes: b, Err: err}) {
					return
				}
			}
		}
	}
}
```

- [ ] **Step 5: Run the level test to verify it passes**

Run: `go test ./formats/dzi/ -run TestLevelTileReadsFile -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add formats/dzi/doc.go formats/dzi/level.go formats/dzi/level_test.go
git commit -m "feat(dzi): filesystem tile-read level engine"
```

---

## Task 4: formats/dzi Tiler + path-open factory

**Files:**
- Create: `formats/dzi/tiler.go`, `formats/dzi/factory.go`
- Modify: `formats/all/all.go`

- [ ] **Step 1: Create the Tiler**

Create `formats/dzi/tiler.go`:

```go
package dzi

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

// Compile-time assertion: *Tiler satisfies format.Reader (a superset of the
// root's slideReader interface, so the OpenFile hook's type-assertion succeeds).
var _ format.Reader = (*Tiler)(nil)

// Tiler is the bare-DZI reader. It holds the parsed manifest and the absolute
// path to the <base>_files tile tree; tiles are read from the filesystem on
// demand. There is no scan-properties.xml and no associated_images/ in a bare
// DZI, so Metadata is empty and AssociatedImages is nil.
type Tiler struct {
	filesDir string // absolute path to <base>_files
	manifest idzi.Manifest

	dziImage     opentile.Pyramid // the single pyramid (DZI has exactly one image)
	levelEngines []*level
}

// openBareDZI parses the manifest at dziPath, rejects Overlap>0, and builds the
// pyramid. filesDir is <dir(dziPath)>/<base(dziPath) without .ext>_files.
func openBareDZI(dziPath string, manifestBytes []byte, filesDir string) (*Tiler, error) {
	m, err := idzi.ParseManifest(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("dzi: parse manifest %s: %w", dziPath, err)
	}
	if m.Overlap > 0 {
		return nil, fmt.Errorf("dzi: %s: Overlap=%d: %w", dziPath, m.Overlap, idzi.ErrOverlapNotSupported)
	}
	t := &Tiler{filesDir: filesDir, manifest: m}
	t.buildLevels()
	return t, nil
}

// buildLevels populates dziImage + levelEngines, one entry per DZI pyramid
// level. opentile L_i = DZI (MaxLevel - i); index 0 = highest resolution.
func (t *Tiler) buildLevels() {
	maxLevel := idzi.MaxLevel(t.manifest.Width, t.manifest.Height)

	var comp opentile.Compression
	switch strings.ToLower(t.manifest.Format) {
	case "jpeg", "jpg":
		comp = opentile.CompressionJPEG
	case "png":
		comp = opentile.CompressionPNG
	default:
		comp = opentile.CompressionUnknown
	}

	valueLevels := make([]opentile.Level, maxLevel+1)
	engines := make([]*level, maxLevel+1)
	l0W, _ := idzi.LevelDims(t.manifest.Width, t.manifest.Height, maxLevel)
	for i := 0; i <= maxLevel; i++ {
		dziL := maxLevel - i
		w, h := idzi.LevelDims(t.manifest.Width, t.manifest.Height, dziL)
		cols, rows := idzi.GridDims(w, h, t.manifest.TileSize)
		engines[i] = &level{
			filesDir:    t.filesDir,
			format:      t.manifest.Format,
			dziLevel:    dziL,
			openTileIdx: i,
			width:       w,
			height:      h,
			cols:        cols,
			rows:        rows,
			tileSize:    t.manifest.TileSize,
		}
		valueLevels[i] = opentile.Level{
			Index:        i,
			PyramidIndex: i,
			Size:         opentile.Size{W: w, H: h},
			TileSize:     opentile.Size{W: t.manifest.TileSize, H: t.manifest.TileSize},
			Grid:         opentile.Size{W: cols, H: rows},
			Compression:  comp,
			Downsample:   float64(l0W) / float64(w),
		}
	}
	t.dziImage = opentile.Pyramid{Name: "", Index: 0, Levels: valueLevels}
	t.levelEngines = engines
}

// Format returns opentile.FormatDZI.
func (t *Tiler) Format() opentile.Format { return opentile.FormatDZI }

// Close is a no-op: bare DZI holds no open handles (tiles are read per call).
func (t *Tiler) Close() error { return nil }

// Pyramids returns the single Pyramid carried by the bare DZI.
func (t *Tiler) Pyramids() []opentile.Pyramid {
	if t.levelEngines == nil {
		return nil
	}
	return []opentile.Pyramid{t.dziImage}
}

// Level returns the value-type Level for (image, level).
func (t *Tiler) Level(image, level int) (opentile.Level, error) {
	if image != 0 {
		return opentile.Level{}, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelEngines) {
		return opentile.Level{}, opentile.ErrLevelOutOfRange
	}
	return t.dziImage.Levels[level], nil
}

// AssociatedImages returns nil — a bare DZI has no associated images.
func (t *Tiler) AssociatedImages() []opentile.AssociatedImage { return nil }

// Metadata returns an empty cross-format metadata view — a bare DZI manifest
// carries no scan/resolution metadata.
func (t *Tiler) Metadata() opentile.Metadata { return opentile.Metadata{Format: opentile.FormatDZI} }

// ICCProfile returns nil — bare DZI surfaces no ICC profile.
func (t *Tiler) ICCProfile() []byte { return nil }

// WarmLevel validates bounds; tile reads are direct file reads, so warming is a
// no-op hint.
func (t *Tiler) WarmLevel(image, level int) error {
	if image != 0 {
		return opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelEngines) {
		return opentile.ErrLevelOutOfRange
	}
	return nil
}

// engine returns the *level engine for (image, level), validating bounds.
func (t *Tiler) engine(image, level int) (*level, error) {
	if image != 0 {
		return nil, opentile.ErrImageIndexOutOfRange
	}
	if level < 0 || level >= len(t.levelEngines) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return t.levelEngines[level], nil
}

// ImageRawTile returns the raw tile bytes at (image, level, tx, ty).
func (t *Tiler) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return nil, err
	}
	return eng.Tile(tx, ty)
}

// ImageRawTileInto fills dst with raw tile bytes.
func (t *Tiler) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0, err
	}
	return eng.TileInto(tx, ty, dst)
}

// ImageTileMaxSize returns the upper bound on tile byte size.
func (t *Tiler) ImageTileMaxSize(image, level int) int {
	eng, err := t.engine(image, level)
	if err != nil {
		return 0
	}
	return eng.TileMaxSize()
}

// ImageTilePrefix returns nil — DZI tiles carry no shared prefix.
func (t *Tiler) ImageTilePrefix(image, level int) []byte { return nil }

// ImageTileBodyMaxSize equals ImageTileMaxSize (no prefix).
func (t *Tiler) ImageTileBodyMaxSize(image, level int) int { return t.ImageTileMaxSize(image, level) }

// ImageTileBodyInto equals ImageRawTileInto (TilePrefix is nil).
func (t *Tiler) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return t.ImageRawTileInto(image, level, tx, ty, dst)
}

// ImageTileReader returns a streaming reader for the tile at (image, level, tx, ty).
func (t *Tiler) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	eng, err := t.engine(image, level)
	if err != nil {
		return nil, err
	}
	return eng.TileReader(tx, ty)
}

// ImageRangeTiles returns a row-major iterator over all tiles at (image, level).
func (t *Tiler) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.Point, opentile.TileResult] {
	eng, err := t.engine(image, level)
	if err != nil {
		return func(yield func(opentile.Point, opentile.TileResult) bool) {}
	}
	return eng.Tiles(ctx)
}
```

> Note on the import alias: `internal/dzi` is imported as `idzi` to avoid colliding with the package's own name `dzi`. The `Metadata{Format: ...}` field — confirm `opentile.Metadata` has a `Format` field; if not, return a bare `opentile.Metadata{}` instead (check `metadata.go` / the `Metadata` struct in the root package and match it).

- [ ] **Step 2: Create the path-open factory**

Create `formats/dzi/factory.go`:

```go
package dzi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
)

func init() {
	opentile.SetDZIPathOpenHook(openForHook)
}

// openForHook is the entry point installed as the root's dziPathOpenHook. It
// accepts a .dzi file path, or a directory containing exactly one *.dzi. Returns
// opentile.ErrUnsupportedFormat (so OpenFile falls through to normal dispatch)
// when the path is neither.
func openForHook(path string) (any, error) {
	dziPath, err := resolveDZIPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(dziPath)
	if err != nil {
		return nil, fmt.Errorf("dzi: read manifest %s: %w", dziPath, err)
	}
	base := strings.TrimSuffix(filepath.Base(dziPath), filepath.Ext(dziPath))
	filesDir := filepath.Join(filepath.Dir(dziPath), base+"_files")
	return openBareDZI(dziPath, data, filesDir)
}

// resolveDZIPath returns the .dzi manifest path for a path that is either a
// .dzi file or a directory containing exactly one *.dzi. Non-DZI inputs return
// opentile.ErrUnsupportedFormat.
func resolveDZIPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		// Non-existent / unreadable path: not our concern — fall through.
		return "", opentile.ErrUnsupportedFormat
	}
	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(path), ".dzi") {
			return path, nil
		}
		return "", opentile.ErrUnsupportedFormat
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.dzi"))
	if err != nil || len(matches) != 1 {
		// Zero or multiple .dzi in the dir → ambiguous → fall through.
		return "", opentile.ErrUnsupportedFormat
	}
	return matches[0], nil
}
```

- [ ] **Step 3: Register the package in formats/all**

In `formats/all/all.go`, add a blank import. Place it next to SZI (order doesn't matter for DZI since it installs a path hook rather than a content matcher, but group it logically):

```go
	// SZI before generictiff so ZIP-magic detection runs first.
	_ "github.com/wsilabs/opentile-go/formats/szi"
	// DZI installs a path-aware OpenFile hook (like dicom); no content matcher.
	_ "github.com/wsilabs/opentile-go/formats/dzi"
	// generictiff must be last: it's the catch-all.
	_ "github.com/wsilabs/opentile-go/formats/generictiff"
```

- [ ] **Step 4: Verify it builds**

Run: `go build ./...`
Expected: builds clean. If the `format.Reader` compile assertion fails ("`*Tiler` does not implement `format.Reader`"), the error names the missing method — add it by porting the corresponding `formats/szi/tiler.go` delegation, then rebuild.

Run: `go build -tags nocgo ./...`
Expected: builds clean (the reader is pure Go; only DecodedTile of JPEG tiles needs cgo at call time).

Run: `go vet ./formats/dzi/ .`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add formats/dzi/tiler.go formats/dzi/factory.go formats/all/all.go
git commit -m "feat(dzi): bare DZI Tiler + path-aware OpenFile hook; register in formats/all"
```

---

## Task 5: Integration tests (synthetic temp-dir DZI)

**Files:**
- Create: `formats/dzi/dzi_test.go` (package `dzi_test` — **external**, so it can import `formats/all` + `opentile.OpenFile` without an import cycle; an internal `package dzi` test importing `formats/all` would cycle because `formats/all` imports `formats/dzi`).

- [ ] **Step 1: Create the synthetic-DZI helper + open/read tests**

Create `formats/dzi/dzi_test.go` with `package dzi_test`. This builds a real bare-DZI tree (512×512, TileSize 256 → L0 is dziLevel `MaxLevel`, a 2×2 grid) in a temp dir, writes JPEG tiles, and drives it through `opentile.OpenFile`.

The import block is:

```go
package dzi_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)
```

```go
// jpegTile encodes a solid-color w×h JPEG (stdlib encoder, no cgo).
func jpegTile(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeSyntheticDZI writes a complete bare DZI (manifest + every tile of every
// level) for a width×height image into dir, named "<base>.dzi". Returns the
// .dzi path. Each tile is a solid mid-gray JPEG sized to its clamped extent.
func writeSyntheticDZI(t *testing.T, dir, base string, width, height, tileSize, overlap int) string {
	t.Helper()
	manifest := fmtSprintfManifest(overlap, tileSize, width, height)
	dziPath := filepath.Join(dir, base+".dzi")
	if err := os.WriteFile(dziPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	filesDir := filepath.Join(dir, base+"_files")
	maxLevel := idzi.MaxLevel(width, height)
	for dziL := 0; dziL <= maxLevel; dziL++ {
		w, h := idzi.LevelDims(width, height, dziL)
		cols, rows := idzi.GridDims(w, h, tileSize)
		levelDir := filepath.Join(filesDir, itoa(dziL))
		if err := os.MkdirAll(levelDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
				tw := tileSize
				if (col+1)*tileSize > w {
					tw = w - col*tileSize
				}
				th := tileSize
				if (row+1)*tileSize > h {
					th = h - row*tileSize
				}
				if tw <= 0 || th <= 0 {
					continue
				}
				b := jpegTile(t, tw, th, color.RGBA{R: 128, G: 128, B: 128, A: 255})
				p := filepath.Join(levelDir, itoa(col)+"_"+itoa(row)+".jpeg")
				if err := os.WriteFile(p, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return dziPath
}

func fmtSprintfManifest(overlap, tileSize, w, h int) string {
	return `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="` +
		itoa(overlap) + `" TileSize="` + itoa(tileSize) + `"><Size Width="` + itoa(w) +
		`" Height="` + itoa(h) + `"/></Image>`
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }

func TestOpenBareDZIFromFilePath(t *testing.T) {
	dir := t.TempDir()
	dziPath := writeSyntheticDZI(t, dir, "img", 512, 512, 256, 0)

	s, err := opentile.OpenFile(dziPath)
	if err != nil {
		t.Fatalf("OpenFile(%q): %v", dziPath, err)
	}
	defer s.Close()

	if s.Format() != opentile.FormatDZI {
		t.Fatalf("Format = %q, want dzi", s.Format())
	}
	l0, err := s.Level(0)
	if err != nil {
		t.Fatal(err)
	}
	if l0.Size != (opentile.Size{W: 512, H: 512}) {
		t.Fatalf("L0 Size = %v, want 512x512", l0.Size)
	}
	if l0.Grid != (opentile.Size{W: 2, H: 2}) {
		t.Fatalf("L0 Grid = %v, want 2x2", l0.Grid)
	}
	if l0.Overlapping {
		t.Fatal("DZI L0 must not be Overlapping (clean grid)")
	}
	// Raw tile bytes are a real JPEG.
	raw, err := l0.Tile(0, 0)
	if err != nil {
		t.Fatalf("Tile(0,0): %v", err)
	}
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xD8 {
		t.Fatalf("tile not a JPEG (SOI missing): % x", raw[:2])
	}
	// DecodedTile yields a TileSize image (JPEG is lossy — assert dims, not bytes).
	img, err := l0.DecodedTile(0, 0)
	if err != nil {
		t.Fatalf("DecodedTile(0,0): %v", err)
	}
	if img.Width != 256 || img.Height != 256 {
		t.Fatalf("DecodedTile dims = %dx%d, want 256x256", img.Width, img.Height)
	}
	// Out-of-grid tile.
	if _, err := l0.Tile(9, 9); err == nil {
		t.Fatal("Tile(9,9) want out-of-bounds error")
	}
}

func TestOpenBareDZIFromDirPath(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticDZI(t, dir, "slide", 512, 512, 256, 0)
	s, err := opentile.OpenFile(dir) // directory containing exactly one .dzi
	if err != nil {
		t.Fatalf("OpenFile(dir): %v", err)
	}
	defer s.Close()
	if s.Format() != opentile.FormatDZI {
		t.Fatalf("Format = %q, want dzi", s.Format())
	}
}

func TestBareDZIMissingTile(t *testing.T) {
	dir := t.TempDir()
	// Manifest only, no _files tree → reading any tile fails.
	dziPath := filepath.Join(dir, "img.dzi")
	if err := os.WriteFile(dziPath, []byte(fmtSprintfManifest(0, 256, 512, 512)), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := opentile.OpenFile(dziPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	l0, _ := s.Level(0)
	if _, err := l0.Tile(0, 0); err == nil {
		t.Fatal("Tile(0,0) want missing-file error (no _files tree)")
	}
}

func TestBareDZIOverlapGuard(t *testing.T) {
	dir := t.TempDir()
	dziPath := filepath.Join(dir, "img.dzi")
	if err := os.WriteFile(dziPath, []byte(fmtSprintfManifest(1, 256, 512, 512)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := opentile.OpenFile(dziPath); !errors.Is(err, idzi.ErrOverlapNotSupported) {
		t.Fatalf("Overlap=1 err = %v, want ErrOverlapNotSupported", err)
	}
}

func TestBareDZIDirWithoutManifestFallsThrough(t *testing.T) {
	dir := t.TempDir() // empty dir, no .dzi
	if _, err := opentile.OpenFile(dir); err == nil {
		t.Fatal("OpenFile(empty dir) should fail (hook falls through, no format matches)")
	} else if errors.Is(err, idzi.ErrOverlapNotSupported) {
		t.Fatal("empty dir must not surface the overlap sentinel")
	}
}
```

(`itoa` uses `fmt.Sprintf`, already imported. If you prefer, replace `itoa` with `strconv.Itoa` and swap the `fmt` import for `strconv` — match whatever keeps `go vet`/`goimports` clean.)

- [ ] **Step 2: Run the integration tests**

Run: `go test ./formats/dzi/ -race -count=1`
Expected: PASS — both the internal level test (Task 3) and the external integration tests (file-path open, dir-path open, missing tile, overlap guard, empty-dir fall-through). Go compiles `package dzi` and `package dzi_test` together for the same directory.

- [ ] **Step 3: Commit**

```bash
git add formats/dzi/dzi_test.go
git commit -m "test(dzi): synthetic temp-dir integration — open (file+dir), tiles, OOB, overlap guard, fall-through"
```

---

## Task 6: Docs, retire R19, CHANGELOG, verification

**Files:**
- Create: `docs/formats/dzi.md`
- Modify: `README.md`, `docs/deferred.md`, `CHANGELOG.md`

- [ ] **Step 1: Write the format doc**

Create `docs/formats/dzi.md`:

```markdown
# DZI (bare Deep Zoom Image)

`FormatDZI` — a filesystem Deep Zoom Image: a `<name>.dzi` XML manifest plus a
sibling `<name>_files/<level>/<col>_<row>.<ext>` tile tree (the OpenSeadragon /
Microsoft Deep Zoom layout). The filesystem sibling of [SZI](szi.md) (the
ZIP-wrapped variant); both share `internal/dzi` for pyramid and tile math.

## Opening

Bare DZI is path-based, so it is opened via `opentile.OpenFile` (not
`opentile.Open(io.ReaderAt)`, which cannot locate the sibling tile files):

- `OpenFile("/path/to/slide.dzi")` — the manifest file (primary).
- `OpenFile("/path/to/dir")` — a directory containing exactly one `*.dzi`.

This uses a path-aware hook, the same mechanism DICOM uses.

## Tiles

Each tile is a complete JPEG/PNG file. The pyramid grid is clean
(`Level.Overlapping == false`, `Grid` tiles `Size`); `Tile(x,y)` returns the raw
file bytes, and `DecodedTile`/`ReadRegion`/`ScaledStrips` work as for any clean
format.

## Limitations

- **`Overlap=0` only.** A manifest with `Overlap>0` is rejected at open with
  `internal/dzi.ErrOverlapNotSupported` (cropping the per-tile overlap border is
  deferred). The same guard now also applies to SZI.
- **No metadata / associated images.** A bare `.dzi` carries only the manifest;
  `Metadata()` is empty and `AssociatedImages()` is nil.
- **Dense pyramids assumed.** A missing in-range tile is an error, not a blank
  fill.
```

- [ ] **Step 2: Update the README format list**

In `README.md`, find the list of supported formats (search for "SZI" or "Smart Zoom"). Add a bare-DZI entry alongside SZI, e.g.:

```markdown
- **DZI** (bare Deep Zoom Image) — filesystem `.dzi` manifest + `_files/` tile tree; `Overlap=0`. Opened via `OpenFile` (the `.dzi` file or its directory). See [docs/formats/dzi.md](docs/formats/dzi.md).
```

Also bump any "N formats" count in the README/intro from 11 to 12 if such a count is present (search for "11 formats" / "11 WSI").

- [ ] **Step 3: Retire R19 in deferred.md**

In `docs/deferred.md`, find the `R19` row (Deep Zoom Image, bare directory layout) and mark it landed, mirroring how other landed R-items are annotated (e.g. R18). Append to the R19 cell: ` ✅ landed v0.52 — formats/dzi (Overlap=0). Path-aware OpenFile hook (file or single-.dzi dir); reuses internal/dzi. Overlap>0 rejected via shared internal/dzi.ErrOverlapNotSupported (also applied to SZI). Overlap>0 crop/placement deferred.` (Match the table's existing wording style; do not restructure the table.)

- [ ] **Step 4: Add the CHANGELOG entry**

In `CHANGELOG.md`, insert above the top release section:

```markdown
## [Unreleased]

### Added

- **Bare DZI reader (`FormatDZI`)** — reads filesystem Deep Zoom Image slides: a
  `.dzi` XML manifest + sibling `<name>_files/<level>/<col>_<row>.<ext>` tile
  tree (OpenSeadragon / Microsoft Deep Zoom). opentile-go's 12th format and the
  filesystem sibling of SZI; reuses `internal/dzi` for pyramid math. Opened via
  `OpenFile` — the `.dzi` file or a directory containing exactly one — through a
  path-aware hook (the DICOM mechanism); `Open(io.ReaderAt)` is unsupported (no
  path to locate tiles). Clean grid (`Overlapping=false`); no metadata or
  associated images (a bare manifest carries neither). Closes the long-parked
  R19. `Overlap=0` only.

### Changed

- **`Overlap>0` DZI/SZI manifests are now rejected at open** with
  `internal/dzi.ErrOverlapNotSupported`, instead of being silently treated as
  `Overlap=0` (which mis-placed every interior tile). Applies to both the new
  bare-DZI reader and the existing SZI reader. `Overlap=0` files (all current
  fixtures/consumers) are byte-for-byte unaffected. Full `Overlap>0` support
  (per-tile border cropping) is deferred.
```

- [ ] **Step 5: Full verification**

Run each; macOS linker warnings (`ld: warning: ignoring duplicate libraries`) are pre-existing noise.
- `go vet ./...` — clean.
- `go build ./...` && `go build -tags nocgo ./...` — both clean.
- `go test ./... -race -count=1` — PASS across all packages.
- `go test ./formats/dzi/ ./formats/szi/ ./internal/dzi/ -race -count=1` — PASS.
- `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/szi/ -count=1` — PASS (the `CMU-1.szi` Overlap=0 fixture still opens/reads identically — the guard regression check).

- [ ] **Step 6: Commit**

```bash
git add docs/formats/dzi.md README.md docs/deferred.md CHANGELOG.md
git commit -m "docs(dzi): format doc, README/deferred R19 retire, CHANGELOG [Unreleased]"
```

---

## Notes for the executor

- **Port faithfully:** the `Tiler`/`level` are a port of `formats/szi` with an FS tile source and no ZIP/scan-properties/associated machinery. When a method is ambiguous, read the SZI sibling and mirror it.
- **`format.Reader` assertion is the gate:** if `var _ format.Reader = (*Tiler)(nil)` fails to compile, the named missing method must be added (port from `formats/szi/tiler.go`). The full method set there is the contract.
- **`opentile.Metadata` field check:** Task 4 Step 1 returns `opentile.Metadata{Format: opentile.FormatDZI}` — confirm the struct has a `Format` field; if not, return `opentile.Metadata{}`.
- **No silent corruption:** the headline behavior change is that `Overlap>0` now errors. Do not weaken any test to make an `Overlap>0` input "succeed."
- **Overlap>0 is out of scope** (border crop/placement) — deferred to a separate design conversation per the owner.
- **Version:** additive MINOR. Ship as the next minor (v0.52.0) after CI is green.
```
