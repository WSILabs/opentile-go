package bifxml_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wsilabs/opentile-go/internal/bifxml"
)

// testdata loads a file from the testdata/ subdirectory.
func testdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("testdata: read %q: %v", name, err)
	}
	return b
}

// ── ParseIScan ────────────────────────────────────────────────────────────────

// TestParseIScan_Ventana1 covers the spec-compliant fixture (Ventana-1 IFD 0).
// The XMP wraps <iScan> inside <Metadata>; all typed attributes are present.
func TestParseIScan_Ventana1(t *testing.T) {
	xmp := testdata(t, "ventana1_iscan.xml")
	got, err := bifxml.ParseIScan(xmp)
	if err != nil {
		t.Fatalf("ParseIScan: %v", err)
	}

	if got.ScannerModel != "VENTANA DP 200" {
		t.Errorf("ScannerModel = %q; want %q", got.ScannerModel, "VENTANA DP 200")
	}
	if got.Magnification != 40 {
		t.Errorf("Magnification = %v; want 40", got.Magnification)
	}
	if got.ScanRes != 0.25 {
		t.Errorf("ScanRes = %v; want 0.25", got.ScanRes)
	}
	if !got.ScanWhitePointPresent {
		t.Error("ScanWhitePointPresent should be true")
	}
	if got.ScanWhitePoint != 235 {
		t.Errorf("ScanWhitePoint = %d; want 235", got.ScanWhitePoint)
	}
	if got.ZLayers != 1 {
		t.Errorf("ZLayers = %d; want 1", got.ZLayers)
	}
	if got.ZSpacing != 1 {
		t.Errorf("ZSpacing = %v; want 1 (per Ventana-1 fixture XMP)", got.ZSpacing)
	}
	if got.BuildVersion != "1.1.0.15854" {
		t.Errorf("BuildVersion = %q; want %q", got.BuildVersion, "1.1.0.15854")
	}
	if got.BuildDate != "11/27/2019 11:6:28 AM" {
		t.Errorf("BuildDate = %q; want %q", got.BuildDate, "11/27/2019 11:6:28 AM")
	}
	if got.UnitNumber != "2000515" {
		t.Errorf("UnitNumber = %q; want %q", got.UnitNumber, "2000515")
	}
	if got.UserName != "Operator" {
		t.Errorf("UserName = %q; want %q", got.UserName, "Operator")
	}
	// One <AOI0> child
	if len(got.AOIs) != 1 {
		t.Fatalf("len(AOIs) = %d; want 1", len(got.AOIs))
	}
	aoi := got.AOIs[0]
	if aoi.Index != 0 || aoi.Left != 297 || aoi.Top != 2323 || aoi.Right != 574 || aoi.Bottom != 2069 {
		t.Errorf("AOI0 = %+v; want {Index:0 Left:297 Top:2323 Right:574 Bottom:2069}", aoi)
	}
	// RawAttributes must contain the surplus iScan attributes
	if _, ok := got.RawAttributes["SlideAnnotation"]; !ok {
		t.Error("RawAttributes missing SlideAnnotation")
	}
	if _, ok := got.RawAttributes["ScanMode"]; !ok {
		t.Error("RawAttributes missing ScanMode")
	}
}

// TestParseIScan_OS1 covers the legacy fixture (OS-1 IFD 0).
// The XMP is a bare <iScan> root; ScannerModel and ScanWhitePoint are absent.
func TestParseIScan_OS1(t *testing.T) {
	xmp := testdata(t, "os1_iscan.xml")
	got, err := bifxml.ParseIScan(xmp)
	if err != nil {
		t.Fatalf("ParseIScan: %v", err)
	}

	if got.ScannerModel != "" {
		t.Errorf("ScannerModel = %q; want empty (missing)", got.ScannerModel)
	}
	if got.Magnification != 40 {
		t.Errorf("Magnification = %v; want 40", got.Magnification)
	}
	// ScanRes in OS-1 is "0.232500"
	if got.ScanRes < 0.2324 || got.ScanRes > 0.2326 {
		t.Errorf("ScanRes = %v; want ~0.2325", got.ScanRes)
	}
	// ScanWhitePoint absent → zero-value, Present = false
	if got.ScanWhitePointPresent {
		t.Error("ScanWhitePointPresent should be false for OS-1")
	}
	if got.ScanWhitePoint != 0 {
		t.Errorf("ScanWhitePoint = %d; want 0 (attribute absent)", got.ScanWhitePoint)
	}
	if got.ZLayers != 1 {
		t.Errorf("ZLayers = %d; want 1", got.ZLayers)
	}
	if got.BuildVersion != "3.3.1.1" {
		t.Errorf("BuildVersion = %q; want %q", got.BuildVersion, "3.3.1.1")
	}
	// One AOI child
	if len(got.AOIs) != 1 {
		t.Fatalf("len(AOIs) = %d; want 1", len(got.AOIs))
	}
	aoi := got.AOIs[0]
	if aoi.Index != 0 || aoi.Left != 97 || aoi.Top != 471 || aoi.Right != 941 || aoi.Bottom != 1431 {
		t.Errorf("AOI0 = %+v; want {Index:0 Left:97 Top:471 Right:941 Bottom:1431}", aoi)
	}
}

