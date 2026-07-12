//go:build cgo && !nocgo && !nojp2k

package jpeg2000

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestRegistered(t *testing.T) {
	f, ok := decoder.Get("jpeg2000")
	if !ok {
		t.Fatalf("jpeg2000 decoder not registered")
	}
	tags := f.TIFFCompressionTags()
	if len(tags) < 3 {
		t.Errorf("expected at least 3 tags (Aperio 33003 YCbCr + 33005 RGB + libtiff 34712), got %v", tags)
	}
	// 33005 is the Aperio JP2K RGB convention (#110), the counterpart to
	// 33003 (YCbCr); both must be decodable on the pyramid path.
	wantTags := map[uint16]bool{33003: false, 33005: false, 34712: false}
	for _, tag := range tags {
		if _, want := wantTags[tag]; want {
			wantTags[tag] = true
		}
	}
	for tag, ok := range wantTags {
		if !ok {
			t.Errorf("missing TIFF tag %d", tag)
		}
	}
}

// Decode tests against real JP2K fixtures live in
// sample_files/svs/JP2K-33003-1.svs. Defer end-to-end JP2K decode
// validation to wsitools' golden-master pass at the v0.9.0 port.
