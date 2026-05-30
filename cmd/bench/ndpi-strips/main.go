// Command ndpi-strips drives the ScaledStrips DZI-descent path over an
// NDPI slide and snapshots an inuse_space heap profile at the HeapInuse
// peak. It exists to size the v0.30 memory-budget milestone (C1 strip
// tile cache, C2 NDPI pixelCache, C3 framesByKey) from real allocation
// data rather than geometry.
//
// It reproduces wsitools' `convert --to dzi` top-of-cascade iterator
// exactly: full-L0 l0Rect, identity outSize, stripHeight = DZI tile
// size, Nearest kernel, workers = NumCPU, default lookahead. The
// consumer drops each strip, so the only resident bytes are the
// library's own caches plus the in-flight strip buffers — which is
// precisely what we want to attribute.
package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
	"github.com/wsilabs/opentile-go/resample"
)

func main() {
	path := flag.String("in", "", "slide path")
	dziTile := flag.Int("dzitile", 256, "DZI tile size (= stripHeight); sweep 256/512/1024")
	workers := flag.Int("workers", runtime.NumCPU(), "strip decode workers (default NumCPU)")
	lookahead := flag.Int("lookahead", 2, "strip lookahead (default 2)")
	maxStrips := flag.Int("maxstrips", 0, "stop after N strips (0 = all; use to cap Hamamatsu)")
	peakProf := flag.String("peakprof", "", "write inuse_space heap profile at HeapInuse peak to this file")
	maxPeak := flag.Int64("maxpeak", 0, "fail (exit 1) if peak HeapInuse MiB exceeds this (0 = report only)")
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
	fmt.Printf("slide=%s  L0=%dx%d  dziTile=%d  workers=%d  lookahead=%d\n",
		*path, w, h, *dziTile, *workers, *lookahead)

	// Peak sampler: poll HeapInuse; on each new peak, overwrite the
	// heap profile so the final file is the inuse_space snapshot at the
	// spike. Also tracks Sys for a process-footprint proxy.
	var stop atomic.Bool
	var peakHeapInuse, peakSys uint64
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		var ms runtime.MemStats
		for !stop.Load() {
			runtime.ReadMemStats(&ms)
			if ms.HeapInuse > peakHeapInuse {
				peakHeapInuse = ms.HeapInuse
				peakSys = ms.Sys
				if *peakProf != "" {
					if f, err := os.Create(*peakProf); err == nil {
						_ = pprof.WriteHeapProfile(f)
						_ = f.Close()
					}
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
	}()

	opts := []opentile.StripOption{
		opentile.WithStripKernel(resample.Nearest),
		opentile.WithStripWorkers(*workers),
		opentile.WithStripLookahead(*lookahead),
	}
	it := slide.ScaledStrips(
		image.Rect(0, 0, w, h),
		image.Point{X: w, Y: h},
		*dziTile,
		opts...,
	)
	defer it.Close()

	start := time.Now()
	var nStrips int
	var pix int64
	for {
		img, err := it.Next()
		if err != nil {
			break // io.EOF or closed
		}
		nStrips++
		pix += int64(img.Width) * int64(img.Height)
		if *maxStrips > 0 && nStrips >= *maxStrips {
			break
		}
	}
	el := time.Since(start).Seconds()

	stop.Store(true)
	samplerWG.Wait()

	fmt.Printf("strips=%d  pix=%d MiB  %.2fs  %.1f Mpix/s\n",
		nStrips, pix*3>>20, el, float64(pix)/el/1e6)
	peakMiB := int64(peakHeapInuse >> 20)
	fmt.Printf("PEAK HeapInuse=%d MiB  Sys=%d MiB\n", peakMiB, peakSys>>20)
	if *peakProf != "" {
		fmt.Printf("peak inuse_space profile -> %s\n", *peakProf)
	}
	if *maxPeak > 0 && peakMiB > *maxPeak {
		fmt.Fprintf(os.Stderr, "FAIL: peak %d MiB > maxpeak %d MiB\n", peakMiB, *maxPeak)
		os.Exit(1)
	}
}
