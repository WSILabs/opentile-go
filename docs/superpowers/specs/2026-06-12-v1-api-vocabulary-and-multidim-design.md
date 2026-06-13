# v1.0 API Vocabulary, Receiver Restructure & Multi-Dimensional Model — Design

**Status:** Exploratory decision record. **Not approved for implementation.**
This captures a design conversation while the window is open (pre-1.0, no
external consumers depend on the surface yet). It records *what we'd change and
why*, the costs, and the sequencing — so the next person neither rips out
reserved scaffolding as dead nor builds it out blind.

**Date:** 2026-06-12

**Motivation:** The public surface has accreted across 39 minor releases. It
works, but it carries naming inconsistencies, two colliding `Image` types,
geometry-vocabulary sprawl, vestigial multi-dim scaffolding, and doc-rot. There
is no v1.0 ceremony and no API freeze, but the two real importers — `wsitools`
(Go CLI) and `openscope` (Electron viewer + Go sidecar) — make breaking renames
costly *once they're wired up*. The cheapest moment to fix vocabulary is now,
before more of it ossifies. Separately, the forward features openscope cares
about (fluorescence channels, focal-plane z-stacks) force a question about the
tile-read shape that is *expensive to retrofit* — so the addressing decision
should be made deliberately even though the axes themselves aren't built yet.

**Important constraint:** the correctness bar is fixture-backed parity. We
cannot faithfully build or test a z-stack / multi-channel decode path without a
real z-stack / multi-channel file. Speculative multi-dim code violates the
"no guessing — read upstream / verify against real files" invariant. We already
ran that experiment once (see §5.1) and it rotted.

---

## 1. Naming & self-documentation review (findings)

### 1.1 `AssociatedSource` → `AssociatedEncoding`

