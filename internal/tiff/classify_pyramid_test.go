package tiff

import (
	"errors"
	"testing"
)

// tiledRGB is a helper that builds a typical tiled JPEG/RGB
// PyramidLevelInfo for tests — uint8, RGB photometric (2),
// 256x256 tiles. Override fields after construction for negative-
// path tests.
func tiledRGB(idx int, w, h uint32) PyramidLevelInfo {
	return PyramidLevelInfo{
		Index: idx, Width: w, Height: h,
		TileWidth: 256, TileLength: 256,
		Compression: 7, Photometric: 2,
		SamplesPerPixel: 3, BitsPerSample: 8,
	}
}

// strippedJPEG builds a typical stripped JPEG/RGB associated image.
func strippedJPEG(idx int, w, h uint32) PyramidLevelInfo {
	return PyramidLevelInfo{
		Index: idx, Width: w, Height: h,
		// No TileWidth/TileLength → IsTiled() == false.
		Compression: 7, Photometric: 2,
		SamplesPerPixel: 3, BitsPerSample: 8,
	}
}

func TestClassifyPyramid_AcceptCleanPyramid(t *testing.T) {
	// 3-level 2× pyramid, no associated.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
		tiledRGB(2, 256, 256),
	}
	res, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if err != nil {
		t.Fatalf("expected accept, got err: %v", err)
	}
	if len(res.Pyramid) != 3 {
		t.Errorf("pyramid len = %d, want 3", len(res.Pyramid))
	}
	if len(res.Others) != 0 {
		t.Errorf("others len = %d, want 0", len(res.Others))
	}
	// Largest-first ordering.
	if res.Pyramid[0].Width != 1024 || res.Pyramid[2].Width != 256 {
		t.Errorf("not sorted largest-first: got %d → %d → %d",
			res.Pyramid[0].Width, res.Pyramid[1].Width, res.Pyramid[2].Width)
	}
}

func TestClassifyPyramid_Accept4xPyramid(t *testing.T) {
	// 3-level 4× pyramid (matches CMU-1.svs's actual SVS structure).
	infos := []PyramidLevelInfo{
		tiledRGB(0, 46000, 32914),
		tiledRGB(2, 11500, 8228),
		tiledRGB(3, 2875, 2057),
	}
	res, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if err != nil {
		t.Fatalf("expected accept, got err: %v", err)
	}
	if len(res.Pyramid) != 3 {
		t.Errorf("pyramid len = %d, want 3", len(res.Pyramid))
	}
}

func TestClassifyPyramid_AcceptDeepWithRoundingDrift(t *testing.T) {
	// 9-level 2× pyramid with realistic rounding drift (CMU-1.tiff
	// dims). All inter-axis values are well under ±2%; inter-level
	// drift maxes at ~0.07%.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 46000, 32914),
		tiledRGB(1, 23000, 16457),
		tiledRGB(2, 11500, 8228),
		tiledRGB(3, 5750, 4114),
		tiledRGB(4, 2875, 2057),
		tiledRGB(5, 1437, 1028),
		tiledRGB(6, 718, 514),
		tiledRGB(7, 359, 257),
		tiledRGB(8, 179, 128),
	}
	res, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if err != nil {
		t.Fatalf("expected accept, got err: %v", err)
	}
	if len(res.Pyramid) != 9 {
		t.Errorf("pyramid len = %d, want 9", len(res.Pyramid))
	}
}

func TestClassifyPyramid_AcceptPyramidWithStrippedAssociated(t *testing.T) {
	// 3-level pyramid + 3 stripped associated (mimics CMU-1.svs's
	// stripped-tiff output: pyramid IFDs 0/2/3 + associated IFDs
	// 1/4/5).
	infos := []PyramidLevelInfo{
		tiledRGB(0, 46000, 32914),
		strippedJPEG(1, 1024, 732), // thumbnail
		tiledRGB(2, 11500, 8228),
		tiledRGB(3, 2875, 2057),
		{Index: 4, Width: 387, Height: 463, Compression: 5, Photometric: 2, SamplesPerPixel: 3, BitsPerSample: 8}, // LZW label
		strippedJPEG(5, 1280, 431), // macro
	}
	res, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if err != nil {
		t.Fatalf("expected accept, got err: %v", err)
	}
	if len(res.Pyramid) != 3 {
		t.Errorf("pyramid len = %d, want 3", len(res.Pyramid))
	}
	if len(res.Others) != 3 {
		t.Errorf("others len = %d, want 3", len(res.Others))
	}
	// Verify the three associated IFDs are in Others (Indices 1, 4, 5).
	gotIdx := map[int]bool{}
	for _, o := range res.Others {
		gotIdx[o.Index] = true
	}
	for _, want := range []int{1, 4, 5} {
		if !gotIdx[want] {
			t.Errorf("expected IFD index %d in Others, got %v", want, gotIdx)
		}
	}
}

