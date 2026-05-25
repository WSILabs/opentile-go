package opentile

import (
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
)

// DecodedTile returns the decoded pixel data for the tile at (level, tx, ty)
// within image 0. The output pixel format defaults to PixelFormatRGB;
// override via WithFormat. JPEG sources support IDCT-time scaling via
// WithScale.
//
// Requires that a decoder for the level's Compression is registered.
// Blank-import github.com/wsilabs/opentile-go/decoder/all or the
// specific codec subpackage (e.g., decoder/jpeg) to enable. Returns
// ErrCodecNotRegistered (wrapped with the compression name) if not.
//
// Each call constructs a fresh Decoder and Closes it after use.
// Callers running concurrent decoded reads should use one goroutine
// per tile; the decoder package's Factory.New() is goroutine-safe
// but individual Decoder instances are not.
func (s *Slide) DecodedTile(level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	return s.ImageDecodedTile(0, level, tx, ty, opts...)
}

// DecodedTileInto decodes a tile into a caller-provided destination Image.
// dst.Width / dst.Height must match the tile's decoded dimensions;
// mismatched sizes return decoder.ErrDestinationSize from the underlying
// decoder. dst.Format may be RGB or RGBA (decoders convert as needed).
func (s *Slide) DecodedTileInto(level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	return s.ImageDecodedTileInto(0, level, tx, ty, dst, opts...)
}

// ImageDecodedTile is the multi-image variant of DecodedTile.
func (s *Slide) ImageDecodedTile(image, level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	cfg := newDecodeConfig(opts)
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return nil, err
	}
	compressed, err := s.r.ImageRawTile(image, level, tx, ty)
	if err != nil {
		return nil, err
	}
	tag := CompressionToTIFFTag(lvl.Compression)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, fmt.Errorf("%w: %s (blank-import github.com/wsilabs/opentile-go/decoder/all or decoder/<codec>)",
			ErrCodecNotRegistered, lvl.Compression)
	}
	dec := fac.New()
	defer dec.Close()
	return dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
	})
}

// ImageDecodedTileInto is the multi-image variant of DecodedTileInto.
func (s *Slide) ImageDecodedTileInto(image, level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	cfg := newDecodeConfig(opts)
	lvl, err := s.r.Level(image, level)
	if err != nil {
		return err
	}
	compressed, err := s.r.ImageRawTile(image, level, tx, ty)
	if err != nil {
		return err
	}
	tag := CompressionToTIFFTag(lvl.Compression)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return fmt.Errorf("%w: %s (blank-import github.com/wsilabs/opentile-go/decoder/all or decoder/<codec>)",
			ErrCodecNotRegistered, lvl.Compression)
	}
	dec := fac.New()
	defer dec.Close()
	_, err = dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
		Dst:    dst,
	})
	return err
}
