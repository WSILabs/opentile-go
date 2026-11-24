package ife

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// fakeICC returns a minimal ICC-shaped profile of n bytes (n >= 128): the
// big-endian size field, the 'acsp' signature, and a recognizable fill.
func fakeICC(n int, declared uint32) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i * 7)
	}
	binary.BigEndian.PutUint32(p[0:4], declared)
	copy(p[36:40], "acsp")
	return p
}

// iccBlockFile lays out [pad][ICC_PROFILE header][body][trailer] and returns
// the file bytes plus the block offset. storedN is written as ENTRY_NUMBER.
func iccBlockFile(body []byte, storedN uint32, trailer int) ([]byte, uint64) {
	const off = 64
	buf := make([]byte, off+iccHeaderSize+len(body)+trailer)
	binary.LittleEndian.PutUint64(buf[off:off+8], off)
	binary.LittleEndian.PutUint16(buf[off+8:off+10], recoverICCProfile)
	binary.LittleEndian.PutUint32(buf[off+10:off+14], storedN)
	copy(buf[off+iccHeaderSize:], body)
	for i := off + iccHeaderSize + len(body); i < len(buf); i++ {
		buf[i] = 0xEE // the next block, never part of the profile
	}
	return buf, off
}

func readICC(t *testing.T, file []byte, off uint64) []byte {
	t.Helper()
	got, err := readICCProfile(bytes.NewReader(file), off, int64(len(file)))
	if err != nil {
		t.Fatalf("readICCProfile: %v", err)
	}
	return got
}

// TestReadICCProfileTruncatedLength is the #121 repro: the true size is
// 65536 + 500 but ENTRY_NUMBER holds only 500 (size mod 65536).
func TestReadICCProfileTruncatedLength(t *testing.T) {
	const full = 1<<16 + 500
	body := fakeICC(full, full)
	file, off := iccBlockFile(body, full%(1<<16), 32)
	got := readICC(t, file, off)
	if !bytes.Equal(got, body) {
		t.Fatalf("got %d bytes, want the full %d-byte profile", len(got), full)
	}
}

// A truncated length of exactly 0 (true size a multiple of 64 KiB) used to
// read as "no profile".
func TestReadICCProfileTruncatedToZero(t *testing.T) {
	const full = 2 << 16
	body := fakeICC(full, full)
	file, off := iccBlockFile(body, 0, 8)
	if got := readICC(t, file, off); !bytes.Equal(got, body) {
		t.Fatalf("got %d bytes, want the full %d-byte profile", len(got), full)
	}
}

func TestReadICCProfileStoredLengthKept(t *testing.T) {
	cases := []struct {
		name     string
		body     []byte // what's on disk after the header
		storedN  uint32
		wantLen  int
		trailing int
	}{
		// Spec-correct: stored == declared.
		{"consistent", fakeICC(3000, 3000), 3000, 3000, 16},
		// Declared larger, but not by a multiple of 65536: not the truncation signature.
		{"not-mod-65536", fakeICC(70000, 70000), 1234, 1234, 0},
		// Declared smaller than stored (padding after the profile): keep stored.
		{"declared-smaller", fakeICC(4000, 3500), 4000, 4000, 0},
		// Truncation signature, but the declared size runs past EOF.
		{"past-eof", fakeICC(600, 1<<16+600), 600, 600, 0},
		// No 'acsp' signature: not an ICC header, never second-guess it.
		{"no-signature", func() []byte { p := fakeICC(1<<16+300, 1<<16+300); copy(p[36:40], "xxxx"); return p }(), 300, 300, 0},
		// Body shorter than an ICC header: keep stored.
		{"tiny", []byte("ICC_PROFILE_BYTES"), 17, 17, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file, off := iccBlockFile(c.body, c.storedN, c.trailing)
			got := readICC(t, file, off)
			if len(got) != c.wantLen {
				t.Fatalf("got %d bytes, want %d", len(got), c.wantLen)
			}
			if !bytes.Equal(got, c.body[:c.wantLen]) {
				t.Fatal("bytes differ from the stored profile")
			}
		})
	}
}

func TestReadICCProfileZeroStillEmpty(t *testing.T) {
	// ENTRY_NUMBER 0 with no ICC header behind it stays "no profile".
	file, off := iccBlockFile(make([]byte, 200), 0, 0)
	if got := readICC(t, file, off); got != nil {
		t.Fatalf("got %d bytes, want nil", len(got))
	}
}
