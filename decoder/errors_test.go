package decoder

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrorsExist(t *testing.T) {
	for _, e := range []error{
		ErrCodecUnavailable,
		ErrUnsupportedScale,
		ErrUnsupportedFormat,
		ErrDestinationSize,
		ErrCorruptInput,
	} {
		if e == nil {
			t.Errorf("nil sentinel error")
		}
		if e.Error() == "" {
			t.Errorf("empty error message on %T", e)
		}
	}
}

func TestErrorsIsWraps(t *testing.T) {
	wrapped := fmt.Errorf("decoder: jpegxl unavailable: %w", ErrCodecUnavailable)
	if !errors.Is(wrapped, ErrCodecUnavailable) {
		t.Errorf("errors.Is failed to detect ErrCodecUnavailable in wrapped error")
	}
}
