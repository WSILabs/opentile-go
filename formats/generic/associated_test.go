package generic

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// associatedSourceInfoFromPage builds an associatedSourceInfo from a
// tiff.Page — mirrors what the Tiler will do at Open time (T9).
// Lifted into the test file to avoid exposing the helper publicly
// before T9 has wired the full Tiler.
func associatedSourceInfoFromPage(t *testing.T, p *tiff.Page) associatedSourceInfo {
	t.Helper()
	iw, _ := p.ImageWidth()
	il, _ := p.ImageLength()
	tw, hasTW := p.TileWidth()
	th, _ := p.TileLength()
	comp, _ := p.Compression()
	spp, _ := p.SamplesPerPixel()
	rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)
	stripOff, err := p.ScalarArrayU64(tiff.TagStripOffsets)
	if err != nil && hasTW {
		// Tiled IFDs don't have StripOffsets; that's fine.
		stripOff = nil
	}
	stripCnt, err := p.ScalarArrayU64(tiff.TagStripByteCounts)
	if err != nil && hasTW {
		stripCnt = nil
	}
	return associatedSourceInfo{
		tiled:        hasTW && tw != 0 && th != 0,
		width:        iw,
		height:       il,
		rowsPerStrip: rps,
		samples:      spp,
		compression:  comp,
		stripOffsets: stripOff,
		stripCounts:  stripCnt,
	}
}

// TestAssociated_StrippedSVS_All3Kinds reads CMU-1.stripped.tiff
// (the T2-generated reference fixture) and confirms each of its
// 3 stripped associated IFDs (thumbnail / label / macro) reads
// correctly via the generic associated reader. This is the
// "real-world generic-TIFF associated images" coverage.
//
// IFD layout (from T2 probe):
//
//	IFD 1: 1024×732 JPEG stripped — thumbnail
//	IFD 4: 387×463 LZW stripped multi-strip — label
//	IFD 5: 1280×431 JPEG stripped — macro
func TestAssociated_StrippedSVS_All3Kinds(t *testing.T) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	path := filepath.Join(dir, "generic-tiff", "CMU-1.stripped.tiff")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	st, _ := f.Stat()
	tf, err := tiff.Open(f, st.Size())
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	pages := tf.Pages()

	for _, tc := range []struct {
		ifdIdx          int
		kind            string
		wantW, wantH    int
		wantCompression opentile.Compression
		wantSOI         []byte // first 2 bytes if applicable
	}{
		{1, KindThumbnail, 1024, 732, opentile.CompressionJPEG, []byte{0xFF, 0xD8}},
		{4, KindLabel, 387, 463, opentile.CompressionLZW, nil}, // LZW: no SOI marker
		{5, KindMacro, 1280, 431, opentile.CompressionJPEG, []byte{0xFF, 0xD8}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			info := associatedSourceInfoFromPage(t, pages[tc.ifdIdx])
			a, err := newAssociatedImage(tc.kind, info, f)
			if err != nil {
				t.Fatalf("newAssociatedImage: %v", err)
			}
			if a.Kind() != tc.kind {
				t.Errorf("Kind() = %q, want %q", a.Kind(), tc.kind)
			}
			if a.Size().W != tc.wantW || a.Size().H != tc.wantH {
				t.Errorf("Size() = %v, want %dx%d", a.Size(), tc.wantW, tc.wantH)
			}
			if a.Compression() != tc.wantCompression {
				t.Errorf("Compression() = %v, want %v", a.Compression(), tc.wantCompression)
			}
			b, err := a.Bytes()
			if err != nil {
				t.Fatalf("Bytes(): %v", err)
			}
			if len(b) == 0 {
				t.Fatal("Bytes() returned empty")
			}
			if tc.wantSOI != nil {
				if !bytes.Equal(b[:len(tc.wantSOI)], tc.wantSOI) {
					t.Errorf("first %d bytes = % x, want % x", len(tc.wantSOI), b[:len(tc.wantSOI)], tc.wantSOI)
				}
			}
			t.Logf("✓ %s: %dx%d %v %d bytes", tc.kind, tc.wantW, tc.wantH, tc.wantCompression, len(b))
		})
	}
}

// TestAssociated_BytesAreCallerOwned verifies that successive Bytes()
// calls return independent slices — modifying one doesn't affect
// the cached buffer or the next call's return.
func TestAssociated_BytesAreCallerOwned(t *testing.T) {
	a := &associatedImage{
		kind: "thumbnail",
		size: opentile.Size{W: 1, H: 1},
		compression: opentile.CompressionNone,
		bytes: []byte{1, 2, 3, 4, 5},
	}
	b1, _ := a.Bytes()
	b1[0] = 0xFF
	b2, _ := a.Bytes()
	if b2[0] == 0xFF {
		t.Errorf("Bytes() returned a shared slice: mutation leaked back")
	}
	if b2[0] != 1 {
		t.Errorf("Bytes() corrupted cache: got %d, want 1", b2[0])
	}
}

// TestAssociated_RejectsTiled covers the constructor's tiled-IFD
// rejection path (tiled associated images are out of scope for v0.10).
func TestAssociated_RejectsTiled(t *testing.T) {
	info := associatedSourceInfo{
		tiled:        true,
		width:        500,
		height:       500,
		stripOffsets: []uint64{0},
		stripCounts:  []uint64{100},
	}
	_, err := newAssociatedImage("macro", info, bytes.NewReader(make([]byte, 1000)))
	if !errors.Is(err, errUnsupportedAssociatedShape) {
		t.Errorf("got %v, want errUnsupportedAssociatedShape", err)
	}
}

// TestAssociated_RejectsOversized covers the 32 MB sanity ceiling.
func TestAssociated_RejectsOversized(t *testing.T) {
	info := associatedSourceInfo{
		tiled:        false,
		width:        100, height: 100, samples: 3,
		compression:  1,
		stripOffsets: []uint64{0},
		stripCounts:  []uint64{1 << 30}, // 1 GiB — far above 32 MB ceiling
	}
	_, err := newAssociatedImage("thumbnail", info, bytes.NewReader(nil))
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("max")) {
		t.Errorf("expected size-limit error, got %v", err)
	}
}

// TestAssociated_MultiStripUncompressed covers the multi-strip
// uncompressed concat path with synthetic data — no real fixture
// exercises this (CMU-1's stripped associated images are JPEG +
// LZW, not uncompressed). 3 strips of 4 bytes each = 12 bytes total.
func TestAssociated_MultiStripUncompressed(t *testing.T) {
	allBytes := []byte{
		// strip 0
		0x10, 0x11, 0x12, 0x13,
		// strip 1
		0x20, 0x21, 0x22, 0x23,
		// strip 2
		0x30, 0x31, 0x32, 0x33,
	}
	info := associatedSourceInfo{
		tiled:        false,
		width:        2, height: 6, samples: 1,
		rowsPerStrip: 2,
		compression:  1, // None
		stripOffsets: []uint64{0, 4, 8},
		stripCounts:  []uint64{4, 4, 4},
	}
	a, err := newAssociatedImage("custom", info, bytes.NewReader(allBytes))
	if err != nil {
		t.Fatalf("newAssociatedImage: %v", err)
	}
	if a.Compression() != opentile.CompressionNone {
		t.Errorf("Compression = %v, want None", a.Compression())
	}
	got, _ := a.Bytes()
	if !bytes.Equal(got, allBytes) {
		t.Errorf("multi-strip concat: got % x, want % x", got, allBytes)
	}
}
