package format

import (
	"errors"
	"io"

	opentile "github.com/wsilabs/opentile-go"
)

// Reader is the contract every format implementation provides.
// Shape mirrors the pre-v0.23 public Tiler interface exactly, just
// internalized so the public surface (*opentile.Slide) can evolve
// without forcing every format to grow in lockstep.
type Reader interface {
	Format() opentile.Format
	Images() []opentile.Image
	Levels() []opentile.Level
	Level(i int) (opentile.Level, error)
	Associated() []opentile.AssociatedImage
	Metadata() opentile.Metadata
	ICCProfile() []byte
	WarmLevel(i int) error
	Close() error
}

// Config is passed from opentile.Open's option-config down into each
// format's Opener. Fields mirror the existing opentile.Config that
// FormatFactory.Open accepted; preserved verbatim for format packages.
type Config struct {
	// TileSize is the requested output tile size (W, H in pixels) and
	// whether the caller explicitly set one. Mirrors opentile.Config.TileSize().
	TileSize    opentile.Size
	HasTileSize bool

	// CorruptTilePolicy controls how corrupt-edge tiles are reported.
	CorruptTilePolicy opentile.CorruptTilePolicy

	// NDPISynthesizedLabel controls whether NDPI Associated() includes a
	// synthesized label cropped from the overview. Default true.
	NDPISynthesizedLabel bool

	// Backing reports the I/O backend selected via WithBacking.
	Backing opentile.Backing
}

// Opener constructs a Reader from a parsed input. r is the raw bytes;
// cfg is the option-derived config. Returns a non-nil Reader on success.
type Opener func(r io.ReaderAt, size int64, cfg *Config) (Reader, error)

// Match returns nil if the format applies to this input, or an error
// describing why it doesn't (the error is informational; only nil/non-nil
// determines dispatch).
type Match func(r io.ReaderAt, size int64) error

// ErrUnknownFormat is returned by OpenAny when no registered format
// claims the input.
var ErrUnknownFormat = errors.New("opentile: unknown format")
