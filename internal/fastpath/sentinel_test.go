package fastpath_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wsilabs/opentile-go/internal/fastpath"
)

func TestErrUnsupportedIsItself(t *testing.T) {
	if !errors.Is(fastpath.ErrUnsupported, fastpath.ErrUnsupported) {
		t.Fatal("errors.Is(ErrUnsupported, ErrUnsupported) returned false")
	}
}

func TestErrUnsupportedWrapped(t *testing.T) {
	wrapped := fmt.Errorf("dispatch failed: %w", fastpath.ErrUnsupported)
	if !errors.Is(wrapped, fastpath.ErrUnsupported) {
		t.Fatal("errors.Is did not unwrap to ErrUnsupported")
	}
}

func TestErrUnsupportedMessage(t *testing.T) {
	if got := fastpath.ErrUnsupported.Error(); got == "" {
		t.Fatal("ErrUnsupported.Error() returned empty string")
	}
}
