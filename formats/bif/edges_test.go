package bif

import (
	"bytes"
	"testing"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// TestOpenRejectsEncodeInfoVerBelow2: per BIF whitepaper §"IFD 2",
// EncodeInfo @Ver must be ≥ 2. Synthetic fixture with @Ver=1 must
// fail Open.
func TestOpenRejectsEncodeInfoVerBelow2(t *testing.T) {
	encodeInfoV1 := []byte(`<EncodeInfo Ver="1"><SlideInfo/></EncodeInfo>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{
			xmp:         encodeInfoV1,
			description: "level=0 mag=40 quality=95",
			imageWidth:  64, imageLength: 64, tileWidth: 64, tileLength: 64,
		},
	})
	f, err := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("tiff.Open: %v", err)
	}
	if _, err := New().Open(f, nil); err == nil {
		t.Fatal("Open: got nil err on EncodeInfo Ver=1; want spec-violation error")
	}
}

// TestOpenAcceptsEncodeInfoVer2: the same fixture with @Ver=2 opens
// successfully — pinning that the rejection isn't accidentally
// triggered by valid data.
func TestOpenAcceptsEncodeInfoVer2(t *testing.T) {
	encodeInfoV2 := []byte(`<EncodeInfo Ver="2"><SlideInfo/></EncodeInfo>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200" ScanRes="0.25"/>`), description: "Label_Image"},
		{
			xmp:         encodeInfoV2,
			description: "level=0 mag=40 quality=95",
			imageWidth:  64, imageLength: 64, tileWidth: 64, tileLength: 64,
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if _, err := New().Open(f, nil); err != nil {
		t.Errorf("Open: got %v on EncodeInfo Ver=2, want nil", err)
	}
}

// TestOpenAcceptsMissingEncodeInfo: when level-0 IFD has only
// <iScan> (no <EncodeInfo>), Open should still succeed —
// loadEncodeInfo returns nil and the Ver gate doesn't fire.
func TestOpenAcceptsMissingEncodeInfo(t *testing.T) {
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image"},
		{
			description: "level=0 mag=40 quality=95",
			imageWidth:  64, imageLength: 64, tileWidth: 64, tileLength: 64,
			// no XMP on the level=0 IFD
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if _, err := New().Open(f, nil); err != nil {
		t.Errorf("Open with no EncodeInfo XMP on level-0 IFD: got %v, want nil", err)
	}
}

// TestOpenToleratesAnyDirectionValue: per spec §10 the Direction
// attribute on TileJointInfo can be LEFT / RIGHT / UP / DOWN, but
// future scanners might emit other values. opentile-go is
// permissive — Direction is captured verbatim into bifxml.TileJoint
// and never enum-validated, so Open must succeed regardless.
func TestOpenToleratesAnyDirectionValue(t *testing.T) {
	encodeInfo := []byte(`<EncodeInfo Ver="2">
<SlideStitchInfo>
<ImageInfo Width="64" Height="64" NumRows="1" NumCols="1" AOIIndex="0" AOIScanned="1">
<TileJointInfo FlagJoined="1" Confidence="100" Direction="WEIRD-VALUE" Tile1="1" Tile2="2" OverlapX="0" OverlapY="0"/>
</ImageInfo>
</SlideStitchInfo>
</EncodeInfo>`)
	data := buildBIFLikeBigTIFF(t, []iFDSpec{
		{xmp: []byte(`<iScan ScannerModel="VENTANA DP 200"/>`), description: "Label_Image"},
		{
			xmp:         encodeInfo,
			description: "level=0 mag=40 quality=95",
			imageWidth:  64, imageLength: 64, tileWidth: 64, tileLength: 64,
		},
	})
	f, _ := tiff.Open(bytes.NewReader(data), int64(len(data)))
	if _, err := New().Open(f, nil); err != nil {
		t.Errorf("Open with weird Direction value: got %v, want nil (Direction is verbatim passthrough)", err)
	}
}
