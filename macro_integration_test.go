package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestRenderMacroIntegration renders a synthesized macro from a real slide and
// checks the canvas geometry + that it's a composite (white slide background +
// tissue). Fixture-gated.
func TestRenderMacroIntegration(t *testing.T) {
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

	img, err := s.RenderMacro(opentile.Size{W: 600})
	if err != nil {
		t.Fatalf("RenderMacro: %v", err)
	}
	// Scan area is 50×25 mm (2:1) → fit-width 600 → 600×300.
	if img.Width != 600 || img.Height != 300 {
		t.Errorf("macro dims = %dx%d, want 600x300", img.Width, img.Height)
	}
	// Must be a composite: some white (slide background) AND some non-white
	// (tissue). All-white = no tissue rendered; all-non-white = no margin.
	white, nonWhite := 0, 0
	for i := 0; i+2 < len(img.Pix); i += 3 {
		if img.Pix[i] == 0xFF && img.Pix[i+1] == 0xFF && img.Pix[i+2] == 0xFF {
			white++
		} else {
			nonWhite++
		}
	}
	if white == 0 {
		t.Error("no white background — tissue should not fill the whole canvas")
	}
	if nonWhite == 0 {
		t.Error("entirely white — tissue not composited")
	}
	t.Logf("macro 600x300: white=%d nonWhite=%d", white, nonWhite)
}
