package generictiff

import (
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// stripped builds a non-tiled (TileWidth+TileLength = 0)
// PyramidLevelInfo with the given dims + compression.
func stripped(idx int, w, h uint32, comp uint32) tiff.PyramidLevelInfo {
	return tiff.PyramidLevelInfo{
		Index: idx, Width: w, Height: h,
		Compression: comp, Photometric: 2,
		SamplesPerPixel: 3, BitsPerSample: 8,
	}
}

// tiled builds a 256x256-tile RGB JPEG PyramidLevelInfo.
func tiled(idx int, w, h uint32) tiff.PyramidLevelInfo {
	return tiff.PyramidLevelInfo{
		Index: idx, Width: w, Height: h,
		TileWidth: 256, TileLength: 256,
		Compression: 7, Photometric: 2,
		SamplesPerPixel: 3, BitsPerSample: 8,
	}
}

func TestClassifyAssociated_StrippedLZWLabel(t *testing.T) {
	// CMU-1.svs's actual label: 387×463 LZW, portrait orientation.
	// Heuristic-revision history note: original spec required
	// "width > height"; that's wrong — labels are slide-orientation-
	// dependent. LZW alone is the dominant signal.
	baseline := tiled(0, 46000, 32914)
	for _, ifd := range []tiff.PyramidLevelInfo{
		stripped(4, 387, 463, 5), // CMU-1 actual: portrait
		stripped(4, 463, 387, 5), // hypothetical: landscape
		stripped(4, 800, 800, 5), // hypothetical: square
	} {
		t.Run("", func(t *testing.T) {
			if got := ClassifyAssociated(ifd, baseline); got != TypeLabel {
				t.Errorf("ClassifyAssociated(%dx%d LZW) = %q, want %q",
					ifd.Width, ifd.Height, got, TypeLabel)
			}
		})
	}
}

func TestClassifyAssociated_StrippedJPEGMacro(t *testing.T) {
	// CMU-1.svs's actual macro: 1280×431 JPEG (very wide).
	baseline := tiled(0, 46000, 32914)
	for _, tc := range []struct {
		name string
		ifd  tiff.PyramidLevelInfo
		want string
	}{
		{"actual cmu-1 macro", stripped(5, 1280, 431, 7), TypeOverview}, // ~3:1
		{"portrait macro", stripped(5, 431, 1280, 7), TypeOverview},     // tall macro
		{"2.0 ratio borderline", stripped(5, 2000, 1000, 7), TypeOverview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyAssociated(tc.ifd, baseline); got != tc.want {
				t.Errorf("ClassifyAssociated(%dx%d JPEG) = %q, want %q",
					tc.ifd.Width, tc.ifd.Height, got, tc.want)
			}
		})
	}
}

func TestClassifyAssociated_StrippedJPEGThumbnail(t *testing.T) {
	// CMU-1.svs's thumbnail: 1024×732 JPEG, aspect ~1.4 (under 2.0
	// macro threshold).
	baseline := tiled(0, 46000, 32914)
	for _, tc := range []struct {
		name string
		ifd  tiff.PyramidLevelInfo
		want string
	}{
		{"cmu-1 thumbnail", stripped(1, 1024, 732, 7), TypeThumbnail},
		{"square small", stripped(1, 500, 500, 7), TypeThumbnail},
		{"tiny", stripped(1, 100, 100, 7), TypeThumbnail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyAssociated(tc.ifd, baseline); got != tc.want {
				t.Errorf("ClassifyAssociated(%dx%d JPEG) = %q, want %q",
					tc.ifd.Width, tc.ifd.Height, got, tc.want)
			}
		})
	}
}

func TestClassifyAssociated_TiledMacroByArea(t *testing.T) {
	// Tiled IFD that's between 0.1% and 1% of baseline → macro.
	// Baseline 1024×1024 = ~1M area; 0.5% = ~5000. 70x70 = 4900 →
	// well under 0.5% but above 0.1% (1000). Try 80x80 = 6400.
	baseline := tiled(0, 1024, 1024)
	got := ClassifyAssociated(tiled(3, 80, 80), baseline) // 6400 = 0.6%
	if got != TypeOverview {
		t.Errorf("tiled small (80×80, 0.6%% baseline) = %q, want %q", got, TypeOverview)
	}
}