// TestParseIScan_MultiAOI verifies that ordinal AOI<N> elements are collected
// and sorted by index when multiple AOIs are present.
func TestParseIScan_MultiAOI(t *testing.T) {
	const xmp = `<iScan Magnification="20" ScanRes="0.5" BuildVersion="1.0">
  <AOI2 Left="10" Top="20" Right="30" Bottom="40"/>
  <AOI0 Left="1"  Top="2"  Right="3"  Bottom="4"/>
  <AOI1 Left="5"  Top="6"  Right="7"  Bottom="8"/>
</iScan>`
	got, err := bifxml.ParseIScan([]byte(xmp))
	if err != nil {
		t.Fatalf("ParseIScan: %v", err)
	}
	// Parser collects in document order; indices should be 2, 0, 1.
	if len(got.AOIs) != 3 {
		t.Fatalf("len(AOIs) = %d; want 3", len(got.AOIs))
	}
	if got.AOIs[0].Index != 2 || got.AOIs[1].Index != 0 || got.AOIs[2].Index != 1 {
		t.Errorf("AOI indices = %d %d %d; want 2 0 1", got.AOIs[0].Index, got.AOIs[1].Index, got.AOIs[2].Index)
	}
}

// TestParseIScan_Malformed verifies that a completely malformed XMP does not
// panic and returns an (empty) IScan rather than an error (lenient parsing).
func TestParseIScan_Malformed(t *testing.T) {
	got, err := bifxml.ParseIScan([]byte("not xml at all <<<"))
	// Lenient: we return what we have, no error on malformed XML
	if err != nil {
		t.Fatalf("expected no error for malformed input, got: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil IScan for malformed input")
	}
}

// TestParseIScan_ScanWhitePointPresentZero verifies that an explicit
// ScanWhitePoint="0" attribute sets Present=true and Value=0.
func TestParseIScan_ScanWhitePointPresentZero(t *testing.T) {
	const xmp = `<iScan Magnification="40" ScanRes="0.25" ScanWhitePoint="0"/>`
	got, err := bifxml.ParseIScan([]byte(xmp))
	if err != nil {
		t.Fatalf("ParseIScan: %v", err)
	}
	if !got.ScanWhitePointPresent {
		t.Error("ScanWhitePointPresent should be true for ScanWhitePoint=\"0\"")
	}
	if got.ScanWhitePoint != 0 {
		t.Errorf("ScanWhitePoint = %d; want 0", got.ScanWhitePoint)
	}
}

// TestParseIScan_ZSpacing covers the Z-spacing attribute (microns per
// focal plane step) added in v0.7 multi-dim T7. Lenient parsing —
// missing/empty defaults to 0; valid floats round-trip.
func TestParseIScan_ZSpacing(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want float64
	}{
		{"missing", `<iScan Magnification="40"/>`, 0},
		{"zero", `<iScan Z-spacing="0"/>`, 0},
		{"int", `<iScan Z-spacing="2"/>`, 2.0},
		{"float", `<iScan Z-spacing="0.5"/>`, 0.5},
		{"negative", `<iScan Z-spacing="-1.5"/>`, -1.5},
		{"malformed", `<iScan Z-spacing="not-a-number"/>`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bifxml.ParseIScan([]byte(tc.xml))
			if err != nil {
				t.Fatalf("ParseIScan: %v", err)
			}
			if got.ZSpacing != tc.want {
				t.Errorf("ZSpacing = %v; want %v", got.ZSpacing, tc.want)
			}
		})
	}
}

