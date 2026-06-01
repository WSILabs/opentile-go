# opentile-go — raw TIFF tag exposure design

**Status:** DESIGN approved (2026-05-31)
**Author:** brainstorm 2026-05-31
**Work branch (proposed):** `feat/tiff-tags`

---

## 0. Problem statement

Consumers need access to raw TIFF tags that opentile-go parses but doesn't
surface as typed `Metadata` fields — primarily **vendor/private tags**
(Aperio, Hamamatsu, scanner-custom) that no typed accessor covers. The
naive approach (`Metadata.Properties["tiff.<name>"]`) is lossy on two
axes: (1) **IFD multiplicity** — the same tag appears in every IFD
(pyramid levels, associated images) with different values, and a flat map
glosses over which IFD; (2) **value typing** — TIFF tags are typed
scalars/arrays/rationals, and stringifying loses arity and precision.

opentile-go is a **reader**: "round-trip" here means *faithful typed
exposure* a writer (wsitools) can re-emit, not a TIFF write API.

## 1. Goals

- Query a specific tag (usually vendor/private) **by number**, typed.
- Enumerate the tags **available** on a given level / associated image.
- **Faithful** values (arrays, rationals, raw bytes) for re-encoders.
- Tags **anchored to the semantic entity** the consumer already holds
  (a pyramid level by `(image, level)` index; an associated image) — not
  a parallel flat list the consumer must cross-reference.

## 2. Sealed decisions

| # | Decision |
|---|---|
| D1 | Plumbing = **Approach A**: lazy, type-assertion provider (the existing `MetadataOf` pattern). No change to `Level`/`Image` value structs; non-TIFF formats simply don't implement it → `ok=false`. |
| D2 | **Primary access is entity-anchored** by the same `(image, level)` coordinates used for `RawTile`/`ReadRegion`, plus an `AssociatedImage` accessor. NOT a flat role-keyed list. |
| D3 | A **`TIFFDirectoriesOf` completeness view** is retained as the *secondary* escape hatch for orphan IFDs (NDPI Map, hidden, SubIFDs) that aren't a level or associated image. Each directory carries **structured identity** (Kind + indices), not a bare string. |
| D4 | **Typed values + raw bytes**: `TIFFTag` exposes `Raw []byte` (verbatim, faithful) and typed getters (`ASCII`/`Uints`/`Rationals`). |
| D5 | **Pixel-pointer tags excluded** — `StripOffsets`(273)/`StripByteCounts`(279)/`TileOffsets`(324)/`TileByteCounts`(325) are not metadata (regenerated on re-encode); excluded from the API entirely. `Tag`/`ByName` search the enumerated set. |
| D6 | **Name dictionary is best-effort** (baseline TIFF + common WSI vendor tags); `Number` is always authoritative; `Name` is `""` when unknown. |
| D7 | **Lazy**: tags decoded on first call, never at `Open` — preserves the immutable-after-Open / lock-free-hot-path invariant (read-only, idempotent). |
| D8 | Public, **additive** API (new exported names only); no breaking changes. |

## 3. Public API (package `opentile`, root)

