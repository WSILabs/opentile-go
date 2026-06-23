// Package assocdecode is a thin shared helper for decoding image-codec
// associated images through the decoder registry, so each format's
// AssociatedImage.Decode doesn't duplicate the registry plumbing.
//
// Strip-based (LZW / Deflate / uncompressed) associated images do NOT go
// through here — they use internal/tiffstrip, which owns the predictor +
// sample-interpretation knowledge.
package assocdecode

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/boxhalve"
)

// ViaCodec decodes faithful, standalone image-codec bytes (JPEG, JPEG 2000,
// HTJ2K, WebP, AVIF, JPEG XL) through the registered decoder for comp,
// honoring opts. Returns a wrapped decoder.ErrCodecUnavailable when no
// decoder is registered for the codec (e.g. a jp2k image under a nojp2k
// build), matching the region-decode path's behavior.
//
// PNG (CompressionPNG) is handled directly via the standard library — it is
// pure Go, needs no cgo decoder, and is not a TIFF codec (no compression tag),
// so it does not go through the registry. This covers PNG-encoded associated
// images, e.g. IFE IMAGE_ENCODING_PNG=1 (GH #74), and works under nocgo.
func ViaCodec(comp opentile.Compression, data []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if comp == opentile.CompressionPNG {
		return decodePNG(data, opts)
	}
	tag := opentile.CompressionToTIFFTag(comp)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, fmt.Errorf("assocdecode: no decoder registered for %s: %w", comp, decoder.ErrCodecUnavailable)
	}
	d := fac.New()
	defer d.Close()
	return d.Decode(data, opts)
}

// decodePNG decodes PNG bytes via image/png into a decoder.Image in the
// requested pixel format, honoring opts.Scale (1/2/4/8 via box downscale, to
// match the codec decoders' contract).
func decodePNG(data []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("assocdecode: png decode: %w", err)
	}
	out := pngImageToDecoder(src, opts.Format)
	switch opts.Scale {
	case 0, 1:
		// no scaling
	case 2:
		out = boxhalve.Halve(out, 1)
	case 4:
		out = boxhalve.Halve(out, 2)
	case 8:
		out = boxhalve.Halve(out, 3)
	default:
		return nil, decoder.ErrUnsupportedScale
	}
	return out, nil
}

// pngImageToDecoder converts a decoded image.Image to a decoder.Image in the
// requested format (RGB or RGBA; the zero PixelFormat is RGB). Uses the generic
// At().RGBA() path — associated images are small, so per-pixel access is fine.
func pngImageToDecoder(src image.Image, format decoder.PixelFormat) *decoder.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := decoder.NewImageFormat(w, h, format)
	bpp := 3
	if format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	for y := 0; y < h; y++ {
		rowOff := y * out.Stride
		for x := 0; x < w; x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA() // 16-bit per channel
			o := rowOff + x*bpp
			out.Pix[o] = uint8(r >> 8)
			out.Pix[o+1] = uint8(g >> 8)
			out.Pix[o+2] = uint8(bl >> 8)
			if bpp == 4 {
				out.Pix[o+3] = uint8(a >> 8)
			}
		}
	}
	return out
}
