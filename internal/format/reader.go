package format

import (
	"context"
	"errors"
	"io"
	"iter"

	opentile "github.com/wsilabs/opentile-go"
)

// Reader is the contract every format implementation provides. The
// public *opentile.Slide type wraps a Reader and delegates all
// method calls. Internal to opentile-go.
//
// In v0.24, Level and Pyramid are value-type structs; tile reads happen
// at the Reader level with (image, level) addressing.
type Reader interface {
	Format() opentile.Format
	Pyramids() []opentile.Pyramid
	Level(image, level int) (opentile.Level, error)
	Associated() []opentile.AssociatedImage
	Metadata() opentile.Metadata
	ICCProfile() []byte
	WarmLevel(image, level int) error

	// Raw tile access.
	ImageRawTile(image, level, tx, ty int) ([]byte, error)
	ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error)

	// Splice-prefix optimization family.
	ImageTileMaxSize(image, level int) int
	ImageTilePrefix(image, level int) []byte
	ImageTileBodyMaxSize(image, level int) int
	ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error)
	ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error)

	// Range-over-function iterator.
	ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[opentile.TilePos, opentile.TileResult]

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
