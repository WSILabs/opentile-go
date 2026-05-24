package decoder_test

import (
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

func TestPackageCompiles(t *testing.T) {
	var _ decoder.PixelFormat
}

// fakeFactory is a registry test double.
type fakeFactory struct {
	name string
	tags []uint16
}

func (f *fakeFactory) Name() string                  { return f.name }
func (f *fakeFactory) TIFFCompressionTags() []uint16 { return f.tags }
func (f *fakeFactory) New() decoder.Decoder          { return nil } // unused in registry tests

func TestRegisterAndGet(t *testing.T) {
	f := &fakeFactory{name: "fake-codec-1", tags: []uint16{9001}}
	decoder.Register(f)

	got, ok := decoder.Get("fake-codec-1")
	if !ok {
		t.Fatalf("Get(fake-codec-1): not registered")
	}
	if got.Name() != "fake-codec-1" {
		t.Errorf("Get returned %q want fake-codec-1", got.Name())
	}
}

func TestGetByCompressionTag(t *testing.T) {
	f := &fakeFactory{name: "fake-codec-2", tags: []uint16{9002, 9003}}
	decoder.Register(f)

	for _, tag := range []uint16{9002, 9003} {
		got, ok := decoder.GetByCompressionTag(tag)
		if !ok {
			t.Errorf("GetByCompressionTag(%d): not registered", tag)
			continue
		}
		if got.Name() != "fake-codec-2" {
			t.Errorf("tag %d: got %q want fake-codec-2", tag, got.Name())
		}
	}
}

func TestGetMissing(t *testing.T) {
	if _, ok := decoder.Get("does-not-exist"); ok {
		t.Errorf("Get(does-not-exist): expected (nil, false)")
	}
	if _, ok := decoder.GetByCompressionTag(0xFFFF); ok {
		t.Errorf("GetByCompressionTag(0xFFFF): expected (nil, false)")
	}
}

func TestRegistered(t *testing.T) {
	decoder.Register(&fakeFactory{name: "fake-codec-3"})
	names := decoder.Registered()
	found := false
	for _, n := range names {
		if n == "fake-codec-3" {
			found = true
		}
	}
	if !found {
		t.Errorf("Registered(): fake-codec-3 not in %v", names)
	}
}

func TestRegisterOverwrites(t *testing.T) {
	f1 := &fakeFactory{name: "shadow"}
	f2 := &fakeFactory{name: "shadow"}
	decoder.Register(f1)
	decoder.Register(f2) // last-in-wins
	got, _ := decoder.Get("shadow")
	if got != f2 {
		t.Errorf("last-in-wins broken: got %p want %p", got, f2)
	}
}
