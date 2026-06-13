package format

import (
	"context"
	"errors"
	"io"
	"iter"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// fakeReader satisfies format.Reader for registry tests.
type fakeReader struct{ name string }

func (r *fakeReader) Format() opentile.Format                                 { return opentile.Format(r.name) }
func (r *fakeReader) Pyramids() []opentile.Pyramid                            { return nil }
func (r *fakeReader) Level(_, _ int) (opentile.Level, error)                  { return opentile.Level{}, nil }
func (r *fakeReader) Associated() []opentile.AssociatedImage                  { return nil }
func (r *fakeReader) Metadata() opentile.Metadata                             { return opentile.Metadata{} }
func (r *fakeReader) ICCProfile() []byte                                      { return nil }
func (r *fakeReader) WarmLevel(_, _ int) error                                { return nil }
func (r *fakeReader) ImageRawTile(_, _, _, _ int) ([]byte, error)             { return nil, nil }
func (r *fakeReader) ImageRawTileInto(_, _, _, _ int, _ []byte) (int, error)  { return 0, nil }
func (r *fakeReader) ImageTileMaxSize(_, _ int) int                           { return 0 }
func (r *fakeReader) ImageTilePrefix(_, _ int) []byte                         { return nil }
func (r *fakeReader) ImageTileBodyMaxSize(_, _ int) int                       { return 0 }
func (r *fakeReader) ImageTileBodyInto(_, _, _, _ int, _ []byte) (int, error) { return 0, nil }
func (r *fakeReader) ImageTileReader(_, _, _, _ int) (io.ReadCloser, error)   { return nil, nil }
func (r *fakeReader) ImageRangeTiles(_ context.Context, _, _ int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return nil
}
func (r *fakeReader) Close() error { return nil }

func TestRegisterAndOpenAny(t *testing.T) {
	original := snapshot()
	defer restore(original)

	reset()

	Register("fakefmt",
		func(r io.ReaderAt, size int64) error { return nil }, // always match
		func(r io.ReaderAt, size int64, cfg *Config) (Reader, error) {
			return &fakeReader{name: "fakefmt"}, nil
		},
	)

	rdr, err := OpenAny(nil, 0, nil)
	if err != nil {
		t.Fatalf("OpenAny: %v", err)
	}
	if rdr.Format() != "fakefmt" {
		t.Errorf("Format(): got %q, want fakefmt", rdr.Format())
	}
}

func TestOpenAnyUnknown(t *testing.T) {
	original := snapshot()
	defer restore(original)

	reset()

	_, err := OpenAny(nil, 0, nil)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("OpenAny with no formats: got %v, want ErrUnknownFormat", err)
	}
}

func TestRegistrationOrder(t *testing.T) {
	original := snapshot()
	defer restore(original)

	reset()

	Register("first",
		func(r io.ReaderAt, size int64) error { return nil },
		func(r io.ReaderAt, size int64, cfg *Config) (Reader, error) {
			return &fakeReader{name: "first"}, nil
		},
	)
	Register("second",
		func(r io.ReaderAt, size int64) error { return nil },
		func(r io.ReaderAt, size int64, cfg *Config) (Reader, error) {
			return &fakeReader{name: "second"}, nil
		},
	)

	rdr, err := OpenAny(nil, 0, nil)
	if err != nil {
		t.Fatalf("OpenAny: %v", err)
	}
	if rdr.Format() != "first" {
		t.Errorf("first registrant should win: got %q", rdr.Format())
	}
}

// TestFallbackChecksAfterMain verifies that fallback-registered formats only
// win when no main-registry format matches, regardless of registration order.
//
// Setup: a fallback that matches anything ("catch-all") and a main format
// that matches inputs carrying a synthetic "main-magic" sentinel. On a
// "main-magic" input the main format must win; on any other input the
// fallback must win.
func TestFallbackChecksAfterMain(t *testing.T) {
	original := snapshot()
	defer restore(original)

	reset()

	const mainMagic = "main-magic"

	// Fallback registered FIRST to verify that registration order between
	// main and fallback lists is irrelevant — tier matters, not order.
	RegisterFallback("catch-all",
		func(r io.ReaderAt, size int64) error { return nil }, // always match
		func(r io.ReaderAt, size int64, cfg *Config) (Reader, error) {
			return &fakeReader{name: "catch-all"}, nil
		},
	)
	Register(mainMagic,
		func(r io.ReaderAt, size int64) error {
			// Treat nil reader as "main-magic" sentinel (size == -1).
			if size == -1 {
				return nil
			}
			return errors.New("not main-magic")
		},
		func(r io.ReaderAt, size int64, cfg *Config) (Reader, error) {
			return &fakeReader{name: mainMagic}, nil
		},
	)

	t.Run("main wins on main-magic input", func(t *testing.T) {
		rdr, err := OpenAny(nil, -1, nil)
		if err != nil {
			t.Fatalf("OpenAny: %v", err)
		}
		if string(rdr.Format()) != mainMagic {
			t.Errorf("got %q, want %q", rdr.Format(), mainMagic)
		}
	})

	t.Run("fallback wins when main does not match", func(t *testing.T) {
		rdr, err := OpenAny(nil, 0, nil)
		if err != nil {
			t.Fatalf("OpenAny: %v", err)
		}
		if string(rdr.Format()) != "catch-all" {
			t.Errorf("got %q, want catch-all", rdr.Format())
		}
	})
}
