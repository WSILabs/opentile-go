package format

import (
	"errors"
	"io"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

// fakeReader satisfies format.Reader for registry tests.
type fakeReader struct{ name string }

func (r *fakeReader) Format() opentile.Format              { return opentile.Format(r.name) }
func (r *fakeReader) Images() []opentile.Image             { return nil }
func (r *fakeReader) Levels() []opentile.Level             { return nil }
func (r *fakeReader) Level(i int) (opentile.Level, error)  { return nil, nil }
func (r *fakeReader) Associated() []opentile.AssociatedImage { return nil }
func (r *fakeReader) Metadata() opentile.Metadata          { return opentile.Metadata{} }
func (r *fakeReader) ICCProfile() []byte                   { return nil }
func (r *fakeReader) WarmLevel(i int) error                { return nil }
func (r *fakeReader) Close() error                         { return nil }

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
