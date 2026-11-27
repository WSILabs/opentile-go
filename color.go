package opentile

import (
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

// slideColor holds a slide's lazily built transforms, [0] RGB and [1] RGBA.
type slideColor struct {
	once [2]sync.Once
	t    [2]*srgbTransform
}

func (s *Slide) srgbTransformFor(f decoder.PixelFormat) *srgbTransform {
	var i int
	switch f {
	case decoder.PixelFormatRGB:
		i = 0
	case decoder.PixelFormatRGBA:
		i = 1
	default:
		return nil
	}
	hook := srgbHook
	if hook == nil {
		return nil
	}
	s.color.once[i].Do(func() {
		if apply, free, ok := hook(s.ICCProfile(), i == 1); ok {
			s.color.t[i] = &srgbTransform{apply: apply, free: free}
		}
	})
	return s.color.t[i]
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

// ToSRGB converts img in place from the slide's ICC profile to sRGB and
// sets img.ColorManaged. It reports whether the conversion was applied:
// false, leaving img unchanged, when the slide has no ICC profile, lcms2
// can't use it, the build has no color management
// ([ColorManagementAvailable]) or img is not RGB/RGBA. An image already
// marked ColorManaged is left as is and reports true.
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
	t := s.srgbTransformFor(img.Format)
	if t == nil {
		return false
	}
	t.apply(img.Pix, img.Width, img.Height, img.Stride)
	img.ColorManaged = true
	return true
}

// WithSRGB converts decoded pixels from the slide's ICC profile to sRGB,
// when the slide has a profile lcms2 can use and the build has color
// management. The returned image's ColorManaged field reports whether it
// was applied; a slide without a usable profile is not an error. Honored
// by the Level, Pyramid and RenderThumbnail/RenderMacro reads; see
// [Slide.ToSRGB] for other images.
func WithSRGB() DecodeOption {
	return func(c *decodeConfig) { c.srgb = true }
}

// finishSRGB applies WithSRGB to a public read's result.
func (s *Slide) finishSRGB(img *decoder.Image, opts []DecodeOption) {
	if img == nil {
		return
	}
	if newDecodeConfig(opts).srgb {
		s.ToSRGB(img)
	}
}

// resetColorManaged clears a reused Into destination's flag before a read.
func resetColorManaged(dst *decoder.Image) {
	if dst != nil {
		dst.ColorManaged = false
	}
}
