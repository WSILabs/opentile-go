package tests_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/tests"
)

// slideCandidates lists SVS, NDPI, Philips, OME, BIF, IFE,
// generic-TIFF, and DICOM slides this integration suite knows about.
// Each is tested only if both the on-disk slide and the committed
// fixture JSON are present; otherwise the slide is skipped.
// DICOM "slides" are directories; their entries have no extension and
// are resolved via the "dicom" sub-directory branch in resolveSlide.
var slideCandidates = []string{
	"CMU-1-Small-Region.svs",
	"CMU-1.svs",
	"JP2K-33003-1.svs",
	"scan_620_.svs",
	"svs_40x_bigtiff.svs",
	"CMU-1.ndpi",
	"OS-2.ndpi",
	"Hamamatsu-1.ndpi",
	"Philips-1.tiff",
	"Philips-2.tiff",
	"Philips-3.tiff",
	"Philips-4.tiff",
	"Leica-1.ome.tiff",
	"Leica-2.ome.tiff",
	"Ventana-1.bif",
	"OS-1.bif",
	"cervix_2x_jpeg.iris",
	// Generic TIFF (v0.10): catch-all reader for tiled pyramidal
	// TIFFs without vendor metadata. CMU-1.tiff is the
	// tifffile-stripped derivative of CMU-1.svs (Aperio metadata
	// removed); CMU-1.stripped.tiff additionally re-encodes the
	// associated images as stripped TIFFs to exercise the multi-
	// strip JPEG / LZW reader paths.
	"CMU-1.tiff",
	"CMU-1.stripped.tiff",
	// Grundium TIFF (v0.11): real-world fixtures from a Grundium
	// Ocus scanner. scan_619 is a single-level tiled BigTIFF (relies
	// on the v0.11 MinLevels=1 relaxation); scan_620 is a 4-IFD
	// mixed-ratio chain (relies on the v0.11 LeftoverTiledMaxAreaRatio
	// =0.05 relaxation that lets the orphan L2 surface as associated).
	"scan_619_grundium_pyramid_TIFF.tif",
	"scan_620_grundium_TIFF.tif",
	// Leica SCN (v0.11): BigTIFF dialect from Leica SCN400 scanners
	// (production discontinued ~2015). Leica-1 is the simple single-
	// region case; Leica-2 has 4 disjoint main-scan rectangles
	// (multi-region composite); Leica-Fluorescence-1 is the only
	// real fixture exercising Image.SizeC() > 1 (3-channel).
	"Leica-1.scn",
	"Leica-2.scn",
	"Leica-Fluorescence-1.scn",
	// Generic TIFF v0.14 (wsi-tools novel codecs): each is a single-
	// level 10×13 grid of 240px tiles transcoded from CMU-1-Small-
	// Region.svs to a different tile codec. Exercises the v0.14
	// CompressionAVIF / CompressionHTJ2K / CompressionJPEGXL /
	// CompressionWebP enums and the generictiff parser's mapping
	// from TIFF tags 60001 / 60003 / 50002 / 50001.
	"avif-out.tiff",
	"htj2k-out.tiff",
	"jxl-out.tiff",
	"webp-out.tiff",
	// SZI (v0.16): Smart Zoom Image (ZIP-wrapped DZI). CMU-1.szi is
	// the small full-walk fixture; scan_618_grundium_SZI.szi is the
	// large sampled fixture from a Grundium Ocus scanner.
	"CMU-1.szi",
	"scan_618_grundium_SZI.szi",
	// COG-WSI fixtures (v0.19): wsitools-converted from each source
	// format. Geometry matches the original; tile bytes match where
	// the COG-WSI writer preserved them bit-exact per spec.
	"CMU-1-Small-Region_cog-wsi.tiff",
	"CMU-1_cog-wsi.tiff",
	"JP2K-33003-1_cog-wsi.tiff",
	"scan_617_cog-wsi.tiff",
	"scan_620_cog-wsi.tiff",
	"svs_40x_bigtiff_cog-wsi.tiff",
	"Leica-1_cog-wsi.tiff",
	"Philips-1_cog-wsi.tiff",
	"Ventana-1_cog-wsi.tiff",
	"cervix_2x_jpeg_cog-wsi.tiff",
	// DICOM WSI (v0.32): each entry is a directory name (no extension);
	// resolveSlide resolves them under the "dicom" sub-directory and
	// fixtureJSONFor maps extensionless names to "<stem>.dicom.json".
	// Leica-4 is a TILED_SPARSE GT450 dataset (6 .dcm, 3 levels);
	// 3DHISTECH-1 is TILED_FULL with 10 levels;
	// scan_621_grundium_dicom is TILED_FULL Grundium with 3 levels.
	"Leica-4",
	"3DHISTECH-1",
	"scan_621_grundium_dicom",
}

