package decoder

// DecodeOptions configures a single Decode call. The zero value is
// valid (Scale=1, Format=PixelFormatRGB, Dst=nil → allocate fresh RGB).
type DecodeOptions struct {
	// Scale is the IDCT-time scale factor (JPEG decoders only).
	// Valid values: 1, 2, 4, 8. Other values return ErrUnsupportedScale.
	// Non-JPEG decoders return ErrUnsupportedScale if Scale != 1.
	// The zero value (0) is treated as 1 (no scaling).
	Scale int

	// Format is the requested output pixel format. Decoders return
	// ErrUnsupportedFormat if they can't produce the requested format.
	// Today: PixelFormatRGB and PixelFormatRGBA are universal.
	Format PixelFormat

	// Dst is an optional caller-supplied destination Image. If nil, the
	// decoder allocates. If non-nil and its dimensions match the
	// decoded size, the decoder writes into Dst.Pix and returns Dst.
	// Mismatched dimensions return ErrDestinationSize.
	Dst *Image
}

// Decoder turns compressed tile bytes into a decoded Image. Decoders
// are NOT safe for concurrent use; callers running concurrent decodes
// on the same slide should construct one Decoder per goroutine via
// Factory.New().
type Decoder interface {
	// Decode the compressed bytes per opts. If opts.Dst is non-nil and
	// matches the decoded dimensions, writes into Dst and returns it;
	// otherwise allocates a fresh Image.
	Decode(compressed []byte, opts DecodeOptions) (*Image, error)

	// Close releases the decoder's internal state. Safe to call
	// multiple times. After Close, further Decode calls return an
	// error.
	Close() error
}
