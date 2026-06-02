package dicom

import idicom "github.com/wsilabs/opentile-go/internal/dicom"

type tileKey struct{ tx, ty int }

// buildTileMap returns a (tx,ty) -> frame-index map. For TILED_FULL the
// order is implicit raster (ty*tilesAcross + tx). For TILED_SPARSE the
// 1-based pixel positions are converted to tile indices. Absent cells are
// simply missing from the map (callers blank-fill).
func buildTileMap(dimOrg string, tilesAcross, tilesDown, tileW, tileH int, pos []idicom.FramePos, numFrames int) map[tileKey]int {
	m := make(map[tileKey]int, numFrames)
	if dimOrg == "TILED_SPARSE" {
		for i, p := range pos {
			if p.Col == 0 && p.Row == 0 {
				continue // unpositioned frame; skip
			}
			tx := (p.Col - 1) / tileW
			ty := (p.Row - 1) / tileH
			m[tileKey{tx, ty}] = i
		}
		return m
	}
	// TILED_FULL: raster fill up to numFrames.
	idx := 0
	for ty := 0; ty < tilesDown && idx < numFrames; ty++ {
		for tx := 0; tx < tilesAcross && idx < numFrames; tx++ {
			m[tileKey{tx, ty}] = idx
			idx++
		}
	}
	return m
}
