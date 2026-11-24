package main

import (
	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/resample"
)

// Enum tables. The numeric values are the frozen OT_* constants in
// opentile.h (header_test.go checks every name/value pair listed here
// against the header).

var formatCodes = map[opentile.Format]int32{
	opentile.FormatUnknown:     0,
	opentile.FormatSVS:         1,
	opentile.FormatNDPI:        2,
	opentile.FormatPhilipsTIFF: 3,
	opentile.FormatOMETIFF:     4,
	opentile.FormatBIF:         5,
	opentile.FormatIFE:         6,
	opentile.FormatGenericTIFF: 7,
	opentile.FormatLeicaSCN:    8,
	opentile.FormatSZI:         9,
	opentile.FormatCOGWSI:      10,
	opentile.FormatDICOM:       11,
	opentile.FormatDZI:         12,
}

var compressionCodes = map[opentile.Compression]int32{
	opentile.CompressionUnknown: 0,
	opentile.CompressionNone:    1,
	opentile.CompressionJPEG:    2,
	opentile.CompressionJP2K:    3,
	opentile.CompressionLZW:     4,
	opentile.CompressionAVIF:    5,
	opentile.CompressionIRIS:    6,
	opentile.CompressionDeflate: 7,
	opentile.CompressionWebP:    8,
	opentile.CompressionJPEGXL:  9,
	opentile.CompressionHTJ2K:   10,
	opentile.CompressionPNG:     11,
}

var overlapCodes = map[opentile.OverlapMode]int32{
	opentile.OverlapNone:     0,
	opentile.OverlapBordered: 1,
	opentile.OverlapStitched: 2,
}

var kernelCodes = map[int32]resample.Kernel{
	1: resample.Nearest,
	2: resample.Bilinear,
	3: resample.Lanczos,
	4: resample.Box,
}

const (
	pixelRGB8  = 0
	pixelRGBA8 = 1

	backingDefault = 0
	backingMmap    = 1
	backingPread   = 2

	corruptTileError = 0
	corruptTileBlank = 1

	decodeSRGB = 1 << 0
	knownFlags = decodeSRGB
)

func formatCode(f opentile.Format) int32 { return formatCodes[f] } // unknown -> 0

func compressionCode(c opentile.Compression) int32 { return compressionCodes[c] }

func overlapCode(m opentile.OverlapMode) int32 { return overlapCodes[m] }

func pixelCode(f decoder.PixelFormat) int32 {
	if f == decoder.PixelFormatRGBA {
		return pixelRGBA8
	}
	return pixelRGB8
}

func bytesPerPixel(f decoder.PixelFormat) int {
	if f == decoder.PixelFormatRGBA {
		return 4
	}
	return 3
}

// compressionFromCode is the inverse of compressionCode.
func compressionFromCode(code int32) (opentile.Compression, bool) {
	for c, v := range compressionCodes {
		if v == code {
			return c, true
		}
	}
	return 0, false
}
