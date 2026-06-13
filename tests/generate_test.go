//go:build generate

package tests_test

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	svs "github.com/wsilabs/opentile-go/formats/svs"
	"github.com/wsilabs/opentile-go/tests"
)

var regenerate = flag.Bool("generate", false, "regenerate fixtures from live slides")
var sampledMode = flag.Bool("sampled", false, "generate sampled (not full) tile fixture; auto-on for slides expected to exceed the 5 MB cap")

func sampledByDefault(slide string) bool {
	base := filepath.Base(slide)
	switch base {
	case "Hamamatsu-1.ndpi", "svs_40x_bigtiff.svs":
		return true
	// Philips fixtures are sampled to keep generation under the 5 MB
	// per-fixture cap. Same for OME — Leica-1 is 689 MB and Leica-2
	// is 1.2 GB.
	case "Philips-1.tiff", "Philips-2.tiff", "Philips-3.tiff", "Philips-4.tiff":
		return true
	case "Leica-1.ome.tiff", "Leica-2.ome.tiff":
		return true
	// BIF fixtures: Ventana-1 is 227 MB and OS-1 is 3.6 GB —
	// both qualify under the >100 MB sampled-by-default policy.
	case "Ventana-1.bif", "OS-1.bif":
		return true
	// IFE: cervix_2x_jpeg is 2.1 GB; sample-only.
	case "cervix_2x_jpeg.iris":
		return true
	// SCN: Leica-1 is 278 MB, Leica-2 is 2.1 GB — both qualify under
	// the >100 MB sampled-by-default policy. Leica-Fluorescence-1 is
	// 21 MB so it stays full-walk.
	case "Leica-1.scn", "Leica-2.scn":
		return true
	// SZI: scan_618_grundium_SZI.szi is 709 MB; sample-only.
	// CMU-1.szi (~1 MB) stays full-walk.
	case "scan_618_grundium_SZI.szi":
		return true
	// COG-WSI (v0.19): wsitools-converted fixtures. CMU-1-Small-Region
	// (~1.9 MB) stays full-walk; everything else qualifies under the
	// >100 MB sampled-by-default policy. cervix (2.1 GB) and
	// svs_40x_bigtiff (4.8 GB) are the largest.
	case "CMU-1_cog-wsi.tiff",
		"JP2K-33003-1_cog-wsi.tiff",
		"scan_617_cog-wsi.tiff",
		"scan_620_cog-wsi.tiff",
		"svs_40x_bigtiff_cog-wsi.tiff",
		"Leica-1_cog-wsi.tiff",
		"Philips-1_cog-wsi.tiff",
		"Ventana-1_cog-wsi.tiff",
		"cervix_2x_jpeg_cog-wsi.tiff":
		return true
	// DICOM WSI (v0.32): all three fixtures exceed 100 MB and are
	// sampled by default.
	case "Leica-4", "3DHISTECH-1", "scan_621_grundium_dicom":
		return true
	}
	return false
}

// TestGenerateFixtures is a dev-only helper. Run with:
//
//	OPENTILE_TESTDIR=$PWD/sample_files/svs \
//	  go test ./tests -tags generate -run TestGenerateFixtures -generate -v
//
// Build tag "generate" keeps it out of default CI.
func TestGenerateFixtures(t *testing.T) {
	if !*regenerate {
		t.Skip("pass -generate to regenerate fixtures")
	}
	dir := tests.TestdataDir()
	if dir == "" {
		t.Fatal("OPENTILE_TESTDIR not set")
	}
	for _, name := range slideCandidates {
		t.Run(name, func(t *testing.T) {
			slide, ok := resolveSlide(dir, name)
			if !ok {
				t.Skipf("slide %s not present under %s", name, dir)
			}
			if err := generateFixture(slide); err != nil {
				t.Fatalf("generate %s: %v", name, err)
			}
		})
	}
}

