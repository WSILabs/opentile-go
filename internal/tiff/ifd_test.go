package tiff

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// buildClassicTIFF builds a tiny in-memory TIFF (LE) with one IFD containing
// the supplied entries. All tag values must fit inline (≤4 bytes).
// Returns the raw bytes; first IFD is at offset 8.
func buildClassicTIFF(t *testing.T, entries [][3]uint32) []byte {
	t.Helper()
	// Header: II 42 0x00000008
	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0, 0x08, 0, 0, 0})
	// IFD: count (u16), entries (12 bytes each), next IFD (u32)
	n := uint16(len(entries))
	_ = binary.Write(buf, binary.LittleEndian, n)
	for _, e := range entries {
		// tag u16, type u16 (SHORT=3), count u32, value-or-offset u32
		_ = binary.Write(buf, binary.LittleEndian, uint16(e[0]))
		_ = binary.Write(buf, binary.LittleEndian, uint16(3)) // SHORT
		_ = binary.Write(buf, binary.LittleEndian, uint32(1))
		_ = binary.Write(buf, binary.LittleEndian, e[2])
	}
	_ = binary.Write(buf, binary.LittleEndian, uint32(0)) // next IFD = 0
	return buf.Bytes()
}

func TestWalkIFDs(t *testing.T) {
	data := buildClassicTIFF(t, [][3]uint32{
		{256, 3, 1024}, // ImageWidth = 1024
		{257, 3, 768},  // ImageLength = 768
	})
	r := bytes.NewReader(data)
	h, err := parseHeader(r)
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	b := newByteReader(r, h.littleEndian)

	ifds, err := walkIFDs(b, int64(h.firstIFD), modeClassic)
	if err != nil {
		t.Fatalf("walkIFDs: %v", err)
	}
	if len(ifds) != 1 {
		t.Fatalf("ifd count: got %d, want 1", len(ifds))
	}
	ifd := ifds[0]
	if len(ifd.entries) != 2 {
		t.Fatalf("entry count: got %d, want 2", len(ifd.entries))
	}
	w, ok := ifd.get(256)
	if !ok {
		t.Fatal("ImageWidth missing")
	}
	wv, err := w.Values(b)
	if err != nil || len(wv) != 1 || wv[0] != 1024 {
		t.Fatalf("ImageWidth: got %v, err %v; want [1024]", wv, err)
	}
}

// TestIFDOffsetRetained verifies the walk records each IFD's byte offset and
// that Page.IFDOffset surfaces it. buildClassicTIFF places the sole IFD at
// the header's first-IFD offset (8).
func TestIFDOffsetRetained(t *testing.T) {
	data := buildClassicTIFF(t, [][3]uint32{{256, 3, 1024}})
	r := bytes.NewReader(data)
	h, err := parseHeader(r)
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	b := newByteReader(r, h.littleEndian)
	ifds, err := walkIFDs(b, int64(h.firstIFD), modeClassic)
	if err != nil {
		t.Fatalf("walkIFDs: %v", err)
	}
	if got := ifds[0].offset; got != int64(h.firstIFD) {
		t.Errorf("ifd.offset = %d, want %d", got, h.firstIFD)
	}
	if got := newPage(ifds[0], b).IFDOffset(); got != int64(h.firstIFD) {
		t.Errorf("Page.IFDOffset() = %d, want %d", got, h.firstIFD)
	}
}

// buildClassicTIFFChain builds a TIFF with a chain of empty IFDs whose
// next-IFD pointers walk in the order given by nextOffsets. nextOffsets[i]
// is the next-IFD pointer to write for IFD i; pass 0 to terminate the
// chain. IFD i lives at offset 8 + i*emptyIFDSize where emptyIFDSize = 6
// (count(2) + next(4); zero entries).
func buildClassicTIFFChain(t *testing.T, nextOffsets []uint32) []byte {
	t.Helper()
	const headerLen = 8
	const emptyIFDSize = 2 + 4 // 0 entries: count(2) + next(4)
	off0 := uint32(headerLen)

	buf := new(bytes.Buffer)
	buf.Write([]byte{'I', 'I', 42, 0})
	_ = binary.Write(buf, binary.LittleEndian, off0)
	for _, next := range nextOffsets {
		_ = binary.Write(buf, binary.LittleEndian, uint16(0))
		_ = binary.Write(buf, binary.LittleEndian, next)
	}
	_ = emptyIFDSize // documents the layout assumption above
	return buf.Bytes()
}

// TestWalkIFDsMultiple exercises a real 3-IFD chain: page 0 → page 1 →
// page 2 → terminator. Replaces the v0.1 placeholder which only verified
// "stops at zero" via a single-IFD input.
func TestWalkIFDsMultiple(t *testing.T) {
	const headerLen = 8
	const emptyIFDSize = 2 + 4 // 0 entries: count(2) + next(4)
	off0 := uint32(headerLen)
	off1 := off0 + emptyIFDSize
	off2 := off1 + emptyIFDSize

	data := buildClassicTIFFChain(t, []uint32{off1, off2, 0})
	r := bytes.NewReader(data)
	h, err := parseHeader(r)
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	b := newByteReader(r, h.littleEndian)
	ifds, err := walkIFDs(b, int64(h.firstIFD), modeClassic)
	if err != nil {
		t.Fatalf("walkIFDs: %v", err)
	}
	if len(ifds) != 3 {
		t.Errorf("expected 3 IFDs, got %d", len(ifds))
	}
	_ = off2 // silence unused if we drop the assertion above in future edits
}

// TestWalkIFDsRejectsCycle constructs a 2-IFD chain where IFD 1's next
// pointer loops back to IFD 0, then asserts walkIFDs detects the cycle and
// returns a non-nil error rather than walking forever or returning an
// arbitrarily long page list.
func TestWalkIFDsRejectsCycle(t *testing.T) {
	const headerLen = 8
	const emptyIFDSize = 2 + 4
	off0 := uint32(headerLen)
	off1 := off0 + emptyIFDSize

	data := buildClassicTIFFChain(t, []uint32{off1, off0}) // IFD1 loops back
	r := bytes.NewReader(data)
	h, err := parseHeader(r)
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	b := newByteReader(r, h.littleEndian)
	ifds, err := walkIFDs(b, int64(h.firstIFD), modeClassic)
	if err == nil {
		t.Fatalf("walkIFDs: expected cycle/cap error, got nil and %d IFDs", len(ifds))
	}
	t.Logf("cycle correctly rejected: %v", err)
}

// BenchmarkWalkIFDs measures the cost of walking the IFD chain of a real
// multi-page slide. The relevant variant is OS-2.ndpi (the slide with the
// most pages in our fixture set). Skipped when OPENTILE_TESTDIR is unset
// or the slide is missing.
func BenchmarkWalkIFDs(b *testing.B) {
	dir := os.Getenv("OPENTILE_TESTDIR")
	if dir == "" {
		b.Skip("OPENTILE_TESTDIR not set")
	}
	slide := filepath.Join(dir, "ndpi", "OS-2.ndpi")
	if _, err := os.Stat(slide); err != nil {
		b.Skipf("slide not present: %v", err)
	}
	f, err := os.Open(slide)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Open(f, st.Size())
		if err != nil {
			b.Fatal(err)
		}
	}
}
