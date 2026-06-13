// Package opentile provides utilities to read tiles from whole-slide imaging
// (WSI) TIFF files. See the repository README for a high-level overview.
package opentile

import "fmt"

// Point is a 2D integer position measured in pixels or tile units.
type Point struct {
	X, Y int
}

func (p Point) String() string { return fmt.Sprintf("(%d,%d)", p.X, p.Y) }

// Size is a 2D integer extent measured in pixels or tile units.
type Size struct {
	W, H int
}

func (s Size) Area() int      { return s.W * s.H }
func (s Size) String() string { return fmt.Sprintf("%dx%d", s.W, s.H) }

// SizeMm is a 2D extent measured in millimeters. Used for pixel spacing and
// microns-per-pixel conversion (SizeMm scaled by 1000 equals micrometers).
type SizeMm struct {
	W, H float64
}

func (s SizeMm) IsZero() bool { return s.W == 0 && s.H == 0 }

// MPP is microns-per-pixel, per axis. The zero value means "unknown".
// Most slides report isotropic pixels (X == Y); asymmetric formats (e.g.
// some Philips TIFF fixtures) populate both axes independently.
//
// Added in v1.0.
type MPP struct{ X, Y float64 }

// IsZero reports whether the MPP is unknown (both axes zero).
func (m MPP) IsZero() bool { return m.X == 0 && m.Y == 0 }

// Symmetric returns the single µm/px value when both axes are equal and
// non-zero. Returns 0 when X != Y or when both are zero (unknown).
func (m MPP) Symmetric() float64 {
	if m.X != 0 && m.X == m.Y {
		return m.X
	}
	return 0
}

// TileCoord identifies a tile by its position in the multi-
// dimensional WSI space. X and Y are the existing 2D grid position;
// Z, C, and T select among focal planes (Z), fluorescence /
// spectral channels (C), and time points (T) respectively.
//
// Z, C, T default to zero — a TileCoord literal {X: x, Y: y}
// addresses the same tile that Level.Tile(x, y) returns. Zero is
// the "nominal" / "first" / "T=0" plane in every dimension.
//
// Valid ranges per axis:
//
//	0 <= X < Level.Grid().W
//	0 <= Y < Level.Grid().H
//	0 <= Z < Image.SizeZ()
//	0 <= C < Image.SizeC()
//	0 <= T < Image.SizeT()
//
// Out-of-range values yield *TileError wrapping
// ErrDimensionUnavailable (axis is unsupported on this Image —
// SizeZ/C/T == 1) or ErrTileOutOfBounds (axis exists but the index
// is past its declared size).
//
// Added in v0.7 alongside Level.TileAt and Image.SizeZ/SizeC/SizeT.
type TileCoord struct {
	X, Y int
	Z    int
	C    int
	T    int
}

func (t TileCoord) String() string {
	if t.Z == 0 && t.C == 0 && t.T == 0 {
		return fmt.Sprintf("(%d,%d)", t.X, t.Y)
	}
	return fmt.Sprintf("(%d,%d, Z=%d, C=%d, T=%d)", t.X, t.Y, t.Z, t.C, t.T)
}

// Region is an axis-aligned rectangle in pixel or tile units.
type Region struct {
	Origin Point
	Size   Size
}

// Contains reports whether p lies inside r (inclusive of origin, exclusive of
// the far edge).
func (r Region) Contains(p Point) bool {
	return p.X >= r.Origin.X && p.X < r.Origin.X+r.Size.W &&
		p.Y >= r.Origin.Y && p.Y < r.Origin.Y+r.Size.H
}
