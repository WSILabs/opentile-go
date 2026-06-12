package tiffstrip

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

// applyHorizontalPredictor is the forward (encode) of Predictor=2: store each
// sample as the delta from the previous pixel's same component. Right-to-left
// so we read original values.
func applyHorizontalPredictor(raster []byte, w, h, samples int) {
	rowBytes := w * samples
	for row := 0; row < h; row++ {
		base := row * rowBytes
		for x := w - 1; x >= 1; x-- {
			for c := 0; c < samples; c++ {
				raster[base+x*samples+c] -= raster[base+(x-1)*samples+c]
			}
		}
	}
}

func lzwEncode(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tifflzw.NewWriter(&buf, tifflzw.MSB, 8)
	if _, err := w.Write(raw); err != nil {
		t.Fatalf("lzw write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("lzw close: %v", err)
	}
	return buf.Bytes()
}

// makeRGB builds a deterministic w×h RGB raster with a gradient + texture so
// horizontal differencing actually exercises wraparound.
func makeRGB(w, h int) []byte {
	raw := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			raw[i] = byte(x*7 + y*3)
			raw[i+1] = byte(x*13 + y*5 + 40)
			raw[i+2] = byte(x*3 ^ y*11)
		}
	}
	return raw
}

func TestDecodeLZWPredictor2_RGB(t *testing.T) {
	const w, h = 17, 9 // not strip-aligned; multi-strip below
	orig := makeRGB(w, h)
	enc := append([]byte(nil), orig...)
	applyHorizontalPredictor(enc, w, h, 3)

	// Single strip.
	p := Params{
		Width: w, Height: h, Samples: 3, Photometric: 2, Predictor: 2,
		Compression: CompLZW, RowsPerStrip: h, Strips: [][]byte{lzwEncode(t, enc)},
	}
	img, err := Decode(p, decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img.Width != w || img.Height != h || img.Format != decoder.PixelFormatRGB {
		t.Fatalf("img = %dx%d fmt=%v", img.Width, img.Height, img.Format)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			so := (y*w + x) * 3
			do := y*img.Stride + x*3
			for c := 0; c < 3; c++ {
				if img.Pix[do+c] != orig[so+c] {
					t.Fatalf("pixel (%d,%d) c%d: got %d want %d", x, y, c, img.Pix[do+c], orig[so+c])
				}
			}
		}
	}
}

func TestDecodeLZWPredictor2_MultiStrip(t *testing.T) {
	const w, h, rps = 17, 9, 4 // 3 strips: 4,4,1 rows
	orig := makeRGB(w, h)
	enc := append([]byte(nil), orig...)
	applyHorizontalPredictor(enc, w, h, 3)

	var strips [][]byte
	rowBytes := w * 3
	for start := 0; start < h; start += rps {
		rows := rps
		if start+rows > h {
			rows = h - start
		}
		strips = append(strips, lzwEncode(t, enc[start*rowBytes:(start+rows)*rowBytes]))
	}
	p := Params{Width: w, Height: h, Samples: 3, Photometric: 2, Predictor: 2,
		Compression: CompLZW, RowsPerStrip: rps, Strips: strips}
	img, err := Decode(p, decoder.DecodeOptions{Format: decoder.PixelFormatRGBA})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img.Format != decoder.PixelFormatRGBA {
		t.Fatalf("format = %v", img.Format)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			so := (y*w + x) * 3
			do := y*img.Stride + x*4
			for c := 0; c < 3; c++ {
				if img.Pix[do+c] != orig[so+c] {
					t.Fatalf("pixel (%d,%d) c%d: got %d want %d", x, y, c, img.Pix[do+c], orig[so+c])
				}
			}
			if img.Pix[do+3] != 0xFF {
				t.Fatalf("alpha (%d,%d) = %d want 255", x, y, img.Pix[do+3])
			}
		}
	}
}

func TestDecodeNone_Gray(t *testing.T) {
	const w, h = 5, 3
	raw := make([]byte, w*h)
	for i := range raw {
		raw[i] = byte(i * 9)
	}
	p := Params{Width: w, Height: h, Samples: 1, Photometric: 1, Compression: CompNone, Strips: [][]byte{raw}}
	img, err := Decode(p, decoder.DecodeOptions{})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := raw[y*w+x]
			do := y*img.Stride + x*3
			if img.Pix[do] != v || img.Pix[do+1] != v || img.Pix[do+2] != v {
				t.Fatalf("gray (%d,%d): got %d,%d,%d want %d", x, y, img.Pix[do], img.Pix[do+1], img.Pix[do+2], v)
			}
		}
	}
}

func TestDecodeScaleUnsupported(t *testing.T) {
	p := Params{Width: 2, Height: 2, Samples: 3, Compression: CompNone, Strips: [][]byte{make([]byte, 12)}}
	if _, err := Decode(p, decoder.DecodeOptions{Scale: 2}); err != decoder.ErrUnsupportedScale {
		t.Fatalf("Scale=2: got %v, want ErrUnsupportedScale", err)
	}
}