func TestClassifyPyramid_RejectFewerThanMinLevels(t *testing.T) {
	// Only 2 tiled IFDs. With MinLevels=3 (v0.10 strictness; explicit
	// here to express the test intent — v0.11 default is MinLevels=1
	// to admit Grundium-style single-level tiled TIFFs), should
	// reject.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
	}
	cfg := DefaultClassifyPyramidConfig()
	cfg.MinLevels = 3
	_, err := ClassifyPyramid(infos, cfg)
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_RejectAllStripped(t *testing.T) {
	// 0 tiled IFDs (only stripped). v0.11 MinLevels=1 still rejects
	// because there are zero TILED IFDs (the count-tiled gate).
	infos := []PyramidLevelInfo{
		strippedJPEG(0, 800, 600),
		strippedJPEG(1, 400, 300),
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_RejectInterAxisFailure(t *testing.T) {
	// L0→L1 has anisotropic downsampling (W ratio 1.71, H ratio 2.05).
	// Inter-axis 20% > ±2% → leftover. With MinLevels=3 (v0.10
	// strictness; explicit here for test-intent clarity) the surviving
	// 2-IFD chain falls below the floor → ErrPyramidScaleMismatch.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 600, 500),
		tiledRGB(2, 256, 256),
	}
	cfg := DefaultClassifyPyramidConfig()
	cfg.MinLevels = 3
	cfg.LeftoverTiledMaxAreaRatio = 0.01
	_, err := ClassifyPyramid(infos, cfg)
	if !errors.Is(err, ErrPyramidScaleMismatch) {
		t.Errorf("got %v, want ErrPyramidScaleMismatch", err)
	}
}

func TestClassifyPyramid_RejectInterLevelDriftBeyondTolerance(t *testing.T) {
	// L0→L1 ratio = 2.0; L1→L2 ratio = 3.0 (via 1024→512→170).
	// Inter-level drift = |2-3|/2 = 50%, way over ±5%. The 3×-step
	// IFD is dropped to leftover (not an integer-multiple of 2);
	// with MinLevels=3 (v0.10 strictness; explicit here) the
	// surviving 2-level chain falls below the floor → reject.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
		tiledRGB(2, 170, 170), // ~3× step from L1, not an integer-multiple
	}
	cfg := DefaultClassifyPyramidConfig()
	cfg.MinLevels = 3
	cfg.LeftoverTiledMaxAreaRatio = 0.01
	_, err := ClassifyPyramid(infos, cfg)
	if !errors.Is(err, ErrPyramidScaleMismatch) {
		t.Errorf("got %v, want ErrPyramidScaleMismatch", err)
	}
}

func TestClassifyPyramid_RejectMultiPyramidByLeftoverCount(t *testing.T) {
	// 3-level pyramid + 3 leftover tiny tiled IFDs (>MaxLeftoverTiled=2).
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
		tiledRGB(2, 256, 256),
		tiledRGB(3, 50, 50), // tiny tiled (would be allowed individually)
		tiledRGB(4, 50, 50), // ditto
		tiledRGB(5, 50, 50), // 3rd leftover → multi-pyramid trip
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidMultiplePyramid) {
		t.Errorf("got %v, want ErrPyramidMultiplePyramid", err)
	}
}

func TestClassifyPyramid_RejectMultiPyramidByLeftoverArea(t *testing.T) {
	// 3-level pyramid + 1 large leftover tiled (>5% baseline area
	// per v0.11 LeftoverTiledMaxAreaRatio=0.05). Baseline area =
	// 1024×1024 = ~1M; 5% = ~52500. Leftover at 300×300 = 90000 →
	// 8.6% → exceeds 5% threshold → reject.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
		tiledRGB(2, 256, 256),
		tiledRGB(3, 300, 300), // 8.6% of baseline area
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidMultiplePyramid) {
		t.Errorf("got %v, want ErrPyramidMultiplePyramid", err)
	}
}

