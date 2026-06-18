package bif

import "github.com/wsilabs/opentile-go/internal/bifxml"

// StitchInput is the pure, file-free description the stitch engine needs to
// compute a layout. EncodeInfo may be nil (legacy slides without it) → naive.
type StitchInput struct {
	Cols, Rows   int
	TileW, TileH int
	EncodeInfo   *bifxml.EncodeInfo
	Generation   Generation
}

// TilePlacement is where one image-grid tile lands in stitched output space.
type TilePlacement struct {
	Col, Row int
	X, Y     int
}

// Layout is the stitch engine's result: per-tile placement + stitched extent.
// Built once at Open and cached on the level; immutable thereafter.
type Layout struct {
	Width, Height int
	cols, rows    int
	tileW, tileH  int
	origin        map[[2]int]TilePlacement // (col,row) → placement
}

// TileOrigin returns the stitched-space top-left of image-grid tile (col,row).
func (l *Layout) TileOrigin(col, row int) (x, y int, ok bool) {
	p, ok := l.origin[[2]int{col, row}]
	if !ok {
		return 0, 0, false
	}
	return p.X, p.Y, true
}

// Placements returns every tile placement (row-major order).
func (l *Layout) Placements() []TilePlacement {
	out := make([]TilePlacement, 0, len(l.origin))
	for row := 0; row < l.rows; row++ {
		for col := 0; col < l.cols; col++ {
			if p, ok := l.origin[[2]int{col, row}]; ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// TilesIntersecting returns image-grid tiles whose stitched extent (tileW×tileH
// at their placement) overlaps the output rectangle [x,y,x+w,y+h).
func (l *Layout) TilesIntersecting(x, y, w, h int) []TilePlacement {
	x1, y1 := x+w, y+h
	var out []TilePlacement
	for _, p := range l.Placements() {
		px1, py1 := p.X+l.tileW, p.Y+l.tileH
		if p.X < x1 && px1 > x && p.Y < y1 && py1 > y {
			out = append(out, p)
		}
	}
	return out
}

// BuildLayout computes the tile layout for a level. Dispatches to the
// whitepaper-exact DP path when the inputs support it (Task 4); otherwise
// returns the naive regular-grid layout used by legacy fallback and pyramid
// levels ≥1 (per Roche whitepaper §"Image Pyramid": only level 0 overlaps).
func BuildLayout(in StitchInput) *Layout {
	if dp := buildDPLayout(in); dp != nil {
		return dp
	}
	return buildNaiveLayout(in)
}

func buildNaiveLayout(in StitchInput) *Layout {
	l := newLayout(in.Cols, in.Rows, in.TileW, in.TileH)
	for row := 0; row < in.Rows; row++ {
		for col := 0; col < in.Cols; col++ {
			l.origin[[2]int{col, row}] = TilePlacement{Col: col, Row: row, X: col * in.TileW, Y: row * in.TileH}
		}
	}
	l.Width = in.Cols * in.TileW
	l.Height = in.Rows * in.TileH
	return l
}

func newLayout(cols, rows, tileW, tileH int) *Layout {
	return &Layout{cols: cols, rows: rows, tileW: tileW, tileH: tileH, origin: make(map[[2]int]TilePlacement, cols*rows)}
}

// buildDPLayout is filled in Task 4; until then it always declines (nil).
func buildDPLayout(in StitchInput) *Layout { return nil }
