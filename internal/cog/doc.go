// Package cog parses the GDAL Cloud Optimized GeoTIFF ghost-area
// (the contiguous block of ASCII key-value metadata immediately
// following the TIFF header).
//
// The ghost area lets a reader probe COG (and COG-WSI) structural
// properties without walking IFDs. Per the GDAL convention:
//
//	GDAL_STRUCTURAL_METADATA_SIZE=NNNNNN bytes
//	LAYOUT=IFDS_BEFORE_DATA
//	BLOCK_ORDER=ROW_MAJOR
//	BLOCK_LEADER=SIZE_AS_UINT4
//	BLOCK_TRAILER=LAST_4_BYTES_REPEATED
//	KNOWN_INCOMPATIBLE_EDITION=NO
//	COG_WSI_VERSION=0.1   (COG-WSI files only; absent in plain COG)
//
// This package is pure: no I/O, no TIFF parsing — callers read the
// raw bytes from the file (after the TIFF header) and pass them in.
// Designed to support both COG-WSI detection (the v0.19 use case)
// and any future plain-COG awareness (a side-benefit, not the
// purpose of this package).
package cog
