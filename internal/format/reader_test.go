package format_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
)

func TestReaderInterfaceExists(t *testing.T) {
	var _ format.Reader = (*nilReader)(nil)
}

// nilReader satisfies format.Reader for compile-time verification.
type nilReader struct{}

func (*nilReader) Format() opentile.Format                { return "" }
func (*nilReader) Images() []opentile.Image               { return nil }
func (*nilReader) Levels() []opentile.Level               { return nil }
func (*nilReader) Level(i int) (opentile.Level, error)    { return nil, nil }
func (*nilReader) Associated() []opentile.AssociatedImage { return nil }
func (*nilReader) Metadata() opentile.Metadata            { return opentile.Metadata{} }
func (*nilReader) ICCProfile() []byte                     { return nil }
func (*nilReader) WarmLevel(i int) error                  { return nil }
func (*nilReader) Close() error                           { return nil }
