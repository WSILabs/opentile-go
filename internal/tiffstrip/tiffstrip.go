// Package tiffstrip decodes strip-based (non-image-codec) TIFF associated
// images — uncompressed, LZW, or Deflate, with optional horizontal
// differencing (Predictor=2) — into faithful decoder.Image RGB(A) pixels.
//
// Image-codec associated images (JPEG, JP2K, WebP, …) are NOT handled here;
// those decode through the decoder registry on their compressed bytes. This
// package owns the TIFF strip + predictor + sample-interpretation knowledge
// that the per-format Bytes() path historically lost — notably the
// LZW + Predictor=2 Aperio SVS label, whose re-encoded Bytes() dropped the
// predictor and round-tripped incompatibly (GH #20).
//
// Only 8-bit samples are supported — the universe of strip-based associated
// images opentile-go surfaces (labels, uncompressed maps).
package tiffstrip

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

// TIFF Compression tag (259) values this package handles.
const (
	CompNone         = 1
	CompLZW          = 5
	CompDeflate      = 8
	CompAdobeDeflate = 32946
)

// Params describes a strip-based TIFF associated image.
type Params struct {
	Width, Height int
	Samples       int // SamplesPerPixel: 1 (gray), 3 (RGB), 4 (RGBA). 0 → 1.
	Photometric   int // 0 WhiteIsZero, 1 BlackIsZero, 2 RGB
	Predictor     int // 1/0 none (default), 2 horizontal differencing
	Compression   int // TIFF tag 259
	RowsPerStrip  int // 0 → whole image in one strip
	Strips        [][]byte
}

// Decode decompresses the strips, reverses Predictor=2 if present, and
// interprets the samples into RGB(A) pixels honoring opts.Format
// (PixelFormatRGB default, or PixelFormatRGBA). opts.Scale != 1 returns
// decoder.ErrUnsupportedScale — strip formats decode at native resolution.
func Decode(p Params, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Scale > 1 {
		return nil, decoder.ErrUnsupportedScale
	}
	if p.Width <= 0 || p.Height <= 0 {
		return nil, fmt.Errorf("tiffstrip: invalid dimensions %dx%d", p.Width, p.Height)
	}
	samples := p.Samples
	if samples == 0 {
		samples = 1
	}
	if samples != 1 && samples != 3 && samples != 4 {
		return nil, fmt.Errorf("tiffstrip: unsupported SamplesPerPixel %d", samples)
	}

	raster, err := decompress(p, samples)
	if err != nil {
		return nil, err
	}
	if p.Predictor == 2 {
		unapplyHorizontalPredictor(raster, p.Width, p.Height, samples)
	}
	return toImage(raster, p.Width, p.Height, samples, p.Photometric, opts.Format), nil
}

// decompress reads every strip, clips each to its row count, and concatenates
// row-major into a full Width*Height*Samples 8-bit raster.
func decompress(p Params, samples int) ([]byte, error) {
	rowBytes := p.Width * samples
	total := rowBytes * p.Height
	rps := p.RowsPerStrip
	if rps <= 0 {
		rps = p.Height // TIFF default: one strip = whole image
	}
	raster := make([]byte, 0, total)
	for i, strip := range p.Strips {
		dec, err := decompressStrip(p.Compression, strip)
		if err != nil {
			return nil, fmt.Errorf("tiffstrip: strip %d: %w", i, err)
		}
		rows := rps
		if start := i * rps; start+rows > p.Height {
			rows = p.Height - start
		}
		if rows <= 0 {
			continue // extra strips beyond image height — ignore
		}
		want := rows * rowBytes
		if len(dec) < want {
			return nil, fmt.Errorf("tiffstrip: strip %d short: got %d bytes, want %d", i, len(dec), want)
		}
		raster = append(raster, dec[:want]...)
	}
	if len(raster) != total {
		return nil, fmt.Errorf("tiffstrip: raster %d != expected %d (w=%d h=%d samples=%d)",
			len(raster), total, p.Width, p.Height, samples)
	}
	return raster, nil
}

func decompressStrip(comp int, strip []byte) ([]byte, error) {
	switch comp {
	case CompNone, 0:
		return strip, nil
	case CompLZW:
		r := tifflzw.NewReader(bytes.NewReader(strip), tifflzw.MSB, 8)
		defer r.Close()
		return io.ReadAll(r)
	case CompDeflate, CompAdobeDeflate:
		zr, err := zlib.NewReader(bytes.NewReader(strip))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	default:
		return nil, fmt.Errorf("tiffstrip: unsupported compression %d", comp)
	}
}

// unapplyHorizontalPredictor reverses TIFF Predictor=2 (8-bit horizontal
// differencing): each sample after the first in a row is stored as the delta
// from the same component of the previous pixel. Accumulate left-to-right,
// per row, per component, with byte (mod-256) wraparound. Rows are
// independent (the first pixel of each row is absolute), so this is correct
// regardless of strip boundaries.
func unapplyHorizontalPredictor(raster []byte, w, h, samples int) {
	rowBytes := w * samples
	for row := 0; row < h; row++ {
		base := row * rowBytes
		for x := 1; x < w; x++ {
			for c := 0; c < samples; c++ {
				raster[base+x*samples+c] += raster[base+(x-1)*samples+c]
			}
		}
	}
}

// toImage interprets the raster into the requested pixel format. Gray
// (samples=1) replicates to R=G=B, honoring WhiteIsZero (photometric 0).
func toImage(raster []byte, w, h, samples, photometric int, format decoder.PixelFormat) *decoder.Image {
	img := decoder.NewImageFormat(w, h, format)
	dstBpp := 3
	if format == decoder.PixelFormatRGBA {
		dstBpp = 4
	}
	srcRow := w * samples
	whiteIsZero := samples == 1 && photometric == 0
	for y := 0; y < h; y++ {
		so := y * srcRow
		do := y * img.Stride
		for x := 0; x < w; x++ {
			var r, g, b byte
			switch samples {
			case 1:
				v := raster[so+x]
				if whiteIsZero {
					v = 255 - v
				}
				r, g, b = v, v, v
			default: // 3 or 4
				r = raster[so+x*samples]
				g = raster[so+x*samples+1]
				b = raster[so+x*samples+2]
			}
			pix := do + x*dstBpp
			img.Pix[pix] = r
			img.Pix[pix+1] = g
			img.Pix[pix+2] = b
			if dstBpp == 4 {
				img.Pix[pix+3] = 0xFF
			}
		}
	}
	return img
}
