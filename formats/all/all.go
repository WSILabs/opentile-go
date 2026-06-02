// Package all registers every format implemented by opentile-go. Import this
// package for its side effect (via a blank import).
//
//	import _ "github.com/wsilabs/opentile-go/formats/all"
//
// Each format package's init() calls format.Register; importing this package
// triggers all of them in the correct order.
package all

import (
	// Import all format packages for their init() side effects.
	// Order matters: more-specific formats before catch-alls.
	_ "github.com/wsilabs/opentile-go/formats/bif"
	_ "github.com/wsilabs/opentile-go/formats/cogwsi"
	// dicom registers a path-aware hook (not the normal registry opener) —
	// it must be imported here so the hook is installed before any OpenFile call.
	_ "github.com/wsilabs/opentile-go/formats/dicom"
	_ "github.com/wsilabs/opentile-go/formats/ife"
	_ "github.com/wsilabs/opentile-go/formats/leicascn"
	_ "github.com/wsilabs/opentile-go/formats/ndpi"
	_ "github.com/wsilabs/opentile-go/formats/ometiff"
	_ "github.com/wsilabs/opentile-go/formats/philipstiff"
	_ "github.com/wsilabs/opentile-go/formats/svs"
	// SZI before generictiff so ZIP-magic detection runs first.
	_ "github.com/wsilabs/opentile-go/formats/szi"
	// generictiff must be last: it's the catch-all.
	_ "github.com/wsilabs/opentile-go/formats/generictiff"
)
