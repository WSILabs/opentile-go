# DZI (bare Deep Zoom Image)

`FormatDZI` — a filesystem Deep Zoom Image: a `<name>.dzi` XML manifest plus a
sibling `<name>_files/<level>/<col>_<row>.<ext>` tile tree (the OpenSeadragon /
Microsoft Deep Zoom layout). The filesystem sibling of [SZI](szi.md) (the
ZIP-wrapped variant); both share `internal/dzi` for pyramid and tile math.

## Opening

Bare DZI is path-based, so it is opened via `opentile.OpenFile` (not
`opentile.Open(io.ReaderAt)`, which cannot locate the sibling tile files):

- `OpenFile("/path/to/slide.dzi")` — the manifest file (primary).
- `OpenFile("/path/to/dir")` — a directory containing exactly one `*.dzi`.

This uses a path-aware hook, the same mechanism DICOM uses.

## Tiles

Each tile is a complete JPEG/PNG file. The pyramid grid is clean
(`Level.Overlapping == false`, `Grid` tiles `Size`); `Tile(x,y)` returns the raw
file bytes, and `DecodedTile`/`ReadRegion`/`ScaledStrips` work as for any clean
format.

## Limitations

- **`Overlap=0` only.** A manifest with `Overlap>0` is rejected at open with
  `internal/dzi.ErrOverlapNotSupported` (cropping the per-tile overlap border is
  deferred). The same guard now also applies to SZI.
- **No metadata / associated images.** A bare `.dzi` carries only the manifest;
  `Metadata()` is empty and `AssociatedImages()` is nil.
- **Dense pyramids assumed.** A missing in-range tile is an error, not a blank
  fill.
