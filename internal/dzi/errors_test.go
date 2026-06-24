package dzi

import "testing"

func TestErrOverlapNotSupported(t *testing.T) {
	if ErrOverlapNotSupported == nil {
		t.Fatal("ErrOverlapNotSupported must be a non-nil sentinel")
	}
	if got := ErrOverlapNotSupported.Error(); got == "" {
		t.Fatal("ErrOverlapNotSupported must have a message")
	}
}
