# TIFF Tag Exposure — Remaining Readers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Extend the raw-TIFF-tag provider (shipped for SVS) to the other 7 TIFF formats so the public API works on every TIFF-based slide, with a comprehensive cross-format test.

**Architecture:** Each TIFF reader retains, at Open, a per-IFD `(page, role)` mapping and implements the exported `TIFFDirectories() []opentile.TIFFDirectory` method. The lazy type-assertion provider + public types already exist (committed). COG-WSI inherits the provider for free through its existing `UnwrapReader()` delegation to the inner generic-TIFF reader.

**Reference implementation:** the SVS provider, commit `8c535b1` (`formats/svs/svs.go`). Read it — it is the exact template:
- a `svsDirSpec{ pageIdx int; kind opentile.DirectoryKind; level int; assoc string }` captured at Open (level loop → `DirLevel`+levelIdx; associated loop → `DirAssociated`+assoc-string-matching-`Type()`; uncovered pages → `DirOther` via a `seenPages` guard);
- `TIFFDirectories()` builds `[]opentile.TIFFDirectory` lazily via `opentile.TIFFTagsFromPage(page)`.

**Generalization for SubIFD/region/series readers:** where a level's IFD is NOT `file.Pages()[i]` (OME SubIFDs, Leica regions, NDPI series), store the actual `*tiff.Page` in the dir-spec instead of a page index. Define the spec as `{ page *tiff.Page; kind; image; level; assoc }` and call `opentile.TIFFTagsFromPage(spec.page)`.

**Contract every reader's `TIFFDirectories()` must satisfy:**
- One `DirLevel` directory per pyramid level, with `Image`/`Level` set to the indices used by `ImageRawTile(image, level, ...)`.
- One `DirAssociated` per associated image, `Associated` == that image's `Type()` string (VERIFY against the format's `associated.go`).
- Orphan IFDs (not a level/associated) as `DirOther` where reasonably reachable (e.g. NDPI Map page).
- Lazy: Open stores only page refs + roles; tag decode happens in `TIFFDirectories()`.

**Verified facts:**
- `opentile.TIFFTagsFromPage(p *tiff.Page) opentile.TIFFTags` exists (the bridge).
- `opentile.DirLevel`/`DirAssociated`/`DirOther`, `opentile.TIFFDirectory{Kind, Image, Level, Associated, Tags}`.
- Each reader's `*tiff.File` is available in `openFromTIFFFile`; `file.Pages() []*tiff.Page`.
- `cogwsi.Tiler.UnwrapReader() any { return t.inner }` already exists (`formats/cogwsi/tiler.go:173`).
- Open paths: generic-TIFF `generic.go:57`; Philips `philips.go:72` (assoc loop `:174`, `spec.kind`/`spec.pageIdx` — identical shape to SVS); BIF `bif.go:63` (assoc `:116`, uses `c.Page`); Leica-SCN `leicascn.go:50` (levels via `newTiledRegion(rl, file, r)`, assoc `newAssociatedImage(aux, file, r)`); OME `ome.go` `buildLevels` walks SubIFD chain (multi-image; `Image` index per pyramid).
- The `ld: warning: ignoring duplicate libraries` line is benign.

**Scope:** the 7 remaining TIFF readers + COG-WSI confirmation + a cross-format test. Order: generic-TIFF first (unblocks COG-WSI), then the SVS-shaped readers, then OME/NDPI (trickiest), then the test.

---

## Task 1: generic-TIFF provider

**Files:** Modify `formats/generictiff/generic.go`; Create `formats/generictiff/tifftags_test.go`.

