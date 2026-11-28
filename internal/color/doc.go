// Package color links lcms2 for opentile's ICC -> sRGB conversion
// (opentile.WithSRGB, Slide.ToSRGB). Importing it registers the transform
// with the opentile package; decoder/all imports it.
//
// It needs cgo and lcms2 (pkg-config lcms2). Under -tags nolcms2, nocgo or
// with cgo disabled it registers nothing, and color management reports
// unavailable, the same pattern as the no<codec> tags.
package color
