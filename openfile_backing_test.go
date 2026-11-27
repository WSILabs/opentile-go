package opentile_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/internal/format"
)

// notASlide writes a file that every format rejects.
func notASlide(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "not-a-slide.bin")
	if err := os.WriteFile(p, []byte("this is not a slide file"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// With mapping unavailable, the default backing reopens with pread (the
// error is the format rejection, which only an opened file reaches);
// BackingMmap reports ErrMmapUnavailable.
func TestOpenFileMmapUnavailable(t *testing.T) {
	p := notASlide(t)
	restore := opentile.FailMmapForTest()
	defer restore()

	_, err := opentile.OpenFile(p)
	if err == nil || errors.Is(err, opentile.ErrMmapUnavailable) || !errors.Is(err, format.ErrUnknownFormat) {
		t.Errorf("default backing: err = %v, want ErrUnknownFormat from the pread open", err)
	}
	_, err = opentile.OpenFile(p, opentile.WithBacking(opentile.BackingAuto))
	if !errors.Is(err, format.ErrUnknownFormat) {
		t.Errorf("BackingAuto: err = %v, want ErrUnknownFormat", err)
	}
	_, err = opentile.OpenFile(p, opentile.WithBacking(opentile.BackingMmap))
	if !errors.Is(err, opentile.ErrMmapUnavailable) {
		t.Errorf("BackingMmap: err = %v, want ErrMmapUnavailable", err)
	}
}

// A missing file reports the open error under every backing, not
// ErrMmapUnavailable.
func TestOpenFileMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.svs")
	for _, b := range []opentile.Backing{opentile.BackingAuto, opentile.BackingMmap, opentile.BackingPread} {
		_, err := opentile.OpenFile(p, opentile.WithBacking(b))
		if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, opentile.ErrMmapUnavailable) {
			t.Errorf("backing %d: err = %v, want fs.ErrNotExist", b, err)
		}
	}
}

// On a real slide: the default maps the file, falls back to pread when
// mapping fails, and every backing reads the same tile bytes.
func TestOpenFileBackingFallback(t *testing.T) {
	p := crossFixture(t, "svs/CMU-1-Small-Region.svs") // t.Skip if missing
	tile := func(s *opentile.Slide) []byte {
		t.Helper()
		b, err := s.Levels()[0].Tile(0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	open := func(opts ...opentile.Option) *opentile.Slide {
		t.Helper()
		s, err := opentile.OpenFile(p, opts...)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	s := open()
	if got := s.BackingForTest(); got != "mmap" {
		t.Errorf("default backing opened with %s, want mmap", got)
	}
	want := tile(s)
	s.Close()

	s = open(opentile.WithBacking(opentile.BackingPread))
	if got := s.BackingForTest(); got != "pread" {
		t.Errorf("BackingPread opened with %s", got)
	}
	if string(tile(s)) != string(want) {
		t.Error("BackingPread tile differs from mmap")
	}
	s.Close()

	restore := opentile.FailMmapForTest()
	defer restore()
	s = open()
	if got := s.BackingForTest(); got != "pread" {
		t.Errorf("default backing with mmap failing opened with %s, want pread", got)
	}
	if string(tile(s)) != string(want) {
		t.Error("fallback tile differs from mmap")
	}
	s.Close()
	if _, err := opentile.OpenFile(p, opentile.WithBacking(opentile.BackingMmap)); !errors.Is(err, opentile.ErrMmapUnavailable) {
		t.Errorf("BackingMmap with mmap failing: err = %v, want ErrMmapUnavailable", err)
	}
}
