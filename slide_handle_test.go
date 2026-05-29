//go:build cgo && !nocgo

package opentile_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// TestSlideDecoderHandleReuse asserts that 100 sequential DecodedTile
// calls on the same compression produce exactly one cached pool —
// proving the per-call fac.New() pattern is gone and reuse is in place.
//
// Uses a real JPEG-tiled fixture (CMU-1-Small-Region.svs) and the real
// JPEG decoder so no factory instrumentation is needed; map-shape
// inspection via the HandleCountForTest accessor is sufficient
// evidence of caching.
func TestSlideDecoderHandleReuse(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer slide.Close()

	for i := 0; i < 100; i++ {
		_, err := slide.DecodedTile(0, 0, 0)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	if got := slide.HandleCountForTest(); got != 1 {
		t.Fatalf("expected 1 cached pool (single codec reused across 100 calls); got %d", got)
	}
}

// TestSlideCloseReleasesHandles asserts Slide.Close drains the pool map.
func TestSlideCloseReleasesHandles(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := slide.DecodedTile(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := slide.HandleCountForTest(); got == 0 {
		t.Fatal("expected at least one cached pool before Close")
	}
	if err := slide.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := slide.HandleCountForTest(); got != 0 {
		t.Fatalf("expected handles map drained after Close; got %d entries", got)
	}
}

// TestSlideHandleConcurrent fans 32 goroutines through Slide.DecodedTile
// to verify the pool is safe under concurrent fanout. Detects races
// (under -race) and deadlocks.
func TestSlideHandleConcurrent(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	slide, err := opentile.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer slide.Close()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := slide.DecodedTile(0, 0, 0); err != nil {
					t.Errorf("decode: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := slide.HandleCountForTest(); got != 1 {
		t.Fatalf("expected 1 cached pool after concurrent fanout; got %d", got)
	}
}
