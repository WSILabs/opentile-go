// Package none implements the trivial "no-compression" decoder for
// TIFF Compression=1 tiles, where the on-disk bytes ARE the decoded
// pixels.
//
// Because uncompressed tile bytes carry no dimensions or format, the
// caller MUST supply DecodeOptions.Dst pre-sized to the expected
// tile dimensions; the decoder memcpys src into Dst.Pix.
package none

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "none" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{1} }
func (f *factory) New() decoder.Decoder          { return &noneDecoder{} }

type noneDecoder struct{}

func (d *noneDecoder) Decode(compressed []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Dst == nil {
		return nil, fmt.Errorf("decoder/none: Dst is required (uncompressed bytes carry no dimensions): %w", decoder.ErrDestinationSize)
	}
	expect := opts.Dst.Stride * opts.Dst.Height
	if len(compressed) != expect {
		return nil, fmt.Errorf("decoder/none: src length %d != Dst.Stride*Height %d: %w", len(compressed), expect, decoder.ErrDestinationSize)
	}
	copy(opts.Dst.Pix, compressed)
	return opts.Dst, nil
}

func (d *noneDecoder) Close() error { return nil }
