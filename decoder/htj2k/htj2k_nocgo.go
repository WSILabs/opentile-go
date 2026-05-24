//go:build !cgo || nocgo || nohtj2k

// Package htj2k implements the HTJ2K (High-Throughput JPEG 2000) decoder
// stub for builds without openjph (CGO_ENABLED=0, -tags nocgo, or -tags nohtj2k).
// TIFF Compression=60003 (wsi-tools private/experimental).
package htj2k

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "htj2k" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{60003} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/htj2k: requires cgo + openjph (rebuild without -tags nohtj2k or nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
