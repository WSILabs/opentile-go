package opentile

import "github.com/wsilabs/opentile-go/decoder"

// CodecAvailable reports whether this build can decode tiles and associated
// images compressed with c, so callers can check before decoding instead of
// waiting for an error wrapping decoder.ErrCodecUnavailable.
//
// It answers from the same places a decode dispatches through: PNG is
// decoded with the standard library; every other compression goes through
// the decoder registry by its TIFF Compression tag. A codec is available
// when a decoder is registered for it and is not the stub a cgo codec
// registers when it is built out (cgo disabled, -tags nocgo or
// no<codec>). Registration comes from blank-importing decoder/all or a
// decoder/<codec> package; with neither, even the pure-Go codecs (none,
// LZW, Deflate) report false, as their decodes would fail. CompressionIRIS
// and CompressionUnknown have no decoder.
//
// Raw-tile reads do not need the codec, except for the formats whose raw
// tiles are made with libjpeg-turbo (NDPI stripped and one-frame levels,
// Philips sparse tiles, OME-TIFF one-frame levels).
func CodecAvailable(c Compression) bool {
	switch c {
	case CompressionPNG:
		return true // image/png
	case CompressionUnknown, CompressionIRIS:
		return false
	}
	tag := CompressionToTIFFTag(c)
	if tag == 0 {
		return false
	}
	return decoder.AvailableByCompressionTag(tag)
}
