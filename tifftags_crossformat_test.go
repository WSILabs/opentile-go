package opentile_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func crossFixture(t *testing.T, rel string) string {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		dir = "sample_files"
	}
	p := filepath.Join(dir, rel)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture missing: %s", p)
	}
	return p
}

// TestTIFFTagsAllFormats is the sufficiency gate for the whole feature:
// every TIFF-based format must expose tags on level 0 — ImageWidth (256,
// universal in TIFF) present, pixel-pointer tags filtered, a DirLevel for
// (image 0, level 0), directories non-empty.
func TestTIFFTagsAllFormats(t *testing.T) {
	cases := []struct{ format, rel string }{
		{"svs", "svs/CMU-1.svs"},
		{"ndpi", "ndpi/CMU-1.ndpi"},
		{"philips-tiff", "philips-tiff/Philips-1.tiff"},
		{"ome-tiff", "ome-tiff/Leica-1.ome.tiff"},
		{"bif", "bif/Ventana-1.bif"},
		{"generic-tiff", "generic-tiff/CMU-1.tiff"},
		{"leica-scn", "scn/Leica-1.scn"},
		{"cog-wsi", "cog-wsi/CMU-1_cog-wsi.tiff"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, tc.rel))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			tags, ok := s.LevelTIFFTags(0)
			if !ok {
				t.Fatalf("%s: LevelTIFFTags(0) ok=false", tc.format)
			}
			if _, ok := tags.Tag(256); !ok {
				t.Errorf("%s: level 0 missing ImageWidth (256)", tc.format)
			}
			if _, ok := tags.Tag(273); ok {
				t.Errorf("%s: StripOffsets (273) should be filtered", tc.format)
			}
			if _, ok := tags.Tag(324); ok {
				t.Errorf("%s: TileOffsets (324) should be filtered", tc.format)
			}
			dirs, ok := s.TIFFDirectories()
			if !ok || len(dirs) == 0 {
				t.Errorf("%s: TIFFDirectories empty: %d %v", tc.format, len(dirs), ok)
			}
			var hasL0 bool
			for _, d := range dirs {
				if d.Type == opentile.DirLevel && d.Image == 0 && d.Level == 0 {
					hasL0 = true
				}
			}
			if !hasL0 {
				t.Errorf("%s: no DirLevel for (image 0, level 0)", tc.format)
			}
		})
	}
}

// TestTIFFTagsNonTIFFExcluded: non-TIFF formats return ok=false everywhere.
func TestTIFFTagsNonTIFFExcluded(t *testing.T) {
	for _, rel := range []string{"szi/CMU-1.szi", "ife/cervix_2x_jpeg.iris"} {
		s, err := opentile.OpenFile(crossFixture(t, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if _, ok := s.LevelTIFFTags(0); ok {
			t.Errorf("%s: non-TIFF LevelTIFFTags should be ok=false", rel)
		}
		if _, ok := s.TIFFDirectories(); ok {
			t.Errorf("%s: non-TIFF TIFFDirectories should be ok=false", rel)
		}
		s.Close()
	}
}

// TestTIFFTagFidelity: a known ASCII tag decodes non-empty and Raw is kept.
func TestTIFFTagFidelity(t *testing.T) {
	s, err := opentile.OpenFile(crossFixture(t, "svs/CMU-1.svs"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tags, ok := s.LevelTIFFTags(0)
	if !ok {
		t.Fatal("ok=false")
	}
	desc, ok := tags.Tag(270) // ImageDescription
	if !ok {
		t.Fatal("missing ImageDescription")
	}
	v, ok := desc.ASCII()
	if !ok || len(v) == 0 {
		t.Fatalf("ASCII empty: %q %v", v, ok)
	}
	if len(desc.Raw) == 0 {
		t.Fatal("Raw bytes empty")
	}
}
