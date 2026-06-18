package bif

// Serpentine tile ordering is the BIF physical-stage / stitch-graph tile
// numbering (whitepaper Figure 2): tile 1 is the lower-left tile, the path
// snakes up the slide, even stage rows go left-to-right and odd right-to-left.
//
// IMPORTANT (GH #57): this is the numbering of the `TileJointInfo`
// `Tile1`/`Tile2` stitch-graph IDs — NOT the `TILE_OFFSETS` pixel-storage
// order, which is ROW-MAJOR (declared by the `<Frame>` nodes; see level.go's
// indexOf / buildFrameIndex). Applying this serpentine remap to `TILE_OFFSETS`
// indexing scrambled multi-tile levels. These helpers are retained for the
// (currently global-averaged) tile-overlap path — a future per-join overlap
// implementation needs serpentine `Tile1`/`Tile2` ⇄ image-position mapping —
// and are NOT used for pixel-storage indexing.
//
// Stage convention (per Roche BIF whitepaper Figure 2):
//   - Stage rows count UP from the bottom of the slide.
//   - Even stage rows go left-to-right; odd stage rows go right-to-left.
//   - Tile 1 (TileOffsets[0]) is the bottom-left tile.
//
// Image convention (every TIFF reader's view):
//   - Image rows count DOWN from the top.
//   - Tiles are addressed (col, row) in row-major image order.
//
// Conversion (forward):
//   stageRow = rows - 1 - imageRow              (vertical flip)
//   stageCol = imageCol                         (default)
//   if stageRow % 2 == 1:                       (odd stage rows: serpentine flip)
//       stageCol = cols - 1 - imageCol
//   serpentineIdx = stageRow * cols + stageCol
//
// The inverse exists and is symmetric — given a serpentine index,
// divmod by `cols`, then reverse the flips.

// imageToSerpentine returns the index into BIF's `TileOffsets` array
// for the tile at image-space (col, row) within a grid of (cols, rows)
// tiles. Out-of-grid coordinates return -1; callers are expected to
// validate bounds before calling.
func imageToSerpentine(col, row, cols, rows int) int {
	if col < 0 || row < 0 || col >= cols || row >= rows {
		return -1
	}
	stageRow := rows - 1 - row
	stageCol := col
	if stageRow%2 == 1 {
		stageCol = cols - 1 - col
	}
	return stageRow*cols + stageCol
}

// serpentineToImage is the inverse of imageToSerpentine: given a
// serpentine tile index and the grid dimensions, return the
// image-space (col, row). Out-of-range indices return (-1, -1).
func serpentineToImage(idx, cols, rows int) (col, row int) {
	if idx < 0 || idx >= cols*rows {
		return -1, -1
	}
	stageRow := idx / cols
	stageCol := idx % cols
	if stageRow%2 == 1 {
		stageCol = cols - 1 - stageCol
	}
	row = rows - 1 - stageRow
	col = stageCol
	return col, row
}