func TestClassifyPyramid_TiledWithBadPhotometricGoesToOthers(t *testing.T) {
	// CMYK photometric (5) on what would otherwise be a pyramid IFD:
	// reject from pyramid candidates → falls to Others. With
	// MinLevels=3 (v0.10 strictness; explicit) the surviving 2 valid
	// tiled IFDs (1024 + 256, 4× ratio) fall below the floor.
	cmyk := tiledRGB(1, 512, 512)
	cmyk.Photometric = 5 // CMYK
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		cmyk,
		tiledRGB(2, 256, 256),
	}
	cfg := DefaultClassifyPyramidConfig()
	cfg.MinLevels = 3
	_, err := ClassifyPyramid(infos, cfg)
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_TiledWithBadCompressionGoesToOthers(t *testing.T) {
	// PackBits compression (32773) — not in our whitelist. With
	// MinLevels=3 (v0.10 strictness; explicit) the surviving 2 valid
	// tiled IFDs fall below the floor.
	pb := tiledRGB(1, 512, 512)
	pb.Compression = 32773
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		pb,
		tiledRGB(2, 256, 256),
	}
	cfg := DefaultClassifyPyramidConfig()
	cfg.MinLevels = 3
	_, err := ClassifyPyramid(infos, cfg)
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_TiledWith16BitGoesToOthers(t *testing.T) {
	// 16-bit per sample (scientific imaging) — out of v0.10 scope.
	// With MinLevels=3 (v0.10 strictness; explicit) the surviving
	// 2 valid tiled IFDs fall below the floor.
	hi := tiledRGB(1, 512, 512)
	hi.BitsPerSample = 16
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		hi,
		tiledRGB(2, 256, 256),
	}
	cfg := DefaultClassifyPyramidConfig()
	cfg.MinLevels = 3
	_, err := ClassifyPyramid(infos, cfg)
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_DefaultConfig(t *testing.T) {
	c := DefaultClassifyPyramidConfig()
	// v0.11 sealed thresholds (R1 + R2 in v0.11 design):
	//   MinLevels:                 1   (was 3 in v0.10)
	//   InterAxisTolerance:        0.02
	//   InterLevelTolerance:       0.05
	//   MaxLeftoverTiled:          2
	//   LeftoverTiledMaxAreaRatio: 0.05 (was 0.01 in v0.10)
	if c.MinLevels != 1 {
		t.Errorf("MinLevels = %d, want 1", c.MinLevels)
	}
	if c.InterAxisTolerance != 0.02 {
		t.Errorf("InterAxisTolerance = %v, want 0.02", c.InterAxisTolerance)
	}
	if c.InterLevelTolerance != 0.05 {
		t.Errorf("InterLevelTolerance = %v, want 0.05", c.InterLevelTolerance)
	}
	if c.MaxLeftoverTiled != 2 {
		t.Errorf("MaxLeftoverTiled = %d, want 2", c.MaxLeftoverTiled)
	}
	if c.LeftoverTiledMaxAreaRatio != 0.05 {
		t.Errorf("LeftoverTiledMaxAreaRatio = %v, want 0.05", c.LeftoverTiledMaxAreaRatio)
	}
}

func TestPyramidLevelInfoIsTiled(t *testing.T) {
	if !tiledRGB(0, 1024, 1024).IsTiled() {
		t.Error("tiledRGB should report IsTiled()")
	}
	if strippedJPEG(0, 1024, 1024).IsTiled() {
		t.Error("strippedJPEG should not report IsTiled()")
	}
	// Edge: TileWidth set but TileLength zero — not really tiled.
	mixed := PyramidLevelInfo{Index: 0, Width: 100, Height: 100, TileWidth: 256, TileLength: 0}
	if mixed.IsTiled() {
		t.Error("partial tile tags should not report IsTiled()")
	}
}

func TestValidCompression(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp uint32
		want bool
	}{
		// v0.10 originals
		{"None", 1, true},
		{"LZW", 5, true},
		{"JPEG", 7, true},
		{"Deflate", 8, true},
		{"JP2K_Aperio", 33003, true},
		{"JP2K_Aperio_RGB", 33005, true}, // #110

		// v0.14 additions
		{"JP2K_registered", 34712, true},
		{"WebP", 50001, true},
		{"JPEGXL", 50002, true},
		{"AVIF", 60001, true},
		{"HTJ2K", 60003, true},

		// Outside whitelist (sanity)
		{"PackBits", 32773, false},
		{"AdobeDeflate", 32946, false}, // accepted via aliasing in tiledImage but not the validator
		{"unknown_60002", 60002, false},
		{"unknown_99999", 99999, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validCompression(tc.comp); got != tc.want {
				t.Errorf("validCompression(%d) = %v, want %v", tc.comp, got, tc.want)
			}
		})
	}
}

func TestIsIntegerMultipleRatio(t *testing.T) {
	for _, tc := range []struct {
		name      string
		r         float64
		prior     []float64
		tolerance float64
		want      bool
	}{
		{"2x is half of 4x prior", 2.0, []float64{4.0}, 0.05, true},
		{"4x is double of 2x prior", 4.0, []float64{2.0}, 0.05, true},
		{"3x is not integer-multiple of 2x", 3.0, []float64{2.0}, 0.05, false},
		{"2.001x within tolerance of 2x prior", 2.001, []float64{2.0}, 0.05, true},
		{"empty prior", 4.0, nil, 0.05, false},
		{"8x with mixed prior", 8.0, []float64{2.0, 4.0}, 0.05, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := isIntegerMultipleRatio(tc.r, tc.prior, tc.tolerance)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyPyramid_IntegerMultipleChain(t *testing.T) {
	// Synthetic 4×/2×/2× chain: 49152 → 12288 → 6144 → 3072 wide.
	// Pre-v0.19 the second step (49152→12288 = 4×) sets the chain
	// ratio at 4×; the third step (12288→6144 = 2×) drifts 50% from
	// 4× and is rejected. v0.19 accepts via integer-multiple match
	// (2× is a clean half of 4×).
	infos := []PyramidLevelInfo{
		tiledRGB(0, 49152, 32768),
		tiledRGB(1, 12288, 8192),
		tiledRGB(2, 6144, 4096),
		tiledRGB(3, 3072, 2048),
	}
	res, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if err != nil {
		t.Fatalf("expected accept, got err: %v", err)
	}
	if len(res.Pyramid) != 4 {
		t.Errorf("pyramid len = %d, want 4", len(res.Pyramid))
	}
	if len(res.Others) != 0 {
		t.Errorf("others len = %d, want 0", len(res.Others))
	}
}