- [ ] **Step 1:** Failing test mirroring `formats/svs/tifftags_test.go` but for generic-TIFF fixture `generic-tiff/CMU-1.tiff`: open it, `s.LevelTIFFTags(0)` must be `ok`, contain tag `256` (ImageWidth, universal), and NOT contain `324` (TileOffsets); `opentile.TIFFDirectoriesOf(s)` non-empty. (Same structure as the SVS test — write it concretely against the generic-TIFF fixture.)
- [ ] **Step 2:** Run → FAIL (provider not implemented).
- [ ] **Step 3:** Read `generic.go` `openFromTIFFFile`. Apply the SVS template: add a dir-spec type + retain `file *tiff.File` + dir-specs on the tiler; capture `DirLevel`/`DirAssociated`/`DirOther` at the existing level + associated construction points (use `seenPages`); verify the associated `assoc` string matches `associated.go`'s `Type()`. Implement `TIFFDirectories()`.
- [ ] **Step 4:** Run → PASS.
- [ ] **Step 5:** `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/generictiff/ . -race -count=1` → both `ok`.
- [ ] **Step 6:** Commit `feat(generictiff): TIFF-tag provider`.

## Task 2: Philips provider

**Files:** Modify `formats/philipstiff/philips.go`; Create `formats/philipstiff/tifftags_test.go`.

- [ ] Same shape as Task 1, fixture `philips-tiff/Philips-1.tiff`. Philips' `openFromTIFFFile` (`philips.go:72`) uses `spec.kind`/`spec.pageIdx` in its associated loop — nearly identical to SVS; the integration is the closest copy of the SVS template. Verify `Type()` strings in `philipstiff/associated.go`. TDD; run generictiff-style verification; commit `feat(philipstiff): TIFF-tag provider`.

## Task 3: BIF provider

**Files:** Modify `formats/bif/bif.go`; Create `formats/bif/tifftags_test.go`.

- [ ] Same shape, fixture `bif/Ventana-1.bif`. BIF's associated loop (`bif.go:116`) uses `c.Page` (a `*tiff.Page` directly) rather than a page index — use the generalized spec carrying `*tiff.Page` for associated entries, or map `c.Page` back via `file.Pages()`. Levels: capture per BIF's level construction. Note BIF level-0 has tile overlap — irrelevant to tags. Verify `Type()` strings in `bif/associated.go`. TDD; verify; commit `feat(bif): TIFF-tag provider`.

## Task 4: Leica-SCN provider

**Files:** Modify `formats/leicascn/leicascn.go` (and/or `tiled_region.go`); Create `formats/leicascn/tifftags_test.go`.

- [ ] Same shape, fixture `scn/Leica-1.scn`. Leica levels come from `newTiledRegion(rl, file, r)` (region-based) — the backing `*tiff.Page` lives in the `RegionLevel`/region structure. READ `leicascn.go` + `tiled_region.go` to find the page each level/region uses, and retain it (use the `*tiff.Page`-carrying spec). Associated via `newAssociatedImage(aux, file, r)` — find the page for each aux image. Verify `Type()` strings in `leicascn/associated.go`. If the page backing a region isn't readily accessible, report DONE_WITH_CONCERNS describing where it lives. TDD; verify; commit `feat(leicascn): TIFF-tag provider`.

## Task 5: OME-TIFF provider (multi-image)

**Files:** Modify `formats/ometiff/ome.go`; Create `formats/ometiff/tifftags_test.go`.

- [ ] Same shape, fixture `ome-tiff/Leica-1.ome.tiff` (single main pyramid) and assert on `Leica-2.ome.tiff` too if present (MULTI-image — 4 pyramids). OME levels are SubIFDs (`buildLevels` walks the SubIFD chain); store the actual `*tiff.Page` per (image, level) in the spec. CRITICAL: set `Image` to the correct pyramid index (NOT always 0) — keyed like `ImageRawTile(image, level, ...)`. The test must verify `s.ImageLevelTIFFTags(image, level)` returns tags for a non-zero image on the multi-image fixture. Verify `Type()` strings in `ometiff/associated.go`. TDD; verify; commit `feat(ometiff): TIFF-tag provider (multi-image)`.

## Task 6: NDPI provider

**Files:** Modify `formats/ndpi/*.go`; Create `formats/ndpi/tifftags_test.go`.

