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
		strippedJPEG(1, 1024, 732),       // thumbnail
		tiledRGB(2, 11500, 8228),
		tiledRGB(3, 2875, 2057),
		{Index: 4, Width: 387, Height: 463, Compression: 5, Photometric: 2, SamplesPerPixel: 3, BitsPerSample: 8}, // LZW label
		strippedJPEG(5, 1280, 431),       // macro
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
	// Only 2 tiled IFDs. Should reject.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_RejectAllStripped(t *testing.T) {
	// 0 tiled IFDs (only stripped). Should reject as too few levels.
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
	// Inter-axis 20% > ±2% → leftover. With only 2 IFDs surviving,
	// chain falls below MinLevels → ErrPyramidScaleMismatch.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 600, 500),
		tiledRGB(2, 256, 256),
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidScaleMismatch) {
		t.Errorf("got %v, want ErrPyramidScaleMismatch", err)
	}
}

func TestClassifyPyramid_RejectInterLevelDriftBeyondTolerance(t *testing.T) {
	// L0→L1 ratio = 2.0; L1→L2 ratio = 4.0. Inter-level drift =
	// |2-4|/2 = 100%, way over ±5%. The 4×-step IFD is dropped to
	// leftover; resulting chain has 2 levels < min 3 → reject.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
		tiledRGB(2, 128, 128), // 4× step from L1
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
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
	// 3-level pyramid + 1 large leftover tiled (≥1% baseline area).
	// Baseline area = 1024×1024 = ~1M; 1% = ~10500.
	// Leftover at 200×200 = 40000 → 4% → exceeds 1% threshold → reject.
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		tiledRGB(1, 512, 512),
		tiledRGB(2, 256, 256),
		tiledRGB(3, 200, 200), // 4% of baseline area
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidMultiplePyramid) {
		t.Errorf("got %v, want ErrPyramidMultiplePyramid", err)
	}
}

func TestClassifyPyramid_TiledWithBadPhotometricGoesToOthers(t *testing.T) {
	// CMYK photometric (5) on what would otherwise be a pyramid IFD:
	// reject from pyramid candidates → falls to Others. Combined with
	// only 2 valid tiled IFDs, ErrPyramidTooFewLevels.
	cmyk := tiledRGB(1, 512, 512)
	cmyk.Photometric = 5 // CMYK
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		cmyk,
		tiledRGB(2, 256, 256),
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	// Two tiled+valid (1024 and 256) — not enough to form a pyramid
	// (would have 4× ratio, still legal, but only 2 levels < 3).
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_TiledWithBadCompressionGoesToOthers(t *testing.T) {
	// PackBits compression (32773) — not in our whitelist.
	pb := tiledRGB(1, 512, 512)
	pb.Compression = 32773
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		pb,
		tiledRGB(2, 256, 256),
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_TiledWith16BitGoesToOthers(t *testing.T) {
	// 16-bit per sample (scientific imaging) — out of v0.10 scope.
	hi := tiledRGB(1, 512, 512)
	hi.BitsPerSample = 16
	infos := []PyramidLevelInfo{
		tiledRGB(0, 1024, 1024),
		hi,
		tiledRGB(2, 256, 256),
	}
	_, err := ClassifyPyramid(infos, DefaultClassifyPyramidConfig())
	if !errors.Is(err, ErrPyramidTooFewLevels) {
		t.Errorf("got %v, want ErrPyramidTooFewLevels", err)
	}
}

func TestClassifyPyramid_DefaultConfig(t *testing.T) {
	c := DefaultClassifyPyramidConfig()
	if c.MinLevels != 3 {
		t.Errorf("MinLevels = %d, want 3", c.MinLevels)
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
	if c.LeftoverTiledMaxAreaRatio != 0.01 {
		t.Errorf("LeftoverTiledMaxAreaRatio = %v, want 0.01", c.LeftoverTiledMaxAreaRatio)
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
