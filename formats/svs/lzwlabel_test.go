package svs

import (
	"bytes"
	"io"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

func TestReconstructLZWLabelRoundTrip(t *testing.T) {
	const (
		imageW       = 4
		imageH       = 9
		rowsPerStrip = 3
		samples      = 1
	)
	full := make([]byte, imageH*imageW*samples)
	for i := range full {
		full[i] = byte(i)
	}
	var strips [][]byte
	for s := 0; s < 3; s++ {
		var buf bytes.Buffer
		w := tifflzw.NewWriter(&buf, tifflzw.MSB, 8)
		start := s * rowsPerStrip * imageW * samples
		end := start + rowsPerStrip*imageW*samples
		w.Write(full[start:end])
		w.Close()
		strips = append(strips, buf.Bytes())
	}
	got, err := reconstructLZWLabel(strips, rowsPerStrip, imageH, imageW, samples)
	if err != nil {
		t.Fatal(err)
	}
	dr := tifflzw.NewReader(bytes.NewReader(got), tifflzw.MSB, 8)
	defer dr.Close()
	decoded, err := io.ReadAll(dr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, full) {
		t.Errorf("round-trip raster mismatch:\n got: %v\nwant: %v", decoded, full)
	}
}

func TestReconstructLZWLabelPartialLastStrip(t *testing.T) {
	const (
		imageW       = 2
		imageH       = 10
		rowsPerStrip = 4
		samples      = 1
	)
	full := make([]byte, imageH*imageW*samples)
	for i := range full {
		full[i] = byte(i + 1)
	}
	var strips [][]byte
	for s := 0; s < 3; s++ {
		var buf bytes.Buffer
		w := tifflzw.NewWriter(&buf, tifflzw.MSB, 8)
		start := s * rowsPerStrip * imageW * samples
		end := start + rowsPerStrip*imageW*samples
		if end > len(full) {
			end = len(full)
		}
		w.Write(full[start:end])
		w.Close()
		strips = append(strips, buf.Bytes())
	}
	got, err := reconstructLZWLabel(strips, rowsPerStrip, imageH, imageW, samples)
	if err != nil {
		t.Fatal(err)
	}
	dr := tifflzw.NewReader(bytes.NewReader(got), tifflzw.MSB, 8)
	defer dr.Close()
	decoded, err := io.ReadAll(dr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, full) {
		t.Errorf("partial last-strip mismatch:\n got: %v\nwant: %v", decoded, full)
	}
}
