package decoder

// PixelFormat selects the in-memory pixel layout of a decoded Image.
type PixelFormat int

const (
	// PixelFormatRGB is 3 bytes per pixel, no alpha channel.
	// The default — WSI imagery is opaque so alpha is wasted memory.
	PixelFormatRGB PixelFormat = iota

	// PixelFormatRGBA is 4 bytes per pixel with alpha = 0xFF.
	// Use when interop with Go stdlib image.NRGBA matters.
	PixelFormatRGBA
)

// Image is a decoded raster bitmap.
type Image struct {
	Width, Height int
	Stride        int // bytes per row; may over-allocate for SIMD alignment
	Format        PixelFormat
	Pix           []byte // len(Pix) == Stride * Height
}

// NewImage returns a freshly-allocated Image with PixelFormatRGB and
// Stride = w * 3. The Pix slice is zero-filled.
func NewImage(w, h int) *Image {
	return NewImageFormat(w, h, PixelFormatRGB)
}

// NewImageFormat returns a freshly-allocated Image with the requested
// format. Stride is set to the format's bytes-per-pixel times w.
func NewImageFormat(w, h int, fmt PixelFormat) *Image {
	bpp := bytesPerPixel(fmt)
	stride := w * bpp
	return &Image{
		Width:  w,
		Height: h,
		Stride: stride,
		Format: fmt,
		Pix:    make([]byte, stride*h),
	}
}

func bytesPerPixel(fmt PixelFormat) int {
	switch fmt {
	case PixelFormatRGBA:
		return 4
	default:
		return 3 // PixelFormatRGB
	}
}
