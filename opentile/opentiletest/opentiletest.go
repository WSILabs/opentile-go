// Package opentiletest provides test helpers for opentile consumers and the
// library's own internal tests. Helpers here construct opentile.Config
// values with explicit field overrides — never call this from production
// code paths.
package opentiletest

import opentile "github.com/wsilabs/opentile-go"

// NewConfig constructs an opentile.Config for use in tests. A non-zero
// tileSize is treated as explicitly set (TileSize ok=true); a zero Size
// is treated as "use format default" (TileSize ok=false). The policy
// argument follows the same semantics as WithCorruptTilePolicy.
func NewConfig(tileSize opentile.Size, policy opentile.CorruptTilePolicy) *opentile.Config {
	return opentile.NewTestConfig(tileSize, policy)
}
