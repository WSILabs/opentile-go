package opentile

import "testing"

func TestCompressionString(t *testing.T) {
	tests := []struct {
		c    Compression
		want string
	}{
		{CompressionUnknown, "unknown"},
		{CompressionNone, "none"},
		{CompressionJPEG, "jpeg"},
		{CompressionJP2K, "jp2k"},
		{CompressionLZW, "lzw"},
		{CompressionAVIF, "avif"},
		{CompressionIRIS, "iris"},
		{CompressionDeflate, "deflate"},
		{CompressionWebP, "webp"},
		{CompressionJPEGXL, "jpeg-xl"},
		{CompressionHTJ2K, "htj2k"},
		{CompressionPNG, "png"},
		{Compression(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("Compression(%d).String() = %q, want %q", tt.c, got, tt.want)
		}
	}
}
