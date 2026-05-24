// Package deflate implements the decoder for TIFF Compression=8
// (Deflate/Zip). Uses stdlib compress/zlib.
package deflate

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/decoder"
)

func init() {
	decoder.Register(&factory{})
}

type factory struct{}

func (f *factory) Name() string                  { return "deflate" }
func (f *factory) TIFFCompressionTags() []uint16 { return []uint16{8} }
func (f *factory) New() decoder.Decoder          { return &deflateDecoder{} }

type deflateDecoder struct{}

func (d *deflateDecoder) Decode(compressed []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	if opts.Dst == nil {
		return nil, fmt.Errorf("decoder/deflate: Dst is required (decompressed bytes carry no dimensions): %w", decoder.ErrDestinationSize)
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("decoder/deflate: zlib header: %w (%w)", err, decoder.ErrCorruptInput)
	}
	defer zr.Close()
	n, err := io.ReadFull(zr, opts.Dst.Pix)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("decoder/deflate: read: %w (%w)", err, decoder.ErrCorruptInput)
	}
	if n != len(opts.Dst.Pix) {
		return nil, fmt.Errorf("decoder/deflate: decoded %d bytes, expected %d: %w", n, len(opts.Dst.Pix), decoder.ErrDestinationSize)
	}
	return opts.Dst, nil
}

func (d *deflateDecoder) Close() error { return nil }
