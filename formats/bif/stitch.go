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

// buildDPLayout computes the whitepaper-exact layout (Roche BIF whitepaper
// §"Image stitching process", page 15). Declines (nil) — falling back to naive
// — unless the inputs are a spec-compliant DP slide with a usable EncodeInfo
// (Ver≥2, at least one confident joint). Pyramid levels ≥1 carry no joints →
// nil → naive (whitepaper page 16, "IFD 3 and Higher": lower-resolution tiles
// abut each other with no overlap).
func buildDPLayout(in StitchInput) *Layout {
	ei := in.EncodeInfo
	// Whitepaper page 12: "Stop processing the file if the [Ver] attribute
	// value is less than 2." Only spec-compliant DP slides are placed here;
	// legacy iScan slides are routed naive per the whitepaper's own caveat
	// (page 3) that older scanners "cannot be reconstructed correctly".
	if in.Generation != GenerationSpecCompliant || ei == nil || ei.Ver < 2 {
		return nil
	}
	if !hasConfidentJoint(ei) {
		return nil
	}
	// Map storage index → image-grid (col,row) from the Frames (row-major,
	// whitepaper page 14: XY="C,R", first Frame = first TILE_OFFSETS element).
	// Each ImageInfo's Frames are local to that AOI; Tile1/Tile2 in its Joints
	// index into the same per-AOI frame ordering.
	l := newLayout(in.Cols, in.Rows, in.TileW, in.TileH)
	type key = [2]int
	// Single-AOI for this task; multi-AOI offsets added in Task 4.
	for _, ii := range ei.ImageInfos {
		framePos := make([]key, len(ii.Frames)) // storage idx → (col,row)
		for i, f := range ii.Frames {
			framePos[i] = key{f.Col, f.Row}
		}
		// Anchor: place every frame at its nominal grid position first, then
		// relax along confident joints. Iterate to a fixed point (the joint
		// graph is a DAG over a grid, so |cols|+|rows| passes converge).
		pos := make(map[key][2]int, len(framePos))
		for _, p := range framePos {
			pos[p] = [2]int{p[0] * in.TileW, p[1] * in.TileH}
		}
		for pass := 0; pass < in.Cols+in.Rows; pass++ {
			for _, j := range ii.Joints {
				if !j.FlagJoined || j.Confidence != 100 {
					continue // whitepaper page 13: trust only confident, joined pairs
				}
				if j.Tile1 < 0 || j.Tile1 >= len(framePos) || j.Tile2 < 0 || j.Tile2 >= len(framePos) {
					continue
				}
				a, b := framePos[j.Tile1], framePos[j.Tile2]
				switch j.Direction {
				case "RIGHT":
					// Whitepaper page 15 + Figure 4: Tile2 sits OverlapX pixels
					// left of Tile1's right edge — Tile2.X = Tile1.X + tileW -
					// OverlapX, Y unchanged. Take the smallest consistent X
					// (compaction). LEFT is the mirror case used by the real
					// serpentine path; this task's grid emits only RIGHT.
					nx := pos[a][0] + in.TileW - j.OverlapX
					if pass == 0 || nx < pos[b][0] {
						pos[b] = [2]int{nx, pos[b][1]}
					}
				case "DOWN":
					// Whitepaper page 15: "Similar rules apply for the overlap
					// between vertical tile pairs." Tile2.Y = Tile1.Y + tileH -
					// OverlapY; X unchanged. (DP 200 has OverlapY==0, page 15.)
					ny := pos[a][1] + in.TileH - j.OverlapY
					if pass == 0 || ny < pos[b][1] {
						pos[b] = [2]int{pos[b][0], ny}
					}
				}
			}
		}
		for _, p := range framePos {
			l.origin[p] = TilePlacement{Col: p[0], Row: p[1], X: pos[p][0], Y: pos[p][1]}
		}
	}
	finalizeExtent(l, in) // hull + white-pad; completed in Task 4
	return l
}

func hasConfidentJoint(ei *bifxml.EncodeInfo) bool {
	for _, ii := range ei.ImageInfos {
		for _, j := range ii.Joints {
			if j.FlagJoined && j.Confidence == 100 {
				return true
			}
		}
	}
	return false
}

// finalizeExtent is completed in Task 4 (hull + normalize + white-pad). For now
// it sets the extent to the bounding box of placements, padded up to a tile
// multiple. Whitepaper page 5: "If the convex hull of all AOIs combined in the
// BIF-image is not a multiple of the tile size, the image will be padded with
// empty white pixels to the top and right."
func finalizeExtent(l *Layout, in StitchInput) {
	maxX, maxY := 0, 0
	for _, p := range l.origin {
		if p.X+l.tileW > maxX {
			maxX = p.X + l.tileW
		}
		if p.Y+l.tileH > maxY {
			maxY = p.Y + l.tileH
		}
	}
	l.Width = roundUpToMultiple(maxX, in.TileW)
	l.Height = roundUpToMultiple(maxY, in.TileH)
}

func roundUpToMultiple(v, m int) int {
	if m <= 0 || v%m == 0 {
		return v
	}
	return ((v / m) + 1) * m
}
