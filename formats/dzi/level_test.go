package dzi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelTileReadsFile(t *testing.T) {
	dir := t.TempDir()
	filesDir := filepath.Join(dir, "img_files")
	// dziLevel 5, tile (1,0) → <filesDir>/5/1_0.jpeg
	tileDir := filepath.Join(filesDir, "5")
	if err := os.MkdirAll(tileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xff\xd8\xff\xe0FAKEJPEG")
	if err := os.WriteFile(filepath.Join(tileDir, "1_0.jpeg"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	l := &level{filesDir: filesDir, format: "jpeg", dziLevel: 5,
		openTileIdx: 0, width: 512, height: 256, cols: 2, rows: 1, tileSize: 256}

	got, err := l.Tile(1, 0)
	if err != nil {
		t.Fatalf("Tile(1,0): %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("Tile bytes = %q, want %q", got, want)
	}
	// Out of grid.
	if _, err := l.Tile(5, 5); err == nil {
		t.Fatal("Tile(5,5) want out-of-bounds error")
	}
	// In-grid but file absent.
	if _, err := l.Tile(0, 0); err == nil {
		t.Fatal("Tile(0,0) want missing-tile error (no file written)")
	}
}

func TestTilePathUsesOSNativeSeparators(t *testing.T) {
	l := &level{
		filesDir: filepath.Join("root", "slide_files"),
		dziLevel: 5,
		format:   "jpeg",
	}
	got := l.tilePath(3, 4)
	want := filepath.Join("root", "slide_files", "5", "3_4.jpeg")
	if got != want {
		t.Errorf("tilePath = %q, want %q", got, want)
	}
	// On Windows the separator is '\\'; a filesystem path must not carry a
	// stray forward slash (the SZI/ZIP forward-slash form is wrong for os.Open).
	if os.PathSeparator == '\\' && strings.ContainsRune(got, '/') {
		t.Errorf("tilePath %q contains a forward slash on a backslash-separator OS", got)
	}
}
