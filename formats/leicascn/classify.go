package leicascn

import (
	"errors"
	"fmt"
	"math"
)

// IsAuxiliary reports whether img's view covers the entire collection
// (offset 0,0 + dims match collection's). Sealed Q2 (v0.11 spec):
// matches openslide's is_macro check in
// src/openslide-vendor-leica.c:469. Magnification is metadata only;
// the role decision is geometric.
//
// True → auxiliary scan (low-mag whole-slide; surfaces as
// AssociatedImage). False → main scan (sub-region, high-mag;
// contributes to the composite Image's Levels).
func IsAuxiliary(img Image, c *Collection) bool {
	return img.ViewOffsetXNm == 0 &&
		img.ViewOffsetYNm == 0 &&
		img.ViewSizeXNm == c.SizeXNm &&
		img.ViewSizeYNm == c.SizeYNm
}

// CompositeLevel is one level in the multi-region composite pyramid.
// Each Region carries a per-main-scan slot (offset within the union
// pixel space, IFD per channel). Tile dispatch at this level is
// region-local; tiles outside any Region return blank fill.
type CompositeLevel struct {
	Index       int // 0 = baseline (highest resolution)
	PixelSizeX  int // union pixel extent at this level
	PixelSizeY  int
	NMPerPixelX float64
	NMPerPixelY float64
	SizeC       int // 1 for brightfield, >1 for fluorescence
	Regions     []RegionLevel
}

// RegionLevel is one main scan's level slot, positioned within the
// composite level's pixel coordinate space.
type RegionLevel struct {
	OffsetX       int // pixel offset within the composite level
	OffsetY       int
	PixelSizeX    int // this region's pixel extent at this level
	PixelSizeY    int
	IFDPerChannel []int // length = SizeC; one IFD index per channel
}

// ErrUnsupportedSCN is returned by ComposePyramid when the main-scan
// list violates the v0.11 composition invariants (sealed Q5):
// matching pyramid depth, illumination source, objective
// magnification, and ±2% per-level resolution similarity. Wrapped
// errors carry the specific violation message.
var ErrUnsupportedSCN = errors.New("leicascn: unsupported SCN layout")

// ComposePyramid validates and composes the main-scan list into a
// single multi-region pyramid. Mirrors openslide's compositing logic
// in src/openslide-vendor-leica.c:560+. Sealed Q5 invariants:
//
//   - All mains share pyramid depth (number of levels).
//   - All mains share illumination source.
//   - All mains share objective magnification.
//   - Per-level resolution similarity ≤ 2% across mains
//     (resolution = ViewSizeXNm / PixelsSizeX, etc.).
//
// Returns ErrUnsupportedSCN with a descriptive wrapped message on
// any violation. Returns an empty slice with no error if mains is
// empty (caller decides whether that's an error in context).
func ComposePyramid(mains []Image, c *Collection) ([]CompositeLevel, error) {
	if len(mains) == 0 {
		return nil, nil
	}

	// Sealed-invariant checks. Iterate mains[1:] against mains[0].
	first := mains[0]
	depth := pyramidDepth(first)
	for i, m := range mains[1:] {
		if got := pyramidDepth(m); got != depth {
			return nil, fmt.Errorf("%w: main %d depth %d != main 0 depth %d",
				ErrUnsupportedSCN, i+1, got, depth)
		}
		if m.IlluminationSource != first.IlluminationSource {
			return nil, fmt.Errorf("%w: main %d illumination %q != main 0 %q",
				ErrUnsupportedSCN, i+1, m.IlluminationSource, first.IlluminationSource)
		}
		if m.Objective != first.Objective {
			return nil, fmt.Errorf("%w: main %d objective %g != main 0 %g",
				ErrUnsupportedSCN, i+1, m.Objective, first.Objective)
		}
	}

	// Determine SizeC from first main's dimensions: max(c) + 1, or 1
	// if no <dimension c="..."> entries are present (single-channel).
	sizeC := 1
	for _, d := range first.Dimensions {
		if d.C+1 > sizeC {
			sizeC = d.C + 1
		}
	}

	// Compute the L0 nm-per-pixel for the composite. Per
	// openslide-vendor-leica.c:626, when compositing multiple mains
	// the level's nm-per-pixel is MIN(per-main nm-per-pixel) — i.e.,
	// max pixel density. For our fixtures all mains are identical so
	// MIN == any. For correctness on hypothetical drift fixtures we
	// take the MIN.
	level0NmX := math.Inf(1)
	level0NmY := math.Inf(1)
	for _, m := range mains {
		nmX := nmPerPixelX(m)
		nmY := nmPerPixelY(m)
		if nmX < level0NmX {
			level0NmX = nmX
		}
		if nmY < level0NmY {
			level0NmY = nmY
		}
	}

	// Compute the composite L0 union extent. Bounding box across all
	// mains' (offset, view-size) rectangles, in nm; convert to L0
	// pixels via the level0NmPerPixel.
	var minOffsetX, minOffsetY uint64 = math.MaxUint64, math.MaxUint64
	var maxFarX, maxFarY uint64
	for _, m := range mains {
		if m.ViewOffsetXNm < minOffsetX {
			minOffsetX = m.ViewOffsetXNm
		}
		if m.ViewOffsetYNm < minOffsetY {
			minOffsetY = m.ViewOffsetYNm
		}
		if far := m.ViewOffsetXNm + m.ViewSizeXNm; far > maxFarX {
			maxFarX = far
		}
		if far := m.ViewOffsetYNm + m.ViewSizeYNm; far > maxFarY {
			maxFarY = far
		}
	}
	unionWidthNm := maxFarX - minOffsetX
	unionHeightNm := maxFarY - minOffsetY

	// Build per-level CompositeLevel entries.
	out := make([]CompositeLevel, depth)
	for level := 0; level < depth; level++ {
		// Per-level resolution similarity check (±2%): each main's
		// nm-per-pixel at this level must be within 2% of the
		// composite's nm-per-pixel (which is also at this level the
		// MIN across mains).
		levelNmX, levelNmY, err := levelNmPerPixel(mains, level, c)
		if err != nil {
			return nil, err
		}

		cl := CompositeLevel{
			Index:       level,
			NMPerPixelX: levelNmX,
			NMPerPixelY: levelNmY,
			SizeC:       sizeC,
			PixelSizeX:  int(math.Round(float64(unionWidthNm) / levelNmX)),
			PixelSizeY:  int(math.Round(float64(unionHeightNm) / levelNmY)),
		}

		for _, m := range mains {
			d := dimensionByR(m, level, 0) // r=level, c=0 representative
			rl := RegionLevel{
				OffsetX:    int(math.Round(float64(m.ViewOffsetXNm-minOffsetX) / levelNmX)),
				OffsetY:    int(math.Round(float64(m.ViewOffsetYNm-minOffsetY) / levelNmY)),
				PixelSizeX: int(d.SizeX),
				PixelSizeY: int(d.SizeY),
			}
			rl.IFDPerChannel = make([]int, sizeC)
			for ch := 0; ch < sizeC; ch++ {
				rl.IFDPerChannel[ch] = dimensionByR(m, level, ch).IFD
			}
			cl.Regions = append(cl.Regions, rl)
		}
		out[level] = cl
	}
	return out, nil
}

