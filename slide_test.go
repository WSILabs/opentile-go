package opentile_test

import (
	"bytes"
	"errors"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
)

func TestOpenFileNonexistent(t *testing.T) {
	_, err := opentile.OpenFile("/nonexistent/path.svs")
	if err == nil {
		t.Fatalf("expected error opening nonexistent path")
	}
}

func TestOpenEmptyReader(t *testing.T) {
	// An empty reader has no format magic bytes; no format should claim it.
	_, err := opentile.Open(bytes.NewReader(nil), 0)
	if err == nil {
		t.Fatalf("Open(empty): expected error, got nil")
	}
	if !errors.Is(err, format.ErrUnknownFormat) {
		t.Errorf("Open(empty): got %v, want format.ErrUnknownFormat", err)
	}
}
