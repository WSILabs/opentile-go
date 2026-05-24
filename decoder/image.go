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
