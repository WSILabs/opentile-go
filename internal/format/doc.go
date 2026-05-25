// Package format defines the internal Reader interface every
// opentile-go format implementation provides. The public
// opentile.Slide type wraps a Reader and delegates method calls.
// This interface is internal to opentile-go; external callers use
// *opentile.Slide.
//
// Each format package (formats/svs, formats/ometiff, etc.) registers
// itself via Register in its init() function. opentile.OpenFile and
// opentile.Open dispatch through OpenAny, which probes each registered
// format in registration order.
//
// Replaces the public Tiler interface as of opentile-go v0.23.
package format
