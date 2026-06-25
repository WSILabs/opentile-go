// Command ifefixheader rewrites the scale-relative resolution fields in an Iris
// File Extension (.iris) METADATA header so they are conformant with the file's
// own pyramid.
//
// Background: the Iris codec stores resolution as SCALE-RELATIVE quantities —
// micronsPerPixel is the MPP at the lowest-resolution layer and magnification is
// a coefficient (see Iris-Codec/src/IrisCodecEncoder.cpp READ_OPENSLIDE_METADATA):
//
//	micronsPerPixel = MPP_L0       * max_scale
//	magnification   = objective_L0 / max_scale
//
// where max_scale is the finest layer's scale (= the largest LAYER_EXTENTS scale).
// Some files ship a header computed for a DIFFERENT pyramid than they actually
// carry (e.g. the public cervix_2x sample: its header was computed for a 4-layer
// ×1/4/16/64 ladder, max_scale 64, but the file is a 9-layer ×1…256 ladder,
// max_scale 256 — so the convention yields an impossible 160×). This tool reads
// max_scale from the file's own LAYER_EXTENTS and writes the two f32 header fields
// from the supplied true level-0 objective magnification and MPP.
//
// Usage:
//
//	go run ./cmd/ifefixheader -appmag 40 -mpp 0.262968 path/to/slide.iris
//	go run ./cmd/ifefixheader -appmag 40 -mpp 0.262968 -dry-run path/to/slide.iris
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
)

func main() {
	appmag := flag.Float64("appmag", 0, "true level-0 objective magnification (e.g. 40)")
	mpp := flag.Float64("mpp", 0, "true level-0 microns-per-pixel (e.g. 0.262968)")
	dry := flag.Bool("dry-run", false, "print the computed values without writing")
	flag.Parse()
	if flag.NArg() != 1 || *appmag <= 0 || *mpp <= 0 {
		fmt.Fprintln(os.Stderr, "usage: ifefixheader -appmag <x> -mpp <µm> [-dry-run] file.iris")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *appmag, *mpp, *dry); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(path string, appmag, mpp float64, dry bool) error {
	flagW := os.O_RDONLY
	if !dry {
		flagW = os.O_RDWR
	}
	f, err := os.OpenFile(path, flagW, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	readAt := func(off int64, n int) ([]byte, error) {
		b := make([]byte, n)
		_, err := f.ReadAt(b, off)
		return b, err
	}

	// FILE_HEADER: magic u32@0 ("Iris"), tile_table_offset u64@22, metadata_offset u64@30.
	fh, err := readAt(0, 38)
	if err != nil {
		return fmt.Errorf("read FILE_HEADER: %w", err)
	}
	if binary.LittleEndian.Uint32(fh[0:4]) != 0x49726973 {
		return fmt.Errorf("not an IFE file (bad magic)")
	}
	ttOff := binary.LittleEndian.Uint64(fh[22:30])
	mdOff := binary.LittleEndian.Uint64(fh[30:38])
	if mdOff == 0 || mdOff == ^uint64(0) {
		return fmt.Errorf("file has no METADATA block (offset is NULL)")
	}

	// METADATA: validation u64@0 (== mdOff), recovery u16@8 (0x5504).
	md, err := readAt(int64(mdOff), 56)
	if err != nil {
		return fmt.Errorf("read METADATA: %w", err)
	}
	if binary.LittleEndian.Uint64(md[0:8]) != mdOff {
		return fmt.Errorf("METADATA validation mismatch (offset 0x%x)", mdOff)
	}
	if binary.LittleEndian.Uint16(md[8:10]) != 0x5504 {
		return fmt.Errorf("METADATA recovery != 0x5504")
	}
	oldMPP := math.Float32frombits(binary.LittleEndian.Uint32(md[48:52]))
	oldMag := math.Float32frombits(binary.LittleEndian.Uint32(md[52:56]))

	// TILE_TABLE: layer_extents_offset u64@28.
	tt, err := readAt(int64(ttOff), 44)
	if err != nil {
		return fmt.Errorf("read TILE_TABLE: %w", err)
	}
	leOff := binary.LittleEndian.Uint64(tt[28:36])

	// LAYER_EXTENTS: 16-byte header (entry_number u32@12) + N×12-byte entries
	// (scale f32 at entry offset +8). max_scale = the LAST (finest) entry's scale.
	leh, err := readAt(int64(leOff), 16)
	if err != nil {
		return fmt.Errorf("read LAYER_EXTENTS header: %w", err)
	}
	n := binary.LittleEndian.Uint32(leh[12:16])
	if n == 0 {
		return fmt.Errorf("LAYER_EXTENTS empty")
	}
	last, err := readAt(int64(leOff)+16+int64(n-1)*12, 12)
	if err != nil {
		return fmt.Errorf("read finest LAYER_EXTENT: %w", err)
	}
	maxScale := float64(math.Float32frombits(binary.LittleEndian.Uint32(last[8:12])))
	if maxScale <= 0 {
		return fmt.Errorf("invalid max_scale %g", maxScale)
	}

	newMPP := float32(mpp * maxScale)
	newMag := float32(appmag / maxScale)

	fmt.Printf("file              %s\n", path)
	fmt.Printf("layers            %d  (max_scale = %g)\n", n, maxScale)
	fmt.Printf("micronsPerPixel   %g  ->  %g   (= %g µm/px × %g)\n", oldMPP, newMPP, mpp, maxScale)
	fmt.Printf("magnification     %g  ->  %g   (= %g× / %g)\n", oldMag, newMag, appmag, maxScale)
	fmt.Printf("convention check  MPP = %g / %g = %g ; mag = %g × %g = %g\n",
		newMPP, maxScale, float64(newMPP)/maxScale, newMag, maxScale, float64(newMag)*maxScale)

	if dry {
		fmt.Println("(dry-run: nothing written)")
		return nil
	}

	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(newMPP))
	if _, err := f.WriteAt(buf[:], int64(mdOff)+48); err != nil {
		return fmt.Errorf("write micronsPerPixel: %w", err)
	}
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(newMag))
	if _, err := f.WriteAt(buf[:], int64(mdOff)+52); err != nil {
		return fmt.Errorf("write magnification: %w", err)
	}
	fmt.Println("written.")
	return nil
}
