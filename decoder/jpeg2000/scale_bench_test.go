//go:build cgo && !nocgo && !nojp2k

package jpeg2000

import (
	"os"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/boxhalve"
)

func benchJP2KTile(b *testing.B) []byte {
	x, err := os.ReadFile("testdata/subsampled_422_256.j2k")
	if err != nil {
		b.Fatal(err)
	}
	return x
}

// Resolution decode (skips high-freq subbands) vs full decode + box.
func BenchmarkJP2KResolutionDecode2x(b *testing.B) {
	src := benchJP2KTile(b)
	dec := (&factory{}).New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := dec.Decode(src, decoder.DecodeOptions{Scale: 2}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJP2KFullDecodePlusBox2x(b *testing.B) {
	src := benchJP2KTile(b)
	dec := (&factory{}).New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		full, err := dec.Decode(src, decoder.DecodeOptions{Scale: 1})
		if err != nil {
			b.Fatal(err)
		}
		_ = boxhalve.Halve(full, 1)
	}
}