```go
// TIFFType mirrors the TIFF field type. Named constants so consumers can
// interpret TIFFTag.Type without a magic-number table.
type TIFFType uint16

const (
	TIFFByte      TIFFType = 1
	TIFFASCII     TIFFType = 2
	TIFFShort     TIFFType = 3
	TIFFLong      TIFFType = 4
	TIFFRational  TIFFType = 5
	TIFFUndefined TIFFType = 7
	TIFFSShort    TIFFType = 8
	TIFFSLong     TIFFType = 9
	TIFFSRational TIFFType = 10
	TIFFLong8     TIFFType = 16 // BigTIFF LONG8
)

// Rational is a TIFF RATIONAL (unsigned) value.
type Rational struct{ Num, Denom uint32 }

// TIFFTag is one parsed TIFF tag, typed.
type TIFFTag struct {
	Number uint16   // always set — the key for vendor/private tags
	Name   string   // "" when not in the known-tag dictionary
	Type   TIFFType
	Count  int      // arity
	Raw    []byte   // verbatim payload bytes (file byte order) — faithful re-encode
}

// Typed accessors. ok=false when the tag's Type doesn't match.
func (t TIFFTag) ASCII() (string, bool)
func (t TIFFTag) Uints() ([]uint64, bool)      // BYTE/SHORT/LONG/LONG8
func (t TIFFTag) Ints() ([]int64, bool)        // SBYTE/SSHORT/SLONG
func (t TIFFTag) Rationals() ([]Rational, bool)

// TIFFTags is the set of tags on one IFD, with lookup helpers.
type TIFFTags []TIFFTag
func (ts TIFFTags) Tag(number uint16) (TIFFTag, bool)
func (ts TIFFTags) ByName(name string) (TIFFTag, bool)

// --- PRIMARY access: anchored to the semantic entity ---

// LevelTIFFTags returns the tags of image 0's level's backing IFD.
// ok=false for non-TIFF formats or an out-of-range level.
func (s *Slide) LevelTIFFTags(level int) (TIFFTags, bool)

// ImageLevelTIFFTags is the multi-image form, keyed exactly like
// ImageRawTile(image, level, ...).
func (s *Slide) ImageLevelTIFFTags(image, level int) (TIFFTags, bool)

// AssociatedTIFFTags returns the tags of an associated image's IFD.
func (s *Slide) AssociatedTIFFTags(a AssociatedImage) (TIFFTags, bool)

// --- SECONDARY: completeness view (orphan IFDs) ---

type DirectoryKind uint8
const (
	DirOther      DirectoryKind = iota // hidden / Map / SubIFD not surfaced elsewhere
	DirLevel                           // a pyramid level
	DirAssociated                      // an associated image
)

// TIFFDirectory is one IFD with structured identity.
type TIFFDirectory struct {
	Kind       DirectoryKind
	Image      int    // valid when Kind==DirLevel
	Level      int    // valid when Kind==DirLevel
	Associated string // associated image Type() (e.g. "label"), valid when Kind==DirAssociated
	Tags       TIFFTags
}

// TIFFDirectoriesOf enumerates every IFD, including orphans. ok=false for
// non-TIFF formats. The escape hatch for "dump all"; LevelTIFFTags /
// AssociatedTIFFTags are the everyday accessors.
func TIFFDirectoriesOf(s *Slide) ([]TIFFDirectory, bool)
```

Usage (the primary case — a vendor tag off the main image):
```go
tags, ok := slide.LevelTIFFTags(0)
if ok {
	if t, ok := tags.Tag(65420); ok {
		s, _ := t.ASCII()  // ...
	}
}
```

## 4. Architecture (Approach A — lazy provider)

```
consumer
  │  s.LevelTIFFTags(0) / TIFFDirectoriesOf(s)
  ▼
opentile.Slide ── walks UnwrapReader chain (like MetadataOf) ──► tiffTagProvider?
                                                                   │ (type-assert)
  ┌────────────────────────────────────────────────────────────────┘
  ▼
format reader (svs/ndpi/philips/ome/bif/generictiff/leicascn/cogwsi)
  implements:  tiffDirectories() []TIFFDirectory
  │  walks its already-classified file.Pages(), labels each (Kind+indices),
  │  translates page tags via a shared helper
  ▼
internal/tiff.Page.RawTags() []tiff.RawTag   (NEW generic enumerator)
  │  decodes every entry → {Number, Type, Count, Raw, decoded typed values}
  ▼
opentile.tiffTagsFrom(rawTags) TIFFTags       (shared translation + name dict + pixel-pointer filter)
```

- **`internal/tiff.Page.RawTags() []tiff.RawTag`** (NEW): a generic
  enumerator decoding every entry into a neutral exported struct
  (`tiff.RawTag{Number uint16; Type DataType; Count int; Raw []byte}`),
  using the page's `byteReader`. `internal/tiff` is internal — only the
  `opentile` package imports it.
- **`tiffTagProvider` interface** (opentile root, unexported):
  `tiffDirectories() []TIFFDirectory`. Implemented by the 8 TIFF-based
  readers. `Slide` accessors derive everything from this one method:
  `TIFFDirectoriesOf` returns the slice; `LevelTIFFTags(image, level)`
  finds the `Kind==DirLevel` entry matching the indices;
  `AssociatedTIFFTags(a)` matches on `a.Type()`.
- **Shared translation helper** (opentile root):
  `tiffTagsFrom([]tiff.RawTag) TIFFTags` — `tiff.DataType → TIFFType`,
  applies the name dictionary, drops the pixel-pointer denylist (D5).
- **Per-format `tiffDirectories()`** is small: each reader already
  classifies `file.Pages()` into levels/associated (it does this at Open
  for `Levels()`/`Associated()`); it maps that classification to
  `[]TIFFDirectory` and calls the shared translator. ~10–20 lines each.
- **Lazy**: `RawTags()` decode + translation happen only inside the
  accessor call, never at Open.
