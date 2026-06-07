//go:build !cgo || nocgo || nojp2k

package jpeg2000

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&stubFactory{})
}

type stubFactory struct{}

func (f *stubFactory) Name() string                  { return "jpeg2000" }
func (f *stubFactory) TIFFCompressionTags() []uint16 { return []uint16{33003, 34712} }
func (f *stubFactory) New() decoder.Decoder          { return &stubDecoder{} }

type stubDecoder struct{}

func (d *stubDecoder) Decode(_ []byte, _ decoder.DecodeOptions) (*decoder.Image, error) {
	return nil, fmt.Errorf("decoder/jpeg2000: requires cgo + libopenjp2 (rebuild with cgo enabled / without -tags nocgo or -tags nojp2k): %w",
		decoder.ErrCodecUnavailable)
}

func (d *stubDecoder) Close() error { return nil }
