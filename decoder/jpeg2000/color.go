package jpeg2000

import (
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/j2kheader"
)

// JP2 'colr' box enumerated colorspace values.
const (
	jp2EnumSRGB = 16
	jp2EnumGray = 17
	jp2EnumSYCC = 18
)

// decodedColorSpaceFromHeader reports the colorspace of the planes OpenJPEG hands
// opentile's jpeg2000 decoder, before its YCbCr→RGB normalization. It is the
// decode-policy counterpart to the stored ColorEncoding: an MCT codestream
// decodes to RGB (OpenJPEG inverts the MCT); an sYCC box or an unsignalled raw
// codestream (the Aperio-33003 convention) is treated as YCbCr and converted; a
// single component is grayscale.
func decodedColorSpaceFromHeader(h j2kheader.Info) decoder.ColorEncoding {
	switch {
	case h.Components == 1:
		return decoder.ColorGrayscale
	case h.MCT:
		return decoder.ColorRGB // OpenJPEG already inverted the MCT
	}
	switch h.EnumColorspace {
	case jp2EnumSRGB:
		return decoder.ColorRGB
	case jp2EnumGray:
		return decoder.ColorGrayscale
	case jp2EnumSYCC:
		return decoder.ColorYCbCr
	}
	// No MCT, no decisive box: Aperio-33003 convention → YCbCr.
	return decoder.ColorYCbCr
}

// decodedColorSpace is the src-level form; on an unparseable header it falls back
// to the historical Aperio-33003 default (YCbCr), matching Decode.
func decodedColorSpace(src []byte) decoder.ColorEncoding {
	h, err := j2kheader.Parse(src)
	if err != nil {
		return decoder.ColorYCbCr
	}
	return decodedColorSpaceFromHeader(h)
}

// decodeIsYCbCr reports whether the decoded 3-component planes need a YCbCr→RGB
// conversion (GH #53). Single source of truth with DecodedColorSpace.
func decodeIsYCbCr(src []byte) bool {
	return decodedColorSpace(src) == decoder.ColorYCbCr
}
