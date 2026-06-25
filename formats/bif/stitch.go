package bif

import (
	"math"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

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

// SubtilesIntersecting returns the placements (L0 frame col,row) whose SUBTILE
// extent — the frame's compacted origin scaled by 1/2^shift, sized
// (tileW>>shift × tileH>>shift) — overlaps the output rectangle [x,y,x+w,y+h)
// at a reduced level. Used by the subtile compositing model (#80/#83): each L0
// frame is placed independently at its scaled compacted position, so the frame
// overlap baked inside a reduced tile is removed.
func (l *Layout) SubtilesIntersecting(shift uint, x, y, w, h int) []TilePlacement {
	uw, uh := l.tileW>>shift, l.tileH>>shift
	x1, y1 := x+w, y+h
	var out []TilePlacement
	for _, p := range l.Placements() {
		px, py := p.X>>shift, p.Y>>shift
		if px < x1 && px+uw > x && py < y1 && py+uh > y {
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
	if lg := buildLegacyLayout(in); lg != nil {
		return lg
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
	// Whitepaper page 14, §"AoiOrigin": each AOI's upper-left corner in the
	// merged high-resolution image is <AOI<N> OriginX/OriginY> (pixel
	// coordinates, always multiples of the tile size). Page 16 + Figure 5: the
	// BIF-image is the convex hull of all AOIs, each AOI's tiles offset by its
	// origin. Build the index→origin map once; absent index → zero offset
	// (whitepaper: single-AOI files have OriginX=OriginY=0).
	aoiOrigin := map[int][2]int{}
	for _, o := range ei.AoiOrigins {
		aoiOrigin[o.Index] = [2]int{o.OriginX, o.OriginY}
	}
	for _, ii := range ei.ImageInfos {
		framePos := make([]key, len(ii.Frames)) // storage idx → (col,row)
		for i, f := range ii.Frames {
			framePos[i] = key{f.Col, f.Row}
		}
		// Serpentine grid for resolving Tile1/Tile2. The whitepaper (page 13)
		// defines Tile1/Tile2 as indices in the PHYSICAL (stage) coordinate
		// system — the serpentine path of Figure 2/4 (lower-left = tile 1, even
		// stage rows left→right, odd rows right→left), and they are 1-BASED
		// ("starting with tile 1"). They are NOT the row-major TILE_OFFSETS
		// storage index the <Frame> nodes declare. Use this AOI's scanned grid
		// (NumCols×NumRows) for the conversion; fall back to the level grid when
		// the AOI omits it (defensive — real DP files always carry it).
		scols, srows := ii.NumCols, ii.NumRows
		if scols <= 0 || srows <= 0 {
			scols, srows = in.Cols, in.Rows
		}
		// jointTile resolves a 1-based serpentine Tile index to its image-space
		// (col,row). Returns ok=false for out-of-grid indices (skip the joint).
		jointTile := func(idx int) (key, bool) {
			c, r := serpentineToImage(idx-1, scols, srows)
			if c < 0 {
				return key{}, false
			}
			return key{c, r}, true
		}
		// Anchor: place every frame at its nominal grid position first, then
		// relax along confident joints. Iterate to a fixed point (the joint
		// graph is a DAG over a grid, so |cols|+|rows| passes converge).
		pos := make(map[key][2]int, len(framePos))
		for _, p := range framePos {
			pos[p] = [2]int{p[0] * in.TileW, p[1] * in.TileH}
		}
		seeded := make(map[key]bool, len(framePos)) // has this tile been compacted yet?
		for pass := 0; pass < scols+srows; pass++ {
			for _, j := range ii.Joints {
				if !j.FlagJoined || j.Confidence != 100 {
					continue // whitepaper page 13: trust only confident, joined pairs
				}
				a, aok := jointTile(j.Tile1)
				b, bok := jointTile(j.Tile2)
				if !aok || !bok {
					continue
				}
				if _, ok := pos[a]; !ok {
					continue
				}
				if _, ok := pos[b]; !ok {
					continue
				}
				// Whitepaper page 15 + Figure 4: a horizontal joint places Tile2
				// one image-column to the RIGHT of Tile1, overlapping by OverlapX
				// (Tile2's left edge sits OverlapX pixels inside Tile1's right
				// edge); a vertical joint places Tile2 one image-row ABOVE Tile1
				// (serpentine "UP"), overlapping by OverlapY. Direction names the
				// serpentine traversal step, NOT a spatial inversion: confirmed
				// against the real Ventana-1 joints, every LEFT joint has Tile2 at
				// col+1 same row, every UP joint has Tile2 at row−1 same col.
				//
				// LEFT/RIGHT are the two horizontal traversal labels (the real
				// DP-200 serpentine emits only LEFT; synthetic grids emit RIGHT);
				// UP/DOWN are the two vertical labels (DP-200 emits only UP). All
				// four reduce to the same anchor→neighbor compaction: the tile
				// that is spatially right/below is shifted inward toward its
				// left/upper neighbor by the overlap. We normalize to "anchor =
				// the left/upper tile, target = the right/lower tile" by sorting
				// the pair on the relevant image-axis, so every direction label
				// is handled uniformly and correctly.
				switch j.Direction {
				case "LEFT", "RIGHT":
					left, right := a, b
					if right[0] < left[0] {
						left, right = right, left
					}
					nx := pos[left][0] + in.TileW - j.OverlapX
					if !seeded[right] || nx < pos[right][0] {
						pos[right] = [2]int{nx, pos[right][1]}
						seeded[right] = true
					}
				case "UP", "DOWN":
					// Whitepaper page 15: "Similar rules apply for the overlap
					// between vertical tile pairs." (DP 200 has OverlapY==0.)
					upper, lower := a, b
					if lower[1] < upper[1] {
						upper, lower = lower, upper
					}
					ny := pos[upper][1] + in.TileH - j.OverlapY
					if !seeded[lower] || ny < pos[lower][1] {
						pos[lower] = [2]int{pos[lower][0], ny}
						seeded[lower] = true
					}
				}
			}
		}
		// Shift this AOI's compacted local placements by its origin
		// (whitepaper page 14/16). The per-AOI frame (col,row) keys are local;
		// the origin lands them in the merged image's shared coordinate space.
		off := aoiOrigin[ii.AOIIndex] // zero value {0,0} if absent
		for _, p := range framePos {
			l.origin[p] = TilePlacement{Col: p[0], Row: p[1], X: pos[p][0] + off[0], Y: pos[p][1] + off[1]}
		}
	}
	finalizeExtent(l, in) // hull + normalize + white-pad
	return l
}

// legacyConfidenceCutoff is the minimum TileJointInfo Confidence trusted when
// reconstructing legacy iScan placement. Phase 0: the value is non-critical
// (98 vs 0 move OS-1 dims by a few px); 98 keeps only high-confidence joins.
const legacyConfidenceCutoff = 98

// buildLegacyLayout reconstructs tile placement for legacy iScan BIF (Coreo/HT),
// which carry no <Frame> nodes — the only position signal is the TileJointInfo
// overlap graph plus the per-AOI geometry (origin / Pos / NumCols×NumRows).
//
// MULTI-AOI (#67): a legacy slide may carry several Areas of Interest, each a
// sub-grid of the global tile grid placed at its own origin (OS-2 has 3 AOIs,
// one unscanned). Following openslide's ventana reader: pair ImageInfo[i] with
// AoiOrigin[i] by document order, skip AOIScanned=false AOIs, and place each
// scanned AOI's local NumCols×NumRows grid at (Pos-X, flipped Pos-Y) with
// start col/row = Origin / tileSize. Pos-Y is measured from the AOI bottom, so
// it is flipped to image space (top - Pos-Y - height). Single-AOI slides (OS-1)
// are the degenerate case (one AOI at OriginX=0, Pos-X=0 → lands at origin,
// byte-identical to before).
//
// WITHIN each AOI, placement uses the SEPARABLE per-axis per-gap-average overlap
// model (#63, localOffsets): tile (c,r) at (Pos-X + X[c], top'+Y[r]) where X/Y
// accumulate (tile - perGapAvgOverlap) over that AOI's joints (serpentine over
// its LOCAL grid). Clean-room — derived from the file's own joints; openslide /
// bio-formats are test oracles only. Declines (nil → naive) unless legacy with
// live joints.
func buildLegacyLayout(in StitchInput) *Layout {
	ei := in.EncodeInfo
	if in.Generation != GenerationLegacyIScan || ei == nil || len(ei.ImageInfos) == 0 {
		return nil
	}
	if !hasLiveJoint(ei) {
		return nil
	}
	tw, th := in.TileW, in.TileH

	type area struct {
		startCol, startRow int   // global-grid offset = Origin / tileSize
		posX, posY         int   // Pos anchor (Pos-Y measured from the AOI bottom)
		cols, rows         int   // local grid = NumCols × NumRows
		x, y               []int // in-axis per-gap offsets (x[0]=y[0]=0)
		xRow, yCol         []int // cross-axis per-row/per-column baselines (#68), min 0
		h                  int   // local pixel height (includes cross-axis Y span)
	}
	var areas []*area
	for i := range ei.ImageInfos {
		ii := &ei.ImageInfos[i]
		if !ii.AOIScanned || ii.NumCols < 1 || ii.NumRows < 1 || i >= len(ei.AoiOrigins) {
			continue
		}
		o := ei.AoiOrigins[i]
		a := &area{
			startCol: o.OriginX / tw, startRow: o.OriginY / th,
			posX: ii.PosX, posY: ii.PosY, cols: ii.NumCols, rows: ii.NumRows,
		}
		a.x, a.y, a.xRow, a.yCol = localOffsets(ii, tw, th)
		// Bottom extent: the last row's in-axis Y plus the largest per-column
		// drift (yCol is normalized so its min is 0).
		a.h = a.y[a.rows-1] + maxInt(a.yCol) + th
		areas = append(areas, a)
	}
	if len(areas) == 0 {
		return nil
	}

	// Y-flip (openslide): Pos-Y is the distance from the AOI bottom to a point
	// below all AOIs, so the per-AOI top in image space is top - Pos-Y - height.
	top := 0
	for _, a := range areas {
		if a.posY+a.h > top {
			top = a.posY + a.h
		}
	}

	l := newLayout(in.Cols, in.Rows, tw, th)
	minX, minY := 1<<62, 1<<62
	maxX, maxY := 0, 0
	for _, a := range areas {
		ayTop := top - a.posY - a.h
		for r := 0; r < a.rows; r++ {
			for c := 0; c < a.cols; c++ {
				gc, gr := a.startCol+c, a.startRow+r
				if gc < 0 || gc >= in.Cols || gr < 0 || gr >= in.Rows {
					continue
				}
				x := a.posX + a.x[c] + a.xRow[r]
				y := ayTop + a.y[r] + a.yCol[c]
				l.origin[[2]int{gc, gr}] = TilePlacement{Col: gc, Row: gr, X: x, Y: y}
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x+tw > maxX {
					maxX = x + tw
				}
				if y+th > maxY {
					maxY = y + th
				}
			}
		}
	}
	// Normalize the hull's top-left to (0,0). Pos-X and the Y-flip already land
	// near origin for the slides we have; normalize defensively.
	if minX != 0 || minY != 0 {
		for k, p := range l.origin {
			p.X -= minX
			p.Y -= minY
			l.origin[k] = p
		}
		maxX -= minX
		maxY -= minY
	}
	l.Width, l.Height = maxX, maxY
	return l
}

// localOffsets computes the per-axis cumulative pixel offsets for one AOI's
// local grid (NumCols×NumRows) from its TileJointInfo graph (#63 in-axis, #68
// cross-axis). It returns four arrays; tile (c,r) is placed at
// (X[c] + xRow[r], Y[r] + yCol[c]):
//
//   - X[c]    in-axis column advance: Σ (tileW − avg horizontal-join OverlapX).
//   - Y[r]    in-axis row advance:    Σ (tileH − avg vertical-join OverlapY).
//   - yCol[c] CROSS-axis per-column Y baseline: Σ (− avg horizontal-join
//     OverlapY). Horizontally-adjacent camera frames are captured at a small
//     vertical offset that the scanner records as the join's OverlapY; ignoring
//     it (the pre-v0.59 separable model placed every tile in a row at the same
//     Y) accumulates into a visible per-column vertical shear (#68).
//   - xRow[r] CROSS-axis per-row X baseline: Σ (− avg vertical-join OverlapX).
//
// This is the full 2-D integration of the join displacement vectors —
// horizontal join → (tw−OverlapX, −OverlapY), vertical join → (−OverlapX,
// th−OverlapY) — under the (measured-good) assumption that overlaps depend only
// on a gap's axis position, so the field separates into per-column and per-row
// baselines. Both cross-axis sign conventions are confirmed against OS-2 pixel
// cross-correlation (per-column Y drift ≈ −2 px/col, per-row X drift ≈ +2
// px/row). Empty gaps take the AOI global-mean for that axis/component. The
// cross arrays are normalized so their minimum is 0 (the AOI top-left stays
// anchored at its Pos origin). A single-AOI slide (OS-1) reduces to the same
// whole-grid model; the cross terms are new there too (they correct OS-1's
// per-column drift, so it is no longer byte-identical to the pre-#68 layout).
func localOffsets(ii *bifxml.ImageInfo, tw, th int) (X, Y, xRow, yCol []int) {
	cols, rows := ii.NumCols, ii.NumRows
	resolve := func(idx int) (c, r int, ok bool) {
		c, r = serpentineToImage(idx-1, cols, rows) // 1-based serpentine over the LOCAL grid
		if c < 0 {
			return 0, 0, false
		}
		return c, r, true
	}
	colSumX := make([]float64, cols) // horizontal joins, in-axis OverlapX, per col-gap
	colSumY := make([]float64, cols) // horizontal joins, CROSS OverlapY, per col-gap
	colN := make([]int, cols)
	rowSumY := make([]float64, rows) // vertical joins, in-axis OverlapY, per row-gap
	rowSumX := make([]float64, rows) // vertical joins, CROSS OverlapX, per row-gap
	rowN := make([]int, rows)
	var gXs, gYs, gCYs, gCXs float64
	var gXn, gYn int
	for _, j := range ii.Joints {
		if !j.FlagJoined || j.Confidence < legacyConfidenceCutoff {
			continue
		}
		ac, ar, aok := resolve(j.Tile1)
		bc, br, bok := resolve(j.Tile2)
		if !aok || !bok {
			continue
		}
		if ar == br && absDelta(ac, bc) == 1 {
			g := min(ac, bc)
			colSumX[g] += float64(j.OverlapX)
			colSumY[g] += float64(j.OverlapY)
			colN[g]++
			gXs += float64(j.OverlapX)
			gCYs += float64(j.OverlapY)
			gXn++
		}
		if ac == bc && absDelta(ar, br) == 1 {
			g := min(ar, br)
			rowSumY[g] += float64(j.OverlapY)
			rowSumX[g] += float64(j.OverlapX)
			rowN[g]++
			gYs += float64(j.OverlapY)
			gCXs += float64(j.OverlapX)
			gYn++
		}
	}
	mean := func(s float64, n int) float64 {
		if n > 0 {
			return s / float64(n)
		}
		return 0
	}
	gX, gCY := mean(gXs, gXn), mean(gCYs, gXn) // col-gap averages share the horizontal-join count
	gY, gCX := mean(gYs, gYn), mean(gCXs, gYn) // row-gap averages share the vertical-join count

	X = make([]int, cols)
	yCol = make([]int, cols)
	var accX, accCY float64
	for col := 1; col < cols; col++ {
		ovX, ovCY := gX, gCY
		if colN[col-1] > 0 {
			ovX = colSumX[col-1] / float64(colN[col-1])
			ovCY = colSumY[col-1] / float64(colN[col-1])
		}
		accX += float64(tw) - ovX
		accCY += -ovCY
		X[col] = int(accX + 0.5)
		yCol[col] = int(math.Round(accCY)) // accCY is negative pre-normalization
	}
	Y = make([]int, rows)
	xRow = make([]int, rows)
	var accY, accCX float64
	for row := 1; row < rows; row++ {
		ovY, ovCX := gY, gCX
		if rowN[row-1] > 0 {
			ovY = rowSumY[row-1] / float64(rowN[row-1])
			ovCX = rowSumX[row-1] / float64(rowN[row-1])
		}
		accY += float64(th) - ovY
		accCX += -ovCX
		Y[row] = int(accY + 0.5)
		xRow[row] = int(math.Round(accCX)) // accCX is negative pre-normalization
	}
	shiftToZero(yCol)
	shiftToZero(xRow)
	return X, Y, xRow, yCol
}

// shiftToZero subtracts the minimum element so the slice's min becomes 0,
// keeping an AOI's cross-axis baselines anchored to its Pos origin.
func shiftToZero(a []int) {
	if len(a) == 0 {
		return
	}
	mn := a[0]
	for _, v := range a {
		if v < mn {
			mn = v
		}
	}
	if mn != 0 {
		for i := range a {
			a[i] -= mn
		}
	}
}

// hasLiveJoint reports whether any joint is FlagJoined with confidence at or
// above the legacy cutoff (so buildLegacyLayout has overlap data to use).
func hasLiveJoint(ei *bifxml.EncodeInfo) bool {
	for _, ii := range ei.ImageInfos {
		for _, j := range ii.Joints {
			if j.FlagJoined && j.Confidence >= legacyConfidenceCutoff {
				return true
			}
		}
	}
	return false
}

// absDelta returns |a-b|.
func absDelta(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// maxInt returns the largest element of a (0 for an empty slice).
func maxInt(a []int) int {
	mx := 0
	for _, v := range a {
		if v > mx {
			mx = v
		}
	}
	return mx
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

// finalizeExtent computes the stitched extent as the convex hull (bounding box)
// of all AOI tile placements (whitepaper page 16 + Figure 5: "The BIF-image
// approximates the convex hull of all AOIs"), normalized so the hull's top-left
// corner is (0,0).
//
// No tile-multiple rounding. The Roche whitepaper (page 5/16) pads the
// underlying raw-frame TIFF up to a tile multiple (that is the IFD ImageWidth ×
// ImageLength — for Ventana-1, the padded 24×21 grid = 24576×21504, including
// the phantom 24th column), but the STITCHED CONTENT extent that consumers want
// is the compacted hull itself. Black-box bio-formats (showinf, dimension
// oracle only — never a source reference) reports Ventana-1 L0 as exactly
// 23432×21504 (the compacted hull), and its lower pyramid levels are exact /2
// downsamples of those numbers — so the un-rounded hull, not 23552, is the
// ground truth. Rounding here would re-introduce a (different) padding artifact;
// the #60 bug being fixed is precisely that opentile-go reported a padded
// extent. White padding to the IFD grid is a pixel-fill concern of the
// compositing layer, not the reported stitched dimensions.
//
// For VENTANA DP 200 spec-compliant slides OverlapY is always 0 (page 15: "do
// not contain vertical tile overlap"), so every Y coordinate is already a clean
// tile multiple and the height is exactly rows × tileH.
func finalizeExtent(l *Layout, in StitchInput) {
	_ = in
	if len(l.origin) == 0 {
		l.Width, l.Height = 0, 0
		return
	}
	minX, minY := int(^uint(0)>>1), int(^uint(0)>>1)
	for _, p := range l.origin {
		if p.X < minX {
			minX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
	}
	maxX, maxY := 0, 0
	for k, p := range l.origin {
		p.X -= minX
		p.Y -= minY
		l.origin[k] = p
		if p.X+l.tileW > maxX {
			maxX = p.X + l.tileW
		}
		if p.Y+l.tileH > maxY {
			maxY = p.Y + l.tileH
		}
	}
	l.Width = maxX
	l.Height = maxY
}
