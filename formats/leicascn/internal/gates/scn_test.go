//go:build gates

// Package gates holds the v0.11 JIT verification probes that run
// against real SCN fixtures. Build-tag-gated so they don't run in
// default CI but can be invoked explicitly:
//
//	OPENTILE_TESTDIR=$PWD/sample_files go test -tags gates ./formats/leicascn/internal/gates/
//
// Mirrors formats/generictiff/internal/gates/.
package gates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wsilabs/opentile-go/formats/leicascn"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

func TestSCNFixtureGate(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR unset")
	}

	for _, tc := range []struct {
		file              string
		expectImages      int
		expectAuxiliaries int
		expectMains       int
		expectMaxC        int
	}{
		{"Leica-1.scn", 2, 1, 1, 1},
		{"Leica-2.scn", 5, 1, 4, 1},
		{"Leica-Fluorescence-1.scn", 3, 2, 1, 3},
	} {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(dir, "scn", tc.file)
			f, err := os.Open(path)
			if err != nil {
				t.Skipf("missing fixture: %v", err)
			}
			defer f.Close()
			st, _ := f.Stat()
			tf, err := tiff.Open(f, st.Size())
			if err != nil {
				t.Fatalf("tiff.Open: %v", err)
			}

			pages := tf.Pages()
			if len(pages) == 0 {
				t.Fatal("tiff.File has zero pages")
			}
			xmlText, ok := pages[0].ImageDescription()
			if !ok {
				t.Fatal("IFD 0 ImageDescription missing")
			}
			c, err := leicascn.ParseDescription(xmlText)
			if err != nil {
				t.Fatalf("ParseDescription: %v", err)
			}

			if got := len(c.Images); got != tc.expectImages {
				t.Errorf("Images = %d, want %d", got, tc.expectImages)
			}

			var auxs, mains []leicascn.Image
			maxC := 1
			for _, img := range c.Images {
				if leicascn.IsAuxiliary(img, c) {
					auxs = append(auxs, img)
				} else {
					mains = append(mains, img)
				}
				for _, d := range img.Dimensions {
					if d.C+1 > maxC {
						maxC = d.C + 1
					}
				}
			}
			if got := len(auxs); got != tc.expectAuxiliaries {
				t.Errorf("auxiliaries = %d, want %d", got, tc.expectAuxiliaries)
			}
			if got := len(mains); got != tc.expectMains {
				t.Errorf("mains = %d, want %d", got, tc.expectMains)
			}
			if maxC != tc.expectMaxC {
				t.Errorf("max channel = %d, want %d", maxC, tc.expectMaxC)
			}

			composite, err := leicascn.ComposePyramid(mains, c)
			if err != nil {
				t.Fatalf("ComposePyramid: %v", err)
			}
			t.Logf("✓ %s: %d images / %d aux / %d mains / SizeC=%d / composite levels=%d",
				tc.file, len(c.Images), len(auxs), len(mains), maxC, len(composite))
			for i, lvl := range composite {
				t.Logf("    L%d: %d×%d, SizeC=%d, regions=%d",
					i, lvl.PixelSizeX, lvl.PixelSizeY, lvl.SizeC, len(lvl.Regions))
			}
		})
	}
}
