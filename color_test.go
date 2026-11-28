package opentile_test

import (
	"bytes"
	"errors"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/internal/testutil"
)

func TestColorManagementAvailable(t *testing.T) {
	if got := opentile.ColorManagementAvailable(); got != wantColorManagement {
		t.Errorf("ColorManagementAvailable() = %v, want %v for this build", got, wantColorManagement)
	}
}

func cloneImage(img *decoder.Image) *decoder.Image {
	c := *img
	c.Pix = append([]byte(nil), img.Pix...)
	return &c
}

// openWithProfile opens Ventana-1.bif, which carries an ICC profile.
func openWithProfile(t *testing.T) *opentile.Slide {
	t.Helper()
	s, err := opentile.OpenFile(crossFixture(t, "bif/Ventana-1.bif")) // t.Skip if missing
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if len(s.ICCProfile()) == 0 {
		t.Fatal("Ventana-1.bif: expected an ICC profile")
	}
	return s
}

// srgbReads runs every WithSRGB read path once and returns their errors.
func srgbReads(s *opentile.Slide) map[string]error {
	levels := s.Levels()
	lvl := levels[len(levels)-1]
	p := s.Pyramid(0)
	r := opentile.Region{Size: opentile.Size{W: 16, H: 16}}
	tile := decoder.NewImageFormat(lvl.TileSize.W, lvl.TileSize.H, decoder.PixelFormatRGB)
	region := decoder.NewImageFormat(16, 16, decoder.PixelFormatRGB)
	errs := map[string]error{}
	_, errs["DecodedTile"] = lvl.DecodedTile(0, 0, opentile.WithSRGB())
	errs["DecodedTileInto"] = lvl.DecodedTileInto(0, 0, tile, opentile.WithSRGB())
	_, errs["ReadRegion"] = lvl.ReadRegion(r, opentile.WithSRGB())
	errs["ReadRegionInto"] = lvl.ReadRegionInto(r.Origin, region, opentile.WithSRGB())
	_, errs["StitchedTile"] = lvl.StitchedTile(0, 0, opentile.WithSRGB())
	_, errs["ReadRegionScaled"] = p.ReadRegionScaled(r, opentile.Size{W: 8, H: 8}, opentile.WithSRGB())
	errs["ReadRegionScaledInto"] = p.ReadRegionScaledInto(r, decoder.NewImageFormat(8, 8, decoder.PixelFormatRGB), opentile.WithSRGB())
	_, errs["RenderThumbnail"] = s.RenderThumbnail(opentile.Size{W: 64}, opentile.WithSRGB())
	_, errs["RenderMacro"] = s.RenderMacro(opentile.Size{W: 64}, opentile.WithSRGB())
	return errs
}

// A profile that can't be applied fails every WithSRGB read, before
// decoding, with the error CheckSRGB reports; ToSRGB reports false. The
// hook swap simulates both causes in any build.
func TestWithSRGBProfileNotApplied(t *testing.T) {
	cases := []struct {
		name string
		hook func([]byte, bool) (func([]byte, int, int, int), func(), bool)
		want error
	}{
		{"no color management", nil, opentile.ErrColorManagementUnavailable},
		{"unusable profile", func([]byte, bool) (func([]byte, int, int, int), func(), bool) {
			return nil, nil, false
		}, opentile.ErrICCProfileUnusable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer opentile.SetSRGBHookForTest(c.hook)()
			s := openWithProfile(t)
			if err := s.CheckSRGB(); !errors.Is(err, c.want) {
				t.Fatalf("CheckSRGB() = %v, want %v", err, c.want)
			}
			for name, err := range srgbReads(s) {
				if !errors.Is(err, c.want) {
					t.Errorf("%s + WithSRGB: err = %v, want %v", name, err, c.want)
				}
			}
			img := decoder.NewImageFormat(4, 4, decoder.PixelFormatRGB)
			if s.ToSRGB(img) || img.ColorManaged {
				t.Error("ToSRGB reported a conversion it couldn't apply")
			}
			// Without WithSRGB the slide reads normally.
			levels := s.Levels()
			_, err := levels[len(levels)-1].DecodedTile(0, 0)
			testutil.SkipIfNoDecode(t, err)
			if err != nil {
				t.Errorf("DecodedTile without WithSRGB: %v", err)
			}
		})
	}
}

// A slide with no ICC profile is taken as sRGB: WithSRGB returns the
// pixels as decoded, unflagged, in any build.
func TestWithSRGBNoProfile(t *testing.T) {
	for _, rel := range []string{
		"svs/CMU-1-Small-Region.svs", "svs/590_crop_none_imagescope.tif",
		"svs/590_crop_lzw_imagescope.tif", "ome-tiff/CMU-1-Small-Region.ome.tiff",
		"ndpi/CMU-1.ndpi", "philips-tiff/Philips-4.tiff",
	} {
		t.Run(rel, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, rel))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if len(s.ICCProfile()) != 0 {
				t.Skip("has an ICC profile")
			}
			if err := s.CheckSRGB(); err != nil {
				t.Fatalf("CheckSRGB() = %v, want nil", err)
			}
			levels := s.Levels()
			lvl := levels[len(levels)-1]
			plain, err := lvl.DecodedTile(0, 0)
			testutil.SkipIfNoDecode(t, err)
			if err != nil {
				t.Fatal(err)
			}
			conv, err := lvl.DecodedTile(0, 0, opentile.WithSRGB())
			if err != nil {
				t.Fatal(err)
			}
			if conv.ColorManaged || !bytes.Equal(conv.Pix, plain.Pix) {
				t.Errorf("WithSRGB changed an untagged slide (ColorManaged %v)", conv.ColorManaged)
			}
			if s.ToSRGB(plain) {
				t.Error("ToSRGB reported a conversion on an untagged slide")
			}
		})
	}
}

