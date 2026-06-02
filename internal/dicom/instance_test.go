package dicom

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureDir(t *testing.T) string {
	base := os.Getenv("OPENTILE_TESTDIR")
	if base == "" {
		t.Skip("OPENTILE_TESTDIR not set")
	}
	dir := filepath.Join(base, "dicom", "Leica-4")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("Leica-4 fixture not present: %v", err)
	}
	return dir
}

// largestVolume returns the path of the biggest .dcm by file size (the L0 VOLUME).
func largestVolume(t *testing.T, dir string) string {
	entries, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	var best string
	var bestSize int64
	for _, p := range entries {
		fi, _ := os.Stat(p)
		if fi.Size() > bestSize {
			bestSize, best = fi.Size(), p
		}
	}
	return best
}

func TestParseInstanceLeicaL0(t *testing.T) {
	in, err := ParseInstance(largestVolume(t, fixtureDir(t)))
	if err != nil {
		t.Fatalf("ParseInstance: %v", err)
	}
	if in.SOPClassUID != WSMStorageUID {
		t.Errorf("SOPClassUID = %q, want WSM", in.SOPClassUID)
	}
	if got, want := roleOf(in.ImageType), "VOLUME"; got != want {
		t.Errorf("role = %q, want %q", got, want)
	}
	if in.TotalCols != 23374 || in.TotalRows != 22079 {
		t.Errorf("TotalPixelMatrix = %dx%d, want 23374x22079", in.TotalCols, in.TotalRows)
	}
	if in.TileCols != 256 || in.TileRows != 256 {
		t.Errorf("tile = %dx%d, want 256x256", in.TileCols, in.TileRows)
	}
	if in.NumFrames != 8004 {
		t.Errorf("NumFrames = %d, want 8004", in.NumFrames)
	}
	if in.DimOrg != "TILED_SPARSE" {
		t.Errorf("DimOrg = %q, want TILED_SPARSE", in.DimOrg)
	}
	if len(in.FramePositions) != in.NumFrames {
		t.Errorf("FramePositions = %d, want %d", len(in.FramePositions), in.NumFrames)
	}
	// First Leica frame observed at col=1,row=1281 (1-based pixel coords).
	if in.FramePositions[0].Col != 1 {
		t.Errorf("FramePositions[0].Col = %d, want 1", in.FramePositions[0].Col)
	}
}
