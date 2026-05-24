//go:build !cgo || nocgo || noavif

package avif

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "avif" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{60001} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/avif: requires cgo + libavif (rebuild without -tags noavif or nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
