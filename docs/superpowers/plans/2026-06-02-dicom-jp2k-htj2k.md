# DICOM JP2K + HTJ2K decode — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decode JPEG 2000 (Part 1) and HTJ2K DICOM WSM levels + associated images, verified against a Python wsidicom pixel oracle.

**Architecture:** The decode dispatch (`Compression → CompressionToTIFFTag → decoderFor → registry`) and the OpenJPEG/openjph decoders already exist. The only new code is in `formats/dicom/` (extend `compressionForSyntax`, plus a color-transform contingency), a new `tests/oracle` wsidicom harness, and fixture wiring. JP2K is implemented and verified first; HTJ2K is a one-mapping delta after.

**Tech Stack:** Go 1.23+, cgo (OpenJPEG via `decoder/jpeg2000`, openjph via `decoder/htj2k`), `github.com/suyashkumar/dicom` (cold-path parse), Python `wsidicom` (oracle).

**Spec:** `docs/superpowers/specs/2026-06-02-dicom-jp2k-htj2k-design.md`

---

## File Structure

- `formats/dicom/associated.go` — modify `compressionForSyntax` (the single transfer-syntax → `Compression` switch).
- `formats/dicom/associated_test.go` — add unit cases for the new UID mappings.
- `formats/dicom/photometric.go` — **create only if** Task 6 finds the decoder doesn't resolve color; holds the YBR→RGB contingency.
- `tests/oracle/wsidicom_session.go` — create; long-lived wsidicom subprocess (mirrors `openslide_session.go`).
- `tests/oracle/wsidicom_runner.py` — create; decodes a tile via wsidicom, emits raw RGB (mirrors `openslide_runner.py`).
- `tests/oracle/wsidicom_test.go` — create; build-tagged pixel-parity test.
- `tests/oracle/requirements.txt` — modify; pin `wsidicom` + codec backend.
- `tests/integration_test.go` — modify; register the new JP2K + HTJ2K fixtures.
- `tests/generate_test.go` — modify; add the new fixture stems to the generate switch.
- `sample_files/dicom/<jp2k-name>/`, `sample_files/dicom/<htj2k-name>/` — fixtures (gitignored; snapshots committed).
- `docs/formats/dicom.md` — modify; move JP2K/HTJ2K from "not supported" to "supported".
- `CHANGELOG.md` — modify; stamp v0.33.

---

## Task 1: Acquire + register a JP2K DICOM WSM fixture

**This is the gating task** — the parity bar needs a real fixture. It is the one human-in-loop step; if automated acquisition fails, surface to the controller rather than fabricating data.

**Files:**
- Create: `sample_files/dicom/JP2K-1/` (the `.dcm` instance(s))
- Modify: `tests/integration_test.go` (fixture list), `tests/generate_test.go` (switch)

- [ ] **Step 1: Obtain a JP2K WSM file.** Preferred — a real instance from NCI Imaging Data Commons. Using the `idc-index` helper:

```bash
python3 -m pip install --upgrade idc-index
python3 - <<'PY'
from idc_index import IDCClient
c = IDCClient()
# Find a small brightfield WSM series compressed with JPEG 2000 (.90/.91).
df = c.index
wsm = df[df.Modality == "SM"]
print(wsm[["SeriesInstanceUID","collection_id","series_size_MB"]].sort_values("series_size_MB").head(20))
PY
# Then download one small series by UID:
# python3 -c "from idc_index import IDCClient; IDCClient().download_from_selection(seriesInstanceUID='<UID>', downloadDir='sample_files/dicom/JP2K-1')"
```

Fallback if no suitable IDC series is reachable — transcode an existing fixture with GDCM (bundles OpenJPEG):

```bash
# brew install gdcm  (or apt-get install libgdcm-tools)
gdcmconv --j2k \
  sample_files/dicom/3DHISTECH-1/<one>.dcm \
  sample_files/dicom/JP2K-1/3DHISTECH-1-j2k.dcm
```

- [ ] **Step 2: Verify it opens and reports JP2K.** Confirm the transfer syntax is `1.2.840.10008.1.2.4.90` or `.91`:

```bash
python3 -c "import pydicom,glob; ds=pydicom.dcmread(sorted(glob.glob('sample_files/dicom/JP2K-1/*.dcm'))[0]); print(ds.file_meta.TransferSyntaxUID, ds.get('PhotometricInterpretation'))"
```

Expected: prints `1.2.840.10008.1.2.4.91` (or `.90`) and a photometric value (`YBR_ICT` / `RGB`).

- [ ] **Step 3: Register the fixture stem** in `tests/integration_test.go` — add `"JP2K-1"` to the DICOM fixture name list (alongside `"scan_621_grundium_dicom"` etc.), and in `tests/generate_test.go` add `"JP2K-1"` to the DICOM case of the generate switch (line ~67: `case "Leica-4", "3DHISTECH-1", "scan_621_grundium_dicom":`).

- [ ] **Step 4: Commit the registration** (fixture bytes are gitignored; snapshot comes in Task 4).

```bash
git add tests/integration_test.go tests/generate_test.go
git commit -m "test(dicom): register JP2K-1 fixture stem"
```

---

## Task 2: Map JP2K transfer syntaxes to CompressionJP2K

**Files:**
- Modify: `formats/dicom/associated.go:69` (`compressionForSyntax`)
- Test: `formats/dicom/associated_test.go`

- [ ] **Step 1: Write the failing test.** Add to `associated_test.go`:

```go
func TestCompressionForSyntax_JP2K(t *testing.T) {
	for _, ts := range []string{
		"1.2.840.10008.1.2.4.90", // JPEG 2000 Lossless Only
		"1.2.840.10008.1.2.4.91", // JPEG 2000
	} {
		if got := compressionForSyntax(ts); got != opentile.CompressionJP2K {
			t.Errorf("compressionForSyntax(%q) = %v, want CompressionJP2K", ts, got)
		}
	}
}
```

- [ ] **Step 2: Run it; verify it fails.**

Run: `go test ./formats/dicom/ -run TestCompressionForSyntax_JP2K -v`
Expected: FAIL (currently returns the best-effort JPEG default).

- [ ] **Step 3: Add the JP2K cases** to the switch in `compressionForSyntax`:

```go
	case "1.2.840.10008.1.2.4.90", // JPEG 2000 Image Compression (Lossless Only)
		"1.2.840.10008.1.2.4.91": // JPEG 2000 Image Compression
		return opentile.CompressionJP2K
```

(Insert before the `default:` clause; keep alongside the existing `…4.50` JPEG case.)

- [ ] **Step 4: Run it; verify it passes.**

Run: `go test ./formats/dicom/ -run TestCompressionForSyntax_JP2K -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add formats/dicom/associated.go formats/dicom/associated_test.go
git commit -m "feat(dicom): map JPEG 2000 transfer syntaxes to CompressionJP2K"
```

---

## Task 3: Verify JP2K frame extraction parity (codec-agnostic)

Confirms the existing fragment-walk extracts the JP2K compressed bytes correctly, independent of decoding.

**Files:**
- Test: `formats/dicom/parity_test.go` (add a case) — or a new `formats/dicom/jp2k_test.go`

- [ ] **Step 1: Write the failing test** in a new `formats/dicom/jp2k_test.go`. It opens the JP2K-1 fixture (skip if absent) and asserts the level reports JP2K compression + RawTile is non-empty and starts with a JPEG 2000 codestream/SOC marker (`FF 4F FF 51`):

```go
package dicom_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func jp2kFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(testdataDir(t), "dicom", "JP2K-1")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("JP2K-1 fixture absent")
	}
	return dir
}

func TestDICOMJP2KExtraction(t *testing.T) {
	s, err := opentile.OpenFile(jp2kFixture(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	lvl := s.Images()[0].Levels()[0]
	if lvl.Compression() != opentile.CompressionJP2K {
		t.Fatalf("compression = %v, want JP2K", lvl.Compression())
	}
	b, err := s.RawTile(0, 0, 0)
	if err != nil {
		t.Fatalf("rawtile: %v", err)
	}
	// JPEG 2000 codestream begins SOC (FF4F) then SIZ (FF51).
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0x4F || b[2] != 0xFF || b[3] != 0x51 {
		t.Fatalf("RawTile not a J2K codestream: % x", b[:min(8, len(b))])
	}
}
```

