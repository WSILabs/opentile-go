package decoder

import "errors"

// Sentinel errors used by Decode implementations. Wrap with
// fmt.Errorf("...: %w", ErrXxx) for codec-specific context; callers
// detect via errors.Is.
var (
	// ErrCodecUnavailable is returned by the stub Decoder of a codec
	// subpackage that was excluded from this build (via -tags
	// no<codec> or -tags nocgo). The wrapping error message names the
	// codec and the build tag to remove.
	ErrCodecUnavailable = errors.New("decoder: codec not available in this build")

	// ErrUnsupportedScale is returned by Decode when DecodeOptions.Scale
	// is not a value the decoder supports. JPEG decoders accept 1, 2,
	// 4, 8; other decoders accept only 1.
	ErrUnsupportedScale = errors.New("decoder: scale factor not supported by this codec")

	// ErrUnsupportedFormat is returned by Decode when DecodeOptions.Format
	// is not producible by the decoder.
	ErrUnsupportedFormat = errors.New("decoder: pixel format not supported by this codec")

	// ErrDestinationSize is returned by Decode when DecodeOptions.Dst
	// is non-nil but its dimensions don't match the decoded size.
	ErrDestinationSize = errors.New("decoder: dst Image dimensions don't match decoded size")

	// ErrCorruptInput is returned by Decode when the compressed bytes
	// can't be parsed.
	ErrCorruptInput = errors.New("decoder: corrupt input data")
)
