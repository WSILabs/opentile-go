package opentile_test

import (
	"errors"
	"fmt"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
)

// allCompressions is every Compression value. TestAllCompressionsListed
// fails when one is added without being listed here.
var allCompressions = []opentile.Compression{
	opentile.CompressionUnknown, opentile.CompressionNone, opentile.CompressionJPEG,
	opentile.CompressionJP2K, opentile.CompressionLZW, opentile.CompressionAVIF,
	opentile.CompressionIRIS, opentile.CompressionDeflate, opentile.CompressionWebP,
	opentile.CompressionJPEGXL, opentile.CompressionHTJ2K, opentile.CompressionPNG,
}

func TestAllCompressionsListed(t *testing.T) {
	next := allCompressions[len(allCompressions)-1] + 1
	if s := next.String(); s != fmt.Sprintf("unknown(%d)", uint8(next)) {
		t.Fatalf("Compression %d (%q) is not in allCompressions", next, s)
	}
}

// TestCodecAvailableMatchesDecode checks CodecAvailable against what a decode
// through the registry does: a codec reported unavailable has no decoder or
// one that fails with ErrCodecUnavailable, and a codec reported available has
// a decoder that fails, on junk input, with some other error. CI runs this
// under the default build, -tags nohtj2k and -tags nocgo.
func TestCodecAvailableMatchesDecode(t *testing.T) {
	junk := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	for _, c := range allCompressions {
		got := opentile.CodecAvailable(c)
		t.Logf("%-8s available=%v", c, got)
		if c == opentile.CompressionPNG {
			if !got {
				t.Errorf("png: want available (decoded with image/png)")
			}
			continue
		}
		tag := opentile.CompressionToTIFFTag(c)
		fac, ok := decoder.GetByCompressionTag(tag)
		if tag == 0 || !ok {
			if got {
				t.Errorf("%s: available, but no decoder is registered (tag %d)", c, tag)
			}
			continue
		}
		dec := fac.New()
		_, err := dec.Decode(junk, decoder.DecodeOptions{Dst: decoder.NewImage(2, 2)})
		dec.Close()
		unavailable := errors.Is(err, decoder.ErrCodecUnavailable)
		if got == unavailable {
			t.Errorf("%s: CodecAvailable=%v, but decode returned %v", c, got, err)
		}
	}
}

// The pure-Go codecs and PNG are available in every build, including nocgo.
func TestCodecAvailablePureGo(t *testing.T) {
	for _, c := range []opentile.Compression{
		opentile.CompressionNone, opentile.CompressionLZW,
		opentile.CompressionDeflate, opentile.CompressionPNG,
	} {
		if !opentile.CodecAvailable(c) {
			t.Errorf("%s: want available", c)
		}
	}
	if opentile.CodecAvailable(opentile.CompressionIRIS) || opentile.CodecAvailable(opentile.CompressionUnknown) {
		t.Errorf("iris/unknown: want unavailable")
	}
}