// TestParseIScan_ScanWhitePointOutOfRange verifies that out-of-range values
// are clamped to [0, 255] rather than silently zeroed.
func TestParseIScan_ScanWhitePointOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want uint8
	}{
		{"too-high", `<iScan ScanWhitePoint="300"/>`, 255},
		{"negative", `<iScan ScanWhitePoint="-50"/>`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bifxml.ParseIScan([]byte(tc.xml))
			if err != nil {
				t.Fatalf("ParseIScan: %v", err)
			}
			if !got.ScanWhitePointPresent {
				t.Error("expected Present=true")
			}
			if got.ScanWhitePoint != tc.want {
				t.Errorf("ScanWhitePoint = %d; want %d", got.ScanWhitePoint, tc.want)
			}
		})
	}
}

// ── ParseEncodeInfo ───────────────────────────────────────────────────────────

// TestParseEncodeInfo_Ventana1 covers the spec-compliant fixture (Ventana-1 IFD 2).
// Exercises: AoiInfo tile dims, Direction="LEFT", 922 joints, 483 frames.
func TestParseEncodeInfo_Ventana1(t *testing.T) {
	xmp := testdata(t, "ventana1_encodeinfo.xml")
	got, err := bifxml.ParseEncodeInfo(xmp)
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}

	if got.Ver != 2 {
		t.Errorf("Ver = %d; want 2", got.Ver)
	}

	// AoiInfo
	ai := got.AoiInfo
	if ai.XImageSize != 1024 || ai.YImageSize != 1024 {
		t.Errorf("AoiInfo tile size = %dx%d; want 1024x1024", ai.XImageSize, ai.YImageSize)
	}
	if ai.NumRows != 21 || ai.NumCols != 23 {
		t.Errorf("AoiInfo grid = %dx%d rows/cols; want 21x23", ai.NumRows, ai.NumCols)
	}
	if ai.PosX != 25165 || ai.PosY != 175179 {
		t.Errorf("AoiInfo Pos = %d,%d; want 25165,175179", ai.PosX, ai.PosY)
	}

	// ImageInfos
	if len(got.ImageInfos) != 1 {
		t.Fatalf("len(ImageInfos) = %d; want 1", len(got.ImageInfos))
	}
	ii := got.ImageInfos[0]
	if !ii.AOIScanned {
		t.Error("ImageInfo AOIScanned should be true")
	}
	if ii.AOIIndex != 0 {
		t.Errorf("ImageInfo AOIIndex = %d; want 0", ii.AOIIndex)
	}
	if ii.Width != 1024 || ii.Height != 1024 {
		t.Errorf("ImageInfo tile dims = %dx%d; want 1024x1024", ii.Width, ii.Height)
	}
	if ii.NumRows != 21 || ii.NumCols != 23 {
		t.Errorf("ImageInfo grid = %dx%d; want 21x23", ii.NumRows, ii.NumCols)
	}

	// TileJoints: Ventana-1 has 922 joints, all Direction="LEFT"
	if len(ii.Joints) != 922 {
		t.Errorf("len(Joints) = %d; want 922", len(ii.Joints))
	}
	if len(ii.Joints) > 0 {
		j0 := ii.Joints[0]
		if j0.Direction != "LEFT" {
			t.Errorf("Joints[0].Direction = %q; want %q", j0.Direction, "LEFT")
		}
		if !j0.FlagJoined {
			t.Error("Joints[0].FlagJoined should be true")
		}
		if j0.Tile1 != 1 || j0.Tile2 != 2 {
			t.Errorf("Joints[0] tiles = %d,%d; want 1,2", j0.Tile1, j0.Tile2)
		}
		if j0.OverlapX != 0 || j0.OverlapY != 0 {
			t.Errorf("Joints[0] overlap = %d,%d; want 0,0", j0.OverlapX, j0.OverlapY)
		}
	}

	// Frames: Ventana-1 has 483 frames (21×23)
	if len(ii.Frames) != 483 {
		t.Errorf("len(Frames) = %d; want 483", len(ii.Frames))
	}
	if len(ii.Frames) > 0 {
		f0 := ii.Frames[0]
		if f0.Col != 0 || f0.Row != 0 || f0.Z != 0 {
			t.Errorf("Frames[0] = {Col:%d Row:%d Z:%d}; want {0 0 0}", f0.Col, f0.Row, f0.Z)
		}
	}
	// Check a frame in the second row: XY="0,1" should be at index 23
	if len(ii.Frames) > 23 {
		f := ii.Frames[23]
		if f.Col != 0 || f.Row != 1 {
			t.Errorf("Frames[23] = {Col:%d Row:%d}; want {Col:0 Row:1}", f.Col, f.Row)
		}
	}

	// AoiOrigins
	if len(got.AoiOrigins) != 1 {
		t.Fatalf("len(AoiOrigins) = %d; want 1", len(got.AoiOrigins))
	}
	if got.AoiOrigins[0].Index != 0 || got.AoiOrigins[0].OriginX != 0 || got.AoiOrigins[0].OriginY != 0 {
		t.Errorf("AoiOrigins[0] = %+v; want {Index:0 OriginX:0 OriginY:0}", got.AoiOrigins[0])
	}
}

