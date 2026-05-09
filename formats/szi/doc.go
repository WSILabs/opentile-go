// Package szi reads Smart Zoom Image (.szi) files — ZIP-wrapped
// Microsoft Deep Zoom pyramids with scan-properties.xml and an
// associated_images/ folder. Spec: smartinmedia/SZI-Format
// (LGPL + CC-BY licensed).
//
// SZI structure:
//
//	<root>/
//	  <name>.dzi                 -- DZI manifest XML
//	  scan-properties.xml        -- SZI-specific scan metadata
//	  <name>_files/<lvl>/<c>_<r>.<fmt>  -- tile pyramid (Microsoft DZI)
//	  associated_images/         -- optional
//	    macro.jpg                -- exposed as Type() == "overview"
//	    label.jpg                -- exposed as Type() == "label"
//	    thumbnail.jpg            -- exposed as Type() == "thumbnail"
//	  vendor/                    -- optional; v0.16 ignores contents
//
// Tile fetch is byte-passthrough: each ZIP entry is an
// uncompressed-stored JPEG (or PNG, per DZI spec); reads resolve
// directly to a SectionReader on the .szi file. No decompression,
// no copy on the hot path.
//
// Sparse SZI files are not supported per the spec (a missing tile
// is treated as a corrupt archive). Bare DZI (filesystem-backed,
// no ZIP) is deferred to a future formats/dzi/ package.
package szi
