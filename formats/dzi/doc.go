// Package dzi reads bare Deep Zoom Image (DZI) slides: a filesystem .dzi XML
// manifest plus a sibling <name>_files/<level>/<col>_<row>.<ext> tile tree (the
// OpenSeadragon / Microsoft Deep Zoom layout). It is the filesystem sibling of
// formats/szi (the ZIP-wrapped variant) and shares internal/dzi for all pyramid
// and tile-coordinate math.
//
// Bare DZI is opened from a path via opentile.OpenFile — either the .dzi file or
// a directory containing exactly one .dzi — through a path-aware hook installed
// in init() (the same mechanism DICOM uses). Open(io.ReaderAt) does not support
// bare DZI because tiles live in sibling files that an io.ReaderAt cannot locate.
//
// Only Overlap=0 manifests are supported; Overlap>0 is rejected at open with
// internal/dzi.ErrOverlapNotSupported.
package dzi