// Ventana-1.bif carries an ICC profile: WithSRGB converts the read when
// the build has color management, and fails it when not.
func TestWithSRGB(t *testing.T) {
	s := openWithProfile(t)
	if !wantColorManagement {
		if err := s.CheckSRGB(); !errors.Is(err, opentile.ErrColorManagementUnavailable) {
			t.Fatalf("CheckSRGB() = %v, want ErrColorManagementUnavailable", err)
		}
		return // the read paths are covered by TestWithSRGBProfileNotApplied
	}
	if err := s.CheckSRGB(); err != nil {
		t.Fatalf("CheckSRGB() = %v", err)
	}
	for name, err := range srgbReads(s) {
		testutil.SkipIfNoDecode(t, err)
		if err != nil {
			t.Errorf("%s + WithSRGB: %v", name, err)
		}
	}
	levels := s.Levels()
	lvl := levels[len(levels)-1]
	r := opentile.Region{Size: lvl.Size}

	for _, f := range []decoder.PixelFormat{decoder.PixelFormatRGB, decoder.PixelFormatRGBA} {
		plain, err := lvl.ReadRegion(r, opentile.WithFormat(f))
		testutil.SkipIfNoDecode(t, err)
		if err != nil {
			t.Fatal(err)
		}
		if plain.ColorManaged {
			t.Fatal("ColorManaged set without WithSRGB")
		}
		conv, err := lvl.ReadRegion(r, opentile.WithFormat(f), opentile.WithSRGB())
		if err != nil {
			t.Fatal(err)
		}
		if !conv.ColorManaged {
			t.Fatalf("format %d: ColorManaged not set", f)
		}
		if bytes.Equal(conv.Pix, plain.Pix) {
			t.Errorf("format %d: WithSRGB left every pixel unchanged", f)
		}
		// ToSRGB on the unconverted read matches the WithSRGB read.
		manual := cloneImage(plain)
		if !s.ToSRGB(manual) || !manual.ColorManaged {
			t.Fatal("ToSRGB not applied")
		}
		if !bytes.Equal(manual.Pix, conv.Pix) {
			t.Errorf("format %d: ToSRGB differs from WithSRGB", f)
		}
		// Converting again is a no-op.
		if !s.ToSRGB(manual) || !bytes.Equal(manual.Pix, conv.Pix) {
			t.Errorf("format %d: second ToSRGB changed the image", f)
		}
		if f == decoder.PixelFormatRGBA {
			for i := 3; i < len(conv.Pix); i += 4 {
				if conv.Pix[i] != plain.Pix[i] {
					t.Fatalf("alpha changed at byte %d", i)
				}
			}
		}
	}

	// Into: a reused destination's flag reflects the latest read.
	dst := decoder.NewImageFormat(r.Size.W, r.Size.H, decoder.PixelFormatRGB)
	if err := lvl.ReadRegionInto(r.Origin, dst, opentile.WithSRGB()); err != nil {
		t.Fatal(err)
	}
	if !dst.ColorManaged {
		t.Error("ReadRegionInto + WithSRGB: ColorManaged not set")
	}
	if err := lvl.ReadRegionInto(r.Origin, dst); err != nil {
		t.Fatal(err)
	}
	if dst.ColorManaged {
		t.Error("ReadRegionInto without WithSRGB kept ColorManaged from the previous read")
	}

	// The thumbnail and macro renders convert once, after compositing.
	thumb, err := s.RenderThumbnail(opentile.Size{W: 128}, opentile.WithSRGB())
	if err != nil {
		t.Fatal(err)
	}
	plainThumb, err := s.RenderThumbnail(opentile.Size{W: 128})
	if err != nil {
		t.Fatal(err)
	}
	s.ToSRGB(plainThumb)
	if !thumb.ColorManaged || !bytes.Equal(thumb.Pix, plainThumb.Pix) {
		t.Error("RenderThumbnail + WithSRGB differs from ToSRGB on the plain render")
	}
	macro, err := s.RenderMacro(opentile.Size{W: 200}, opentile.WithSRGB())
	if err != nil {
		t.Fatal(err)
	}
	plainMacro, err := s.RenderMacro(opentile.Size{W: 200})
	if err != nil {
		t.Fatal(err)
	}
	s.ToSRGB(plainMacro)
	if !macro.ColorManaged || !bytes.Equal(macro.Pix, plainMacro.Pix) {
		t.Error("RenderMacro + WithSRGB differs from ToSRGB on the plain render (double conversion?)")
	}
}
