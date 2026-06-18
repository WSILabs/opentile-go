package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestRenderThumbnailIntegration renders thumbnails from a real slide and
// checks the fit math + that real pixels come back. Fixture-gated.
func TestRenderThumbnailIntegration(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CMU-1-Small-Region.svs not present")
	}
	s, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer s.Close()

	l0 := s.Pyramid(0).Levels[0].Size
	aspect := float64(l0.W) / float64(l0.H)

	notAllWhite := func(t *testing.T, img *decoder.Image) {
		t.Helper()
		for i := 0; i+2 < len(img.Pix); i += 3 {
			if img.Pix[i] != 0xFF || img.Pix[i+1] != 0xFF || img.Pix[i+2] != 0xFF {
				return
			}
		}
		t.Error("thumbnail is entirely white — render likely broken")
	}

	t.Run("fit-box 256", func(t *testing.T) {
		img, err := s.RenderThumbnail(opentile.Size{W: 256, H: 256})
		if err != nil {
			t.Fatalf("RenderThumbnail: %v", err)
		}
		if img.Width > 256 || img.Height > 256 {
			t.Errorf("dims %dx%d exceed 256×256 box", img.Width, img.Height)
		}
		if img.Width != 256 && img.Height != 256 {
			t.Errorf("dims %dx%d: neither axis hit the 256 box bound", img.Width, img.Height)
		}
		// aspect preserved within ±1px
		gotAspect := float64(img.Width) / float64(img.Height)
		if d := gotAspect - aspect; d < -0.02 || d > 0.02 {
			t.Errorf("aspect %.4f vs L0 %.4f (dims %dx%d)", gotAspect, aspect, img.Width, img.Height)
		}
		notAllWhite(t, img)
	})

	t.Run("fit-width 200", func(t *testing.T) {
		img, err := s.RenderThumbnail(opentile.Size{W: 200, H: 0})
		if err != nil {
			t.Fatalf("RenderThumbnail: %v", err)
		}
		if img.Width != 200 {
			t.Errorf("fit-width: got width %d, want 200", img.Width)
		}
		wantH := int(float64(200)/aspect + 0.5)
		if d := img.Height - wantH; d < -1 || d > 1 {
			t.Errorf("fit-width: height %d, want ≈%d (aspect)", img.Height, wantH)
		}
		notAllWhite(t, img)
	})

	t.Run("fit-height 200", func(t *testing.T) {
		img, err := s.RenderThumbnail(opentile.Size{W: 0, H: 200})
		if err != nil {
			t.Fatalf("RenderThumbnail: %v", err)
		}
		if img.Height != 200 {
			t.Errorf("fit-height: got height %d, want 200", img.Height)
		}
		notAllWhite(t, img)
	})

	t.Run("both-zero errors", func(t *testing.T) {
		if _, err := s.RenderThumbnail(opentile.Size{W: 0, H: 0}); err == nil {
			t.Error("want error for unconstrained bounds")
		}
	})
}
