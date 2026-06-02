package dicom

import (
	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
)

// Metadata is the format-specific accessor payload (embeds the cross-format
// opentile.Metadata).
type Metadata struct {
	opentile.Metadata
	SeriesUID      string
	TransferSyntax string
	DimOrg         string
}

func buildMetadata(l0 idicom.Instance, s series) (opentile.Metadata, Metadata) {
	md := opentile.Metadata{
		Magnification:       l0.ObjectivePower,
		ScannerManufacturer: l0.Manufacturer,
		ScannerModel:        l0.Model,
		Writer:              l0.Writer,
		Properties:          map[string]string{},
	}
	if l0.Software != "" {
		md.ScannerSoftware = []string{l0.Software}
	}
	// PixelSpacing is in mm; opentile MPP is µm.
	md.MicronsPerPixelX = l0.PixelSpacingX * 1000
	md.MicronsPerPixelY = l0.PixelSpacingY * 1000
	if md.MicronsPerPixelX == md.MicronsPerPixelY {
		md.MicronsPerPixel = md.MicronsPerPixelX
	}
	dm := Metadata{Metadata: md, SeriesUID: l0.SeriesUID, TransferSyntax: l0.TransferSyntax, DimOrg: l0.DimOrg}
	return md, dm
}

// MetadataOf returns the DICOM-specific Metadata for a Slide-or-reader that
// wraps a *Tiler, walking the UnwrapReader chain (mirrors szi.MetadataOf).
func MetadataOf(v any) (*Metadata, bool) {
	const maxUnwrapHops = 16
	for i := 0; v != nil && i <= maxUnwrapHops; i++ {
		if t, ok := v.(*Tiler); ok {
			return &t.dmeta, true
		}
		u, ok := v.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		v = u.UnwrapReader()
	}
	return nil, false
}
