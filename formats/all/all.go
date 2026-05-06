// Package all registers every format implemented by opentile-go. Import this
// package for its side effect (via a blank import) or call Register() once
// from main for equivalent behavior without relying on import ordering.
//
//	import _ "github.com/cornish/opentile-go/formats/all"
//
// Or:
//
//	import formats_all "github.com/cornish/opentile-go/formats/all"
//	...
//	formats_all.Register()
package all

import (
	"sync"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/formats/bif"
	"github.com/cornish/opentile-go/formats/generictiff"
	"github.com/cornish/opentile-go/formats/ife"
	"github.com/cornish/opentile-go/formats/leicascn"
	"github.com/cornish/opentile-go/formats/ndpi"
	"github.com/cornish/opentile-go/formats/ome"
	"github.com/cornish/opentile-go/formats/philips"
	"github.com/cornish/opentile-go/formats/svs"
)

var once sync.Once

// Register registers all known format factories with the top-level opentile
// package. Safe to call multiple times; only the first call registers.
func Register() {
	once.Do(func() {
		opentile.Register(svs.New())
		opentile.Register(ndpi.New())
		opentile.Register(philips.New())
		opentile.Register(ome.New())
		opentile.Register(bif.New())
		opentile.Register(ife.New())
		opentile.Register(leicascn.New())
		// Generic TIFF must register LAST: it's the catch-all
		// for tiled pyramidal TIFFs that no vendor format claims.
		// Registering earlier would let it steal vendor-shaped TIFFs.
		opentile.Register(generictiff.New())
	})
}

func init() { Register() }
