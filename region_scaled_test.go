package opentile_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/resample"
)

func openScaledSample(t *testing.T) *opentile.Slide {
	t.Helper()
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs/CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	t.Cleanup(func() { slide.Close() })
	return slide
}

func TestReadRegionScaledOutDims(t *testing.T) {
	slide := openScaledSample(t)
	lvl := slide.Levels()[0]
	img, err := slide.Pyramid(0).ReadRegionScaled(opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: lvl.Size}, opentile.Size{W: 256, H: 256})
	if err != nil {
		t.Fatalf("ReadRegionScaled: %v", err)
	}
	if img.Width != 256 || img.Height != 256 {
		t.Errorf("dims: got %dx%d, want 256x256", img.Width, img.Height)
	}
}

func TestReadRegionScaledKernelChoice(t *testing.T) {
	slide := openScaledSample(t)
	lvl := slide.Levels()[0]
	lanczos, err := slide.Pyramid(0).ReadRegionScaled(opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: lvl.Size}, opentile.Size{W: 128, H: 128},
		opentile.WithResampleKernel(resample.Lanczos))
	if err != nil {
		t.Fatalf("Lanczos: %v", err)
	}
	box, err := slide.Pyramid(0).ReadRegionScaled(opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: lvl.Size}, opentile.Size{W: 128, H: 128},
		opentile.WithResampleKernel(resample.Box))
	if err != nil {
		t.Fatalf("Box: %v", err)
	}
	if bytes.Equal(lanczos.Pix, box.Pix) {
		t.Error("Lanczos and Box produced identical output (kernel option may not be wired)")
	}
}

func TestReadRegionScaledTinyOutput(t *testing.T) {
	slide := openScaledSample(t)
	lvl := slide.Levels()[0]
	img, err := slide.Pyramid(0).ReadRegionScaled(opentile.Region{Origin: opentile.Point{X: 0, Y: 0}, Size: lvl.Size}, opentile.Size{W: 10, H: 10})
	if err != nil {
		t.Fatalf("ReadRegionScaled tiny: %v", err)
	}
	if img.Width != 10 || img.Height != 10 {
		t.Errorf("dims: got %dx%d, want 10x10", img.Width, img.Height)
	}
}
