package opentile

// Format identifies the source file format.
type Format string

const (
	FormatSVS  Format = "svs"
	FormatNDPI Format = "ndpi"
	// FormatPhilipsTIFF is the Philips IntelliSite Pathology Solution TIFF
	// reader. Renamed in v0.12 from FormatPhilips to FormatPhilipsTIFF to
	// align with v0.10's FormatGenericTIFF and v0.11's FormatLeicaSCN naming
	// convention; Philips has multiple WSI file formats (TIFF; iSyntax), so
	// the bare "philips" identifier was ambiguous. Reports as "philips-tiff".
	FormatPhilipsTIFF Format = "philips-tiff"
	// FormatOMETIFF is the OME-TIFF reader. Renamed in v0.12 to align
	// with v0.10/v0.11's FormatGenericTIFF / FormatLeicaSCN convention;
	// OME has multiple file formats (OME-TIFF, OME-Zarr, OME-NGFF), so
	// the bare "ome" identifier ambiguously claimed the family.
	FormatOMETIFF Format = "ome-tiff"
	FormatBIF     Format = "bif"
	FormatIFE     Format = "ife"
	// FormatGenericTIFF is the catch-all reader for tiled pyramidal TIFF
	// without vendor metadata (added in v0.10). Activates when no
	// vendor format factory claims the file. Reports as "generic-tiff"
	// to differentiate from possible future generic-non-TIFF readers.
	FormatGenericTIFF Format = "generic-tiff"
	// FormatLeicaSCN is the Leica SCN reader (added in v0.11). SCN is a
	// BigTIFF dialect produced by Leica SCN400 / SCN400F scanners;
	// scanner production stopped ~2015. Reports as "leica-scn" to
	// differentiate from other Leica-related formats (LIF, LMS) that
	// aren't SCN.
	FormatLeicaSCN Format = "leica-scn"
	// FormatSZI identifies a Smart Zoom Image file (ZIP-wrapped
	// Microsoft Deep Zoom pyramid + scan-properties.xml +
	// associated_images/, per the smartinmedia/SZI-Format spec).
	//
	// Added in v0.16.
	FormatSZI Format = "szi"
	// FormatCOGWSI identifies a Cloud Optimized GeoTIFF for WSI file —
	// a strict extension of GDAL Cloud Optimized GeoTIFF carrying
	// WSI-specific private tags + ghost-area marker. Spec at
	// docs/specs/2026-05-20-cog-wsi-format.md. Added in v0.19.
	FormatCOGWSI Format = "cog-wsi"
)
