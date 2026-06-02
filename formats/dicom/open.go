package dicom

import (
	"fmt"
	"os"
	"path/filepath"

	opentile "github.com/wsilabs/opentile-go"
	idicom "github.com/wsilabs/opentile-go/internal/dicom"
	"golang.org/x/exp/mmap"
)

// IsDICOM reports whether path is openable as a DICOM WSM series: either a
// directory containing at least one WSM instance with DICM magic, or a
// single .dcm file with the DICM magic preamble.
func IsDICOM(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		entries, _ := filepath.Glob(filepath.Join(path, "*.dcm"))
		for _, p := range entries {
			if hasDICMMagic(p) {
				return true
			}
		}
		return false
	}
	return hasDICMMagic(path)
}

// openForHook is the entry point called by the root's dicomPathOpenHook.
// It returns opentile.ErrUnsupportedFormat when the path is not DICOM, so
// the root's OpenFile can fall through to normal single-file dispatch.
func openForHook(path string) (any, error) {
	if !IsDICOM(path) {
		return nil, opentile.ErrUnsupportedFormat
	}
	return OpenSeries(path)
}

// hasDICMMagic reads the 4-byte "DICM" preamble at offset 128 of the file.
func hasDICMMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [132]byte
	if _, err := f.ReadAt(buf[:], 0); err != nil {
		return false
	}
	return string(buf[128:132]) == "DICM"
}

// OpenSeries opens a WSM series given a directory or a single instance path.
// The sibling-scan contract is bounded: same directory only, same SeriesUID
// only, WSM-filtered. When a directory is given, all WSM .dcm files in that
// directory are parsed and the dominant series (most VOLUME instances) is
// opened. When a single instance is given, the series is expanded to all
// sibling instances sharing the same SeriesUID.
func OpenSeries(path string) (*Tiler, error) {
	dir := path
	var anchorSeries string

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("dicom: stat %s: %w", path, err)
	}
	if !fi.IsDir() {
		dir = filepath.Dir(path)
		in, err := idicom.ParseInstance(path)
		if err != nil {
			return nil, fmt.Errorf("dicom: parse %s: %w", path, err)
		}
		if in.SOPClassUID != idicom.WSMStorageUID {
			return nil, fmt.Errorf("dicom: %s is not a WSM instance (SOP class %s)", path, in.SOPClassUID)
		}
		anchorSeries = in.SeriesUID
	}

	entries, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	var parsed []idicom.Instance
	for _, p := range entries {
		in, err := idicom.ParseInstance(p)
		if err != nil || in.SOPClassUID != idicom.WSMStorageUID {
			continue // skip unreadable / non-WSM
		}
		if anchorSeries != "" && in.SeriesUID != anchorSeries {
			continue // bound to the anchor's series
		}
		parsed = append(parsed, in)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("dicom: no WSM instances under %s", dir)
	}
	// If multiple series remain (no anchor), pick the one with most VOLUME levels.
	parsed = selectDominantSeries(parsed)

	return openSeriesFromInstances(parsed, mmapOpener)
}

// mmapOpener maps an instance file and returns its bytes + a closer.
// v1 copies the file into a []byte via golang.org/x/exp/mmap.ReaderAt
// rather than a true syscall.Mmap []byte view. This keeps the dependency
// surface to x/exp/mmap (already used elsewhere). Converting to a true
// zero-copy mmap is a drop-in future optimization.
func mmapOpener(path string) ([]byte, func() error, error) {
	r, err := mmap.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("dicom: mmap %s: %w", path, err)
	}
	b := make([]byte, r.Len())
	if _, err := r.ReadAt(b, 0); err != nil {
		r.Close()
		return nil, nil, fmt.Errorf("dicom: read %s: %w", path, err)
	}
	return b, r.Close, nil
}
