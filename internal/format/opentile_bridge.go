package format

import (
	"io"

	opentile "github.com/wsilabs/opentile-go"
)

func init() {
	opentile.SetOpenAnyHook(bridgeOpenAny)
}

// bridgeOpenAny translates opentile's flat config arguments into a format.Config
// and calls OpenAny. Returns format.Reader as an any interface; opentile.go
// type-asserts it to slideReader (which has the same method set as format.Reader).
//
// This bridge exists to break the import cycle:
//
//	opentile → internal/format → opentile  (would be a cycle)
//
// Instead: internal/format → opentile (one-way), opentile sets the hook via
// an exported function that takes only opentile-defined types.
func bridgeOpenAny(
	r io.ReaderAt,
	size int64,
	tileSize opentile.Size,
	hasTileSize bool,
	corruptTilePolicy opentile.CorruptTilePolicy,
	ndpiSynthLabel bool,
	backing opentile.Backing,
) (any, error) {
	cfg := &Config{
		TileSize:             tileSize,
		HasTileSize:          hasTileSize,
		CorruptTilePolicy:    corruptTilePolicy,
		NDPISynthesizedLabel: ndpiSynthLabel,
		Backing:              backing,
	}
	return OpenAny(r, size, cfg)
}
