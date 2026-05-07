//go:build bfparity

// Package oracle's bf_runner.go invokes the bio-formats CLI
// (`/opt/bftools/showinf`) and parses the output into a structured
// summary. Used by the leicascn bio-formats parity oracle.
//
// Per v0.11 design Q9: bio-formats parity is structural-equivalence
// (series count, per-series Width / Height / SizeC, Thumbnail-series
// flag) — NOT byte-equality. Tile bytes from `bfconvert` are decoded
// and re-encoded by bio-formats so they don't byte-match our
// raw-passthrough output.
package oracle

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ShowinfSummary is the parsed output of `showinf -nopix`. Each
// SeriesEntry corresponds to one bio-formats "Series" — note that
// bio-formats labels the lowest-resolution IFD of each pyramid as
// its own "Thumbnail series". Our reader collapses those into the
// auxiliary AssociatedImage at highest resolution; the parity
// comparator filters bio-formats's Thumbnail series to align.
type ShowinfSummary struct {
	SeriesCount int
	Series      []ShowinfSeriesEntry
}

// ShowinfSeriesEntry is one bio-formats series. SizeC is the
// channel count; bio-formats reports "(effectively 1)" when an RGB
// series has 3 interleaved channels in a single tile (we report
// SizeC=1 in that case via SingleImage default).
type ShowinfSeriesEntry struct {
	Width, Height int
	SizeC         int
	Thumbnail     bool
	RGB           bool
}

// RunShowinfForTest invokes `/opt/bftools/showinf -nopix -no-upgrade
// <file>` and parses the output. Returns a structured summary or an
// error if showinf fails / output can't be parsed.
//
// `showinf` must exist at /opt/bftools/showinf (where the local
// bio-formats CLI is installed); callers should LookPath / Skip
// before invoking. Exported so the bfparity-tagged test files in
// oracle_test can use it; not for non-test callers.
func RunShowinfForTest(file string) (*ShowinfSummary, error) {
	cmd := exec.Command("/opt/bftools/showinf", "-nopix", "-no-upgrade", file)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("showinf failed: %w (output: %s)", err, out.String())
	}
	return parseShowinfOutput(out.String())
}

// parseShowinfOutput is the testable parser separated from exec.
func parseShowinfOutput(text string) (*ShowinfSummary, error) {
	s := &ShowinfSummary{}
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var current *ShowinfSeriesEntry
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Series count = "):
			n, err := strconv.Atoi(strings.TrimPrefix(line, "Series count = "))
			if err != nil {
				return nil, fmt.Errorf("parse Series count: %w", err)
			}
			s.SeriesCount = n
		case strings.HasPrefix(line, "Series #"):
			s.Series = append(s.Series, ShowinfSeriesEntry{})
			current = &s.Series[len(s.Series)-1]
		case current != nil:
			if v, ok := parseTabbedKV(line, "Width"); ok {
				current.Width, _ = strconv.Atoi(v)
			} else if v, ok := parseTabbedKV(line, "Height"); ok {
				current.Height, _ = strconv.Atoi(v)
			} else if v, ok := parseTabbedKV(line, "SizeC"); ok {
				// Format: "3 (effectively 1)" or just "3".
				v = strings.SplitN(v, " ", 2)[0]
				current.SizeC, _ = strconv.Atoi(v)
			} else if v, ok := parseTabbedKV(line, "Thumbnail series"); ok {
				current.Thumbnail = strings.TrimSpace(v) == "true"
			} else if v, ok := parseTabbedKV(line, "RGB"); ok {
				current.RGB = strings.HasPrefix(v, "true")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan output: %w", err)
	}
	if s.SeriesCount == 0 && len(s.Series) > 0 {
		s.SeriesCount = len(s.Series)
	}
	return s, nil
}

// parseTabbedKV pulls the value from showinf's "\tKey = Value" lines.
// Returns (value, true) on match, ("", false) otherwise.
func parseTabbedKV(line, key string) (string, bool) {
	prefix := "\t" + key + " = "
	if strings.HasPrefix(line, prefix) {
		return strings.TrimPrefix(line, prefix), true
	}
	return "", false
}
