package opentile_test

import (
	"bytes"
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

// Ventana-1.bif carries an ICC profile. WithSRGB converts the read when
// the build has color management, and is a no-op (not an error) when not.
func TestWithSRGB(t *testing.T) {
	s, err := opentile.OpenFile(crossFixture(t, "bif/Ventana-1.bif")) // t.Skip if missing
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.ICCProfile()) == 0 {
		t.Fatal("Ventana-1.bif: expected an ICC profile")
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
		if conv.ColorManaged != wantColorManagement {
			t.Fatalf("format %d: ColorManaged = %v, want %v", f, conv.ColorManaged, wantColorManagement)
		}
		if !wantColorManagement {
			if !bytes.Equal(conv.Pix, plain.Pix) {
				t.Errorf("format %d: pixels changed without color management", f)
			}
			if s.ToSRGB(cloneImage(plain)) {
				t.Errorf("ToSRGB reported applied without color management")
			}
			continue
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
	if dst.ColorManaged != wantColorManagement {
		t.Errorf("ReadRegionInto + WithSRGB: ColorManaged = %v", dst.ColorManaged)
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
	if thumb.ColorManaged != wantColorManagement {
		t.Errorf("RenderThumbnail: ColorManaged = %v", thumb.ColorManaged)
	}
	if wantColorManagement {
		s.ToSRGB(plainThumb)
	}
	if !bytes.Equal(thumb.Pix, plainThumb.Pix) {
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
	if wantColorManagement {
		s.ToSRGB(plainMacro)
	}
	if macro.ColorManaged != wantColorManagement || !bytes.Equal(macro.Pix, plainMacro.Pix) {
		t.Error("RenderMacro + WithSRGB differs from ToSRGB on the plain render (double conversion?)")
	}
}
