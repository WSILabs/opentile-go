package opentile_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// pixSpan reports the min and max byte across a strided sample of pix.
func pixSpan(pix []byte) (mn, mx byte) {
	mn, mx = 255, 0
	step := 1
	if len(pix) > 1<<16 {
		step = len(pix) / (1 << 16)
	}
	for i := 0; i < len(pix); i += step {
		if pix[i] < mn {
			mn = pix[i]
		}
		if pix[i] > mx {
			mx = pix[i]
		}
	}
	return
}

// checkDecode decodes a and asserts: correct dims, requested format, and a
// non-constant raster (a constant image signals garbage / a wrong decode).
func checkDecode(t *testing.T, label string, a opentile.AssociatedImage, format decoder.PixelFormat) {
	t.Helper()
	img, err := a.Decode(decoder.DecodeOptions{Format: format})
	if err != nil {
		t.Errorf("%s %q Decode(fmt=%v): %v", label, a.Type(), format, err)
		return
	}
	if img.Width != a.Size().W || img.Height != a.Size().H {
		t.Errorf("%s %q: decoded %dx%d != Size %dx%d", label, a.Type(), img.Width, img.Height, a.Size().W, a.Size().H)
	}
	if img.Format != format {
		t.Errorf("%s %q: format %v != requested %v", label, a.Type(), img.Format, format)
	}
	bpp := 3
	if format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	if want := img.Height * img.Width * bpp; len(img.Pix) < want-img.Width*bpp { // allow stride padding
		t.Errorf("%s %q: pix len %d too small for %dx%d bpp %d", label, a.Type(), len(img.Pix), img.Width, img.Height, bpp)
	}
	if mn, mx := pixSpan(img.Pix); mn == mx {
		t.Errorf("%s %q: decoded to a constant image (%d) — likely garbage", label, a.Type(), mn)
	}
}

// TestAssociatedDecodeAllFormats is the GH #20 acceptance gate: every
// associated image of every format fixture decodes to correct RGB(A) through
// the opentile-go API alone — no source-file re-parsing. Covers JPEG
// (thumbnail/overview), LZW+Predictor=2 (Aperio labels), uncompressed
// (NDPI map), and the generic-TIFF strip codecs.
func TestAssociatedDecodeAllFormats(t *testing.T) {
	cases := []string{
		"svs/CMU-1-Small-Region.svs",
		"svs/CMU-1.svs",
		"svs/JP2K-33003-1.svs",
		"svs/scan_617_.svs",
		"ndpi/CMU-1.ndpi",
		"ndpi/OS-2.ndpi", // carries a Map page (uncompressed)
		"philips-tiff/Philips-3.tiff",
		"ome-tiff/Leica-1.ome.tiff",
		"bif/Ventana-1.bif",
		"ife/cervix_2x_jpeg.iris",
		"generic-tiff/CMU-1.stripped.tiff", // LZW label + JPEG thumbnail/macro
		"leica-scn/Leica-1.scn",
		"szi/CMU-1.szi",
		"cog-wsi/CMU-1_cog-wsi.tiff",
	}
	for _, rel := range cases {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, rel)) // t.Skip if missing
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			assoc := s.AssociatedImages()
			if len(assoc) == 0 {
				t.Skipf("%s: no associated images", rel)
			}
			for _, a := range assoc {
				// (cog-wsi/CMU-1_cog-wsi.tiff's label was previously corrupt
				// under WSILabs/wsitools#1 and special-cased to expect an error.
				// The cog-wsi fixtures were regenerated with a fixed
				// cogwsiwriter; the label now decodes correctly, so it goes
				// through the normal check like every other associated image.)
				checkDecode(t, rel, a, decoder.PixelFormatRGB)
				checkDecode(t, rel, a, decoder.PixelFormatRGBA)
			}
		})
	}
}

// TestAssociatedDecodeLZWLabel is the focused acceptance for the issue's
// headline: Aperio SVS labels are LZW + Predictor=2, and must decode to the
// full correct image (not a truncated / hue-shifted fraction). We assert the
// label decodes to its full advertised size with real content, and that the
// decode is deterministic (re-decode is byte-identical).
func TestAssociatedDecodeLZWLabel(t *testing.T) {
	for _, rel := range []string{"svs/CMU-1.svs", "svs/CMU-2.svs", "svs/scan_617_.svs"} {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, rel))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var label opentile.AssociatedImage
			for _, a := range s.AssociatedImages() {
				if a.Type() == "label" {
					label = a
				}
			}
			if label == nil {
				t.Skipf("%s: no label", rel)
			}
			if label.Compression() != opentile.CompressionLZW {
				t.Skipf("%s: label is %s, not LZW", rel, label.Compression())
			}
			img, err := label.Decode(decoder.DecodeOptions{})
			if err != nil {
				t.Fatalf("label Decode: %v", err)
			}
			if img.Width != label.Size().W || img.Height != label.Size().H {
				t.Fatalf("label decoded %dx%d != %dx%d (truncated?)", img.Width, img.Height, label.Size().W, label.Size().H)
			}
			if mn, mx := pixSpan(img.Pix); mn == mx {
				t.Fatalf("label decoded to a constant image (%d)", mn)
			}
			// Determinism: re-decode must be byte-identical.
			img2, err := label.Decode(decoder.DecodeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if string(img.Pix) != string(img2.Pix) {
				t.Fatal("label Decode is non-deterministic")
			}
		})
	}
}
