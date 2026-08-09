//go:build ignore

// Emits the tight RGB888 bytes of level-0 region (x,y,w,h) of a slide to stdout,
// as the parity oracle for the Python read_region path.
package main

import (
	"os"
	"strconv"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {
	path := os.Args[1]
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	x, y, w, h := atoi(os.Args[2]), atoi(os.Args[3]), atoi(os.Args[4]), atoi(os.Args[5])
	s, err := opentile.OpenFile(path)
	if err != nil {
		panic(err)
	}
	defer s.Close()
	lv, err := s.Level(0)
	if err != nil {
		panic(err)
	}
	img, err := lv.ReadRegion(opentile.Region{Origin: opentile.Point{X: x, Y: y}, Size: opentile.Size{W: w, H: h}})
	if err != nil {
		panic(err)
	}
	bands := 3
	if img.Format == decoder.PixelFormatRGBA {
		bands = 4
	}
	row := img.Width * bands
	for r := 0; r < img.Height; r++ {
		os.Stdout.Write(img.Pix[r*img.Stride : r*img.Stride+row])
	}
}
