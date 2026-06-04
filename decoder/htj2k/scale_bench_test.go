//go:build cgo && !nocgo && !nohtj2k

package htj2k

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/boxhalve"
)

func benchHTJ2KTile(b *testing.B) []byte {
	enc, err := encodeTestLossless(makeTestRGB(256, 256), 256, 256, 3)
	if err != nil {
		b.Fatal(err)
	}
	return enc
}

func BenchmarkHTJ2KResolutionDecode2x(b *testing.B) {
	src := benchHTJ2KTile(b)
	dec := (&factory{}).New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := dec.Decode(src, decoder.DecodeOptions{Scale: 2}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHTJ2KFullDecodePlusBox2x(b *testing.B) {
	src := benchHTJ2KTile(b)
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
