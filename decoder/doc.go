// Package decoder defines the public Decoder interface and registry for
// opentile-go's pluggable codec layer. Codec-specific subpackages
// (decoder/jpeg, decoder/jpeg2000, decoder/lzw, etc.) register
// themselves into this package's registry at init() time.
//
// Most consumers wanting "all codecs available" should blank-import
// the decoder/all subpackage:
//
//	import _ "github.com/wsilabs/opentile-go/decoder/all"
//
// Smaller-footprint consumers can blank-import only the codec
// subpackages they need.
//
// The decoder layer backs Slide.DecodedTile / ReadRegion / ScaledStrips.
// It is also usable standalone for third-party Go pathology code that
// wants decoded tile bytes from opentile-go-readable WSI files.
//
// Design spec: docs/superpowers/specs/2026-05-23-opentile-go-v22-decoder-resample-lift-design.md.
package decoder
