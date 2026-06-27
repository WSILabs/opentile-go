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

Each tile is a complete JPEG/PNG file. `Tile(x,y)` returns the raw on-disk file
bytes; `DecodedTile`, `ReadRegion`, `ReadRegionScaled`, `ScaledStrips`, and
`StitchedTile` return decoded pixels.

## Overlap

DZI manifests carry an `Overlap` attribute (typically 0, 1, or 2 pixels) that adds
a croppable border to each tile. `Overlap = 0` is the simple case: the grid is
clean, every tile holds exactly its content pixels, and `Level.Overlapping == false`.

`Overlap > 0` is now read correctly (previously rejected with
`internal/dzi.ErrOverlapNotSupported`).

### The DZI/OpenSeadragon overlap model

Each stored tile carries `Overlap` extra pixels on every edge that has a neighbour:
left border when `col > 0`, top border when `row > 0`, right/bottom borders when
not the last column/row. The *content cell* is unchanged — its pixel dimensions are
`min(TileSize, slideW − col×TileSize)` × `min(TileSize, slideH − row×TileSize)`.
The content sits at offset `(col>0 ? Overlap : 0, row>0 ? Overlap : 0)` inside the
stored tile. Edge tiles at the right/bottom boundary are stored UNPADDED at their
actual content size (unlike TIFF-based formats which store full-tile-padded edge
tiles).

### Read-path contract for `Overlap > 0`

| API | What you get |
|-----|-------------|
| `Tile(col, row)` | On-disk padded bytes (the stored JPEG/PNG including overlap border) |
| `DecodedTile(col, row, ...)` | Padded pixels; use `TileContentRect(col, row)` to obtain the crop |
| `StitchedTile(col, row, ...)` | Clean overlap-removed display tile (composited) |
| `ReadRegion` / `ReadRegionScaled` / `ScaledStrips` | Clean composited pixels — overlap border is cropped out |

### Relevant `Level` fields for `Overlap > 0`

- `Level.OverlapMode == OverlapBordered` — tiles are padded with a croppable border.
- `Level.TileOverlap == {Overlap, Overlap}` — the magnitude in both axes.
- `Level.Overlapping == true` — the derived convenience (`OverlapMode != OverlapNone`).
- `Level.TileContentRect(col, row) (Region, bool)` — returns the content
  sub-rectangle within a decoded tile (the crop to drop the overlap border).
- `Level.Grid` still tiles `Level.Size` exactly (unlike `OverlapStitched` / BIF
  where the grid does not tile Size).

`Overlap = 0` reads are byte-identical to prior behaviour; `Level.OverlapMode ==
OverlapNone` and `Level.Overlapping == false`.

## Limitations

- **No metadata / associated images.** A bare `.dzi` carries only the manifest;
  `Metadata()` is empty and `AssociatedImages()` is nil.
- **Dense pyramids assumed.** A missing in-range tile is an error, not a blank
  fill.
