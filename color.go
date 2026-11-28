package opentile

import (
	"fmt"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// srgbHook builds an ICC profile -> sRGB transform for 3-byte (rgba false)
// or 4-byte pixels; ok is false when the profile can't be used. It is set
// by internal/color, which links lcms2 and is imported by decoder/all; nil
// means this build has no color management.
var srgbHook func(profile []byte, rgba bool) (apply func(pix []byte, width, height, stride int), free func(), ok bool)

// SetSRGBTransformHook is called by internal/color to register the lcms2
// transform; it is not for general use.
func SetSRGBTransformHook(fn func(profile []byte, rgba bool) (apply func(pix []byte, width, height, stride int), free func(), ok bool)) {
	srgbHook = fn
}

// ColorManagementAvailable reports whether this build can convert pixels
// from a slide's ICC profile to sRGB ([WithSRGB], [Slide.ToSRGB]). It needs
// cgo and lcms2, linked by blank-importing decoder/all; -tags nolcms2 or
// nocgo leaves it out.
func ColorManagementAvailable() bool { return srgbHook != nil }

// srgbTransform is one slide's transform for one pixel format.
type srgbTransform struct {
	apply func(pix []byte, width, height, stride int)
	free  func()
}

// slideColor holds a slide's lazily resolved transforms, [0] RGB and [1]
// RGBA. A nil transform has err saying why (nil err: the slide has no ICC
// profile).
type slideColor struct {
	once [2]sync.Once
	t    [2]*srgbTransform
	err  [2]error
}

// srgbTransformFor resolves the slide's transform for pixel format f. A nil
// transform with a nil error means the slide has no ICC profile, so its
// pixels are taken as sRGB already.
func (s *Slide) srgbTransformFor(f decoder.PixelFormat) (*srgbTransform, error) {
	var i int
	switch f {
	case decoder.PixelFormatRGB:
		i = 0
	case decoder.PixelFormatRGBA:
		i = 1
	default:
		return nil, fmt.Errorf("%w: pixel format %d is not RGB", ErrICCProfileUnusable, f)
	}
	s.color.once[i].Do(func() {
		profile := s.ICCProfile()
		switch {
		case len(profile) == 0:
		case srgbHook == nil:
			s.color.err[i] = ErrColorManagementUnavailable
		default:
			if apply, free, ok := srgbHook(profile, i == 1); ok {
				s.color.t[i] = &srgbTransform{apply: apply, free: free}
			} else {
				s.color.err[i] = ErrICCProfileUnusable
			}
		}
	})
	return s.color.t[i], s.color.err[i]
}

// closeColor frees the transforms. Called by Close, after which the Slide
// must not be used.
func (s *Slide) closeColor() {
	for i, t := range s.color.t {
		if t != nil {
			t.free()
			s.color.t[i] = nil
		}
	}
}

// CheckSRGB reports whether [WithSRGB] reads of this slide will succeed.
// It returns nil when the slide has an ICC profile lcms2 can use, and also
// when it has no profile: untagged pixels are taken as sRGB and returned as
// decoded. Otherwise it returns the error those reads would:
// [ErrColorManagementUnavailable] when the slide has a profile but the
// build has no color management, or [ErrICCProfileUnusable] when lcms2
// can't use the profile. The answer is fixed for the life of the Slide, so
// a caller can check once at open instead of handling it on every read.
func (s *Slide) CheckSRGB() error {
	_, err := s.srgbTransformFor(decoder.PixelFormatRGB)
	return err
}

// ToSRGB converts img in place from the slide's ICC profile to sRGB and
// sets img.ColorManaged. It reports whether the conversion was applied:
// false, leaving img unchanged, when the slide has no ICC profile, the
// profile can't be applied ([Slide.CheckSRGB] says why) or img is not
// RGB/RGBA. An image already marked ColorManaged is left as is and reports
// true.
//
// The [WithSRGB] decode option does this for the Level, Pyramid and render
// reads. Use ToSRGB for images decoded another way, such as associated
// images (whose pixels it converts with the slide's profile) or
// ScaledStrips strips. Safe for concurrent use; the transform is built once
// per slide and pixel format.
func (s *Slide) ToSRGB(img *decoder.Image) bool {
	if img == nil {
		return false
	}
	if img.ColorManaged {
		return true
	}
	t, _ := s.srgbTransformFor(img.Format)
	if t == nil {
		return false
	}
	t.apply(img.Pix, img.Width, img.Height, img.Stride)
	img.ColorManaged = true
	return true
}

// WithSRGB converts decoded pixels from the slide's ICC profile to sRGB.
// It is honored by the Level, Pyramid and RenderThumbnail/RenderMacro
// reads; see [Slide.ToSRGB] for other images. The returned image's
// ColorManaged field reports whether a conversion was applied.
//
// A slide without an ICC profile is not an error: its pixels are taken as
// sRGB and returned as decoded, with ColorManaged false. A slide whose
// profile can't be applied fails the read, before decoding, with
// [ErrColorManagementUnavailable] (no lcms2 in this build) or
// [ErrICCProfileUnusable], rather than returning pixels that aren't sRGB.
// [Slide.CheckSRGB] reports this up front; a caller that wants best effort
// reads without WithSRGB instead.
func WithSRGB() DecodeOption {
	return func(c *decodeConfig) { c.srgb = true }
}

// checkSRGB fails a read before decoding when WithSRGB can't be honored.
func (s *Slide) checkSRGB(opts []DecodeOption) error {
	if !newDecodeConfig(opts).srgb {
		return nil
	}
	return s.CheckSRGB()
}

// finishSRGB applies WithSRGB to a public read's result.
func (s *Slide) finishSRGB(img *decoder.Image, opts []DecodeOption) error {
	if img == nil || img.ColorManaged || !newDecodeConfig(opts).srgb {
		return nil
	}
	t, err := s.srgbTransformFor(img.Format)
	if t != nil {
		t.apply(img.Pix, img.Width, img.Height, img.Stride)
		img.ColorManaged = true
	}
	return err
}

// resetColorManaged clears a reused Into destination's flag before a read.
func resetColorManaged(dst *decoder.Image) {
	if dst != nil {
		dst.ColorManaged = false
	}
}
