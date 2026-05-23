package ife

import (
	"fmt"

	opentile "github.com/wsilabs/opentile-go"
)

// IFE Encoding enum values per Iris-Headers/IrisCodecTypes.hpp:
//
//	TILE_ENCODING_UNDEFINED = 0
//	TILE_ENCODING_IRIS      = 1   // Iris-proprietary codec
//	TILE_ENCODING_JPEG      = 2
//	TILE_ENCODING_AVIF      = 3
const (
	encodingUndefined uint8 = 0
	encodingIRIS      uint8 = 1
	encodingJPEG      uint8 = 2
	encodingAVIF      uint8 = 3
)

// compressionFromEncoding maps the IFE TILE_TABLE.Encoding byte to
// opentile-go's [opentile.Compression] enum. Returns an error on
// undefined or unknown values; opentile-go reads but does not decode
// `CompressionIRIS` bytes — consumers either embed an Iris codec or
// 501 the request.
func compressionFromEncoding(e uint8) (opentile.Compression, error) {
	switch e {
	case encodingIRIS:
		return opentile.CompressionIRIS, nil
	case encodingJPEG:
		return opentile.CompressionJPEG, nil
	case encodingAVIF:
		return opentile.CompressionAVIF, nil
	case encodingUndefined:
		return opentile.CompressionUnknown, fmt.Errorf("ife: TILE_ENCODING_UNDEFINED is not a valid encoding")
	default:
		return opentile.CompressionUnknown, fmt.Errorf("ife: unknown encoding %d (want 1=IRIS, 2=JPEG, 3=AVIF)", e)
	}
}
