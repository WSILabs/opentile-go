# Decoder codestream Inspect — design (#41)

> **Naming note:** this shipped in v0.42.0 as `decoder.Prober` / `Probe`, then
> was renamed to `decoder.CodestreamInspector` / `Inspect` immediately after
> (before any consumer adopted it) for self-documentation. This document uses
> the current names.

**Goal:** expose codec-domain metadata about an encoded tile/frame
(components, bit depth, reversibility, color encoding, boxed-vs-raw)
**without fully decoding it**, so a frame-copying consumer (wsitools
`convert --to dicom`) can derive a DICOM TransferSyntax +
PhotometricInterpretation per tile without re-decoding or re-implementing a
codestream parser — especially for JPEG XL, where a from-scratch parser is
non-trivial.

## API (additive, non-breaking)

A new **optional** interface on `decoder.Factory`. Codecs opt in; consumers
type-assert (the codebase's established optional-capability pattern). Inspect
lives on the `Factory` (stateless header parse — no `New()` lifecycle):

```go
type CodestreamInspector interface { Inspect(src []byte) (CodestreamInfo, error) }

type CodestreamInfo struct {
    Components    int           // sample/channel count
    BitDepth      int           // bits per component
    Lossless      Lossless      // tri-state (see below)
    ColorEncoding ColorEncoding // Unknown/Grayscale/RGB/YCbCr/YBR_ICT/YBR_RCT
    Boxed         bool          // boxed container vs raw codestream
}

type Lossless uint8       // LosslessUnknown | LosslessYes | LosslessNo
type ColorEncoding uint8  // Color{Unknown,Grayscale,RGB,YCbCr,YBRICT,YBRRCT}
```

Consumer path:

```go
f, _ := decoder.GetByCompressionTag(tag)
if p, ok := f.(decoder.CodestreamInspector); ok { info, err := p.Inspect(src) }
```

**Decisions**

- **Optional interface, not a Factory-interface method** — adding `Inspect`
  to `Factory` would break every existing factory. `CodestreamInspector` is opt-in; codecs
  without a meaningful header (`none`/`lzw`/`deflate`/`webp`/`avif`) simply
  don't implement it (assertion `ok == false`), which is the idiomatic
  "unsupported".
- **Lossless is tri-state.** libjxl's `JxlBasicInfo` carries **no** header-only
  reversibility flag (the issue assumed it did). J2K/HTJ2K report Yes/No from
  the COD transform and JPEG baseline is always lossy, but **JXL reports
  `LosslessUnknown`** — honest rather than silently wrong.
- **Inspect on `Factory`** (stateless), not `Decoder` — no instance needed; the
  consumer already holds the factory via `Get`/`GetByCompressionTag`.
- **Color encoding is codec-domain** (how samples are stored, what a frame-copy
  preserves), not display intent: JP2K MCT → YBR_RCT (reversible) / YBR_ICT
  (irreversible); no MCT → RGB (or grayscale/sYCC from a JP2 `colr` box).

## Per-codec implementation

| codec | header source | notes |
|---|---|---|
| jpeg | `tjDecompressHeader3` (already linked) | components/color from colorspace; 8-bit; always lossy; raw |
| jpeg2000 | pure-Go `internal/j2kheader` (SIZ + COD + JP2 boxes) | reversibility from COD transform (5/3 vs 9/7); MCT → RCT/ICT; boxed from JP2 signature |
| htj2k | same `internal/j2kheader` | HTJ2K SIZ/COD are the same J2K-family markers |
| jpegxl | new libjxl `wsi_jxl_inspect` shim (`JxlBasicInfo`, stops at `JXL_DEC_BASIC_INFO`) | channels/bits from basic info; boxed from JXL signature box; **Lossless = Unknown** |

`internal/j2kheader` is a pure-Go, library-version-independent SIZ/COD/box
parser shared by the jpeg2000 and htj2k decoders (which have separate
`nojp2k`/`nohtj2k` build tags), avoiding OpenJPEG/openjph quirks and matching
what wsitools already does for SIZ/COD. It exposes `Info.CodestreamInfo()` so
the two codecs share the mapping.

## Build tags / availability

`Inspect` lives in each codec's cgo file, so it is present only when that codec
is built. A compiled-out codec (`nojp2k`/`nohtj2k`/`nojxl`/`nocgo`) leaves the
factory unregistered or an Inspect-less stub, so the consumer's `CodestreamInspector` assertion
reports `ok == false` — a clear "can't inspect", not a link failure. The pure-Go
`CodestreamInfo`/`CodestreamInspector`/enum types in `decoder/codestream.go` always compile.

## Verification

- Per-codec inspect tests (jpeg synthetic, jpeg2000 committed `.j2k` testdata,
  htj2k via the encode helper, jpegxl via a committed raw tile).
- The `decoder/all` no-panic-on-garbage matrix is extended to fuzz `Inspect`.
- End-to-end on the real DICOM acceptance fixtures: 3DHISTECH HTJ2K and JP2K
  frames inspect to 3 components / 8-bit / reversible (lossless) / RGB / raw —
  matching the issue's acceptance criterion — without a full decode.

## Out of scope (this cut)

- JXL `lossless` (no header-only signal in libjxl) and JXL color beyond
  channel-count-derived Grayscale/RGB.
- ICC presence, subsampling, tiling — room to grow on `CodestreamInfo`.
- The `CodestreamInfo → DICOM` mapping stays in the consumer (wsitools).
