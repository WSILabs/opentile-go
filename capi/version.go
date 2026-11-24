package main

import (
	"runtime/debug"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

// API version; must match OT_API_VERSION_* in opentile.h.
const (
	apiVersionMajor = 1
	apiVersionMinor = 0
)

// version is the opentile-go version this library was built from. Release
// builds set it with -ldflags "-X main.version=v0.63.0"; otherwise it comes
// from the module build info ("devel" for an in-repo replace build).
var version = ""

func libraryVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/wsilabs/opentile-go" && d.Replace == nil && d.Version != "" {
				return d.Version
			}
		}
	}
	return "devel"
}

// codecAvailable reports whether this build can decode compression c.
func codecAvailable(c opentile.Compression) bool {
	switch c {
	case opentile.CompressionUnknown:
		return false
	case opentile.CompressionNone, opentile.CompressionLZW, opentile.CompressionDeflate, opentile.CompressionPNG:
		return true // decoded in pure Go
	}
	tag := opentile.CompressionToTIFFTag(c)
	if tag == 0 {
		return false
	}
	_, ok := decoder.GetByCompressionTag(tag)
	return ok
}