- [ ] Same shape, fixtures `ndpi/CMU-1.ndpi` (and assert the Map page appears as `DirOther` on `ndpi/OS-2.ndpi`, which carries a Map page). READ how NDPI builds its levels (page-series) + associated (overview, `associated.go`) + the Map page (`mappage.go`) at Open, and retain the backing `*tiff.Page` per directory. NDPI levels map to page-series — find the canonical page per level. Capture the Map page + overview as their roles (`DirAssociated` for overview if it's an associated image; `DirOther` for the Map page if not surfaced as associated — match `AssociatedImage.Type()` for whatever IS associated). This is the most involved reader; if the page→level mapping is genuinely hard to capture, report DONE_WITH_CONCERNS with specifics. Verify `Type()` strings. TDD; verify; commit `feat(ndpi): TIFF-tag provider`.

## Task 7: COG-WSI confirmation (free via delegation)

**Files:** Create `formats/cogwsi/tifftags_test.go`.

- [ ] COG-WSI wraps a generic-TIFF reader and already exposes `UnwrapReader() any { return t.inner }`, so once Task 1 lands, `tiffProviderOf` finds the provider through the unwrap chain — no production code needed. Write a test ONLY: open `cog-wsi/CMU-1_cog-wsi.tiff`, assert `s.LevelTIFFTags(0)` is `ok` with tag `256` present and `324` absent, and `TIFFDirectoriesOf` non-empty. Run → expect PASS (delegation works). If it returns `ok=false`, the unwrap walk isn't reaching the inner reader — investigate (the inner generic-TIFF tiler must implement the provider AND cogwsi's `UnwrapReader` must return it); report findings. Commit `test(cogwsi): confirm TIFF-tag provider via generic-TIFF delegation`.

## Task 8: cross-format "sufficient" test + README

**Files:** Create `tiff_tags_crossformat_test.go` (root, package `opentile_test`); Modify `README.md`.

- [ ] **Step 1:** Create `tiff_tags_crossformat_test.go` — the comprehensive matrix:

```go
package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func fixturePath(t *testing.T, rel string) string {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "sample_files"
	}
	p := filepath.Join(dir, rel)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	return p
}

// Every TIFF-based format must expose tags on level 0: ImageWidth (256,
// universal in TIFF) present, pixel-pointer tags filtered, directories
// non-empty. The single sufficiency gate for the whole feature.
func TestTIFFTagsAllFormats(t *testing.T) {
	cases := []struct{ format, rel string }{
		{"svs", "svs/CMU-1.svs"},
		{"ndpi", "ndpi/CMU-1.ndpi"},
		{"philips-tiff", "philips-tiff/Philips-1.tiff"},
		{"ome-tiff", "ome-tiff/Leica-1.ome.tiff"},
		{"bif", "bif/Ventana-1.bif"},
		{"generic-tiff", "generic-tiff/CMU-1.tiff"},
		{"leica-scn", "scn/Leica-1.scn"},
		{"cog-wsi", "cog-wsi/CMU-1_cog-wsi.tiff"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			s, err := opentile.OpenFile(fixturePath(t, tc.rel))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			tags, ok := s.LevelTIFFTags(0)
			if !ok {
				t.Fatalf("%s: LevelTIFFTags(0) ok=false", tc.format)
			}
			if _, ok := tags.Tag(256); !ok {
				t.Errorf("%s: level 0 missing ImageWidth (256)", tc.format)
			}
			if _, ok := tags.Tag(273); ok {
				t.Errorf("%s: StripOffsets (273) should be filtered", tc.format)
			}
			if _, ok := tags.Tag(324); ok {
				t.Errorf("%s: TileOffsets (324) should be filtered", tc.format)
			}
			dirs, ok := opentile.TIFFDirectoriesOf(s)
			if !ok || len(dirs) == 0 {
				t.Errorf("%s: TIFFDirectoriesOf empty: %d %v", tc.format, len(dirs), ok)
			}
			// At least one DirLevel for level 0.
			var hasL0 bool
			for _, d := range dirs {
				if d.Kind == opentile.DirLevel && d.Image == 0 && d.Level == 0 {
					hasL0 = true
				}
			}
			if !hasL0 {
				t.Errorf("%s: no DirLevel for (image 0, level 0)", tc.format)
			}
		})
	}
}

// Non-TIFF formats must return ok=false everywhere.
func TestTIFFTagsNonTIFFExcluded(t *testing.T) {
	for _, rel := range []string{"szi/CMU-1.szi", "ife/cervix_2x_jpeg.iris"} {
		s, err := opentile.OpenFile(fixturePath(t, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if _, ok := s.LevelTIFFTags(0); ok {
			t.Errorf("%s: non-TIFF LevelTIFFTags should be ok=false", rel)
		}
		if _, ok := opentile.TIFFDirectoriesOf(s); ok {
			t.Errorf("%s: non-TIFF TIFFDirectoriesOf should be ok=false", rel)
		}
		s.Close()
	}
}

// Fidelity: a known ASCII tag decodes non-empty and Raw is preserved.
func TestTIFFTagFidelity(t *testing.T) {
	s, err := opentile.OpenFile(fixturePath(t, "svs/CMU-1.svs"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tags, ok := s.LevelTIFFTags(0)
	if !ok {
		t.Fatal("ok=false")
	}
	desc, ok := tags.Tag(270) // ImageDescription
	if !ok {
		t.Fatal("missing ImageDescription")
	}
	v, ok := desc.ASCII()
	if !ok || len(v) == 0 {
		t.Fatalf("ASCII empty: %q %v", v, ok)
	}
	if len(desc.Raw) == 0 {
		t.Fatal("Raw bytes empty")
	}
}
```