// TestParseEncodeInfo_OS1 covers the legacy fixture (OS-1 IFD 2).
// OS-1 has no <FrameInfo>/<Frame> elements; tile dims are 1024x1360;
// 17209 joints with both RIGHT and UP directions.
func TestParseEncodeInfo_OS1(t *testing.T) {
	xmp := testdata(t, "os1_encodeinfo.xml")
	got, err := bifxml.ParseEncodeInfo(xmp)
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}

	if got.Ver != 2 {
		t.Errorf("Ver = %d; want 2", got.Ver)
	}

	// AoiInfo: non-square tiles
	ai := got.AoiInfo
	if ai.XImageSize != 1024 || ai.YImageSize != 1360 {
		t.Errorf("AoiInfo tile size = %dx%d; want 1024x1360", ai.XImageSize, ai.YImageSize)
	}
	if ai.NumRows != 75 || ai.NumCols != 116 {
		t.Errorf("AoiInfo grid = %dx%d; want 75x116", ai.NumRows, ai.NumCols)
	}

	// ImageInfos
	if len(got.ImageInfos) != 1 {
		t.Fatalf("len(ImageInfos) = %d; want 1", len(got.ImageInfos))
	}
	ii := got.ImageInfos[0]
	if ii.Width != 1024 || ii.Height != 1360 {
		t.Errorf("ImageInfo tile dims = %dx%d; want 1024x1360", ii.Width, ii.Height)
	}

	// Joints: 17209, mix of RIGHT and UP directions
	if len(ii.Joints) != 17209 {
		t.Errorf("len(Joints) = %d; want 17209", len(ii.Joints))
	}
	rightCount, upCount := 0, 0
	for _, j := range ii.Joints {
		switch j.Direction {
		case "RIGHT":
			rightCount++
		case "UP":
			upCount++
		}
	}
	if rightCount != 8625 {
		t.Errorf("RIGHT joint count = %d; want 8625", rightCount)
	}
	if upCount != 8584 {
		t.Errorf("UP joint count = %d; want 8584", upCount)
	}

	// No frames in OS-1
	if len(ii.Frames) != 0 {
		t.Errorf("len(Frames) = %d; want 0 (OS-1 has no FrameInfo)", len(ii.Frames))
	}

	// AoiOrigins
	if len(got.AoiOrigins) != 1 {
		t.Fatalf("len(AoiOrigins) = %d; want 1", len(got.AoiOrigins))
	}
	if got.AoiOrigins[0].Index != 0 {
		t.Errorf("AoiOrigins[0].Index = %d; want 0", got.AoiOrigins[0].Index)
	}
}

// TestParseEncodeInfo_DirectionPassthrough verifies that all four spec-valid
// directions are stored verbatim, including LEFT and DOWN which openslide rejects.
func TestParseEncodeInfo_DirectionPassthrough(t *testing.T) {
	const xmp = `<?xml version="1.0"?>
<EncodeInfo Ver="2">
  <SlideStitchInfo>
    <ImageInfo AOIScanned="1" Width="512" Height="512" NumRows="2" NumCols="2">
      <TileJointInfo FlagJoined="1" Direction="LEFT"  Tile1="1" Tile2="2" OverlapX="4" OverlapY="0"/>
      <TileJointInfo FlagJoined="1" Direction="RIGHT" Tile1="2" Tile2="3" OverlapX="4" OverlapY="0"/>
      <TileJointInfo FlagJoined="1" Direction="UP"    Tile1="3" Tile2="4" OverlapX="0" OverlapY="4"/>
      <TileJointInfo FlagJoined="1" Direction="DOWN"  Tile1="4" Tile2="5" OverlapX="0" OverlapY="4"/>
      <TileJointInfo FlagJoined="0" Direction="WEIRD" Tile1="5" Tile2="6" OverlapX="0" OverlapY="0"/>
    </ImageInfo>
  </SlideStitchInfo>
</EncodeInfo>`
	got, err := bifxml.ParseEncodeInfo([]byte(xmp))
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}
	if len(got.ImageInfos) != 1 {
		t.Fatalf("len(ImageInfos) = %d; want 1", len(got.ImageInfos))
	}
	joints := got.ImageInfos[0].Joints
	if len(joints) != 5 {
		t.Fatalf("len(Joints) = %d; want 5", len(joints))
	}
	wantDirs := []string{"LEFT", "RIGHT", "UP", "DOWN", "WEIRD"}
	wantOverlapX := []int{4, 4, 0, 0, 0}
	for i, j := range joints {
		if j.Direction != wantDirs[i] {
			t.Errorf("Joints[%d].Direction = %q; want %q", i, j.Direction, wantDirs[i])
		}
		if j.OverlapX != wantOverlapX[i] {
			t.Errorf("Joints[%d].OverlapX = %d; want %d", i, j.OverlapX, wantOverlapX[i])
		}
	}
}