(Use the existing `testdataDir`/`Images()[0].Levels()[0]` helpers as the other `formats/dicom` tests do; match their exact signatures when implementing.)

- [ ] **Step 2: Run it.**

Run: `OPENTILE_TESTDIR="$PWD/sample_files" go test ./formats/dicom/ -run TestDICOMJP2KExtraction -v`
Expected: PASS if fixture present (extraction is codec-agnostic, already works); SKIP if absent.

- [ ] **Step 3: If FF4F assertion fails** because the scanner stores the JP2 box format (`00 00 00 0C 6A 50 …`) rather than a raw codestream, note it — the decoder must accept that form. Add a comment recording which form the fixture uses; do not change extraction (the bytes are passed through verbatim per the byte-passthrough invariant).

- [ ] **Step 4: Commit.**

```bash
git add formats/dicom/jp2k_test.go
git commit -m "test(dicom): JP2K frame-extraction parity + codestream-form check"
```

---

## Task 4: Generate the JP2K fixture snapshot + integration parity

**Files:**
- Create: `sample_files/dicom/JP2K-1.dicom.json` (generated)

- [ ] **Step 1: Generate the snapshot** for the new fixture:

```bash
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests -tags generate -run TestGenerateFixtures -generate -v
```

Expected: writes/updates `sample_files/dicom/JP2K-1.dicom.json` with tile SHAs + geometry.

- [ ] **Step 2: Run TestSlideParity** to confirm the fixture round-trips against its snapshot:

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -v
```

Expected: PASS including the `JP2K-1` case.

- [ ] **Step 3: Commit the snapshot.**

```bash
git add sample_files/dicom/JP2K-1.dicom.json
git commit -m "test(dicom): JP2K-1 fixture snapshot + TestSlideParity"
```

---

## Task 5: Build the wsidicom pixel oracle harness

**Files:**
- Create: `tests/oracle/wsidicom_runner.py`, `tests/oracle/wsidicom_session.go`, `tests/oracle/wsidicom_test.go`
- Modify: `tests/oracle/requirements.txt`

- [ ] **Step 1: Pin wsidicom** in `tests/oracle/requirements.txt` — add (with a known-good version + JP2K codec backend):

```
wsidicom>=0.20
pylibjpeg-openjpeg
```

- [ ] **Step 2: Write the runner** `tests/oracle/wsidicom_runner.py` — reads a request (slide dir, level, tile x/y) on stdin, decodes via wsidicom, writes raw RGB bytes to stdout. Mirror the request/response framing of `openslide_runner.py` exactly (read its protocol first and match it):

```python
import sys, struct
from wsidicom import WsiDicom

def main():
    slide_dir = sys.argv[1]
    wsi = WsiDicom.open(slide_dir)
    # Protocol: match openslide_runner.py — read "level x y w h" lines on stdin,
    # respond with a 4-byte big-endian length then raw RGB bytes.
    for line in sys.stdin:
        level, x, y, w, h = map(int, line.split())
        region = wsi.read_region((x, y), level, (w, h))  # PIL RGB
        raw = region.convert("RGB").tobytes()
        sys.stdout.buffer.write(struct.pack(">I", len(raw)))
        sys.stdout.buffer.write(raw)
        sys.stdout.buffer.flush()

if __name__ == "__main__":
    main()
```

(When implementing, open `openslide_runner.py` and copy its exact stdin parsing + length framing so `wsidicom_session.go` can reuse the same decode loop.)

- [ ] **Step 3: Write the session wrapper** `tests/oracle/wsidicom_session.go` — copy `openslide_session.go` structure verbatim, renaming `Openslide*` → `Wsidicom*`, pointing at `wsidicom_runner.py`, honoring `OPENTILE_ORACLE_PYTHON` via the existing `oracle.PythonBin()` helper. Keep the same build tag as the other oracle files (`//go:build parity` — confirm by reading the tag on `openslide_session.go`).

