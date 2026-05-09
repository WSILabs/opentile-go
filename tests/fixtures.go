// Package tests contains integration test helpers and fixture schemas shared
// between integration and fixture-generation tests.
package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AssociatedFixture is the per-associated-image portion of a fixture.
type AssociatedFixture struct {
	Type        string `json:"kind"`
	Size        [2]int `json:"size"`
	Compression string `json:"compression"`
	SHA256      string `json:"sha256"`
}

// Fixture is the on-disk schema for a single-slide parity fixture.
//
// The Levels / TileSHA256 / SampledTileSHA256 fields hold the
// single-image (or Images[0]) view; for multi-image formats (OME-TIFF)
// the Images field is populated with one entry per main pyramid and
// integration tests prefer it over the top-level Levels.
type Fixture struct {
	Slide             string                 `json:"slide"`
	Format            string                 `json:"format"`
	Levels            []LevelFixture         `json:"levels"`
	Metadata          MetadataFixture        `json:"metadata"`
	TileSHA256        map[string]string      `json:"tiles,omitempty"`
	SampledTileSHA256 map[string]SampledTile `json:"sampled_tiles,omitempty"`
	ICCProfileSHA256  string                 `json:"icc_profile_sha256,omitempty"`
	AssociatedImages  []AssociatedFixture    `json:"associated,omitempty"`
	// Images is the multi-image view, populated for files where
	// tiler.Images() returns more than one entry (multi-image OME).
	// When populated, integration tests walk Images instead of the
	// top-level Levels / TileSHA256 / SampledTileSHA256 fields. Each
	// ImageFixture's per-tile hashes are namespaced by image index
	// in the keys so OME images don't collide.
	Images []ImageFixture `json:"images,omitempty"`
}

// ImageFixture is one main pyramid in a multi-image fixture.
// SampledTileSHA256 keys use the image index prefix to avoid collisions
// across images in the same fixture file.
type ImageFixture struct {
	Index             int                    `json:"index"`
	Name              string                 `json:"name,omitempty"`
	Levels            []LevelFixture         `json:"levels"`
	TileSHA256        map[string]string      `json:"tiles,omitempty"`
	SampledTileSHA256 map[string]SampledTile `json:"sampled_tiles,omitempty"`
}

// SampledTile is one entry in SampledTileSHA256: a hash paired with a
// human-readable label describing what code path the tile exercises.
type SampledTile struct {
	SHA256 string `json:"sha256"`
	Reason string `json:"reason"`
}

type LevelFixture struct {
	Index       int     `json:"index"`
	Size        [2]int  `json:"size"`
	TileSize    [2]int  `json:"tile_size"`
	Grid        [2]int  `json:"grid"`
	Compression string  `json:"compression"`
	MPPUm       float64 `json:"mpp_um"`
	PyramidIdx  int     `json:"pyramid_index"`
}

type MetadataFixture struct {
	Magnification       float64 `json:"magnification"`
	ScannerManufacturer string  `json:"scanner_manufacturer,omitempty"`
	ScannerSerial       string  `json:"scanner_serial,omitempty"`
	SoftwareLine        string  `json:"software_line,omitempty"`
	MPP                 float64 `json:"mpp_um,omitempty"`
	AcquisitionRFC3339  string  `json:"acquisition_rfc3339,omitempty"`
}

// LoadFixture reads a Fixture from fixturePath.
func LoadFixture(fixturePath string) (*Fixture, error) {
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		return nil, err
	}
	f := &Fixture{}
	if err := json.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("parse fixture %s: %w", fixturePath, err)
	}
	return f, nil
}

// SaveFixture writes f to fixturePath as indented JSON.
func SaveFixture(fixturePath string, f *Fixture) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(fixturePath, data, 0o644)
}

// TileKey formats a (level, x, y) triple for fixture lookup.
func TileKey(level, x, y int) string {
	return fmt.Sprintf("%d:%d:%d", level, x, y)
}

// TestdataDir returns the directory holding slide files for integration tests.
// Resolved from OPENTILE_TESTDIR env var; empty string means integration tests
// should t.Skip.
func TestdataDir() string { return os.Getenv("OPENTILE_TESTDIR") }
