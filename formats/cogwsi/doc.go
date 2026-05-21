// Package cogwsi reads Cloud Optimized GeoTIFF for WSI (COG-WSI)
// files — a strict COG extension produced by the wsitools
// transcoder (cornish/wsitools). COG-WSI carries WSI-specific
// private tags (65080-65087) for level/associated classification
// and metadata, plus a COG_WSI_VERSION marker in the ghost area.
//
// Detection: ghost-area parsing via internal/cog. A file is
// COG-WSI iff its ghost area contains a COG_WSI_VERSION=<x.y> key.
//
// Reading: pyramid + associated extraction delegates to generic-
// TIFF's WSI-tag-aware path (closes Issue #5 + #6).
//
// Spec validation: open-time conformance check via
// ErrNotConformantCOGWSI sentinel.
//
// Spec: docs/specs/2026-05-20-cog-wsi-format.md.
package cogwsi