// pyramidDepth returns the number of resolution levels in img's
// dimension list (= max R + 1).
func pyramidDepth(img Image) int {
	maxR := -1
	for _, d := range img.Dimensions {
		if d.R > maxR {
			maxR = d.R
		}
	}
	return maxR + 1
}

// dimensionByR finds the dimension entry matching (r, c). Returns
// the zero Dimension if absent.
func dimensionByR(img Image, r, c int) Dimension {
	for _, d := range img.Dimensions {
		if d.R == r && d.C == c {
			return d
		}
	}
	return Dimension{}
}

// nmPerPixelX returns image's L0 X-axis nm-per-pixel. The XML
// invariant is ViewSizeXNm / PixelsSizeX (i.e., the slide-physical
// extent of the scan divided by its pixel width).
func nmPerPixelX(img Image) float64 {
	if img.PixelsSizeX == 0 {
		return 0
	}
	return float64(img.ViewSizeXNm) / float64(img.PixelsSizeX)
}

func nmPerPixelY(img Image) float64 {
	if img.PixelsSizeY == 0 {
		return 0
	}
	return float64(img.ViewSizeYNm) / float64(img.PixelsSizeY)
}

// levelNmPerPixel computes the nm-per-pixel at a given level for the
// composite pyramid. Verifies ±2% similarity across mains (Q5 invariant).
//
// Returns the MIN across mains (max pixel density), per openslide.
func levelNmPerPixel(mains []Image, level int, c *Collection) (float64, float64, error) {
	const tol = 0.02

	var nmsX, nmsY []float64
	for _, m := range mains {
		d := dimensionByR(m, level, 0)
		if d.SizeX == 0 || d.SizeY == 0 {
			return 0, 0, fmt.Errorf("%w: main %q level %d missing dimension entry",
				ErrUnsupportedSCN, m.Name, level)
		}
		nmsX = append(nmsX, float64(m.ViewSizeXNm)/float64(d.SizeX))
		nmsY = append(nmsY, float64(m.ViewSizeYNm)/float64(d.SizeY))
	}
	minX, maxX := minMax(nmsX)
	minY, maxY := minMax(nmsY)
	if minX > 0 && (maxX-minX)/minX > tol {
		return 0, 0, fmt.Errorf("%w: level %d X-resolution drift %g..%g exceeds ±%g%%",
			ErrUnsupportedSCN, level, minX, maxX, tol*100)
	}
	if minY > 0 && (maxY-minY)/minY > tol {
		return 0, 0, fmt.Errorf("%w: level %d Y-resolution drift %g..%g exceeds ±%g%%",
			ErrUnsupportedSCN, level, minY, maxY, tol*100)
	}
	return minX, minY, nil
}

func minMax(xs []float64) (float64, float64) {
	mn, mx := math.Inf(1), math.Inf(-1)
	for _, x := range xs {
		if x < mn {
			mn = x
		}
		if x > mx {
			mx = x
		}
	}
	return mn, mx
}
