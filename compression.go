package opentile

import "fmt"

// Compression identifies the bitstream format of a tile as stored in a TIFF.
//
// opentile-go returns tile bytes in the compression format of the source TIFF
// without decoding them. Consumers that need decoded pixels should pass the
// bytes to a codec appropriate for the reported compression.
//
// The zero value is CompressionUnknown: a forgotten-to-initialize field
// surfaces loudly rather than masquerading as a known compression.
type Compression uint8

const (
	CompressionUnknown Compression = iota // zero value; unset or unrecognized
	CompressionNone
	CompressionJPEG
	CompressionJP2K
	CompressionLZW  // TIFF tag 259 value 5 (Aperio SVS label is commonly LZW)
	CompressionAVIF // tile bytes are an AVIF image; consumer decodes via libavif
	// CompressionIRIS is the Iris-proprietary tile codec used by IFE files
	// when written through Iris-Codec. opentile-go reports it but does not
	// decode the bytes; consumers either embed an Iris codec or 501 the
	// request. JPEG and AVIF tiles in IFE remain decodable by external
	// codecs.
	CompressionIRIS
	// CompressionDeflate identifies the Deflate (zlib) bitstream
	// commonly used by scientific imaging TIFFs and the
	// generic-TIFF catch-all reader (v0.10). TIFF tag 259 values
	// 8 (Deflate) and 32946 (Adobe Deflate) both map here; the
	// payload is identical zlib-wrapped DEFLATE either way.
	CompressionDeflate
	// CompressionWebP identifies a WebP-encoded tile (RIFF + WEBP +
	// VP8/VP8L/VP8X chunks). TIFF tag 259 value 50001 in libtiff
	// convention; same value is what the user's wsi-tools transcoder
	// emits. Tile bytes are a complete self-contained WebP file.
	// Consumer decodes via libwebp or golang.org/x/image/webp.
	//
	// Added in v0.14.
	CompressionWebP
	// CompressionJPEGXL identifies a JPEG XL codestream tile. TIFF
	// tag 259 value 50002 (wsi-tools convention; not formally
	// registered). Tile bytes are a bare JXL codestream beginning
	// with the 0xFF 0x0A marker. Consumer decodes via libjxl (cgo)
	// or stdlib image/jxl when available.
	//
	// Added in v0.14.
	CompressionJPEGXL
	// CompressionHTJ2K identifies an HTJ2K (High-Throughput JPEG
	// 2000, ISO/IEC 15444-15) codestream tile. TIFF tag 259 value
	// 60003 (wsi-tools convention). Distinct from CompressionJP2K
	// because HTJ2K uses a different entropy coder (FBCOT instead
	// of EBCOT) and a standard JP2K decoder will fail on HTJ2K
	// bytes. Consumer decodes via OpenJPEG 2.5+, OpenHTJ2K, or
	// Kakadu.
	//
	// Added in v0.14.
	CompressionHTJ2K
)

func (c Compression) String() string {
	switch c {
	case CompressionUnknown:
		return "unknown"
	case CompressionNone:
		return "none"
	case CompressionJPEG:
		return "jpeg"
	case CompressionJP2K:
		return "jp2k"
	case CompressionLZW:
		return "lzw"
	case CompressionAVIF:
		return "avif"
	case CompressionIRIS:
		return "iris"
	case CompressionDeflate:
		return "deflate"
	case CompressionWebP:
		return "webp"
	case CompressionJPEGXL:
		return "jpeg-xl"
	case CompressionHTJ2K:
		return "htj2k"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(c))
	}
}
