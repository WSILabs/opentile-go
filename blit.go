package opentile

import "github.com/wsilabs/opentile-go/decoder"

// blitInto copies a rectangular sub-region of src into dst at the
// given destination position. src and dst must have the same Format
// (RGB or RGBA). srcRect is (srcX, srcY, srcW, srcH) in src's coord
// space; (dstX, dstY) is the top-left of the destination position.
//
// Caller is responsible for bounds-checking (no clipping in this
// helper). Pure-Go memcpy per row.
func blitInto(src *decoder.Image, srcX, srcY, srcW, srcH int, dst *decoder.Image, dstX, dstY int) {
	bpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	rowBytes := srcW * bpp
	for row := 0; row < srcH; row++ {
		srcOff := (srcY+row)*src.Stride + srcX*bpp
		dstOff := (dstY+row)*dst.Stride + dstX*bpp
		copy(dst.Pix[dstOff:dstOff+rowBytes], src.Pix[srcOff:srcOff+rowBytes])
	}
}
