package opentile_test

import (
	"bytes"
	"errors"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func TestOpenFileNonexistent(t *testing.T) {
	_, err := opentile.OpenFile("/nonexistent/path.svs")
	if err == nil {
		t.Fatalf("expected error opening nonexistent path")
	}
}

func TestOpenEmptyReader(t *testing.T) {
	// An empty reader has no format magic bytes. The old factory dispatch
	// path (preserved for the v0.23 transition) hands it to tiff.Open,
	// which returns ErrInvalidTIFF. After T3.2 this will become
	// ErrUnknownFormat via format.OpenAny.
	_, err := opentile.Open(bytes.NewReader(nil), 0)
	if err == nil {
		t.Fatalf("Open(empty): expected error, got nil")
	}
	if !errors.Is(err, opentile.ErrUnsupportedFormat) && !errors.Is(err, opentile.ErrInvalidTIFF) {
		t.Errorf("Open(empty): got %v, want ErrUnsupportedFormat or ErrInvalidTIFF", err)
	}
}
