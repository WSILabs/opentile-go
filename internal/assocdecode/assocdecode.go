// Package assocdecode is a thin shared helper for decoding image-codec
// associated images through the decoder registry, so each format's
// AssociatedImage.Decode doesn't duplicate the registry plumbing.
//
// Strip-based (LZW / Deflate / uncompressed) associated images do NOT go
// through here — they use internal/tiffstrip, which owns the predictor +
// sample-interpretation knowledge.
package assocdecode

import (
	"fmt"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

// ViaCodec decodes faithful, standalone image-codec bytes (JPEG, JPEG 2000,
// HTJ2K, WebP, AVIF, JPEG XL) through the registered decoder for comp,
// honoring opts. Returns a wrapped decoder.ErrCodecUnavailable when no
// decoder is registered for the codec (e.g. a jp2k image under a nojp2k
// build), matching the region-decode path's behavior.
func ViaCodec(comp opentile.Compression, data []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	tag := opentile.CompressionToTIFFTag(comp)
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, fmt.Errorf("assocdecode: no decoder registered for %s: %w", comp, decoder.ErrCodecUnavailable)
	}
	d := fac.New()
	defer d.Close()
	return d.Decode(data, opts)
}
