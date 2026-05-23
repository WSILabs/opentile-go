package svs

import (
	"bytes"
	"fmt"
	"io"

	"github.com/wsilabs/opentile-go/internal/tifflzw"
)

// reconstructLZWLabel decodes each TIFF strip of an LZW-compressed label,
// concatenates the decoded raster row-major, and re-encodes as a single
// LZW stream covering the full image height. Used to "fix" the upstream
// Python opentile bug where SvsLabelImage.get_tile((0,0)) returns only
// strip 0 (a RowsPerStrip-tall sliver of the full label).
//
// Inputs:
//   - strips: each strip's raw LZW bytes, in scan order
//   - rowsPerStrip, imageHeight, imageWidth, samples: TIFF tag values
//
// Output: a single LZW-compressed bytestream covering the entire image
// raster, MSB bit ordering, 8-bit literal width — matching TIFF LZW
// expectations.
func reconstructLZWLabel(strips [][]byte, rowsPerStrip, imageHeight, imageWidth, samples int) ([]byte, error) {
	if len(strips) == 0 {
		return nil, fmt.Errorf("svs: reconstructLZWLabel: no strips")
	}
	if rowsPerStrip <= 0 || imageHeight <= 0 || imageWidth <= 0 || samples <= 0 {
		return nil, fmt.Errorf("svs: reconstructLZWLabel: invalid dimensions (rps=%d h=%d w=%d s=%d)",
			rowsPerStrip, imageHeight, imageWidth, samples)
	}
	expectedTotal := imageHeight * imageWidth * samples
	raster := make([]byte, 0, expectedTotal)

	for i, strip := range strips {
		dr := tifflzw.NewReader(bytes.NewReader(strip), tifflzw.MSB, 8)
		decoded, err := io.ReadAll(dr)
		dr.Close()
		if err != nil {
			return nil, fmt.Errorf("svs: lzw decode strip %d: %w", i, err)
		}
		// Last strip may have fewer rows: rowsThisStrip = min(rowsPerStrip,
		// imageHeight - i*rowsPerStrip).
		rowsThisStrip := rowsPerStrip
		if start := i * rowsPerStrip; start+rowsThisStrip > imageHeight {
			rowsThisStrip = imageHeight - start
		}
		if rowsThisStrip <= 0 {
			// Extra strips beyond what the image height needs — ignore.
			continue
		}
		expectedThisStrip := rowsThisStrip * imageWidth * samples
		if len(decoded) > expectedThisStrip {
			decoded = decoded[:expectedThisStrip]
		}
		if len(decoded) < expectedThisStrip {
			return nil, fmt.Errorf("svs: lzw strip %d short: got %d bytes, want %d (rows=%d w=%d samples=%d)",
				i, len(decoded), expectedThisStrip, rowsThisStrip, imageWidth, samples)
		}
		raster = append(raster, decoded...)
	}
	if len(raster) != expectedTotal {
		return nil, fmt.Errorf("svs: lzw raster size %d != expected %d", len(raster), expectedTotal)
	}

	var out bytes.Buffer
	w := tifflzw.NewWriter(&out, tifflzw.MSB, 8)
	if _, err := w.Write(raster); err != nil {
		return nil, fmt.Errorf("svs: lzw encode: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("svs: lzw encode close: %w", err)
	}
	return out.Bytes(), nil
}