- [ ] **Step 4: Write the parity test** `tests/oracle/wsidicom_test.go` (build-tagged) — for the JP2K-1 fixture, decode tile (0,0) at level 0 via both opentile-go `DecodedTile` and the wsidicom session, compare per-channel within a tolerance (lossy JP2K):

```go
//go:build parity

package oracle

import "testing"

func TestWsidicomJP2KParity(t *testing.T) {
	dir := requireFixture(t, "dicom", "JP2K-1") // skip if absent — match existing helper
	sess := NewWsidicomSession(t, dir)
	defer sess.Close()

	got := decodeOpentileTile(t, dir, 0, 0, 0) // []byte RGB — use the existing oracle helper
	want := sess.ReadTile(t, 0, 0, 0)

	const tol = 4 // per-channel, lossy JP2K (YBR_ICT round-trip)
	assertPixelsWithinTolerance(t, got, want, tol) // reuse openslide_test.go's comparator
}
```

(Reuse the pixel-comparison + fixture-skip helpers already in `openslide_test.go`/`oracle.go`; match their exact names when implementing.)

- [ ] **Step 5: Run the oracle.**

```bash
OPENTILE_ORACLE_PYTHON=/path/to/venv/bin/python \
OPENTILE_TESTDIR="$PWD/sample_files" \
  go test ./tests/oracle/... -tags parity -run TestWsidicomJP2KParity -v
```

Expected: PASS if our decoded pixels match wsidicom within tolerance. **If FAIL → Task 6.**

- [ ] **Step 6: Commit.**

```bash
git add tests/oracle/wsidicom_runner.py tests/oracle/wsidicom_session.go tests/oracle/wsidicom_test.go tests/oracle/requirements.txt
git commit -m "test(oracle): wsidicom JP2K pixel-parity harness"
```

---

## Task 6: Color/photometric decision gate (contingency)

**Run only if Task 5 Step 5 FAILED.** If it passed, the decoder resolves color from the codestream — skip to Task 7 and record "decoder handles MCT; no transform needed" in the spec's §6.

**Files:**
- Create: `formats/dicom/photometric.go`
- Test: `formats/dicom/photometric_test.go`

- [ ] **Step 1: Read upstream first.** Read wsidicom's decode path (how it handles `PhotometricInterpretation` `YBR_ICT`/`YBR_RCT`/`RGB` for JP2K) and DICOM PS3.5 §8.2.4. Determine whether the mismatch is (a) a missing YBR→RGB conversion, (b) an OpenJPEG color-channel ordering issue, or (c) an MCT-applied-twice issue. Record the finding as a comment in `photometric.go`.

- [ ] **Step 2: Write the failing test** `formats/dicom/photometric_test.go` asserting the corrected pixel for a known input (use the wsidicom-expected RGB from a single tile captured in Task 5):

