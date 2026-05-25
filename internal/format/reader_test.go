package format_test

import (
	"context"
	"io"
	"iter"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
)

func TestReaderInterfaceExists(t *testing.T) {
	var _ format.Reader = (*nilReader)(nil)
}

// nilReader satisfies format.Reader for compile-time verification.
type nilReader struct{}

func (*nilReader) Format() opentile.Format                                 { return "" }
func (*nilReader) Images() []opentile.Image                                { return nil }
func (*nilReader) Level(_, _ int) (opentile.Level, error)                  { return opentile.Level{}, nil }
func (*nilReader) Associated() []opentile.AssociatedImage                  { return nil }
func (*nilReader) Metadata() opentile.Metadata                             { return opentile.Metadata{} }
func (*nilReader) ICCProfile() []byte                                      { return nil }
func (*nilReader) WarmLevel(_, _ int) error                                { return nil }
func (*nilReader) ImageRawTile(_, _, _, _ int) ([]byte, error)             { return nil, nil }
func (*nilReader) ImageRawTileInto(_, _, _, _ int, _ []byte) (int, error)  { return 0, nil }
func (*nilReader) ImageTileMaxSize(_, _ int) int                           { return 0 }
func (*nilReader) ImageTilePrefix(_, _ int) []byte                         { return nil }
func (*nilReader) ImageTileBodyMaxSize(_, _ int) int                       { return 0 }
func (*nilReader) ImageTileBodyInto(_, _, _, _ int, _ []byte) (int, error) { return 0, nil }
func (*nilReader) ImageTileReader(_, _, _, _ int) (io.ReadCloser, error)   { return nil, nil }
func (*nilReader) ImageRangeTiles(_ context.Context, _, _ int) iter.Seq2[opentile.TilePos, opentile.TileResult] {
	return nil
}
func (*nilReader) Close() error { return nil }
