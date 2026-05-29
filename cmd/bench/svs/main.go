package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

func main() {
	path := flag.String("in", "", "slide path")
	cpuProf := flag.String("cpuprofile", "", "write cpu profile to file")
	maxTiles := flag.Int("maxtiles", 0, "stop after N tiles (0 = all)")
	goroutines := flag.Int("goroutines", 1, "number of goroutines fanning out tile reads")
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
	fmt.Printf("source L0: %dx%d  TileSize=%v Grid=%v\n", w, h, l0.TileSize, l0.Grid)

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
	var pixTotal atomic.Int64
	var nTotal atomic.Int64

	if *goroutines <= 1 {
		for ty := 0; ty < l0.Grid.H; ty++ {
			for tx := 0; tx < l0.Grid.W; tx++ {
				img, err := slide.DecodedTile(0, tx, ty)
				if err != nil {
					panic(err)
				}
				pixTotal.Add(int64(img.Width * img.Height))
				if n := nTotal.Add(1); *maxTiles > 0 && int(n) >= *maxTiles {
					goto done
				}
			}
		}
	} else {
		type tp struct{ tx, ty int }
		jobs := make(chan tp, *goroutines*4)
		var wg sync.WaitGroup
		for i := 0; i < *goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					img, err := slide.DecodedTile(0, j.tx, j.ty)
					if err != nil {
						panic(err)
					}
					pixTotal.Add(int64(img.Width * img.Height))
					nTotal.Add(1)
				}
			}()
		}
	submit:
		for ty := 0; ty < l0.Grid.H; ty++ {
			for tx := 0; tx < l0.Grid.W; tx++ {
				jobs <- tp{tx, ty}
				if *maxTiles > 0 && nTotal.Load() >= int64(*maxTiles) {
					break submit
				}
			}
		}
		close(jobs)
		wg.Wait()
	}

done:
	el := time.Since(start).Seconds()
	pix := pixTotal.Load()
	n := nTotal.Load()
	fmt.Printf("%d tiles, %d MiB pixels in %.2f s (%.1f Mpix/s, %.1f MiB/s)\n",
		n, pix*3>>20, el, float64(pix)/el/1e6, float64(pix)*3/el/1024/1024)
}