```go
func TestPhotometricYBRICTToRGB(t *testing.T) {
	// in = decoder output for a YBR_ICT JP2K tile; want = wsidicom RGB.
	in := []byte{ /* captured Y,Cb,Cr (or mis-ordered RGB) sample */ }
	want := []byte{ /* wsidicom RGB sample */ }
	got := applyPhotometric("YBR_ICT", in)
	for i := range want {
		if abs(int(got[i])-int(want[i])) > 2 {
			t.Fatalf("byte %d: got %d want %d", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 3: Implement `applyPhotometric`** in `photometric.go` — the conversion identified in Step 1, gated on `Instance.Photometric` (already parsed). Wire it into the DICOM decode path so it runs only for DICOM JP2K/HTJ2K levels whose photometric needs it. Keep it out of the generic decoder (format-specific quirk → format package, per the placement invariant).

- [ ] **Step 4: Run unit + oracle.**

```bash
go test ./formats/dicom/ -run TestPhotometric -v
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests/oracle/... -tags parity -run TestWsidicomJP2KParity -v
```

Expected: both PASS.

- [ ] **Step 5: Commit.**

```bash
git add formats/dicom/photometric.go formats/dicom/photometric_test.go formats/dicom/*.go
git commit -m "fix(dicom): apply photometric transform for JP2K color correctness"
```

---

## Task 7: HTJ2K — mapping delta + fixture + oracle

**Files:**
- Modify: `formats/dicom/associated.go` (`compressionForSyntax`), `formats/dicom/associated_test.go`
- Modify: `tests/integration_test.go`, `tests/generate_test.go`
- Create: `sample_files/dicom/HTJ2K-1/` + `sample_files/dicom/HTJ2K-1.dicom.json`

- [ ] **Step 1: Write the failing mapping test** in `associated_test.go`:

```go
func TestCompressionForSyntax_HTJ2K(t *testing.T) {
	for _, ts := range []string{
		"1.2.840.10008.1.2.4.201", // HTJ2K Lossless Only
		"1.2.840.10008.1.2.4.202", // HTJ2K with RPCL Options (Lossless Only)
		"1.2.840.10008.1.2.4.203", // HTJ2K
	} {
		if got := compressionForSyntax(ts); got != opentile.CompressionHTJ2K {
			t.Errorf("compressionForSyntax(%q) = %v, want CompressionHTJ2K", ts, got)
		}
	}
}
```

- [ ] **Step 2: Run it; verify it fails.**

Run: `go test ./formats/dicom/ -run TestCompressionForSyntax_HTJ2K -v`
Expected: FAIL.

- [ ] **Step 3: Add the HTJ2K cases** to `compressionForSyntax`:

```go
	case "1.2.840.10008.1.2.4.201", // HTJ2K (Lossless Only)
		"1.2.840.10008.1.2.4.202", // HTJ2K with RPCL Options (Lossless Only)
		"1.2.840.10008.1.2.4.203": // HTJ2K Image Compression
		return opentile.CompressionHTJ2K
```

- [ ] **Step 4: Run it; verify it passes.**

Run: `go test ./formats/dicom/ -run TestCompressionForSyntax_HTJ2K -v`
Expected: PASS.

- [ ] **Step 5: Acquire the HTJ2K fixture.** Prefer a real IDC HTJ2K WSM (repeat Task 1 Step 1 filtering for `.201`/`.203`). If none is reachable, transcode — encode an existing fixture's frames to HTJ2K with openjph and re-encapsulate via pydicom:

```bash
# ojph_compress (from openjph) produces .jph HTJ2K codestreams; a pydicom
# script re-wraps them as encapsulated PixelData with TransferSyntaxUID .203.
# Document the exact script in sample_files/dicom/HTJ2K-1/MAKE.md when used.
```

Place under `sample_files/dicom/HTJ2K-1/`; verify with the Task 1 Step 2 pydicom check (expect `1.2.840.10008.1.2.4.203` or `.201`). If only a synthetic fixture is available, note "synthetic — upgrade to real when available" in `MAKE.md`.

- [ ] **Step 6: Register + snapshot** — add `"HTJ2K-1"` to `tests/integration_test.go` and `tests/generate_test.go`, then regenerate:

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -tags generate -run TestGenerateFixtures -generate -v
```

- [ ] **Step 7: Add the HTJ2K oracle case** `TestWsidicomHTJ2KParity` in `wsidicom_test.go` (copy `TestWsidicomJP2KParity`, point at `HTJ2K-1`; lossless `.201` → `tol = 0`, lossy `.203` → `tol = 4`). Run it:

```bash
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests/oracle/... -tags parity -run TestWsidicomHTJ2KParity -v
```

Expected: PASS. (HTJ2K reuses the Task 6 photometric path if it was needed.)

- [ ] **Step 8: Commit.**

```bash
git add formats/dicom/associated.go formats/dicom/associated_test.go tests/integration_test.go tests/generate_test.go tests/oracle/wsidicom_test.go sample_files/dicom/HTJ2K-1.dicom.json
git commit -m "feat(dicom): HTJ2K transfer-syntax support + oracle + fixture"
```

---

## Task 8: nocgo/nohtj2k guards, docs, version stamp

**Files:**
- Modify: `docs/formats/dicom.md`, `CHANGELOG.md`

- [ ] **Step 1: Verify the `nohtj2k` build still compiles** (HTJ2K decoder tagged out — the DICOM mapping returns `CompressionHTJ2K`, decode must return the standard "no decoder registered" error, not a build break):

```bash
go build -tags nohtj2k ./...
go test -tags nohtj2k ./formats/dicom/ -run TestCompressionForSyntax -v
```

Expected: build OK; mapping tests still PASS (mapping is tag-independent). Add a test asserting a `CompressionHTJ2K` DICOM level returns `ErrUnsupportedCompression` (or the registry's not-found error) under `nohtj2k`, matching how other codecs degrade.

- [ ] **Step 2: Verify the `nocgo` build** returns `ErrCGORequired` on the JP2K decode path (unchanged behavior, just confirm):

```bash
CGO_ENABLED=0 go build ./...
```

Expected: build OK.

- [ ] **Step 3: Update `docs/formats/dicom.md`** — move JP2K + HTJ2K rows from "What's not supported" to "What's supported"; update the milestone note. Specifically edit the row `| JP2K / HTJ2K / JPEG-LS / RLE transfer syntaxes | ❌ deferred …` to reflect JP2K + HTJ2K now supported, JPEG-LS/RLE still deferred. Add a line recording the color resolution outcome from Task 5/6.

- [ ] **Step 4: Stamp `CHANGELOG.md`** with a v0.33 entry: "DICOM JPEG 2000 + HTJ2K transfer-syntax decode; wsidicom pixel oracle."

- [ ] **Step 5: Full suite under race.**

```bash
make test
OPENTILE_TESTDIR="$PWD/sample_files" go test ./tests -run TestSlideParity -count=1
```

Expected: all green.

- [ ] **Step 6: Commit.**

```bash
git add docs/formats/dicom.md CHANGELOG.md formats/dicom/
git commit -m "docs(dicom): JP2K+HTJ2K supported; nohtj2k/nocgo guards; v0.33"
```

---

## Task 9: Update roadmap to-do + finish

**Files:**
- Modify: memory `project_roadmap.md` / repo backlog (record the still-deferred items).

- [ ] **Step 1: Record the explicit to-do** (multi-fragment-per-frame, Basic/Extended Offset Table, JPEG-LS/RLE, JP2K Part 2 multi-component, raw DICOM-attribute API, multi-optical-path/Z-stack/concatenations) in the roadmap so they aren't lost.

- [ ] **Step 2: Finishing the branch** — use `superpowers:finishing-a-development-branch` to merge `feat/dicom-jp2k-htj2k` → `main`, confirm CI green (mac + linux), and check the post-merge run per the project's post-merge-CI habit.

---

## Self-Review

- **Spec coverage:** §4 syntaxes → Tasks 2 + 7. §5 mapping surface → Task 2/7. §6 color → Task 5 gate + Task 6 contingency. §7 fixtures → Tasks 1 + 7. §8 oracle → Task 5. §9 to-do → Task 9. §10 sealed decisions all reflected (one milestone, decoder reuse, oracle this phase, scope = Part 1 + HTJ2K, extraction unchanged/error-on-multi-fragment, color is the deliverable). §11 risks each have a task touchpoint.
- **Placeholders:** the only conditional is Task 6 (explicitly gated on Task 5's empirical result — that is a real decision gate, not a placeholder; both branches specify concrete actions). Fixture acquisition (Tasks 1, 7) documents concrete commands with verification criteria; its human-in-loop nature is inherent, not a gap.
- **Type consistency:** `compressionForSyntax(string) opentile.Compression`, `opentile.CompressionJP2K`/`CompressionHTJ2K`, `Instance.Photometric`, `OpenFile`, `RawTile`, `Images()[0].Levels()[0]`, `DecodedTile` — all match the surveyed code. Oracle helper names (`requireFixture`, `decodeOpentileTile`, `assertPixelsWithinTolerance`, `oracle.PythonBin`) are flagged "match existing names when implementing" since they mirror `openslide_test.go`.