`AssociatedSource` (shipped v0.39.0, GH #22) is weakly named — "source" is
vague (source of what?) and the accessor had to be `AssociatedSourceOf`, whose
"Of" suffix exposes two real inconsistencies:

- It is the **only `*Slide` method with an "Of" suffix.** Across the surface,
  "Of" is reserved for *package-level functions that take a `*Slide`*
  (`TIFFDirectoriesOf(s)`, `bif.MetadataOf(s)`). Every *method* drops it
  (`AssociatedTIFFTags`, `AssociatedIFDOffset`, `LevelTIFFTags`,
  `BestLevelForDownsample`).
- The "Of" exists only to dodge a **name collision** with the type
  `AssociatedSource`. The collision is the symptom — the type is named after
  the concept the method wants.

**Decision:** rename the type to `AssociatedEncoding`, tying it to the
`Decode()`/encode duality. An associated image then has three faces:
`Bytes()` (raw payload) / `Decode()` (pixels) / the encoded re-emittable form.
The accessor becomes `a.Encoding()` once accessors move onto the interface
(§1.3), eliminating the prefix/"Of" question entirely.

**Within-struct fix (same pass):** the struct mixes a typed enum with magic
ints — `Compression Compression` next to `Predictor int`, `Photometric int`.
Type all three (`Predictor`, `Photometric` enums) to match `Compression`, *or*
consciously accept all three as raw TIFF tag values (the struct is explicitly
about writing TIFF tags). Either is defensible; the *mix* is the inconsistency.
Recommendation: type them, for symmetry with `Compression`.

### 1.2 `Slide.Associated()` → `AssociatedImages()`

The two sibling list-accessors are plural nouns — `Slide.Images()`,
`Slide.Levels()` — and `Slide.Associated()` sits between them as a lone
adjective ("associated *what*?"). The type is the full noun `AssociatedImage`;
the method eroded it. `AssociatedImages()` realigns with `Images()`/`Levels()`.
Value: context over brevity (per owner preference).

### 1.3 Associated-image accessors → onto the `AssociatedImage` interface

`AssociatedIFDOffset`, `AssociatedTIFFTags`, and the new encoding accessor live
on `*Slide` today, taking an `AssociatedImage` argument and dispatching through
the reader chain. But each `AssociatedImage` *impl already holds its own
reader*. Moving the accessors onto the interface kills the `Associated`-vs-
`AssociatedImage` prefix debate (methods become `a.Foo()`):

```go
type AssociatedImage interface {
    Type() AssociatedType                                        // §1.5
    Size() Size
    Compression() Compression
    Bytes() ([]byte, error)                                      // raw payload
    Decode(opts decoder.DecodeOptions) (*decoder.Image, error)   // pixels
    Encoding() (AssociatedEncoding, bool)                        // re-emittable
    TIFFTags() (TIFFTags, bool)
    IFDOffset() (int64, bool)
}
```

Cost: non-TIFF impls (DICOM, SZI, IFE) carry trivial `return _, false` stubs.
That is the Go-idiomatic price of optional capability and is acceptable.
`Slide.AssociatedImages()` then becomes the *only* Associated-named thing on
`Slide`, and a clean plural noun.

#### Where associated-image access lives (object model)

The whole associated-image surface collapses onto one branch of `Slide`
(`AssociatedImages()`) plus methods on the returned interface. `Slide` keeps
*no* associated-image accessors:

```
*Slide
│
├── Pyramids() ─────────► []*Pyramid          ┐
│   Pyramid(i)  ─────────► *Pyramid            │  pyramidal
│                            ├── Levels()       │  (multi-resolution)
│                            ├── Level(i) ──► *Level   path — tiles,
│                            ├── BestLevelForDownsample │  regions, Z/C/T
│                            ├── ReadRegionScaled      │
│                            └── ScaledStrips          ┘
│
├── AssociatedImages() ──► []AssociatedImage   ◄── non-pyramidal
│                                                   (slide-level) path
│   each AssociatedImage (interface):
│        ├── Type() AssociatedType        ── "label" / "overview" / …
│        ├── Size() Size
│        ├── Compression() Compression
│        ├── Bytes()      ──► []byte                       (raw payload)
│        ├── Decode(opts) ──► *decoder.Image               (pixels)
│        ├── Encoding()   ──► (AssociatedEncoding, bool)   ← was Slide.AssociatedEncoding(a)
│        ├── TIFFTags()   ──► (TIFFTags, bool)             ← was Slide.AssociatedTIFFTags(a)
│        └── IFDOffset()  ──► (int64, bool)                ← was Slide.AssociatedIFDOffset(a)
│
├── Metadata() ──► Metadata
├── ICCProfile() ──► []byte
├── Format() ──► Format
└── Close()
```

What moves — today (v0.40.0) vs the plan:

| | today (v0.40.0) | new plan |
|---|---|---|
| list | `slide.Associated()` | `slide.AssociatedImages()` |
| raw bytes | `a.Bytes()` | `a.Bytes()` *(already on `a`)* |
| decoded pixels | `a.Decode(opts)` | `a.Decode(opts)` *(already on `a`)* |
| encoded source | `slide.AssociatedEncoding(a)` | `a.Encoding()` *(moves onto `a`)* |
| TIFF tags | `slide.AssociatedTIFFTags(a)` | `a.TIFFTags()` *(moves onto `a`)* |
| source IFD offset | `slide.AssociatedIFDOffset(a)` | `a.IFDOffset()` *(moves onto `a`)* |

```go
// TODAY — accessor on Slide, image round-trips through it as an argument
for _, a := range slide.Associated() {
    enc, ok := slide.AssociatedEncoding(a)
    off, ok := slide.AssociatedIFDOffset(a)
}

// NEW PLAN — everything hangs off the image you already hold
for _, a := range slide.AssociatedImages() {
    enc, ok  := a.Encoding()
    off, ok  := a.IFDOffset()
    tags, ok := a.TIFFTags()
}
```

This is in the **coordinated-breaking bucket** (§8), not v0.40.0: moving the
three accessors onto the public `AssociatedImage` interface is breaking (it adds
interface methods), so it waits for the wsitools/openscope sign-off pass.

### 1.4 `opentile.Image` → `Pyramid` (resolve the `Image` collision)

`opentile.Image` is a pyramid group (`Name, Index, Levels`); `decoder.Image` is
a raster bitmap (`Pix, Stride`). Both are consumer-facing and used together
(`Slide.Images()` returns `[]opentile.Image`; `ReadRegion` returns
`*decoder.Image`). The doc literally says "Image identifies one pyramid group."

**Decision:** rename `opentile.Image` → `Pyramid`, `Slide.Images()` →
`Pyramids()`, `Slide.Image(i)` → `Pyramid(i)`. `decoder.Image` keeps the name
it deserves (it has pixels). Go's package qualifier already mitigated the
collision; the rename removes it. `Pyramid` also becomes the natural home for
the Z/C/T extent (§5).

Naming nuance: a multi-channel z-stack "pyramid" is really a pyramidal
hypercube. `Pyramid` names the resolution axis with dims hung off it; it still
reads fine and beats `Series` (which collides with DICOM's series == our
`Slide`).

### 1.5 Type the stringly / magic values

- `AssociatedImage.Type()` returns bare `string` with documented-but-unexported
  values → `type AssociatedType string` + exported consts
  (`AssociatedLabel`, `AssociatedOverview`, `AssociatedThumbnail`,
  `AssociatedMap`, `AssociatedProbability`, `AssociatedMacro`,
  `AssociatedGeneric`). The value set stays open (format-specific extensions),
  so a named-string-with-consts fits better than a closed enum. Consumers get
  compile-time help instead of hardcoded `"label"`. **Additive — free.**
- Unify MPP units: `Metadata.MicronsPerPixel float64` (microns) vs
  `Level.MPP SizeMm` (a *millimeter*-named type holding microns). One
  `type MPP struct{ X, Y float64 }` in microns, used by both; `SizeMm` stops
  moonlighting as microns.

### 1.6 Geometry-vocabulary unification

Today: `Point`, `Size`, `Region`, `TilePos`, `TileCoord`, **plus** stdlib
`image.Point` / `image.Rectangle` leaking into `ScaledStrips` and
`Level.TileOverlap`. Collapse to one set:

- **Delete `TilePos`** — structurally identical to `Point{X,Y}`; `Tiles()`
  yields `Point`.
- **Actually use `Region`** — region reads take `Region{Origin, Size}` instead
  of four bare ints.
- **Drop stdlib geometry from public signatures** — `ScaledStrips`,
  `TileOverlap` speak `Region`/`Point`/`Size`.
- Keep `TileCoord{X,Y,Z,C,T}` (distinct: multi-dim addressing, §5) and
  `Size`/`Point`.

### 1.7 Doc-rot & dead surface (cheap hygiene)

- `NewTestConfig` — marked *"Deprecated… removed in v0.4"*; we are at v0.39.
  Remove it or fix the comment.
- `ReadRegion` doc: *"use the v0.26 ScaledStrips iterator when it ships"* — it
  shipped. Same future-tense rot in the `decoder` package doc (*"designed to
  back v1.0's …"* — shipped pre-1.0).
- `CorruptTilePolicy`: `CorruptTileBlank` (v0.3) and `CorruptTileFix` (v1.0)
  are still unimplemented enum values; doc says "v0.1 supports only
  CorruptTileError." Implement, document, or drop.
- `ErrTileSizeRequired` — "Reserved for future use; currently unfired." Cruft
  to stay aware of.
- `TIFFDirectoriesOf(s)` → `s.TIFFDirectories()` if we collapse
  free-functions-taking-Slide onto Slide.

---

## 2. Receiver-method restructure

The biggest surface smell is the ~15 `Image*`-prefixed method *pairs*
(`ReadRegion`/`ImageReadRegion`, `RawTile`/`ImageRawTile`, …) — duplication
caused by threading "image index" and "level index" through `*Slide` as int
params (Go has no overloading / default args). Put the methods where the nouns
are:

- **Per-level reads on `*Level`** (give `Level` a back-ref to its Slide — still
  immutable-after-Open, so the lock-free hot-path invariant holds):
  `level.Tile(x,y)`, `level.DecodedTile(x,y,opts)`, `level.ReadRegion(r,opts)`,
  `level.Tiles(ctx)`, `level.TilePrefix()`, `level.Warm()`.
- **Cross-level / L0-coord reads on `*Pyramid`** (they choose a level
  internally): `pyramid.ReadRegionScaled(src, out, opts)`,
  `pyramid.ScaledStrips(src, out, stripH, opts)`,
  `pyramid.BestLevelForDownsample(d) *Level`.
- Bare `Slide.RawTile`/`ReadRegion`/etc. disappear — `slide.Level(0).Tile(...)`
  *is* the shortcut. ~40 Slide methods collapse to a handful.

### 2.1 Performance is neutral by construction

These are surface renames over internals that do not move. The v0.27–v0.30
machinery (NDPI decode-once-blit, decoder-handle pool, ReadRegion `sync.Pool`,
ScaledStrips workers, memory budget) is internal; `level.Tile()` is a renamed
thin wrapper over the same code path.

- Renames: identifier-only, byte-identical machine code.
- Receiver methods: `*Level`/`*Pyramid` are **concrete** types → static,
  inlinable dispatch (no vtable); one *fewer* step than today (`level.Tile()`
  already holds the resolved level, skipping `Images()[0].Levels[i]`). Back-ref
  set once at Open, not per read.
- `Region`/`TileCoord` value structs vs bare ints: small (16–40 B), stack-
  passed, escape-analyzed to not allocate; the extra copy is in the noise
  against a memcpy/JPEG-decode (µs–ms).

**Load-bearing rule:** *concrete types on the tile-read path; interfaces only
on the cold path.* `AssociatedImage` may be an interface (cold);
`*Level`/`*Pyramid`/`decoder.Image` must stay concrete so reads keep static
dispatch + inlining + escape-free value passing. The one change that *would*
regress is making `decoder.Image` an `image.Image`-style interface (§6).

**Risk is accidental, not inherent:** the restructure rewrites every internal
call site and the bench harness. Hazards: introducing an escape (storing
`&coord`), or breaking the v0.27 fast-path dispatch
(`s.r.(decodedTiler)` threaded through `fileCloser`/`mmapCloser` wrapper
delegation). Mitigation: route `level.DecodedTile` into the same internal entry
point, and re-run `make bench-ndpi` (≥270), `make bench-svs` (≥475),
`make bench-ndpi-mem`, and `-race` before/after. Any movement = an accidental
escape or broken dispatch, caught immediately.

---

## 3. The three faces of a tile (vocabulary going into §4–§6)

| Face | 2D method | Returns |
|---|---|---|
| raw bytes | `Tile(x,y)` | `[]byte` (compressed, as on disk) |
| decoded pixels | `DecodedTile(x,y,opts)` | `*decoder.Image` |
| reuse (zero-alloc) | `…Into(…, dst)` | count / writes into `dst` |

Associated images mirror this with `Bytes()` / `Decode()` / `Encoding()`.

---

## 4. Tile-read addressing: 2D-primary + nD-twin

### 4.1 Decision

Keep the common 2D case bare-int and clean; express multi-dimensional
addressing with a *separate* method that takes a coordinate, rather than
unifying everything under `Tile(TileCoord)`:

```go
// v1.0 — clean 2D, fixtured (brightfield)
func (l *Level) Tile(x, y int) ([]byte, error)
func (l *Level) DecodedTile(x, y int, opts ...DecodeOption) (*decoder.Image, error)

// later — additive, lands with first multi-channel/Z fixture
func (l *Level) TileAt(c TileCoord) ([]byte, error)
func (l *Level) DecodedTileAt(c TileCoord, opts ...DecodeOption) (*decoder.Image, error)
```

### 4.2 Why this over `Tile(TileCoord)` + `At(x,y)` constructor

- **Multi-dim becomes purely additive.** `TileAt` is a new method name; adding
  it never touches `Tile(x,y)`. So there is *no signature to reserve* and *no
  future break to dodge*. The unified `Tile(TileCoord)` would have forced
  baking the coordinate into the v1.0 signature pre-emptively (retrofitting
  `Tile(x,y)` → `Tile(coord)` later is breaking).
- **The 2D path is definitionally dimension-error-free.** `Tile(x,y)` always
  reads the nominal plane (Z=C=T=0), which always exists. `ErrDimensionUnavailable`
  lives only on `TileAt`, where an axis was explicitly addressed. A brightfield
  consumer cannot trip a dimension error.
- **Cost lands on the rare case, not the common one.** 99%+ of usage is 2D
  brightfield; `l.Tile(tx, ty)` pays nothing. The unified form taxed every 2D
  call with `l.Tile(opentile.At(tx, ty))` ceremony to serve a feature almost
  nobody uses yet (and, for T, may never).
- **Precedent:** the project already had `Level.Tile(x,y)` + `Level.TileAt`.
  The v0.7 removal was about *dormant nD accessors with no fixtures*, not about
  the split being wrong (§5.1).

### 4.3 Naming discipline

`At` binds the coordinate; `Into` binds the buffer; **`At` comes before
`Into`** — `DecodedTileAtInto(coord, dst, opts)`, never `DecodedTileIntoAt`.
Take `TileCoord`, never axis-in-the-name (`TileXYZTC` goes brittle against the
struct's field order). Field names in `TileCoord{X:.., Y:.., C:1}` self-
document.

---

## 5. Multi-dimensional model (Z / C / T)

### 5.1 Reserve the seam, defer the axes — we already ran this experiment

v0.7 built the multi-dim surface speculatively (`Image.SizeZ/SizeC/SizeT`,
`Level.TileAt`) with no fixtures. It rotted and was removed in the Slide
refactor — the `bfparity` oracle still references the *removed* `Image.SizeC`.
What survived the cull: `TileCoord`, `Level.FocalPlane`, `ErrDimensionUnavailable`
(harmless types). What rotted: the *dormant accessors*. That is the whole
lesson in miniature: a live addressing type is fine to keep; dormant navigation
accessors with no fixture are what to defer.

### 5.2 Axis taxonomy — they are not the same shape

| Axis | Nature | Geometry across axis | Home |
|---|---|---|---|
| **Z** focal planes | same FOV, different focus | identical | coordinate |
| **T** time | same FOV, different instant | identical | coordinate |
| **C** channels | co-registered, separate single-band rasters w/ own LUT | identical geometry, different *pixel type* | coordinate + descriptor |
| **independent images** (OME multi-image) | different FOV/stain | different | separate `Pyramid` |

**Principle:** Z/C/T share geometry → coordinates into a shared `Level`; only
genuinely-different-geometry images are separate `Pyramid`s. This is the line
OME draws (an OME "Image" = one pyramid + XYZCT planes) and the line `TileCoord`
already drew.

### 5.3 What ships when

- **v1.0 (no fixtures):** 2D `Tile`/`DecodedTile` (§4). Keep `TileCoord` as
  reserved *vocabulary* (a type, not a method). Ship nothing speculative.
- **Per axis, with its first fixture (additive, parity-tested):**
  `Pyramid.SizeZ/C/T()`, `Pyramid.FocalPlane(z)`, `Pyramid.Channel(c) Channel`
  (name, excitation/emission, suggested LUT color, bit depth), and the
  `TileAt`/`DecodedTileAt` family. The `ErrDimensionUnavailable` guard (reject
  `Z/C/T != 0` on a monoplane pyramid) is the testable seam — synthetic test
  today (pass `C=1`, expect the error), real dispatch slots in later.
- **Fixture gradient:** **C** is least speculative (Leica SCN already decodes
  per-channel internally; OME-TIFF is the canonical container) → likely first.
  **Z** plausible (cytology z-stacks, DICOM-deferred). **T** near-YAGNI for
  pathology (not live imaging) — reserve the always-zero field, spend no effort.

### 5.4 Resolve the existing inconsistency

`Level.FocalPlane` (a *structural* "this level is a focal plane") vs
`TileCoord.Z` (a *coordinate*) are two partial mechanisms. Pick one: retire
`Level.FocalPlane`, make Z a pure coordinate with focal-plane metadata on
`Pyramid`. Decide it in the redesign rather than carrying both.

### 5.5 Reader stays raw; compositing is the consumer's

Raw per-channel and per-Z reads are the reader's job; blending channels by LUT
or max-intensity-projecting a z-stack is a *consumer* concern (openscope's).
Keep the reader raw-only — `bands + descriptors` out, consumer composites —
consistent with the `Bytes()`/`Decode()` "faithful, no editorializing"
contract. The realistic compositing loop (read all C channels at a tile, blend
with each channel's LUT) lives in the consumer.

### 5.6 Homogeneous-channel assumption

The coordinate model assumes channels are co-registered, same-geometry (true
for essentially all real fluorescence WSI). Heterogeneous channels (different
bit depth/geometry) would instead be separate `Pyramid`s. Document the
homogeneous assumption; do not build for a case no scanner emits.

---

## 6. The pixel-model fork (the one place Go's type system shapes the design)

A fluorescence channel is **single-band**, frequently **16-bit**.
`decoder.Image{Format: RGB/RGBA, Pix []byte}` cannot represent it faithfully.
Multi-channel forces `decoder.Image` to grow. Go has no sum types, so the
choices are:

- **Flat struct + helpers (recommended):** add `Bands int` + a sample-type
  notion (`uint8`/`uint16`) to the existing struct; add typed accessor helpers
  (`band.Uint16At(x,y)`) to contain the runtime-typed `Pix []byte`
  reinterpretation. Brightfield path stays **byte-identical** (two fields set
  once per tile, decode loop unchanged, `sync.Pool` key gains one field). Keeps
  the lock-free, allocation-bounded tile hot path — a real asset earned over
  v0.27–v0.30.
- **Interface + concrete types (stdlib `image.Image` style) — rejected for the
  hot path:** `decoder.Image` interface with `*RGB8`, `*Gray16` variants is
  more type-safe and matches stdlib, but adds per-tile allocation, interface
  dispatch, and per-pixel `At()` calls on the hottest loop. This is exactly the
  cost the flat struct was chosen to avoid.

**Decision:** keep the flat struct; grow it with `Bands` + sample type +
helpers. This is the one v1.0 decision that is expensive to reverse, so it is
recorded explicitly. Brightfield/Z-stacks never need >8-bit or >1-band changes;
the expansion is isolated to the channel decode path and is net-new (no
baseline to regress).

---

## 7. The tile-read cube

"Into" (buffer reuse) and "2D vs nD" (addressing) are **orthogonal** axes.
Crossing them naively yields 8 methods:

| | alloc | into (reuse) |
|---|---|---|
| **raw 2D** | `Tile(x,y)` | `TileInto(x,y,dst)` |
| **raw nD** | `TileAt(c)` | `TileAtInto(c,dst)` |
| **decoded 2D** | `DecodedTile(x,y,opts)` | `DecodedTileInto(x,y,dst,opts)` |
| **decoded nD** | `DecodedTileAt(c,opts)` | `DecodedTileAtInto(c,dst,opts)` |

### 7.1 "Into" must stay a method — options allocate

The instinct to collapse the "into" column by folding `dst` into options
(`DecodedTile(x,y, WithDst(buf))`) is **wrong on perf grounds**: functional
options allocate (`WithDst(buf)` is a heap-escaping closure, ~1 alloc/call,
plus the variadic slice) — exactly the allocation `Into` exists to eliminate.
`DecodedTileInto(x,y,dst)` with an *empty* variadic is genuinely zero-alloc.
So the explicit `Into` method is the zero-alloc escape hatch and cannot be
merged into options. (`decoder.DecodeOptions.Dst` remains the *internal*
mechanism; the Slide-level zero-alloc surface is the explicit method.)

### 7.2 The cube is filled per cell as fixtures justify it

The §4 split makes the right-and-bottom of the cube additive and deferrable:

- **v1.0 ships the 2D quad only:** `Tile`, `TileInto`, `DecodedTile`,
  `DecodedTileInto`. Fixtured hot-path methods (DZI conversion, viewport
  serving).
- **nD lands with the first multi-channel fixture, additively:** `TileAt`,
  `DecodedTileAt`, and — *not* speculative — `DecodedTileAtInto`. Compositing a
  multiplex viewport (openscope blending N channels per tile across a region at
  interactive rates) is a zero-alloc hot loop: decode each channel into a reused
  `Gray16` buffer, blend, advance. That cell is a real need the moment channels
  are real.
- **`TileAtInto` (raw nD reuse) is the speculative corner** — only a multi-
  channel transcoder reading raw compressed channel bytes at volume needs it.
  Defer indefinitely; add on consumer request. No fixture, no method.

No cell is built speculatively; each is filled when its fixture justifies it,
and every cell past the 2D quad is a new method name (additive, non-breaking).

---

## 8. Sequencing

| Bucket | Items | Consumer impact |
|---|---|---|
| **Free now — SHIPPED v0.40.0** | rename `AssociatedSource`→`AssociatedEncoding`, `AssociatedSourceOf`→`AssociatedEncoding` (drop "Of"); `AssociatedType*` constants (**untyped** strings — see correction); doc-rot sweep (§1.7) | none |
| **Coordinated breaking pass** (sign-off w/ wsitools + openscope) | `Associated()`→`AssociatedImages()`; accessors onto the interface (§1.3); `opentile.Image`→`Pyramid`; geometry unification (§1.6); receiver-method restructure (§2); `decoder.Image` grows `Bands`+sample type (§6); **`MPP`-type unify** and **typed `Type() AssociatedType` return** (both *breaking* — see correction); `TIFFDirectoriesOf(s)`→`s.TIFFDirectories()` | both importers migrate once; one migration note |
| **Deferred, fixture-gated** | `TileAt`/`DecodedTileAt`/`DecodedTileAtInto`; `Pyramid.SizeZ/C/T`, `Channel`, `FocalPlane`; per-axis decode (C → Z → T) | additive, no break |
| **Leave** | `Image*` method fan-out *as a concept* is absorbed by §2; the `…Into` zero-alloc methods stay | — |

**Correction (post-execution, v0.40.0):** three items originally listed as
"free now" are actually *breaking* and were moved to the coordinated pass:
(a) **MPP-type unify** — changing `Level.MPP` from `SizeMm` to a new `MPP` type
breaks consumers reading `level.MPP.W`; there is no additive form. (b) **Typed
`Type()` return** — changing `AssociatedImage.Type() string` → `AssociatedType`
breaks consumers storing/passing the result as `string`, so v0.40.0 shipped the
constants as *untyped* strings (compare directly against `a.Type()`) and the
typed return is deferred. (c) `TIFFDirectoriesOf`→method is a rename, breaking.

**Locked in v0.40.0:** `AssociatedSource`→`AssociatedEncoding` and
`AssociatedSourceOf`→`AssociatedEncoding(a)` shipped now (interim Slide-method
form). wsitools was notified on GH #22 with the final name so it wires up once;
the breaking pass later moves the accessor to the interface (`a.Encoding()`).

---

## 9. Open questions — RESOLVED (2026-06-12)

All five resolved with the owner. Decisions:

1. **Receiver-method restructure: YES.** Tile/region reads move off `*Slide`
   onto `*Level` (per-level: `Tile`/`DecodedTile`/`ReadRegion`/`Tiles`) and
   `*Pyramid` (cross-level, L0-coord: `ReadRegionScaled`/`ScaledStrips`/
   `BestLevelForDownsample`). Collapses ~40 `Slide` methods + the ~15 `Image*`
   pairs; perf-neutral (concrete types, static dispatch, immutable back-refs).
   The largest item in the breaking pass.
2. **Container name: `Pyramid`** (was `opentile.Image`). Names the resolution
   structure; Z/C/T hang off it. Beats `Series` (DICOM collision) and a
   dim-neutral name (vaguer). Accessors onto the `AssociatedImage` interface
   (`a.Encoding()`/`a.TIFFTags()`/`a.IFDOffset()`) confirmed over
   long Slide-method names.
3. **`AssociatedEncoding.Predictor`/`Photometric`: raw `int`** (documented as
   TIFF tag 317/262 values). `Compression` stays typed because it is a
   first-class API concept used everywhere; `Predictor`/`Photometric` appear in
   exactly one struct, so two new public enum types would earn their keep
   nowhere else. The asymmetry is correct, not a wart.
4. **`decoder.Image` channel pixels: `Bands int` + `Sample` type**
   (`Uint8`/`Uint16`) on the flat struct — NOT `PixelFormatGray8/Gray16`. Two
   orthogonal axes compose (`{Bands:1,Sample:Uint16}` channel,
   `{Bands:3,Sample:Uint8}` brightfield) and avoid the `PixelFormat`
   combinatorial blowup. Brightfield stays byte-identical; pool key gains two
   fields. Directional — built only when a real channel fixture exists.
5. **Region plane selection: `ReadRegion…At(r Region, p Plane{Z,C,T})`** — plane
   is part of the address (mirrors `TileAt`), NOT a `DecodeOption`. `Format`/
   `Scale` are "how to render"; Z/C/T are "which pixels." Deferred behind a
   channel fixture; this locks the direction only.

---

## 10. Not approved — next steps

This is a decision record, not a plan. Nothing here is implemented. Before any
code:

1. Owner reviews and accepts/edits the §1–§7 decisions.
2. The "free now" bucket (§8) can proceed as a small additive PR independently.
3. The "coordinated breaking pass" needs a `writing-plans` plan and sign-off
   from wsitools + openscope, since both import the package.
4. The deferred multi-dim work is gated on fixtures and tracked per axis
   (C → Z → T); each lands as its own fixtured, parity-tested addition.