- [ ] **Step 2:** Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test . -run 'TestTIFFTagsAllFormats|TestTIFFTagsNonTIFFExcluded|TestTIFFTagFidelity' -race -count=1 2>&1 | grep -vE "duplicate libraries" | grep -E "^(ok|FAIL|--- FAIL)"`. Expected: `ok`. Any per-format `--- FAIL: TestTIFFTagsAllFormats/<fmt>` means that reader's provider is wrong — fix that reader before proceeding.
- [ ] **Step 3:** Full suite: `OPENTILE_TESTDIR="$PWD/sample_files" make test 2>&1 | grep -vE "duplicate libraries" | grep -E "^FAIL" | head` → no output.
- [ ] **Step 4:** Update `README.md`: in the "Raw TIFF tags" subsection, change the final sentence "Currently implemented for SVS; other TIFF formats follow." to "Implemented for all TIFF-based formats (SVS, NDPI, Philips, OME-TIFF, BIF, generic-TIFF, Leica-SCN, COG-WSI)."
- [ ] **Step 5:** Commit `test(tifftags): cross-format sufficiency suite + README all-formats`.

---

## Self-Review Notes

- **Sufficient test (user's explicit ask):** Task 8's `TestTIFFTagsAllFormats` is a table-driven gate over all 8 TIFF formats asserting the provider works (ImageWidth present, pixel-pointers filtered, a `DirLevel` for level 0, directories non-empty) + `TestTIFFTagsNonTIFFExcluded` (SZI/IFE → false) + `TestTIFFTagFidelity` (ASCII decode + Raw preserved). This is the single gate proving the whole feature.
- **COG-WSI free via delegation** — Task 7 is test-only; verified `UnwrapReader` already returns the inner reader.
- **Generalization** for OME/Leica/NDPI (retain `*tiff.Page`, not page index) is called out per-task; the SVS index approach still works for generic/Philips/BIF.
- **Risk:** NDPI (Task 6) and Leica (Task 4) page→level mapping is reader-specific; each task instructs DONE_WITH_CONCERNS if the backing page isn't readily capturable, rather than forcing a fragile integration.
- **Placeholder note:** per-reader Tasks 1-6 reference the concrete SVS template (commit `8c535b1`) + each reader's exact Open location + gotchas rather than re-pasting 6 variants of the same ~40-line integration; this is patterned integration into existing, differently-shaped readers (proven viable by the SVS task) — the implementer reads its reader and adapts. The test task (8) is fully concrete.
