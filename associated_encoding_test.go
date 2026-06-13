package opentile_test

import (
	"bytes"
	"image"
	stdjpeg "image/jpeg"
	"io"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

// adobeAPP14RGB signals an RGB (transform=0) colorspace to a bare JPEG decoder
// — equivalent to what a TIFF reader infers from PhotometricInterpretation=RGB.
var adobeAPP14RGB = []byte{0xFF, 0xEE, 0x00, 0x0E, 'A', 'd', 'o', 'b', 'e', 0x00, 0x64, 0x00, 0x00, 0x00, 0x00, 0x00}

// spliceJPEG inserts the JPEGTables DQT/DHT (sans SOI/EOI) — and, when the
// page is photometric RGB, an Adobe APP14 RGB marker — right after a strip's
// SOI. This is what a conforming reader does with tags 347 + 262 to make the
// abbreviated strip a standalone, correctly-colored JPEG.
func spliceJPEG(strip, tables []byte, rgb bool) []byte {
	if len(strip) < 2 {
		return strip
	}
	out := make([]byte, 0, len(strip)+len(tables)+len(adobeAPP14RGB))
	out = append(out, strip[:2]...) // SOI
	if len(tables) >= 4 {
		out = append(out, tables[2:len(tables)-2]...) // tables, drop SOI/EOI
	}
	if rgb {
		out = append(out, adobeAPP14RGB...)
	}
	out = append(out, strip[2:]...)
	return out
}

// reconstructFromEncoding rebuilds the full RGB image a conforming reader would
// produce from an AssociatedEncoding — using stdlib image/jpeg for JPEG strips
// (independent of opentile-go's decode) and internal/tifflzw + the exposed
// Predictor for LZW. This is the faithful-standalone check (GH #22).
func reconstructFromEncoding(t *testing.T, src opentile.AssociatedEncoding, w, h int) image.Image {
	t.Helper()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	switch src.Compression {
	case opentile.CompressionJPEG:
		y0 := 0
		for i, strip := range src.Strips {
			sub, err := stdjpeg.Decode(bytes.NewReader(spliceJPEG(strip, src.JPEGTables, src.Photometric == 2)))
			if err != nil {
				t.Fatalf("strip %d: stdlib image/jpeg rejected the standalone strip: %v", i, err)
			}
			b := sub.Bounds()
			for y := 0; y < b.Dy() && y0+y < h; y++ {
				for x := 0; x < b.Dx() && x < w; x++ {
					out.Set(x, y0+y, sub.At(b.Min.X+x, b.Min.Y+y))
				}
			}
			y0 += b.Dy()
		}
	case opentile.CompressionLZW, opentile.CompressionNone:
		samples := src.Samples
		if samples == 0 {
			samples = 1
		}
		rps := src.RowsPerStrip
		if rps <= 0 {
			rps = h
		}
		rowBytes := w * samples
		raster := make([]byte, 0, rowBytes*h)
		for i, strip := range src.Strips {
			dec := strip
			if src.Compression == opentile.CompressionLZW {
				var err error
				if dec, err = io.ReadAll(tifflzw.NewReader(bytes.NewReader(strip), tifflzw.MSB, 8)); err != nil {
					t.Fatalf("strip %d lzw: %v", i, err)
				}
			}
			rows := rps
			if start := i * rps; start+rows > h {
				rows = h - start
			}
			raster = append(raster, dec[:rows*rowBytes]...)
		}
		// reverse Predictor=2 (horizontal differencing) per row when present
		if src.Predictor == 2 {
			for row := 0; row < h; row++ {
				base := row * rowBytes
				for x := 1; x < w; x++ {
					for c := 0; c < samples; c++ {
						raster[base+x*samples+c] += raster[base+(x-1)*samples+c]
					}
				}
			}
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				p := y*rowBytes + x*samples
				var r, g, b byte
				if samples == 1 {
					v := raster[p]
					if src.Photometric == 0 { // WhiteIsZero
						v = 255 - v
					}
					r, g, b = v, v, v
				} else {
					r, g, b = raster[p], raster[p+1], raster[p+2]
				}
				o := (y*w + x) * 4
				out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = r, g, b, 0xFF
			}
		}
	default:
		t.Fatalf("unexpected source compression %s", src.Compression)
	}
	return out
}

// TestAssociatedEncodingRoundtrip is the GH #22 acceptance: the source strips +
// tags from AssociatedEncoding, written into a standalone IFD, decode
// correctly via an independent reader and match AssociatedImage.Decode().
func TestAssociatedEncodingRoundtrip(t *testing.T) {
	for _, rel := range []string{"svs/CMU-1-Small-Region.svs", "svs/CMU-1.svs", "generic-tiff/CMU-1.stripped.tiff", "cog-wsi/CMU-1_cog-wsi.tiff", "bif/Ventana-1.bif", "philips-tiff/Philips-3.tiff", "ndpi/OS-2.ndpi"} {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, rel))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got := 0
			for _, a := range s.Associated() {
				// Known-corrupt fixture (wsitools cogwsiwriter LZW bug, GH #20
				// note / WSILabs/wsitools#1): its LZW label can't be decoded by
				// anything; AssociatedEncoding returns the (corrupt) strips but
				// they won't reconstruct. Skip until the fixture is regenerated.
				if rel == "cog-wsi/CMU-1_cog-wsi.tiff" && a.Type() == "label" {
					continue
				}
				src, ok := s.AssociatedEncoding(a)
				if !ok {
					t.Logf("%s %q: no source (ok=false)", rel, a.Type())
					continue
				}
				got++
				w, h := a.Size().W, a.Size().H
				recon := reconstructFromEncoding(t, src, w, h)

				ref, err := a.Decode(decoder.DecodeOptions{Format: decoder.PixelFormatRGBA})
				if err != nil {
					t.Fatalf("%s %q Decode: %v", rel, a.Type(), err)
				}
				// Compare a strided sample of pixels (JPEG is lossy-equal to
				// the same decoder; both go through libjpeg vs stdlib so allow
				// a small tolerance).
				var diff, n int64
				for y := 0; y < h; y += 3 {
					for x := 0; x < w; x += 3 {
						rp := recon.(*image.RGBA).PixOffset(x, y)
						dp := y*ref.Stride + x*4
						for c := 0; c < 3; c++ {
							d := int(recon.(*image.RGBA).Pix[rp+c]) - int(ref.Pix[dp+c])
							if d < 0 {
								d = -d
							}
							diff += int64(d)
							n++
						}
					}
				}
				mean := float64(diff) / float64(n)
				if mean > 3.0 {
					t.Errorf("%s %q: reconstruction mean abs diff %.2f vs Decode (too large)", rel, a.Type(), mean)
				}
			}
			if got == 0 {
				t.Fatalf("%s: no associated image exposed a source", rel)
			}
		})
	}
}
