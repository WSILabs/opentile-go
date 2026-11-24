// Package testutil holds helpers shared by the module's tests. It is
// imported only from _test.go files.
package testutil

import (
	"errors"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// NoDecode reports whether err means this build lacks a codec the call
// needs: a nocgo build, or a cgo build with a no<codec> tag. Every such
// failure wraps decoder.ErrCodecUnavailable, including the internal
// libjpeg-turbo crop path that NDPI and Philips raw tiles use.
func NoDecode(err error) bool {
	return errors.Is(err, decoder.ErrCodecUnavailable)
}

// SkipIfNoDecode skips t when err is a NoDecode error, so a test that
// needs pixels skips cleanly under nocgo instead of failing (#104). Call it
// right before the usual err != nil check.
func SkipIfNoDecode(t testing.TB, err error) {
	t.Helper()
	if NoDecode(err) {
		t.Skipf("needs a codec this build lacks: %v", err)
	}
}