// resolveSlide looks up name in dir, dir/svs, dir/ndpi, dir/philips-tiff,
// dir/ome-tiff, dir/bif, dir/ife, dir/generic-tiff, dir/scn, dir/szi, and
// dir/dicom. Returns the first existing absolute path (including
// directories, so DICOM series directories resolve correctly). Used so
// OPENTILE_TESTDIR can be set to the repo sample_files root and cover
// every supported format in one run.
func resolveSlide(dir, name string) (string, bool) {
	for _, sub := range []string{"", "svs", "ndpi", "philips-tiff", "ome-tiff", "bif", "ife", "generic-tiff", "scn", "szi", "cog-wsi", "dicom"} {
		p := filepath.Join(dir, sub, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// TestSlideParity reads each candidate slide, walks every (level, x, y), and
// compares against the committed fixture. Slides without a fixture or without
// an on-disk file are skipped — this lets developers iterate on a subset of
// slides without hunting for failures.
func TestSlideParity(t *testing.T) {
	dir := tests.TestdataDir()
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set; skipping integration test")
	}
	any := false
	for _, name := range slideCandidates {
		t.Run(name, func(t *testing.T) {
			slide, ok := resolveSlide(dir, name)
			if !ok {
				t.Skipf("slide %s not present under %s", name, dir)
			}
			fixturePath := filepath.Join("fixtures", fixtureJSONFor(name))
			if _, err := os.Stat(fixturePath); err != nil {
				t.Skipf("fixture not present at %s (generate with -generate)", fixturePath)
			}
			any = true
			checkSlideAgainstFixture(t, slide, fixturePath)
		})
	}
	if !any {
		t.Log("no slide+fixture pairs found; run the generator to create fixtures")
	}
}

func checkSlideAgainstFixture(t *testing.T, slide, fixturePath string) {
	t.Helper()
	fix, err := tests.LoadFixture(fixturePath)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer tiler.Close()

	if string(tiler.Format()) != fix.Format {
		t.Errorf("Format: got %q, want %q", tiler.Format(), fix.Format)
	}

	// Multi-image fixtures (OME) populate fix.Images and use that
	// view; single-image fixtures use the legacy top-level Levels /
	// TileSHA256 / SampledTileSHA256 fields.
	if len(fix.Images) > 0 {
		images := tiler.Pyramids()
		if len(images) != len(fix.Images) {
			t.Fatalf("image count: got %d, want %d", len(images), len(fix.Images))
		}
		for ii, img := range images {
			fixImg := fix.Images[ii]
			if img.Index != fixImg.Index {
				t.Errorf("image %d Index: got %d, want %d", ii, img.Index, fixImg.Index)
			}
			if img.Name != fixImg.Name {
				t.Errorf("image %d Name: got %q, want %q", ii, img.Name, fixImg.Name)
			}
			checkLevels(t, img.Levels, fixImg.Levels, fixImg.TileSHA256, fixImg.SampledTileSHA256, fmt.Sprintf("image %d ", ii), tiler, ii)
		}
	} else {
		levels := tiler.Levels()
		if len(levels) != len(fix.Levels) {
			t.Fatalf("level count: got %d, want %d", len(levels), len(fix.Levels))
		}
		checkLevels(t, levels, fix.Levels, fix.TileSHA256, fix.SampledTileSHA256, "", tiler, 0)
	}

	// ICCProfile: a non-nil slice must have non-zero length. Some slides
	// legitimately return nil (no embedded profile); only catch the broken
	// case where the tag was found but empty.
	if icc := tiler.ICCProfile(); icc != nil && len(icc) == 0 {
		t.Error("ICCProfile non-nil but empty")
	}

	md := tiler.Metadata()
	if md.Magnification != fix.Metadata.Magnification {
		t.Errorf("magnification: got %v, want %v", md.Magnification, fix.Metadata.Magnification)
	}

	associated := tiler.AssociatedImages()
	if len(associated) != len(fix.AssociatedImages) {
		t.Errorf("associated count: got %d, want %d", len(associated), len(fix.AssociatedImages))
	} else {
		for i, a := range associated {
			exp := fix.AssociatedImages[i]
			if string(a.Type()) != exp.Type {
				t.Errorf("associated[%d] type: got %q, want %q", i, a.Type(), exp.Type)
			}
			if a.Compression().String() != exp.Compression {
				t.Errorf("associated[%d] compression: got %q, want %q", i, a.Compression(), exp.Compression)
			}
			if a.Size().W != exp.Size[0] || a.Size().H != exp.Size[1] {
				t.Errorf("associated[%d] size: got %v, want %v", i, a.Size(), exp.Size)
			}
			b, err := a.Bytes()
			if err != nil {
				t.Errorf("associated[%d] Bytes: %v", i, err)
				continue
			}
			sum := sha256.Sum256(b)
			if got := hex.EncodeToString(sum[:]); got != exp.SHA256 {
				t.Errorf("associated[%d] sha256: got %s, want %s", i, got, exp.SHA256)
			}
		}
	}
}

// checkLevels walks a single Image's level chain against its fixture
// view. Used by checkSlideAgainstFixture for both the single-image
// (top-level Levels field) and multi-image (per-Image) layouts.
//
// The labelPrefix is prepended to t.Errorf messages so multi-image
// failures are unambiguous; pass "" for single-image, "image N " for
// multi-image.
func checkLevels(
	t *testing.T,
	levels []opentile.Level,
	fixLevels []tests.LevelFixture,
	fixTileSHA map[string]string,
	fixSampledSHA map[string]tests.SampledTile,
	labelPrefix string,
	tiler *opentile.Slide,
	imageIdx int,
) {
	t.Helper()
	if len(levels) != len(fixLevels) {
		t.Fatalf("%slevel count: got %d, want %d", labelPrefix, len(levels), len(fixLevels))
	}
	for i, lvl := range levels {
		exp := fixLevels[i]
		if lvl.Index != i {
			t.Errorf("%slevel %d: Index=%d, want %d", labelPrefix, i, lvl.Index, i)
		}
		if lvl.PyramidIndex != exp.PyramidIdx {
			t.Errorf("%slevel %d: PyramidIndex=%d, want %d", labelPrefix, i, lvl.PyramidIndex, exp.PyramidIdx)
		}
		if mpp := lvl.MPP; mpp.X < 0 || mpp.Y < 0 {
			t.Errorf("%slevel %d: MPP negative %v", labelPrefix, i, mpp)
		}
		if fp := lvl.FocalPlane; fp < 0 {
			t.Errorf("%slevel %d: FocalPlane negative %v", labelPrefix, i, fp)
		}
		if lvl.Size.W != exp.Size[0] || lvl.Size.H != exp.Size[1] {
			t.Errorf("%slevel %d size: got %v, want %v", labelPrefix, i, lvl.Size, exp.Size)
		}
		if lvl.TileSize.W != exp.TileSize[0] || lvl.TileSize.H != exp.TileSize[1] {
			t.Errorf("%slevel %d tile size: got %v, want %v", labelPrefix, i, lvl.TileSize, exp.TileSize)
		}
		if lvl.Grid.W != exp.Grid[0] || lvl.Grid.H != exp.Grid[1] {
			t.Errorf("%slevel %d grid: got %v, want %v", labelPrefix, i, lvl.Grid, exp.Grid)
		}
		if lvl.Compression.String() != exp.Compression {
			t.Errorf("%slevel %d compression: got %q, want %q", labelPrefix, i, lvl.Compression, exp.Compression)
		}
		// Full-walk tile hashes
		if len(fixTileSHA) > 0 {
			for y := 0; y < lvl.Grid.H; y++ {
				for x := 0; x < lvl.Grid.W; x++ {
					b, err := tiler.ImageRawTile(imageIdx, i, x, y)
					if err != nil {
						t.Errorf("%sImageRawTile(%d,%d) level %d: %v", labelPrefix, x, y, i, err)
						continue
					}
					sum := sha256.Sum256(b)
					got := hex.EncodeToString(sum[:])
					key := tests.TileKey(i, x, y)
					want, ok := fixTileSHA[key]
					if !ok {
						t.Errorf("%sfixture missing key %s", labelPrefix, key)
						continue
					}
					if got != want {
						t.Errorf("%stile %s hash: got %s, want %s", labelPrefix, key, got, want)
					}
				}
			}
		}
	}
	// Sampled-walk hashes.
	if len(fixSampledSHA) > 0 {
		for i, lvl := range levels {
			positions := tests.SamplePositions(lvl.Grid, lvl.Size, lvl.TileSize)
			for _, p := range positions {
				b, err := tiler.ImageRawTile(imageIdx, i, p.X, p.Y)
				if err != nil {
					t.Errorf("%ssampled ImageRawTile(%d,%d) level %d: %v", labelPrefix, p.X, p.Y, i, err)
					continue
				}
				key := tests.SampleKey(i, p)
				expEntry, ok := fixSampledSHA[key]
				if !ok {
					t.Errorf("%ssampled fixture missing key %s", labelPrefix, key)
					continue
				}
				sum := sha256.Sum256(b)
				got := hex.EncodeToString(sum[:])
				if got != expEntry.SHA256 {
					t.Errorf("%ssampled tile %s (%s): got %s, want %s",
						labelPrefix, key, expEntry.Reason, got, expEntry.SHA256)
				}
			}
		}
	}
}

// fixtureJSONFor returns the fixture filename for a given slide filename.
// SVS slides keep the historical "<stem>.json" naming. NDPI and Philips
// TIFF slides embed their extension as "<stem>.ndpi.json" /
// "<stem>.tiff.json" so that, for example, CMU-1.svs and CMU-1.ndpi
// produce distinct fixtures on disk.
func fixtureJSONFor(slideFilename string) string {
	base := filepath.Base(slideFilename)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	switch ext {
	case ".ndpi":
		return stem + ".ndpi.json"
	case ".tiff", ".tif":
		return stem + ".tiff.json"
	case ".bif":
		return stem + ".bif.json"
	case ".iris":
		return stem + ".ife.json"
	case ".scn":
		return stem + ".scn.json"
	case ".szi":
		return stem + ".szi.json"
	case "":
		// Extensionless entries are DICOM series directories.
		return stem + ".dicom.json"
	}
	return stem + ".json"
}

// Silence the imports when the test file is compiled with no tests run.
var _ = fmt.Sprintf
