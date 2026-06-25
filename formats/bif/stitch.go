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
	if lg := buildLegacyLayout(in); lg != nil {
		return lg
	}
	return buildNaiveLayout(in)
}

// downsampleLayout derives a reduced-pyramid-level layout from the level-0
// compacted layout (#83 / #80). Reduced tile (col,row) inherits L0 frame
// (col<<shift, row<<shift)'s compacted stitched origin, scaled by 1/2^shift
// (the reduced tile spatially covers that 2^shift × 2^shift block of L0 frames,
// so it lands at the block's compacted top-left). Reduced tiles with no backing
// L0 frame (the #60 phantom column / sparse cells) fall back to their naive grid
// position; they are blank and clipped by the level's Size.
//
// The result removes the residual frame overlap from reduced levels (which the
// scanner stored un-compacted, as the raw frame grid downsampled), so a region /
// StitchedTile read composites them stitch-aligned with L0 — DP carries a small
// ~overlap/2^i residual (#83), legacy a dense one (#80).
func downsampleLayout(l0 *Layout, shift uint, cols, rows, tw, th int) *Layout {
	l := newLayout(cols, rows, tw, th)
	maxX, maxY := 0, 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			px, py := col*tw, row*th
			if x, y, ok := l0.TileOrigin(col<<shift, row<<shift); ok {
				px, py = x>>shift, y>>shift
			}
			l.origin[[2]int{col, row}] = TilePlacement{Col: col, Row: row, X: px, Y: py}
			if px+tw > maxX {
				maxX = px + tw
			}
			if py+th > maxY {
				maxY = py + th
			}
		}
	}
	l.Width, l.Height = maxX, maxY
	return l
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
// overlap graph. The graph is too fragmented to traverse per-tile (Phase 0:
// ~5% reachable from a root), so we use a SEPARABLE per-axis model derived from
// the aggregate per-gap overlap statistics (this is #63's recommended
// "accumulate per-gap", NOT a single global average): tile (col,row) lands at
// (X[col], Y[row]) where X[]/Y[] accumulate (tile - perGapAvgOverlap) across
// gaps, in float, with empty gaps taking the global mean overlap. Clean-room —
// derived from the file's own joints; bio-formats/openslide are test oracles
// only. Declines (nil → naive) unless this is a legacy slide with live joints.
func buildLegacyLayout(in StitchInput) *Layout {
	ei := in.EncodeInfo
	if in.Generation != GenerationLegacyIScan || ei == nil || len(ei.ImageInfos) == 0 {
		return nil
	}
	if !hasLiveJoint(ei) {
		return nil
	}
	cols, rows, tw, th := in.Cols, in.Rows, in.TileW, in.TileH
	resolve := func(idx int) (c, r int, ok bool) {
		c, r = serpentineToImage(idx-1, cols, rows) // legacy: 1-based serpentine
		if c < 0 {
			return 0, 0, false
		}
		return c, r, true
	}
	colSum := make([]float64, cols)
	colN := make([]int, cols)
	rowSum := make([]float64, rows)
	rowN := make([]int, rows)
	var gXs, gYs float64
	var gXn, gYn int
	for _, ii := range ei.ImageInfos {
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
				colSum[g] += float64(j.OverlapX)
				colN[g]++
				gXs += float64(j.OverlapX)
				gXn++
			}
			if ac == bc && absDelta(ar, br) == 1 {
				g := min(ar, br)
				rowSum[g] += float64(j.OverlapY)
				rowN[g]++
				gYs += float64(j.OverlapY)
				gYn++
			}
		}
	}
	gX := 0.0
	if gXn > 0 {
		gX = gXs / float64(gXn)
	}
	gY := 0.0
	if gYn > 0 {
		gY = gYs / float64(gYn)
	}
	X := make([]int, cols)
	acc := 0.0
	for col := 1; col < cols; col++ {
		ov := gX
		if colN[col-1] > 0 {
			ov = colSum[col-1] / float64(colN[col-1])
		}
		acc += float64(tw) - ov
		X[col] = int(acc + 0.5)
	}
	Y := make([]int, rows)
	acc = 0.0
	for row := 1; row < rows; row++ {
		ov := gY
		if rowN[row-1] > 0 {
			ov = rowSum[row-1] / float64(rowN[row-1])
		}
		acc += float64(th) - ov
		Y[row] = int(acc + 0.5)
	}
	l := newLayout(cols, rows, tw, th)
	maxX, maxY := 0, 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			l.origin[[2]int{col, row}] = TilePlacement{Col: col, Row: row, X: X[col], Y: Y[row]}
		}
	}
	for col := 0; col < cols; col++ {
		if X[col]+tw > maxX {
			maxX = X[col] + tw
		}
	}
	for row := 0; row < rows; row++ {
		if Y[row]+th > maxY {
			maxY = Y[row] + th
		}
	}
	l.Width = maxX
	l.Height = maxY
	return l
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
