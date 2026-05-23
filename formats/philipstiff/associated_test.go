package philipstiff

import (
	"bytes"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// TestAssociatedImageInterface confirms associatedImage satisfies
// opentile.AssociatedImage and that the trivial accessors return what
// was set at construction.
func TestAssociatedImageInterface(t *testing.T) {
	a := &associatedImage{
		kind:        "label",
		size:        opentile.Size{W: 387, H: 403},
		compression: opentile.CompressionJPEG,
	}
	var _ opentile.AssociatedImage = a
	if a.Type() != "label" {
		t.Errorf("Type: got %q, want %q", a.Type(), "label")
	}
	if a.Size().W != 387 || a.Size().H != 403 {
		t.Errorf("Size: got %v, want 387x403", a.Size())
	}
	if a.Compression() != opentile.CompressionJPEG {
		t.Errorf("Compression: got %v, want JPEG", a.Compression())
	}
}

// TestAssociatedImageMultiStripErrors confirms that we error out cleanly
// rather than silently truncating when a Philips associated page has
// more than one strip. Our 4 fixtures are all single-strip; this is a
// future-proofing guard.
func TestAssociatedImageMultiStripErrors(t *testing.T) {
	a := &associatedImage{
		kind:         "overview",
		stripOffsets: []uint64{0, 100},
		stripCounts:  []uint64{50, 50},
		reader:       bytes.NewReader(make([]byte, 200)),
	}
	if _, err := a.Bytes(); err == nil {
		t.Error("expected error on multi-strip associated image")
	}
}

// TestAssociatedImageEmptyErrors confirms zero strips → error rather than
// returning empty bytes.
func TestAssociatedImageEmptyErrors(t *testing.T) {
	a := &associatedImage{
		kind:         "label",
		stripOffsets: nil,
		stripCounts:  nil,
		reader:       bytes.NewReader(nil),
	}
	if _, err := a.Bytes(); err == nil {
		t.Error("expected error on zero-strip associated image")
	}
}