- **Non-TIFF** (IFE, SZI): readers don't implement `tiffTagProvider` →
  the type-assertion fails → accessors return `ok=false`.

## 5. Components / file structure

| File | Responsibility |
|---|---|
| `tiff_tags.go` (pkg `opentile`, NEW) | Public types (`TIFFType`, `Rational`, `TIFFTag`, `TIFFTags`, `TIFFDirectory`, `DirectoryKind`); the `tiffTagProvider` interface; `Slide` accessors; `TIFFDirectoriesOf`; `tiffTagsFrom` translator; the name dictionary; the pixel-pointer denylist. |
| `internal/tiff/page.go` (MODIFY) | Add `Page.RawTags() []RawTag`. |
| `internal/tiff/tag.go` (MODIFY) | Add exported `RawTag` struct + the per-type decode that fills `Raw`/typed values (reuse existing `Values`/`Values64`/ASCII/Rational decoders). |
| `formats/{svs,ndpi,philipstiff,ometiff,bif,generictiff,leicascn,cogwsi}/*.go` (MODIFY) | Each implements `tiffDirectories()` mapping its page classification → `[]TIFFDirectory`. |

## 6. Behavior details

- **Pixel-pointer denylist (D5):** tags 273/279/324/325 are filtered in
  `tiffTagsFrom`, so they never appear in any `TIFFTags`. `Tag`/`ByName`
  search only the enumerated set, so they won't return these.
- **Name dictionary (D6):** a static `map[uint16]string` of baseline TIFF
  tags plus common WSI vendor tags. Unknown ⇒ `Name == ""`.
- **`Raw` bytes (D4):** the verbatim value payload in file byte order
  (the bytes `Entry.Values`/`decodeExternal` read), so a re-encoder has
  exact fidelity even for types the typed getters don't model.
- **Multi-IFD-per-level:** where a semantic level maps to more than one
  IFD (format-specific), the reader returns the **primary** page's tags
  (the one backing `RawTile`). Documented per reader.
- **Out-of-range / wrong type:** accessors return `ok=false`; typed
  getters return `ok=false` on type mismatch. No panics.

## 7. Testing

- **Translation unit tests** (`tiff_tags_test.go`): `tiff.RawTag → TIFFTag`
  for each TIFF type (ASCII/SHORT/LONG/RATIONAL/SLONG/...); `Tag`/`ByName`
  lookup; name-dictionary hit/miss; pixel-pointer denylist excludes
  273/279/324/325.
- **`internal/tiff` unit test**: `Page.RawTags()` enumerates all entries
  with correct Number/Type/Count/Raw on a synthetic IFD.
- **Per-format integration** (fixtures): `LevelTIFFTags(0)` returns the
  expected `ImageDescription`(270) and a known vendor tag on SVS;
  `AssociatedTIFFTags` on a label image; `TIFFDirectoriesOf` includes an
  orphan IFD (NDPI Map page) as `DirOther`.
- **Non-TIFF**: IFE + SZI slides return `ok=false` from all accessors.
- **Fidelity cross-check**: a known tag's value matches what the parity
  oracle's tifffile runner reports for the same tag (optional, reuses
  `tests/oracle` infra under `-tags parity`).
- `make test` green under `-race`; `make cover` ≥ 80% for the new file.

## 8. Out of scope

- **TIFF write / re-encode** — opentile-go is a reader; faithful exposure
  only. wsitools consumes it.
- **Non-TIFF formats** (IFE, SZI) — no TIFF tags; `ok=false`.
- **Recovering pixel-pointer tags** (273/279/324/325) — excluded by D5;
  re-add behind an opt-in only if a real need appears.
- **A dedicated `tifftags` subpackage** — deferred to the v1.0 package
  reorg (roadmap #19/#20); for now the types live in the `opentile` root.

## 9. Rollout

Design covers all 8 TIFF formats. The plan sequences: land the substrate
(`tiff.RawTag` + `tiff_tags.go` + provider wiring) and the highest-use
readers first (SVS, NDPI, Philips, generic-TIFF), then the remainder
(OME, BIF, Leica-SCN, COG-WSI). Each reader is independently testable.

## 10. Risks

- **Per-format boilerplate** (8 readers) — mitigated by the shared
  translator; each reader only supplies its page→(Kind,indices) mapping,
  which it already computes.
- **Multi-IFD/level ambiguity** (NDPI page-series, OME SubIFDs) — resolved
  by "primary page" rule (D §6); covered by per-format tests.
- **`internal/tiff` surface growth** — one new method + one struct; kept
  minimal and generic (no per-tag accessors).
