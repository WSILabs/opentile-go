// Package lzw implements the decoder for TIFF Compression=5 (LZW).
// Wraps the existing internal/tifflzw package which carries the
// TIFF "off-by-one" code-width transition incompatible with stdlib
// compress/lzw.
package lzw

import (
	"bytes"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "lzw" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{5} }
func (f *factory) New() decoder.Decoder          { return &lzwDecoder{} }

type lzwDecoder struct{}

func (d *lzwDecoder) Decode(compressed []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Dst == nil {
		return nil, fmt.Errorf("decoder/lzw: Dst is required (decompressed bytes carry no dimensions): %w", decoder.ErrDestinationSize)
	}
	r := tifflzw.NewReader(bytes.NewReader(compressed), tifflzw.MSB, 8)
	defer r.Close()
	n, err := io.ReadFull(r, opts.Dst.Pix)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("decoder/lzw: read: %w (%w)", err, decoder.ErrCorruptInput)
	}
	if n != len(opts.Dst.Pix) {
		return nil, fmt.Errorf("decoder/lzw: decoded %d bytes, expected %d: %w", n, len(opts.Dst.Pix), decoder.ErrDestinationSize)
	}
	return opts.Dst, nil
}

func (d *lzwDecoder) Close() error { return nil }
