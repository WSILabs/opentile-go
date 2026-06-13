# Migration: v1.0 API breaking pass

This release moves the opentile-go public API to its v1.0 shape. The changes are
mechanical to migrate; this table maps every breaking change to its replacement.

| Area | Before | After |
|------|--------|-------|
| Container type | `opentile.Image` | `opentile.Pyramid` |
| Pyramid list | `slide.Images()` / `slide.Image(i)` | `slide.Pyramids()` / `slide.Pyramid(i)` |
| Associated list | `slide.Associated()` | `slide.AssociatedImages()` |
| Associated encoding | `slide.AssociatedEncoding(a)` | `a.Encoding()` |
| Associated TIFF tags | `slide.AssociatedTIFFTags(a)` | `a.TIFFTags()` |
| Associated IFD offset | `slide.AssociatedIFDOffset(a)` | `a.IFDOffset()` |
| Type() result | `string` | `AssociatedType` (string-underlying; `==` comparisons unchanged, conversions needed where stored/passed as `string`) |
| MPP | `level.MPP SizeMm` / `md.MicronsPerPixel` | `level.MPP MPP` / `md.MPP MPP` (microns) |
| Raw tile | `slide.RawTile(level, tx, ty)` | `lv, _ := slide.Level(level); lv.Tile(tx, ty)` |
| Decoded tile | `slide.DecodedTile(level, tx, ty, opts)` | `lv, _ := slide.Level(level); lv.DecodedTile(tx, ty, opts)` |
| Region | `slide.ReadRegion(level, x, y, w, h, opts)` | `lv, _ := slide.Level(level); lv.ReadRegion(Region{Origin:Point{x,y}, Size:Size{w,h}}, opts)` |
| Scaled region | `slide.ReadRegionScaled(...)` | `slide.Pyramid(0).ReadRegionScaled(srcRegion, outSize, opts)` |
| Multi-image reads | `slide.ImageRawTile(img, level, tx, ty)` etc. | `lv, _ := slide.Pyramid(img).Level(level); lv.Tile(tx, ty)` etc. |
| Scaled strips | `slide.ScaledStrips(...)` | `slide.Pyramid(0).ScaledStrips(...)` |
| TIFF dirs | `opentile.TIFFDirectoriesOf(s)` | `s.TIFFDirectories()` |
| Level TIFF tags | `slide.LevelTIFFTags(l)` | `lv, _ := slide.Level(l); lv.TIFFTags()` |
| Tile position | `TilePos{X,Y}` (from RangeTiles) | `Point{X,Y}` |

Notes:
- Reads now live on `*Level` (per-level: `Tile`/`DecodedTile`/`ReadRegion`/`Tiles`) and `*Pyramid` (cross-level, L0 coords: `ReadRegionScaled`/`ScaledStrips`/`BestLevelForDownsample`). The bare `slide.Level(i)` (returns `(*Level, error)`) and `slide.Pyramid(i)` (returns `*Pyramid`) navigation accessors remain.
- `Pyramid.LevelPtrs()` returns `[]*Level` for ranging over levels when you need pointer receivers. `Pyramid.Level(i)` returns `(*Level, error)` for indexed access. `Pyramid.Levels []Level` (field) remains for read-only struct inspection.
- The deferred multi-dimensional surface (`Bands`/`Sample` on `decoder.Image`, `TileAt`/`DecodedTileAt`, `Pyramid.SizeZ/C/T`, `ReadRegion…At(r, Plane)`) is NOT part of this release.
