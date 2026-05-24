//go:build cgo && !nocgo && !nowebp

package webp

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("webp")
	if !ok {
		t.Fatalf("webp decoder not registered")
	}
	if got := f.TIFFCompressionTags(); len(got) != 1 || got[0] != 50001 {
		t.Errorf("TIFFCompressionTags: got %v want [50001]", got)
	}
}
