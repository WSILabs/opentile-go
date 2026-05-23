package opentiletest_test

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/opentile/opentiletest"
)

func TestNewConfigZeroSizeUnset(t *testing.T) {
	c := opentiletest.NewConfig(opentile.Size{}, opentile.CorruptTileError)
	_, ok := c.TileSize()
	if ok {
		t.Errorf("TileSize ok: got true, want false (zero size means unset)")
	}
}

func TestNewConfigExplicitSize(t *testing.T) {
	c := opentiletest.NewConfig(opentile.Size{W: 512, H: 256}, opentile.CorruptTileError)
	sz, ok := c.TileSize()
	if !ok {
		t.Fatal("TileSize ok: got false, want true (explicit size)")
	}
	if sz != (opentile.Size{W: 512, H: 256}) {
		t.Errorf("TileSize: got %v, want {512, 256}", sz)
	}
}
