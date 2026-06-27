package szi_test

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	idzi "github.com/wsilabs/opentile-go/internal/dzi"
)

func itoa(n int) string { return strconv.Itoa(n) }

func gradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255})
		}
	}
	return img
}

func maxAbsDiff(img *decoder.Image, src *image.NRGBA) int {
	bpp := 3
	if img.Format == decoder.PixelFormatRGBA {
		bpp = 4
	}
	max := 0
	for y := 0; y < src.Bounds().Dy() && y < img.Height; y++ {
		for x := 0; x < src.Bounds().Dx() && x < img.Width; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			want := []int{int(r >> 8), int(g >> 8), int(b >> 8)}
			for c := 0; c < 3; c++ {
				d := int(img.Pix[y*img.Stride+x*bpp+c]) - want[c]
				if d < 0 {
					d = -d
				}
				if d > max {
					max = d
				}
			}
		}
	}
	return max
}

// writeOverlapSZI builds a single-level overlap=ov SZI (a ZIP wrapping a PNG DZI)
// under dir with root name "img", returning the .szi path.
// Tiles are STOREd (zip.Store) per the SZI spec requirement for mmap-aliased reads.
// Only the deepest (full-res) level's tiles are written.
// A minimal scan-properties.xml is included because the SZI reader requires it.
func writeOverlapSZI(t *testing.T, dir string, src *image.NRGBA, tileSize, ov int) string {
	t.Helper()
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	maxLevel := idzi.MaxLevel(w, h)
	cols, rows := idzi.GridDims(w, h, tileSize)
	const root = "img"

	sziPath := filepath.Join(dir, "img.szi")
	f, err := os.Create(sziPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	// addStored writes a ZIP entry with method zip.Store (uncompressed).
	addStored := func(name string, data []byte) {
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}

	// Manifest: <root>/<root>.dzi
	manifest := `<?xml version="1.0"?>` +
		`<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="png" ` +
		`Overlap="` + itoa(ov) + `" TileSize="` + itoa(tileSize) + `">` +
		`<Size Width="` + itoa(w) + `" Height="` + itoa(h) + `"/></Image>`
	addStored(root+"/"+root+".dzi", []byte(manifest))

	// scan-properties.xml is REQUIRED by the SZI reader (loadScanProperties
	// returns an error if missing). A minimal well-formed empty <image> element
	// satisfies the lenient parser — all fields are optional.
	addStored(root+"/scan-properties.xml", []byte(`<image version="1" date="2024-01-01"></image>`))

	// Tiles: <root>/<root>_files/<level>/<col>_<row>.png
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			ox, oy, cw, ch := idzi.ContentRect(c, r, w, h, tileSize, ov)
			// Right/bottom overlap overlap is included when not on the last col/row.
			rx := 0
			if c < cols-1 {
				rx = ov
			}
			by := 0
			if r < rows-1 {
				by = ov
			}
			sx, sy := c*tileSize-ox, r*tileSize-oy
			tw, th := ox+cw+rx, oy+ch+by
			tile := image.NewNRGBA(image.Rect(0, 0, tw, th))
			for ty := 0; ty < th; ty++ {
				for tx := 0; tx < tw; tx++ {
					tile.Set(tx, ty, src.At(sx+tx, sy+ty))
				}
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, tile); err != nil {
				t.Fatal(err)
			}
			addStored(root+"/"+root+"_files/"+itoa(maxLevel)+"/"+itoa(c)+"_"+itoa(r)+".png", buf.Bytes())
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return sziPath
}

func TestSZIOverlapCompositeMatchesSource(t *testing.T) {
	dir := t.TempDir()
	src := gradient(70, 50) // 70x50 with T=16 => grid 5x4, interior+edge tiles
	sziPath := writeOverlapSZI(t, dir, src, 16, 1)

	s, err := opentile.OpenFile(sziPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	l0, _ := s.Level(0) // deepest level == full res == src dims
	if l0.Size.W != 70 || l0.Size.H != 50 || l0.OverlapMode != opentile.OverlapBordered {
		t.Fatalf("level0 = %+v (mode %v), want 70x50 bordered", l0.Size, l0.OverlapMode)
	}

	// ReadRegion composites the full 70x50 canvas: overlap pixels must be
	// cropped so the result exactly matches the source gradient.
	got, err := l0.ReadRegion(opentile.Region{Origin: opentile.Point{}, Size: opentile.Size{W: 70, H: 50}})
	if err != nil {
		t.Fatal(err)
	}
	// Lossless PNG round-trip → exact pixel match (maxAbsDiff == 0).
	if d := maxAbsDiff(got, src); d != 0 {
		t.Errorf("composite != source: maxAbsDiff=%d (overlap not cropped correctly)", d)
	}

	// StitchedTile of an interior cell returns a clean 16x16 content tile.
	st, err := l0.StitchedTile(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if st.Width != 16 || st.Height != 16 {
		t.Errorf("StitchedTile(1,1) = %dx%d, want 16x16", st.Width, st.Height)
	}

	// DecodedTile returns the PADDED stored tile (interior is 18x18 for ov=1).
	dt, err := l0.DecodedTile(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if dt.Width != 18 || dt.Height != 18 {
		t.Errorf("DecodedTile(1,1) = %dx%d, want 18x18 (padded)", dt.Width, dt.Height)
	}
}
