package bif_test

import (
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestReducedContentMatchesDownsampledL0 is the headline whole-region gate for
// BIF reduced-level stitching (#83 DP + #80 legacy): a reduced level's composited
// content must equal the stitched L0 downsampled by 2ⁱ. This catches the global
// misregistration that the per-seam tests missed — the openslide subtile model
// (each L0 frame placed at its compacted position) makes it hold. Run locally;
// Ventana-1 is in CI, OS-1 is PHI/local-only.
func TestReducedContentMatchesDownsampledL0(t *testing.T) {
	for _, fx := range []struct {
		name  string
		level int // reduced level to check vs L0
		bound float64
	}{
		{"Ventana-1.bif", 1, 16}, // DP — pixel-exact-ish (sparse overlap)
		{"Ventana-1.bif", 2, 16},
		{"OS-1.bif", 1, 22}, // legacy — looser (per-gap-avg L0 model + downsample filter)
		{"OS-1.bif", 2, 22},
	} {
		t.Run(fx.name+"-L"+string(rune('0'+fx.level)), func(t *testing.T) {
			path := "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/" + fx.name
			if dir := os.Getenv("OPENTILE_TESTDIR"); dir != "" {
				path = filepath.Join(dir, "bif", fx.name)
			}
			if _, err := os.Stat(path); err != nil {
				t.Skip(fx.name + " absent")
			}
			s, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			lr, err := s.Level(fx.level)
			if err != nil {
				t.Fatal(err)
			}
			l0, err := s.Level(0)
			if err != nil {
				t.Fatal(err)
			}
			if !lr.Overlapping {
				t.Fatalf("L%d Overlapping=false — reduced-level stitching not engaged", fx.level)
			}
			ds := 1 << uint(fx.level)
			const w, h = 1400, 1400
			// pick a textured interior region of the reduced level
			ox, oy := pickTextured(t, lr, w, h)
			rImg, err := lr.ReadRegion(opentile.Region{Origin: opentile.Point{X: ox, Y: oy}, Size: opentile.Size{W: w, H: h}})
			if err != nil {
				t.Fatal(err)
			}
			l0Img, err := l0.ReadRegion(opentile.Region{Origin: opentile.Point{X: ox * ds, Y: oy * ds}, Size: opentile.Size{W: w * ds, H: h * ds}})
			if err != nil {
				t.Fatal(err)
			}
			down := l0Img
			for k := 0; k < fx.level; k++ {
				down = box2(down)
			}
			mad := regionMAD(rImg, down)
			t.Logf("%s L%d vs L0÷%d: MAD=%.2f (bound %.0f)", fx.name, fx.level, ds, mad, fx.bound)
			if mad > fx.bound {
				t.Errorf("%s L%d content MAD vs downsampled L0 = %.2f, want ≤ %.0f (reduced level not stitch-aligned with L0)", fx.name, fx.level, mad, fx.bound)
			}
		})
	}
}

// TestReducedDeepLevelRegistration is the cross-level registration gate at a
// FRACTIONAL reduced level (L5: legacy TileH=1360, 1360/32=42.5 non-integer).
// The pre-fix subtile crop floored the per-subtile height (42), drifting the
// stored-tile crop origin up to ~15 px vertically at the bottom of each stored
// tile — a deep-zoom "drift" that L1–L4 (exact: 1360=16·85) never exposed.
// For several anchors it downsamples an L0 patch by 2⁵ and cross-correlates it
// against the L5 patch at the mapped position; the residual must be ≤ a few px.
// Local-only (PHI fixtures) → skips in CI.
func TestReducedDeepLevelRegistration(t *testing.T) {
	const level = 5
	const f = 1 << level
	const S, pad = 140, 22
	for _, name := range []string{"OS-1.bif", "OS-2.bif"} {
		t.Run(name, func(t *testing.T) {
			path := "/Volumes/Ext/GitHub/opentile-go/sample_files/bif/" + name
			if dir := os.Getenv("OPENTILE_TESTDIR"); dir != "" {
				path = filepath.Join(dir, "bif", name)
			}
			if _, err := os.Stat(path); err != nil {
				t.Skip(name + " absent")
			}
			s, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			l0, _ := s.Level(0)
			li, err := s.Level(level)
			if err != nil {
				t.Fatal(err)
			}
			checked := 0
			for _, fr := range [][2]float64{{0.50, 0.45}, {0.70, 0.42}, {0.46, 0.55}, {0.60, 0.60}} {
				ax := int(float64(l0.Size.W) * fr[0])
				ay := int(float64(l0.Size.H) * fr[1])
				a0, err := l0.ReadRegion(opentile.Region{Origin: opentile.Point{X: ax, Y: ay}, Size: opentile.Size{W: S * f, H: S * f}})
				if err != nil {
					continue
				}
				a := a0
				for k := 0; k < level; k++ {
					a = box2(a)
				}
				if regionVar(a) < 40 {
					continue // blank/low-texture anchor
				}
				bx, by := int(float64(ax)/li.Downsample)-pad, int(float64(ay)/li.Downsample)-pad
				b, err := li.ReadRegion(opentile.Region{Origin: opentile.Point{X: bx, Y: by}, Size: opentile.Size{W: S + 2*pad, H: S + 2*pad}})
				if err != nil {
					continue
				}
				dx, dy := alignOffset(a, b, pad)
				t.Logf("%s L%d anchor(%.2f,%.2f): residual=(%+d,%+d)", name, level, fr[0], fr[1], dx, dy)
				if dx < -3 || dx > 3 || dy < -3 || dy > 3 {
					t.Errorf("%s L%d residual (%+d,%+d) exceeds ±3 px (deep-level subtile crop drift)", name, level, dx, dy)
				}
				checked++
			}
			if checked == 0 {
				t.Skip("no textured anchors")
			}
		})
	}
}

// alignOffset finds the integer (dx,dy) in [-rad,rad]² that best aligns the
// smaller patch a centred inside b (min mean-abs-diff over RGB).
func alignOffset(a, b *decoder.Image, rad int) (int, int) {
	ba, bb := 3, 3
	if a.Format == decoder.PixelFormatRGBA {
		ba = 4
	}
	if b.Format == decoder.PixelFormatRGBA {
		bb = 4
	}
	cx, cy := (b.Width-a.Width)/2, (b.Height-a.Height)/2
	best := 1 << 60
	bdx, bdy := 0, 0
	for dy := -rad; dy <= rad; dy++ {
		for dx := -rad; dx <= rad; dx++ {
			sum, n := 0, 0
			for y := 0; y < a.Height; y += 2 {
				by := cy + dy + y
				if by < 0 || by >= b.Height {
					continue
				}
				for x := 0; x < a.Width; x += 2 {
					bx := cx + dx + x
					if bx < 0 || bx >= b.Width {
						continue
					}
					for c := 0; c < 3; c++ {
						d := int(a.Pix[y*a.Stride+x*ba+c]) - int(b.Pix[by*b.Stride+bx*bb+c])
						if d < 0 {
							d = -d
						}
						sum += d
						n++
					}
				}
			}
			if n > 0 && sum/n < best {
				best, bdx, bdy = sum/n, dx, dy
			}
		}
	}
	return bdx, bdy
}

func pickTextured(t *testing.T, l *opentile.Level, w, h int) (int, int) {
	t.Helper()
	best := 0.0
	bx, by := l.Size.W/2-w/2, l.Size.H/2-h/2
	for fy := 0.3; fy < 0.7; fy += 0.1 {
		for fx := 0.3; fx < 0.7; fx += 0.1 {
			x, y := int(float64(l.Size.W)*fx), int(float64(l.Size.H)*fy)
			im, err := l.ReadRegion(opentile.Region{Origin: opentile.Point{X: x, Y: y}, Size: opentile.Size{W: 256, H: 256}})
			if err != nil {
				continue
			}
			if v := regionVar(im); v > best {
				best, bx, by = v, x, y
			}
		}
	}
	return bx, by
}

func box2(im *decoder.Image) *decoder.Image {
	bpp := 3
	if im.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	ow, oh := im.Width/2, im.Height/2
	out := decoder.NewImageFormat(ow, oh, im.Format)
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			for c := 0; c < bpp; c++ {
				s := int(im.Pix[(2*y)*im.Stride+(2*x)*bpp+c]) + int(im.Pix[(2*y)*im.Stride+(2*x+1)*bpp+c]) +
					int(im.Pix[(2*y+1)*im.Stride+(2*x)*bpp+c]) + int(im.Pix[(2*y+1)*im.Stride+(2*x+1)*bpp+c])
				out.Pix[y*out.Stride+x*bpp+c] = byte(s / 4)
			}
		}
	}
	return out
}

func regionMAD(a, b *decoder.Image) float64 {
	bpp := 3
	if a.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	w, h := a.Width, a.Height
	if b.Width < w {
		w = b.Width
	}
	if b.Height < h {
		h = b.Height
	}
	var sum, n float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				d := int(a.Pix[y*a.Stride+x*bpp+c]) - int(b.Pix[y*b.Stride+x*bpp+c])
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
			}
		}
	}
	return sum / n
}

func regionVar(im *decoder.Image) float64 {
	bpp := 3
	if im.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	var s, sq, n float64
	for y := 0; y < im.Height; y++ {
		for x := 0; x < im.Width; x++ {
			v := float64(im.Pix[y*im.Stride+x*bpp])
			s += v
			sq += v * v
			n++
		}
	}
	return sq/n - (s/n)*(s/n)
}