func generateFixture(slide string) error {
	tiler, err := opentile.OpenFile(slide)
	if err != nil {
		return fmt.Errorf("OpenFile: %w", err)
	}
	defer tiler.Close()

	name := filepath.Base(slide)
	useSampled := *sampledMode || sampledByDefault(slide)
	f := &tests.Fixture{
		Slide:  name,
		Format: string(tiler.Format()),
	}

	images := tiler.Pyramids()
	if len(images) > 1 {
		// Multi-image (OME) → populate fix.Images. Each Pyramid's
		// Levels / TileSHA256 / SampledTileSHA256 are namespaced under
		// the per-image record.
		for ii, img := range images {
			imgFix := tests.ImageFixture{
				Index: img.Index,
				Name:  img.Name,
			}
			if !useSampled {
				imgFix.TileSHA256 = make(map[string]string)
			}
			if err := generateImageFixture(&imgFix, img.Levels, tiler, useSampled, ii); err != nil {
				return err
			}
			f.Images = append(f.Images, imgFix)
		}
	} else {
		// Single-image: populate the legacy top-level Levels view.
		if !useSampled {
			f.TileSHA256 = make(map[string]string)
		}
		var imgFix tests.ImageFixture
		// Use a temporary to share the helper's logic, then copy the
		// per-image fields into the top-level fixture slots.
		if !useSampled {
			imgFix.TileSHA256 = make(map[string]string)
		}
		if err := generateImageFixture(&imgFix, tiler.Pyramids()[0].Levels, tiler, useSampled, 0); err != nil {
			return err
		}
		f.Levels = imgFix.Levels
		f.TileSHA256 = imgFix.TileSHA256
		f.SampledTileSHA256 = imgFix.SampledTileSHA256
	}
	for _, a := range tiler.AssociatedImages() {
		b, err := a.Bytes()
		if err != nil {
			return fmt.Errorf("Associated(%s).Bytes: %w", a.Type(), err)
		}
		sum := sha256.Sum256(b)
		f.AssociatedImages = append(f.AssociatedImages, tests.AssociatedFixture{
			Type:        string(a.Type()),
			Size:        [2]int{a.Size().W, a.Size().H},
			Compression: a.Compression().String(),
			SHA256:      hex.EncodeToString(sum[:]),
		})
	}
	md := tiler.Metadata()
	f.Metadata = tests.MetadataFixture{
		Magnification:       md.Magnification,
		ScannerManufacturer: md.ScannerManufacturer,
		ScannerSerial:       md.ScannerSerial,
	}
	if sm, ok := svs.MetadataOf(tiler); ok {
		f.Metadata.SoftwareLine = sm.SoftwareLine
		f.Metadata.MPP = sm.MPP
	}
	if !md.AcquisitionDateTime.IsZero() {
		f.Metadata.AcquisitionRFC3339 = md.AcquisitionDateTime.Format(time.RFC3339)
	}

	outPath := filepath.Join("fixtures", fixtureJSONFor(name))
	if err := tests.SaveFixture(outPath, f); err != nil {
		return fmt.Errorf("SaveFixture: %w", err)
	}
	fmt.Printf("wrote %s\n", outPath)
	return nil
}

// generateImageFixture populates an ImageFixture from a single Image's
// level chain. Shared between the multi-image (Image[ii]) and
// single-image (top-level) generator paths. imageIdx is used both for
// ImageRawTile calls and error messages.
func generateImageFixture(out *tests.ImageFixture, levels []opentile.Level, tiler *opentile.Slide, useSampled bool, imageIdx int) error {
	for i, lvl := range levels {
		out.Levels = append(out.Levels, tests.LevelFixture{
			Index:       i,
			Size:        [2]int{lvl.Size.W, lvl.Size.H},
			TileSize:    [2]int{lvl.TileSize.W, lvl.TileSize.H},
			Grid:        [2]int{lvl.Grid.W, lvl.Grid.H},
			Compression: lvl.Compression.String(),
			MPPUm:       lvl.MPP.X,
			PyramidIdx:  lvl.PyramidIndex,
		})
		if useSampled {
			positions := tests.SamplePositions(lvl.Grid, lvl.Size, lvl.TileSize)
			if out.SampledTileSHA256 == nil {
				out.SampledTileSHA256 = make(map[string]tests.SampledTile)
			}
			for _, p := range positions {
				b, err := lvl.Tile(p.X, p.Y)
				if err != nil {
					return fmt.Errorf("Tile(%d,%d) image %d level %d: %w", p.X, p.Y, imageIdx, i, err)
				}
				sum := sha256.Sum256(b)
				out.SampledTileSHA256[tests.SampleKey(i, p)] = tests.SampledTile{
					SHA256: hex.EncodeToString(sum[:]),
					Reason: p.Reason,
				}
			}
		} else {
			for y := 0; y < lvl.Grid.H; y++ {
				for x := 0; x < lvl.Grid.W; x++ {
					b, err := lvl.Tile(x, y)
					if err != nil {
						return fmt.Errorf("Tile(%d,%d) image %d level %d: %w", x, y, imageIdx, i, err)
					}
					sum := sha256.Sum256(b)
					out.TileSHA256[tests.TileKey(i, x, y)] = hex.EncodeToString(sum[:])
				}
			}
		}
	}
	return nil
}
