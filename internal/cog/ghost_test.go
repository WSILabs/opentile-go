package cog

import (
	"errors"
	"strings"
	"testing"
)

const happyGhost = "GDAL_STRUCTURAL_METADATA_SIZE=000159 bytes\n" +
	"LAYOUT=IFDS_BEFORE_DATA\n" +
	"BLOCK_ORDER=ROW_MAJOR\n" +
	"BLOCK_LEADER=SIZE_AS_UINT4\n" +
	"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
	"KNOWN_INCOMPATIBLE_EDITION=NO\n" +
	"COG_WSI_VERSION=0.1\n"

func TestParseGhostArea_HappyPath(t *testing.T) {
	g, err := ParseGhostArea([]byte(happyGhost))
	if err != nil {
		t.Fatalf("ParseGhostArea: %v", err)
	}
	if g.Layout != "IFDS_BEFORE_DATA" {
		t.Errorf("Layout = %q, want IFDS_BEFORE_DATA", g.Layout)
	}
	if g.BlockOrder != "ROW_MAJOR" {
		t.Errorf("BlockOrder = %q", g.BlockOrder)
	}
	if g.COGWSIVersion != "0.1" {
		t.Errorf("COGWSIVersion = %q, want 0.1", g.COGWSIVersion)
	}
	if g.SizeBytes != 159 {
		t.Errorf("SizeBytes = %d, want 159", g.SizeBytes)
	}
	if len(g.RawKeys) != 6 {
		t.Errorf("RawKeys count = %d, want 6", len(g.RawKeys))
	}
}

func TestParseGhostArea_PlainCOG(t *testing.T) {
	// No COG_WSI_VERSION line — represents a plain (non-WSI) COG.
	const plain = "GDAL_STRUCTURAL_METADATA_SIZE=000139 bytes\n" +
		"LAYOUT=IFDS_BEFORE_DATA\n" +
		"BLOCK_ORDER=ROW_MAJOR\n" +
		"BLOCK_LEADER=SIZE_AS_UINT4\n" +
		"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
		"KNOWN_INCOMPATIBLE_EDITION=NO\n"
	g, err := ParseGhostArea([]byte(plain))
	if err != nil {
		t.Fatalf("ParseGhostArea: %v", err)
	}
	if g.COGWSIVersion != "" {
		t.Errorf("COGWSIVersion = %q, want empty (plain COG)", g.COGWSIVersion)
	}
}

func TestParseGhostArea_UnknownKey(t *testing.T) {
	withUnknown := strings.TrimRight(happyGhost, "\n") +
		"\nFUTURE_KEY=somevalue\n"
	// Adjust size header to include the new line. For test simplicity,
	// craft a fresh ghost area with corrected size.
	const data = "GDAL_STRUCTURAL_METADATA_SIZE=000180 bytes\n" +
		"LAYOUT=IFDS_BEFORE_DATA\n" +
		"BLOCK_ORDER=ROW_MAJOR\n" +
		"BLOCK_LEADER=SIZE_AS_UINT4\n" +
		"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
		"KNOWN_INCOMPATIBLE_EDITION=NO\n" +
		"COG_WSI_VERSION=0.1\n" +
		"FUTURE_KEY=somevalue\n"
	g, err := ParseGhostArea([]byte(data))
	if err != nil {
		t.Fatalf("ParseGhostArea: %v", err)
	}
	if got := g.RawKeys["FUTURE_KEY"]; got != "somevalue" {
		t.Errorf("RawKeys[FUTURE_KEY] = %q, want somevalue", got)
	}
	// Required keys still parsed despite unknown key.
	if g.Layout != "IFDS_BEFORE_DATA" {
		t.Errorf("Layout = %q", g.Layout)
	}
	_ = withUnknown // suppress unused (kept for clarity)
}

func TestParseGhostArea_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"no size header", "LAYOUT=IFDS_BEFORE_DATA\n"},
		{"unterminated size header", "GDAL_STRUCTURAL_METADATA_SIZE=000010 bytes"},
		{"missing ' bytes' suffix", "GDAL_STRUCTURAL_METADATA_SIZE=10\nLAYOUT=IFDS_BEFORE_DATA\n"},
		{"invalid size", "GDAL_STRUCTURAL_METADATA_SIZE=NOTANUM bytes\nLAYOUT=IFDS_BEFORE_DATA\n"},
		{"declared size exceeds data",
			"GDAL_STRUCTURAL_METADATA_SIZE=999999 bytes\nLAYOUT=IFDS_BEFORE_DATA\n"},
		{"missing LAYOUT",
			"GDAL_STRUCTURAL_METADATA_SIZE=000110 bytes\n" +
				"BLOCK_ORDER=ROW_MAJOR\n" +
				"BLOCK_LEADER=SIZE_AS_UINT4\n" +
				"BLOCK_TRAILER=LAST_4_BYTES_REPEATED\n" +
				"KNOWN_INCOMPATIBLE_EDITION=NO\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseGhostArea([]byte(tc.data))
			if err == nil {
				t.Error("want error, got nil")
				return
			}
			if !errors.Is(err, ErrGhostAreaMalformed) {
				t.Errorf("err = %v, want ErrGhostAreaMalformed", err)
			}
		})
	}
}

func TestParseCOGWSIVersion(t *testing.T) {
	for _, tc := range []struct {
		input                string
		wantMajor, wantMinor int
		wantErr              bool
	}{
		{"0.1", 0, 1, false},
		{"0.2", 0, 2, false},
		{"1.0", 1, 0, false},
		{"10.20", 10, 20, false},
		{"", 0, 0, true},
		{"abc", 0, 0, true},
		{"1", 0, 0, true}, // missing minor
		{"1.x", 0, 0, true},
		{"x.1", 0, 0, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			maj, min, err := ParseCOGWSIVersion(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil {
				if maj != tc.wantMajor || min != tc.wantMinor {
					t.Errorf("got %d.%d, want %d.%d", maj, min, tc.wantMajor, tc.wantMinor)
				}
			}
		})
	}
}
