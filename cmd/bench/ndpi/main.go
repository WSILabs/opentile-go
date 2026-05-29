package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {
	path := flag.String("in", "", "slide path")
	cpuProf := flag.String("cpuprofile", "", "write cpu profile to file")
	memProf := flag.String("memprofile", "", "write mem profile to file")
	maxTiles := flag.Int("maxtiles", 0, "stop after N tiles (0 = all)")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "missing -in")
		os.Exit(2)
	}
	slide, err := opentile.OpenFile(*path)
	if err != nil {
		panic(err)
	}
	defer slide.Close()
	l0 := slide.Levels()[0]
	w, h := l0.Size.W, l0.Size.H
	fmt.Printf("source L0: %dx%d\n", w, h)
	const TS = 256
	cols := (w + TS - 1) / TS
	rows := (h + TS - 1) / TS

	if *cpuProf != "" {
		f, err := os.Create(*cpuProf)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		defer pprof.StopCPUProfile()
	}

	start := time.Now()
	var pix int64
	var nTiles int
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			x := c * TS
			y := r * TS
			tw := TS
			if x+tw > w {
				tw = w - x
			}
			th := TS
			if y+th > h {
				th = h - y
			}
			img, err := slide.ReadRegion(0, x, y, tw, th)
			if err != nil {
				panic(err)
			}
			pix += int64(tw * th)
			nTiles++
			_ = img
			if *maxTiles > 0 && nTiles >= *maxTiles {
				goto done
			}
		}
	}
done:
	el := time.Since(start).Seconds()
	fmt.Printf("%d tiles, %d MiB pixels in %.2f s (%.1f Mpix/s, %.1f MiB/s)\n",
		nTiles, pix*3>>20, el, float64(pix)/el/1e6, float64(pix)*3/el/1024/1024)

	if *memProf != "" {
		f, err := os.Create(*memProf)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := pprof.WriteHeapProfile(f); err != nil {
			panic(err)
		}
	}
}