// TestParseEncodeInfo_VerTooLow verifies that Ver < 2 returns an error.
func TestParseEncodeInfo_VerTooLow(t *testing.T) {
	const xmp = `<?xml version="1.0"?><EncodeInfo Ver="1"><SlideStitchInfo/></EncodeInfo>`
	_, err := bifxml.ParseEncodeInfo([]byte(xmp))
	if err == nil {
		t.Fatal("expected error for Ver=1, got nil")
	}
}

// TestParseEncodeInfo_MissingVer verifies that a missing/zero Ver attribute
// returns an error (0 < 2).
func TestParseEncodeInfo_MissingVer(t *testing.T) {
	const xmp = `<?xml version="1.0"?><EncodeInfo><SlideStitchInfo/></EncodeInfo>`
	_, err := bifxml.ParseEncodeInfo([]byte(xmp))
	if err == nil {
		t.Fatal("expected error for missing Ver, got nil")
	}
}

// TestParseEncodeInfo_FrameXYParsing verifies Frame XY="col,row" splitting.
func TestParseEncodeInfo_FrameXYParsing(t *testing.T) {
	const xmp = `<?xml version="1.0"?>
<EncodeInfo Ver="2">
  <SlideStitchInfo>
    <ImageInfo AOIScanned="1" Width="256" Height="256" NumRows="3" NumCols="3">
      <FrameInfo AOIScanned="1" AOIIndex="0">
        <Frame XY="0,0" Z="0" FocusZ="0"/>
        <Frame XY="2,1" Z="0" FocusZ="0"/>
        <Frame XY="1,2" Z="1" FocusZ="0"/>
      </FrameInfo>
    </ImageInfo>
  </SlideStitchInfo>
</EncodeInfo>`
	got, err := bifxml.ParseEncodeInfo([]byte(xmp))
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}
	frames := got.ImageInfos[0].Frames
	if len(frames) != 3 {
		t.Fatalf("len(Frames) = %d; want 3", len(frames))
	}
	tests := []bifxml.Frame{
		{Col: 0, Row: 0, Z: 0},
		{Col: 2, Row: 1, Z: 0},
		{Col: 1, Row: 2, Z: 1},
	}
	for i, want := range tests {
		if frames[i] != want {
			t.Errorf("Frames[%d] = %+v; want %+v", i, frames[i], want)
		}
	}
}

// TestParseEncodeInfo_MultiAoiOrigin verifies that multiple AOI<N> origin
// elements are all collected.
func TestParseEncodeInfo_MultiAoiOrigin(t *testing.T) {
	const xmp = `<?xml version="1.0"?>
<EncodeInfo Ver="2">
  <SlideStitchInfo/>
  <AoiOrigin>
    <AOI0 OriginX="0"    OriginY="0"/>
    <AOI1 OriginX="1024" OriginY="0"/>
    <AOI2 OriginX="0"    OriginY="1024"/>
  </AoiOrigin>
</EncodeInfo>`
	got, err := bifxml.ParseEncodeInfo([]byte(xmp))
	if err != nil {
		t.Fatalf("ParseEncodeInfo: %v", err)
	}
	if len(got.AoiOrigins) != 3 {
		t.Fatalf("len(AoiOrigins) = %d; want 3", len(got.AoiOrigins))
	}
	want := []bifxml.AoiOrigin{
		{Index: 0, OriginX: 0, OriginY: 0},
		{Index: 1, OriginX: 1024, OriginY: 0},
		{Index: 2, OriginX: 0, OriginY: 1024},
	}
	for i, w := range want {
		if got.AoiOrigins[i] != w {
			t.Errorf("AoiOrigins[%d] = %+v; want %+v", i, got.AoiOrigins[i], w)
		}
	}
}
