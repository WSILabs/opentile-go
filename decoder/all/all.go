// Package all blank-imports every decoder subpackage so all codecs
// register at init() time. Most consumers wanting "every codec
// available" should blank-import this package:
//
//	import _ "github.com/wsilabs/opentile-go/decoder/all"
//
// Consumers wanting a smaller cgo footprint can blank-import only the
// codec subpackages they need.
package all

import (
	// Pure-Go decoders — always built.
	_ "github.com/wsilabs/opentile-go/decoder/deflate"
	_ "github.com/wsilabs/opentile-go/decoder/lzw"
	_ "github.com/wsilabs/opentile-go/decoder/none"

	// cgo decoders — register stubs when not built.
	_ "github.com/wsilabs/opentile-go/decoder/avif"
	_ "github.com/wsilabs/opentile-go/decoder/htj2k"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg2000"
	_ "github.com/wsilabs/opentile-go/decoder/jpegxl"
	_ "github.com/wsilabs/opentile-go/decoder/webp"
)