func TestClassifyAssociated_TiledThumbnailByArea(t *testing.T) {
	// Tiled IFD <0.1% baseline → thumbnail.
	baseline := tiled(0, 1024, 1024)
	got := ClassifyAssociated(tiled(3, 30, 30), baseline) // 900 = 0.086%
	if got != TypeThumbnail {
		t.Errorf("tiled tiny (30×30, 0.086%% baseline) = %q, want %q", got, TypeThumbnail)
	}
}

func TestClassifyAssociated_FallbackToAssociated(t *testing.T) {
	baseline := tiled(0, 46000, 32914)
	for _, tc := range []struct {
		name string
		ifd  tiff.PyramidLevelInfo
	}{
		// Stripped uncompressed (none of the heuristics fire — comp
		// is neither LZW nor JPEG).
		{"stripped uncompressed", stripped(2, 500, 500, 1)},
		// Stripped Deflate (also not LZW or JPEG).
		{"stripped deflate", stripped(2, 500, 500, 8)},
		// Stripped JPEG too big for thumbnail (≥1500 in some axis)
		// AND aspect ratio < 2.0 (so not macro).
		{"stripped JPEG large square", stripped(2, 2000, 2000, 7)},
		// Stripped LZW too big for label.
		{"stripped LZW oversized", stripped(2, 2000, 2000, 5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyAssociated(tc.ifd, baseline); got != TypeAssociated {
				t.Errorf("ClassifyAssociated(%s) = %q, want %q",
					tc.name, got, TypeAssociated)
			}
		})
	}
}

func TestClassifyAssociated_BoundaryThresholds(t *testing.T) {
	baseline := tiled(0, 46000, 32914)
	// Aspect ratio 1.99 (just under 2.0 macro threshold) with the
	// "larger dim" big enough — should fall through to thumbnail
	// because dims are < 1500.
	got := ClassifyAssociated(stripped(2, 998, 500, 7), baseline) // ratio 1.996
	if got != TypeThumbnail {
		t.Errorf("aspect 1.996 = %q, want %q (macro threshold not met)", got, TypeThumbnail)
	}
	// LZW dim exactly 1500 — outside the < 1500 bound, falls through
	// to associated.
	got = ClassifyAssociated(stripped(2, 1500, 500, 5), baseline)
	if got != TypeAssociated {
		t.Errorf("LZW 1500 dim = %q, want %q (label boundary)", got, TypeAssociated)
	}
}

// TestClassifyAssociated_AgainstStrippedSVSCMU1 is the integration-
// level pin: against the actual IFD shapes from the v0.10
// stripped-SVS fixture (sample_files/generic-tiff/CMU-1.stripped.tiff),
// the classifier returns the right type for each associated IFD.
// Doesn't open the file (avoids fixture dependency in unit tests);
// hand-codes the IFD dims from the T2 probe finding.
func TestClassifyAssociated_AgainstStrippedSVSCMU1(t *testing.T) {
	// CMU-1.svs's actual associated image shapes (from T2 probe).
	baseline := tiled(0, 46000, 32914)
	for _, tc := range []struct {
		ifd      tiff.PyramidLevelInfo
		wantType string
		desc     string
	}{
		{stripped(1, 1024, 732, 7), TypeThumbnail, "IFD 1: thumbnail JPEG"},
		{stripped(4, 387, 463, 5), TypeLabel, "IFD 4: label LZW"},
		{stripped(5, 1280, 431, 7), TypeOverview, "IFD 5: macro JPEG"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if got := ClassifyAssociated(tc.ifd, baseline); got != tc.wantType {
				t.Errorf("%s: got %q, want %q", tc.desc, got, tc.wantType)
			}
		})
	}
}
