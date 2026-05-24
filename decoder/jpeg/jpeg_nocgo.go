//go:build !cgo || nocgo

package jpeg

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "jpeg" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{7} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/jpeg: requires cgo + libjpeg-turbo (rebuild with cgo enabled / without -tags nocgo): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
