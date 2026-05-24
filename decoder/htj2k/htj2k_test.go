//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("htj2k")
	if !ok {
		t.Fatalf("htj2k decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 60003 {
		t.Errorf("TIFFCompressionTags: got %v want [60003]", got)
	}
}
