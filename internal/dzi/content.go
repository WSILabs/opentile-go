package dzi

// ContentRect returns the content sub-rectangle (offX, offY, w, h) within the
// stored/decoded tile (col,row) of a level sized levelW×levelH with the given
// tileSize and overlap. offX/offY are the in-tile content offset (the overlap
// border present on the left/top edge when the tile has a neighbour there); w/h
// are the content cell size, clipped at the level's right/bottom edge. The
// right/bottom overlap (present when not on the last column/row) does not affect
// the content origin or size, so it needs no explicit term here.
func ContentRect(col, row, levelW, levelH, tileSize, overlap int) (offX, offY, w, h int) {
	if col > 0 {
		offX = overlap
	}
	if row > 0 {
		offY = overlap
	}
	w = tileSize
	if rem := levelW - col*tileSize; rem < w {
		w = rem
	}
	h = tileSize
	if rem := levelH - row*tileSize; rem < h {
		h = rem
	}
	return offX, offY, w, h
}
