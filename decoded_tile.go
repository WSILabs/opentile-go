package opentile

import (
	"errors"
	"fmt"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/fastpath"
)

// decodedTiler is the unexported interface that format readers
// implement when they provide a fast pixel-path. imageDecodedTile
// type-asserts on s.r and dispatches when matched. Readers signal
// "this level doesn't support the fast path" by returning
// fastpath.ErrUnsupported; the dispatcher then falls back to the
// existing RawTile + Decode path.
//
// Added in v0.27.
type decodedTiler interface {
	ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error)
}

// imageDecodedTile is the logic-bearing decoded-tile read, backing
// (*Level).DecodedTile via its (PyramidIndex, Index) coordinates. The
// output pixel format defaults to PixelFormatRGB; override via
// WithFormat. JPEG sources support IDCT-time scaling via WithScale.
//
// Requires that a decoder for the level's Compression is registered.
// Blank-import github.com/wsilabs/opentile-go/decoder/all or the
// specific codec subpackage (e.g., decoder/jpeg) to enable. Returns
// ErrCodecNotRegistered (wrapped with the compression name) if not.
//
// v0.27 fast-path dispatch: when s.r implements decodedTiler and the
// fast path succeeds, returns its output directly. ErrUnsupported from
// the reader signals "no fast path for this level" and falls through to
// the v0.26 RawTile + fresh-decoder path. Any other error propagates.
// Other formats and non-striped NDPI levels keep the original RawTile +
// fresh-decoder path, which is preserved unchanged.
func (s *Slide) imageDecodedTile(image, level, tx, ty int, opts ...DecodeOption) (*decoder.Image, error) {
	cfg := newDecodeConfig(opts)

	if dr, ok := s.r.(decodedTiler); ok {
		out, err := dr.ImageDecodedTile(image, level, tx, ty, decoder.DecodeOptions{
			Format: cfg.format,
			Scale:  cfg.scale,
		})
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, fastpath.ErrUnsupported) {
			return nil, err
		}
		// fast path declined — fall through.
	}

	lvl, err := s.r.Level(image, level)
	if err != nil {
		return nil, err
	}
	compressed, err := s.r.ImageRawTile(image, level, tx, ty)
	if err != nil {
		return nil, err
	}
	tag := CompressionToTIFFTag(lvl.Compression)
	pool, err := s.decoderFor(tag)
	if err != nil {
		return nil, err
	}
	dec, err := pool.Borrow()
	if err != nil {
		return nil, err
	}
	defer pool.Return(dec)
	return dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
	})
}

// imageDecodedTileInto is the logic-bearing decode-into-dst read,
// backing (*Level).DecodedTileInto and the region blit loop.
//
// v0.27 fast-path dispatch: when s.r implements decodedTiler and the
// fast path succeeds, copies its output into dst. Otherwise routes
// through the v0.26 path which decodes directly into dst.
func (s *Slide) imageDecodedTileInto(image, level, tx, ty int, dst *decoder.Image, opts ...DecodeOption) error {
	cfg := newDecodeConfig(opts)

	if dr, ok := s.r.(decodedTiler); ok {
		// v0.29 Layer 2: pass dst as opts.Dst so the fast path can
		// write directly into it (eliminating the v0.28 copy step).
		// Fast-path impls that ignore Dst still return a fresh Image;
		// the out != dst branch below covers that defensively.
		out, err := dr.ImageDecodedTile(image, level, tx, ty, decoder.DecodeOptions{
			Format: cfg.format,
			Scale:  cfg.scale,
			Dst:    dst,
		})
		if err == nil {
			if out == dst {
				return nil
			}
			return copyImageInto(out, dst)
		}
		if !errors.Is(err, fastpath.ErrUnsupported) {
			return err
		}
	}

	lvl, err := s.r.Level(image, level)
	if err != nil {
		return err
	}
	compressed, err := s.r.ImageRawTile(image, level, tx, ty)
	if err != nil {
		return err
	}
	tag := CompressionToTIFFTag(lvl.Compression)
	pool, err := s.decoderFor(tag)
	if err != nil {
		return err
	}
	dec, err := pool.Borrow()
	if err != nil {
		return err
	}
	defer pool.Return(dec)
	_, err = dec.Decode(compressed, decoder.DecodeOptions{
		Scale:  cfg.scale,
		Format: cfg.format,
		Dst:    dst,
	})
	return err
}

// copyImageInto copies src's pixels into dst. Dimensions must match;
// formats may differ (RGB ↔ RGBA conversion via per-pixel copy).
func copyImageInto(src, dst *decoder.Image) error {
	if src.Width != dst.Width || src.Height != dst.Height {
		return fmt.Errorf("opentile: ImageDecodedTileInto: size mismatch src=%dx%d dst=%dx%d",
			src.Width, src.Height, dst.Width, dst.Height)
	}
	srcBpp := 3
	if src.Format == decoder.PixelFormatRGBA {
		srcBpp = 4
	}
	dstBpp := 3
	if dst.Format == decoder.PixelFormatRGBA {
		dstBpp = 4
	}
	if srcBpp == dstBpp && src.Stride == dst.Stride {
		copy(dst.Pix, src.Pix)
		return nil
	}
	for r := 0; r < src.Height; r++ {
		so := r * src.Stride
		do := r * dst.Stride
		for c := 0; c < src.Width; c++ {
			dst.Pix[do+0] = src.Pix[so+0]
			dst.Pix[do+1] = src.Pix[so+1]
			dst.Pix[do+2] = src.Pix[so+2]
			if dstBpp == 4 {
				dst.Pix[do+3] = 0xFF
			}
			so += srcBpp
			do += dstBpp
		}
	}
	return nil
}
