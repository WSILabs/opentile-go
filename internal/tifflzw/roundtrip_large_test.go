package tifflzw

import (
	"bytes"
	"io"
	"testing"
)

func TestLargeRoundTrip(t *testing.T) {
	for _, n := range []int{4096, 100_000, 537_543, 1 << 20} {
		raw := make([]byte, n)
		for i := range raw {
			raw[i] = byte(i*131 + (i/777)*7) // structured but not trivially compressible
		}
		var enc bytes.Buffer
		w := NewWriter(&enc, MSB, 8)
		if _, err := w.Write(raw); err != nil {
			t.Fatalf("n=%d write: %v", n, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("n=%d close: %v", n, err)
		}
		dec, err := io.ReadAll(NewReader(bytes.NewReader(enc.Bytes()), MSB, 8))
		if err != nil {
			t.Errorf("n=%d decode err: %v (got %d bytes)", n, err, len(dec))
			continue
		}
		if len(dec) != n {
			t.Errorf("n=%d: decoded %d bytes (TRUNCATED, want %d)", n, len(dec), n)
			continue
		}
		if !bytes.Equal(dec, raw) {
			t.Errorf("n=%d: decoded bytes differ from original", n)
		}
	}
}
