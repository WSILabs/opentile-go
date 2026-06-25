package ife

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
)

// metadataBuilder lays out a complete IFE file with a populated
// METADATA chain so the parser can be unit-tested without depending
// on the cervix fixture.
type metadataBuilder struct {
	codecMajor, codecMinor, codecBuild uint16
	mpp                                float32
	magnification                      float32
	attrs                              []kv         // ATTRIBUTES (FREE_TEXT) — empty means omit the block
	images                             []synthImage // IMAGE_ARRAY
	icc                                []byte       // ICC_PROFILE bytes
	// layers is the LAYER_EXTENTS ladder, COARSEST-FIRST (ascending scale,
	// as the file stores it). Empty defaults to a single 1×1 scale-1 layer
	// (so the metadata-only tests that don't care about the pyramid are
	// unaffected). The finest layer's scale is the convention's max_scale.
	// Reuses synthLayer from synthetic_test.go; its tiles field is unused
	// here (every tile points at one shared blob).
	layers []synthLayer
}

type kv struct {
	K, V string
}

type synthImage struct {
	W, H       uint32
	Encoding   uint8 // 1=PNG, 2=JPEG, 3=AVIF
	Title      string
	ImageBytes []byte
}

// build returns a complete IFE file with one tiny pyramid level + a
// fully populated METADATA chain. Layout:
//
//	[FILE_HEADER][TILE_TABLE][LAYER_EXTENTS][TILE_OFFSETS][tile bytes]
//	[METADATA][ATTRIBUTES][ATTRIBUTES_SIZES][ATTRIBUTES_BYTES]
//	[IMAGE_ARRAY][IMAGE_BYTES per image][ICC_PROFILE]
//
// Each block lays out at a fixed offset so the test can inspect the
// raw bytes when something goes wrong. Tile bytes are arbitrary
// recognizable patterns; no real codec is invoked.
func (mb *metadataBuilder) build() []byte {
	layers := mb.layers
	if len(layers) == 0 {
		layers = []synthLayer{{xTiles: 1, yTiles: 1, scale: 1}}
	}
	var totalTiles uint64
	for _, ly := range layers {
		totalTiles += uint64(ly.xTiles) * uint64(ly.yTiles)
	}

	const ttOff = uint64(fileHeaderSize)
	leOff := ttOff + uint64(tileTableSize)
	leSize := uint64(blockHeaderValidation) + uint64(len(layers))*uint64(layerExtentEntrySize)
	toOff := leOff + leSize
	toSize := uint64(blockHeaderValidation) + totalTiles*uint64(tileEntrySize)
	tileBytesOff := toOff + toSize
	tileBytes := []byte("TILE")
	mdOff := tileBytesOff + uint64(len(tileBytes))

	// METADATA at mdOff. Compute sub-block offsets.
	attrsOff := mdOff + uint64(metadataBlockSize)
	szOff := attrsOff + uint64(attributesBlockSize)
	szBodySize := uint64(len(mb.attrs)) * uint64(attributesSizesEntry)
	bytesOff := szOff + uint64(blockHeaderValidation) + szBodySize
	var attrsBodySize uint64
	for _, kv := range mb.attrs {
		attrsBodySize += uint64(len(kv.K)) + uint64(len(kv.V))
	}
	imagesOff := bytesOff + uint64(attributesBytesHeaderSize) + attrsBodySize
	imagesBodySize := uint64(len(mb.images)) * uint64(imageEntrySize)
	imageBytesStart := imagesOff + uint64(blockHeaderValidation) + imagesBodySize
	imageEntries := make([]uint64, len(mb.images)) // bytes_offset for each image
	cursor := imageBytesStart
	for i, img := range mb.images {
		imageEntries[i] = cursor
		cursor += uint64(imageBytesHeaderSize) + uint64(len(img.Title)) + uint64(len(img.ImageBytes))
	}
	iccOff := cursor

	totalSize := iccOff
	if mb.icc != nil {
		totalSize += uint64(iccHeaderSize) + uint64(len(mb.icc))
	}

	// If a block is omitted, NULL its offset.
	var attrsOffOut, imagesOffOut, iccOffOut uint64
	if len(mb.attrs) == 0 {
		attrsOffOut = NullOffset
	} else {
		attrsOffOut = attrsOff
	}
	if len(mb.images) == 0 {
		imagesOffOut = NullOffset
	} else {
		imagesOffOut = imagesOff
	}
	if mb.icc == nil {
		iccOffOut = NullOffset
	} else {
		iccOffOut = iccOff
	}

	out := make([]byte, totalSize)

	// FILE_HEADER.
	binary.LittleEndian.PutUint32(out[0:4], MagicBytes)
	binary.LittleEndian.PutUint64(out[6:14], totalSize)
	binary.LittleEndian.PutUint16(out[14:16], 1)
	binary.LittleEndian.PutUint64(out[22:30], ttOff)
	binary.LittleEndian.PutUint64(out[30:38], mdOff)

	// TILE_TABLE.
	tt := out[ttOff : ttOff+tileTableSize]
	binary.LittleEndian.PutUint64(tt[0:8], ttOff)
	tt[10] = encodingJPEG
	tt[11] = 3
	binary.LittleEndian.PutUint64(tt[12:20], NullOffset)
	binary.LittleEndian.PutUint64(tt[20:28], toOff)
	binary.LittleEndian.PutUint64(tt[28:36], leOff)

	// LAYER_EXTENTS — coarsest-first (ascending Scale).
	le := out[leOff : leOff+leSize]
	binary.LittleEndian.PutUint64(le[0:8], leOff)
	binary.LittleEndian.PutUint16(le[10:12], layerExtentEntrySize)
	binary.LittleEndian.PutUint32(le[12:16], uint32(len(layers)))
	for i, ly := range layers {
		b := blockHeaderValidation + i*int(layerExtentEntrySize)
		binary.LittleEndian.PutUint32(le[b:b+4], ly.xTiles)
		binary.LittleEndian.PutUint32(le[b+4:b+8], ly.yTiles)
		binary.LittleEndian.PutUint32(le[b+8:b+12], math.Float32bits(ly.scale))
	}

	// TILE_OFFSETS — one entry per tile across all layers; every entry
	// points at the same shared tile blob (the metadata tests never decode).
	to := out[toOff : toOff+toSize]
	binary.LittleEndian.PutUint64(to[0:8], toOff)
	binary.LittleEndian.PutUint16(to[10:12], tileEntrySize)
	binary.LittleEndian.PutUint32(to[12:16], uint32(totalTiles))
	body := to[blockHeaderValidation:]
	for k := uint64(0); k < totalTiles; k++ {
		o := k * uint64(tileEntrySize)
		body[o+0] = byte(tileBytesOff)
		body[o+1] = byte(tileBytesOff >> 8)
		body[o+2] = byte(tileBytesOff >> 16)
		body[o+3] = byte(tileBytesOff >> 24)
		body[o+4] = byte(tileBytesOff >> 32)
		body[o+5] = byte(len(tileBytes))
		body[o+6] = byte(len(tileBytes) >> 8)
		body[o+7] = byte(len(tileBytes) >> 16)
	}

	copy(out[tileBytesOff:], tileBytes)

	// METADATA.
	md := out[mdOff : mdOff+uint64(metadataBlockSize)]
	binary.LittleEndian.PutUint64(md[0:8], mdOff)
	binary.LittleEndian.PutUint16(md[8:10], recoverMetadata)
	binary.LittleEndian.PutUint16(md[10:12], mb.codecMajor)
	binary.LittleEndian.PutUint16(md[12:14], mb.codecMinor)
	binary.LittleEndian.PutUint16(md[14:16], mb.codecBuild)
	binary.LittleEndian.PutUint64(md[16:24], attrsOffOut)
	binary.LittleEndian.PutUint64(md[24:32], imagesOffOut)
	binary.LittleEndian.PutUint64(md[32:40], iccOffOut)
	binary.LittleEndian.PutUint64(md[40:48], NullOffset) // annotations
	binary.LittleEndian.PutUint32(md[48:52], math.Float32bits(mb.mpp))
	binary.LittleEndian.PutUint32(md[52:56], math.Float32bits(mb.magnification))

	// ATTRIBUTES.
	if len(mb.attrs) > 0 {
		ab := out[attrsOff : attrsOff+uint64(attributesBlockSize)]
		binary.LittleEndian.PutUint64(ab[0:8], attrsOff)
		binary.LittleEndian.PutUint16(ab[8:10], recoverAttributes)
		ab[10] = uint8(AttributesFormatFreeText)
		binary.LittleEndian.PutUint16(ab[11:13], 0)
		binary.LittleEndian.PutUint64(ab[13:21], szOff)
		binary.LittleEndian.PutUint64(ab[21:29], bytesOff)

		// ATTRIBUTES_SIZES.
		sb := out[szOff : szOff+uint64(blockHeaderValidation)+szBodySize]
		binary.LittleEndian.PutUint64(sb[0:8], szOff)
		binary.LittleEndian.PutUint16(sb[8:10], recoverAttributesSizes)
		binary.LittleEndian.PutUint16(sb[10:12], attributesSizesEntry)
		binary.LittleEndian.PutUint32(sb[12:16], uint32(len(mb.attrs)))
		for i, kv := range mb.attrs {
			b := blockHeaderValidation + i*int(attributesSizesEntry)
			binary.LittleEndian.PutUint16(sb[b:b+2], uint16(len(kv.K)))
			binary.LittleEndian.PutUint32(sb[b+2:b+6], uint32(len(kv.V)))
		}

		// ATTRIBUTES_BYTES.
		bb := out[bytesOff : bytesOff+uint64(attributesBytesHeaderSize)+attrsBodySize]
		binary.LittleEndian.PutUint64(bb[0:8], bytesOff)
		binary.LittleEndian.PutUint16(bb[8:10], recoverAttributesBytes)
		binary.LittleEndian.PutUint32(bb[10:14], uint32(len(mb.attrs)))
		bcur := uint64(attributesBytesHeaderSize)
		for _, kv := range mb.attrs {
			copy(bb[bcur:], kv.K)
			bcur += uint64(len(kv.K))
			copy(bb[bcur:], kv.V)
			bcur += uint64(len(kv.V))
		}
	}

	// IMAGE_ARRAY.
	if len(mb.images) > 0 {
		ia := out[imagesOff : imagesOff+uint64(blockHeaderValidation)+imagesBodySize]
		binary.LittleEndian.PutUint64(ia[0:8], imagesOff)
		binary.LittleEndian.PutUint16(ia[8:10], recoverImageArray)
		binary.LittleEndian.PutUint16(ia[10:12], imageEntrySize)
		binary.LittleEndian.PutUint32(ia[12:16], uint32(len(mb.images)))
		for i, img := range mb.images {
			b := blockHeaderValidation + i*int(imageEntrySize)
			binary.LittleEndian.PutUint64(ia[b:b+8], imageEntries[i])
			binary.LittleEndian.PutUint32(ia[b+8:b+12], img.W)
			binary.LittleEndian.PutUint32(ia[b+12:b+16], img.H)
			ia[b+16] = img.Encoding
			ia[b+17] = 3
			binary.LittleEndian.PutUint16(ia[b+18:b+20], 0)
		}

		// IMAGE_BYTES per image.
		for i, img := range mb.images {
			ibOff := imageEntries[i]
			ibSize := uint64(imageBytesHeaderSize) + uint64(len(img.Title)) + uint64(len(img.ImageBytes))
			ib := out[ibOff : ibOff+ibSize]
			binary.LittleEndian.PutUint64(ib[0:8], ibOff)
			binary.LittleEndian.PutUint16(ib[8:10], recoverImageBytes)
			binary.LittleEndian.PutUint16(ib[10:12], uint16(len(img.Title)))
			binary.LittleEndian.PutUint32(ib[12:16], uint32(len(img.ImageBytes)))
			copy(ib[imageBytesHeaderSize:], img.Title)
			copy(ib[uint64(imageBytesHeaderSize)+uint64(len(img.Title)):], img.ImageBytes)
		}
	}

	// ICC_PROFILE.
	if mb.icc != nil {
		ic := out[iccOff:]
		binary.LittleEndian.PutUint64(ic[0:8], iccOff)
		binary.LittleEndian.PutUint16(ic[8:10], recoverICCProfile)
		binary.LittleEndian.PutUint32(ic[10:14], uint32(len(mb.icc)))
		copy(ic[iccHeaderSize:], mb.icc)
	}

	return out
}

func TestMetadataBuilderRoundtrip(t *testing.T) {
	mb := &metadataBuilder{
		codecMajor: 2025, codecMinor: 2, codecBuild: 0,
		mpp:           0.5,
		magnification: 20,
		attrs: []kv{
			{"foo", "bar"},
			{"aperio.AppMag", "40"},
			{"empty.value", ""},
			{"tiff.ImageDescription", "Aperio v1.0\nfoo\n"},
		},
		images: []synthImage{
			{W: 100, H: 50, Encoding: 2, Title: "thumbnail", ImageBytes: []byte("\xff\xd8\xff\xe0FAKE_JPEG")},
			{W: 4096, H: 3000, Encoding: 1, Title: "macro", ImageBytes: []byte("\x89PNG\r\n\x1a\nFAKE_PNG")},
			{W: 2000, H: 800, Encoding: 3, Title: "OVERVIEW", ImageBytes: []byte("FAKE_AVIF")},
		},
		icc: []byte("ICC_PROFILE_BYTES"),
	}
	data := mb.build()
	tiler, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if err != nil {
		t.Fatalf("openIFE: %v", err)
	}
	defer tiler.Close()

	// Cross-format Metadata.
	cm := tiler.Metadata()
	// GH #81: this block's header magnification is 20, but it carries an
	// authoritative aperio.AppMag = 40 attribute → cross.Magnification is the L0
	// 40, and the raw header 20 is preserved in MagnificationFromHeader.
	if cm.Magnification != 40 {
		t.Errorf("Magnification = %v, want 40 (overridden from aperio.AppMag; header was 20)", cm.Magnification)
	}
	if md, ok := MetadataOf(tiler); !ok || md.MagnificationFromHeader != 20 {
		t.Errorf("MagnificationFromHeader = %v (ok=%v), want raw header 20", md.MagnificationFromHeader, ok)
	}
	// MPP: no aperio.MPP and no banner MPP field here, so the header f32 (0.5,
	// exact in IEEE-754) is kept unchanged (the #81 override is per-field).
	if cm.MPP.X != 0.5 || cm.MPP.Y != 0.5 {
		t.Errorf("MPP.X/Y = %v / %v, want 0.5 / 0.5 (no aperio.MPP → header kept)", cm.MPP.X, cm.MPP.Y)
	}
	if cm.MPP.Symmetric() != 0.5 {
		t.Errorf("MPP.Symmetric() = %v, want 0.5 (IFE reports symmetric pixels)", cm.MPP.Symmetric())
	}
	// tiff.ImageDescription attribute → cross.ImageDescription.
	if cm.ImageDescription != "Aperio v1.0\nfoo\n" {
		t.Errorf("cross.ImageDescription = %q, want %q", cm.ImageDescription, "Aperio v1.0\nfoo\n")
	}
	// Vendor passthrough: every IFE attribute under "iris." namespace.
	if got := cm.Properties["iris.foo"]; got != "bar" {
		t.Errorf("Properties[iris.foo] = %q, want %q", got, "bar")
	}
	if got := cm.Properties["iris.aperio.AppMag"]; got != "40" {
		t.Errorf("Properties[iris.aperio.AppMag] = %q, want %q", got, "40")
	}
	// Empty values still surface (the attribute existed; consumers
	// distinguish "absent" vs "empty" via map lookup).
	if got, ok := cm.Properties["iris.empty.value"]; !ok || got != "" {
		t.Errorf("Properties[iris.empty.value]: ok=%v val=%q, want ok=true val=\"\"", ok, got)
	}

	// ICC profile.
	icc := tiler.ICCProfile()
	if string(icc) != "ICC_PROFILE_BYTES" {
		t.Errorf("ICC = %q", icc)
	}

	// Associated images.
	assoc := tiler.AssociatedImages()
	if len(assoc) != 3 {
		t.Fatalf("associated count = %d, want 3", len(assoc))
	}
	wantTypes := []opentile.AssociatedType{opentile.AssociatedThumbnail, opentile.AssociatedMacro, opentile.AssociatedOverview}
	for i, a := range assoc {
		if a.Type() != wantTypes[i] {
			t.Errorf("assoc[%d] type = %q, want %q", i, a.Type(), wantTypes[i])
		}
	}
	// Sizes + compression.
	if assoc[0].Size() != (opentile.Size{W: 100, H: 50}) {
		t.Errorf("assoc[0] size = %v", assoc[0].Size())
	}
	if assoc[0].Compression() != opentile.CompressionJPEG {
		t.Errorf("assoc[0] compression = %v", assoc[0].Compression())
	}
	if assoc[1].Compression() != opentile.CompressionPNG {
		t.Errorf("assoc[1] (PNG) compression = %v, want PNG (#74)", assoc[1].Compression())
	}
	if assoc[2].Compression() != opentile.CompressionAVIF {
		t.Errorf("assoc[2] (AVIF) compression = %v", assoc[2].Compression())
	}
	b, _ := assoc[0].Bytes()
	if !bytes.Equal(b, []byte("\xff\xd8\xff\xe0FAKE_JPEG")) {
		t.Errorf("assoc[0] bytes = %q", b)
	}

	// IFE-specific metadata via MetadataOf.
	ifeMD, ok := MetadataOf(tiler)
	if !ok {
		t.Fatal("MetadataOf returned !ok")
	}
	// v0.17: ifeMD.MPP now resolves through field promotion to the embedded
	// opentile.Metadata.MPP. 0.5 is exact in IEEE-754 binary so f32→f64
	// widening preserves the value.
	if ifeMD.MPP.Symmetric() != 0.5 {
		t.Errorf("MPP.Symmetric() = %v, want 0.5", ifeMD.MPP.Symmetric())
	}
	if ifeMD.MagnificationFromHeader != 20 {
		t.Errorf("Mag(hdr) = %v", ifeMD.MagnificationFromHeader)
	}
	if ifeMD.CodecMajor != 2025 || ifeMD.CodecMinor != 2 || ifeMD.CodecBuild != 0 {
		t.Errorf("codec = %d.%d.%d", ifeMD.CodecMajor, ifeMD.CodecMinor, ifeMD.CodecBuild)
	}
	if ifeMD.AttributesFormat != AttributesFormatFreeText {
		t.Errorf("AttributesFormat = %v", ifeMD.AttributesFormat)
	}
	if got, want := len(ifeMD.Attributes), 4; got != want {
		t.Errorf("attrs count = %d, want %d", got, want)
	}
	if ifeMD.Attributes["foo"] != "bar" {
		t.Errorf("foo = %q", ifeMD.Attributes["foo"])
	}
	if ifeMD.Attributes["aperio.AppMag"] != "40" {
		t.Errorf("aperio.AppMag = %q", ifeMD.Attributes["aperio.AppMag"])
	}
	if v, ok := ifeMD.Attributes["empty.value"]; !ok || v != "" {
		t.Errorf("empty.value: ok=%v val=%q", ok, v)
	}
}

// TestResolutionConventionConformant verifies the Iris resolution convention on
// a spec-CONFORMANT synthetic file (no aperio override). Iris numbers layers in
// REVERSE (layer 0 = lowest resolution) and the METADATA header stores
// scale-relative quantities, NOT absolute L0 values:
//
//   - magnification is a COEFFICIENT: physical_mag(layer) = layer.scale × coefficient.
//   - micronsPerPixel is the MPP at scale 1 (the coarsest layer).
//
// The full-resolution (finest layer) values therefore derive from the largest
// LAYER_EXTENTS scale (max_scale = api[0].Scale):
//
//	full_mag = coefficient × max_scale
//	full_MPP = mpp_at_scale1 / max_scale
//
// Here: 4 layers ×1/×4/×16/×64 (max_scale 64), header coefficient 0.625 and
// scale-1 MPP 16.0 → full-res 40× and 0.25 µm/px. All values are exact in
// IEEE-754 binary32 so the assertions can use ==.
func TestResolutionConventionConformant(t *testing.T) {
	mb := &metadataBuilder{
		codecMajor: 2025, codecMinor: 1, codecBuild: 0,
		mpp:           16.0,  // MPP at scale 1; full-res = 16.0 / 64 = 0.25
		magnification: 0.625, // coefficient; full-res = 0.625 × 64 = 40
		layers: []synthLayer{
			{xTiles: 1, yTiles: 1, scale: 1},  // coarsest (api L3)
			{xTiles: 1, yTiles: 1, scale: 4},  // api L2
			{xTiles: 1, yTiles: 1, scale: 16}, // api L1
			{xTiles: 1, yTiles: 1, scale: 64}, // finest (api L0) → max_scale
		},
		// No aperio.* attributes → the convention is authoritative.
	}
	data := mb.build()
	tiler, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if err != nil {
		t.Fatalf("openIFE: %v", err)
	}
	defer tiler.Close()

	cm := tiler.Metadata()
	if cm.Magnification != 40 {
		t.Errorf("Magnification = %v, want 40 (coefficient 0.625 × max_scale 64)", cm.Magnification)
	}
	if cm.MPP.X != 0.25 || cm.MPP.Y != 0.25 {
		t.Errorf("MPP = %v/%v, want 0.25/0.25 (header 16.0 / max_scale 64)", cm.MPP.X, cm.MPP.Y)
	}

	ifeMD, ok := MetadataOf(tiler)
	if !ok {
		t.Fatal("MetadataOf !ok")
	}
	// The raw header values (coefficient + scale-1 MPP) stay available verbatim.
	if ifeMD.MagnificationFromHeader != 0.625 {
		t.Errorf("MagnificationFromHeader = %v, want raw header coefficient 0.625", ifeMD.MagnificationFromHeader)
	}
	if ifeMD.MPPFromHeader.X != 16.0 || ifeMD.MPPFromHeader.Y != 16.0 {
		t.Errorf("MPPFromHeader = %v/%v, want raw header 16.0/16.0", ifeMD.MPPFromHeader.X, ifeMD.MPPFromHeader.Y)
	}
}

func TestMetadataAbsentBlocks(t *testing.T) {
	// A METADATA block with all sub-blocks NULL'd. Tiler.Metadata
	// returns Magnification from the header; ICCProfile returns nil;
	// Associated returns empty; Attributes is empty.
	mb := &metadataBuilder{
		codecMajor: 1, codecMinor: 0, codecBuild: 0,
		mpp:           0,
		magnification: 0,
	}
	data := mb.build()
	tiler, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if err != nil {
		t.Fatalf("openIFE: %v", err)
	}
	defer tiler.Close()
	if tiler.Metadata().Magnification != 0 {
		t.Errorf("Mag = %v", tiler.Metadata().Magnification)
	}
	if len(tiler.AssociatedImages()) != 0 {
		t.Errorf("associated should be empty")
	}
	if tiler.ICCProfile() != nil {
		t.Errorf("ICC should be nil")
	}
	ifeMD, _ := MetadataOf(tiler)
	if len(ifeMD.Attributes) != 0 {
		t.Errorf("attrs should be empty, got %d", len(ifeMD.Attributes))
	}
	if ifeMD.AttributesFormat != AttributesFormatUndefined {
		t.Errorf("AttributesFormat = %v, want undefined", ifeMD.AttributesFormat)
	}
}

func TestMetadataDicomFormatRejected(t *testing.T) {
	// A METADATA block whose ATTRIBUTES.format = 2 (DICOM) is
	// rejected explicitly so a future fixture surfaces the gap rather
	// than silently mis-parsing.
	mb := &metadataBuilder{attrs: []kv{{"k", "v"}}}
	data := mb.build()
	// Find the ATTRIBUTES block and flip the format byte to 2.
	hdr := make([]byte, 38)
	copy(hdr, data)
	mdOff := binary.LittleEndian.Uint64(hdr[30:38])
	attrsOff := binary.LittleEndian.Uint64(data[mdOff+16 : mdOff+24])
	data[attrsOff+10] = uint8(AttributesFormatDICOM)

	_, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if err == nil || !strings.Contains(err.Error(), "DICOM") {
		t.Errorf("openIFE: got %v, want DICOM-rejection error", err)
	}
}

func TestMetadataWrongRecoveryRejected(t *testing.T) {
	// Set the METADATA recovery byte to a wrong value; openIFE must
	// fail rather than silently parse.
	mb := &metadataBuilder{}
	data := mb.build()
	hdr := make([]byte, 38)
	copy(hdr, data)
	mdOff := binary.LittleEndian.Uint64(hdr[30:38])
	binary.LittleEndian.PutUint16(data[mdOff+8:mdOff+10], 0xDEAD)
	_, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if err == nil || !strings.Contains(err.Error(), "recovery") {
		t.Errorf("got %v, want recovery error", err)
	}
}

func TestNormaliseAssociatedType(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want opentile.AssociatedType
	}{
		{"thumbnail", opentile.AssociatedThumbnail},
		{"Thumbnail", opentile.AssociatedThumbnail},
		{"THUMBNAIL", opentile.AssociatedThumbnail},
		{"label", opentile.AssociatedLabel},
		{"overview", opentile.AssociatedOverview},
		{"macro", opentile.AssociatedMacro},
		{"map", opentile.AssociatedMap},
		{"probability", opentile.AssociatedProbability},
		{"freetext", "freetext"},
		{"Custom Label", "custom label"},
	} {
		if got := normaliseAssociatedType(tc.in); got != tc.want {
			t.Errorf("normaliseAssociatedType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMetadataOfWalksWrappers(t *testing.T) {
	// Pin the unwrap behaviour: opentile.OpenFile returns a
	// *fileCloser wrapper; MetadataOf must walk through it.
	// Synthetic test via a minimal wrapper type.
	mb := &metadataBuilder{codecMajor: 1, magnification: 5}
	data := mb.build()
	t1, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &testWrapper{inner: t1}
	md, ok := MetadataOf(wrapper)
	if !ok {
		t.Fatal("MetadataOf through wrapper: !ok")
	}
	if md.MagnificationFromHeader != 5 {
		t.Errorf("Mag(hdr) = %v", md.MagnificationFromHeader)
	}

	// Non-IFE type returns false.
	type notIFE struct{}
	_, ok = MetadataOf(notIFE{})
	if ok {
		t.Error("MetadataOf on non-IFE type returned true")
	}

	// nil → false (don't panic).
	_, ok = MetadataOf(nil)
	if ok {
		t.Error("MetadataOf(nil) returned true")
	}
}

// testWrapper satisfies the unwrap interface that MetadataOf uses.
type testWrapper struct{ inner any }

func (w *testWrapper) UnwrapReader() any { return w.inner }

// Smoke: errors propagate to compatible errors.Is targets; the
// mismatch errors are bare strings (not wrapped sentinels) by design,
// so this just confirms they remain distinct from existing sentinels.
func TestMetadataErrorsAreDistinct(t *testing.T) {
	mb := &metadataBuilder{}
	data := mb.build()
	hdr := make([]byte, 38)
	copy(hdr, data)
	mdOff := binary.LittleEndian.Uint64(hdr[30:38])
	binary.LittleEndian.PutUint64(data[mdOff:mdOff+8], 0xDEADBEEF) // wrong validation
	_, err := openIFE(bytes.NewReader(data), int64(len(data)), &format.Config{})
	if errors.Is(err, opentile.ErrUnsupportedFormat) {
		t.Errorf("validation error wrongly aliased to ErrUnsupportedFormat")
	}
}

// TestCompressionFromImageEncoding pins the IMAGE_ENCODING enum mapping
// (Iris-Headers IrisCodecTypes.hpp): 1=PNG, 2=JPEG, 3=AVIF — distinct from the
// tile ENCODING enum (1=IRIS). GH #74: encoding 1 must map to CompressionPNG
// (was CompressionUnknown).
func TestCompressionFromImageEncoding(t *testing.T) {
	cases := []struct {
		e    uint8
		want opentile.Compression
		err  bool
	}{
		{1, opentile.CompressionPNG, false},
		{2, opentile.CompressionJPEG, false},
		{3, opentile.CompressionAVIF, false},
		{0, opentile.CompressionUnknown, true}, // ENCODING_UNDEFINED is invalid
		{9, opentile.CompressionUnknown, true}, // unknown value
	}
	for _, c := range cases {
		got, err := compressionFromImageEncoding(c.e)
		if c.err {
			if err == nil {
				t.Errorf("encoding %d: want error, got %v", c.e, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("encoding %d = (%v, %v), want (%v, nil)", c.e, got, err, c.want)
		}
	}
}

// GH #81: prefer the source scanner's authoritative L0 AppMag/MPP from
// ATTRIBUTES over a downsampled-level value in the METADATA header.
func TestAperioL0Resolution(t *testing.T) {
	t.Run("discrete aperio keys", func(t *testing.T) {
		kvs := map[string]string{"aperio.AppMag": "40", "aperio.MPP": "0.262968"}
		mag, mpp, okMag, okMPP := aperioL0Resolution(kvs, "")
		if !okMag || mag != 40 {
			t.Errorf("AppMag = %v (ok=%v), want 40", mag, okMag)
		}
		if !okMPP || mpp != 0.262968 {
			t.Errorf("MPP = %v (ok=%v), want 0.262968", mpp, okMPP)
		}
	})
	t.Run("ImageDescription banner fallback", func(t *testing.T) {
		desc := "Aperio Leica Biosystems GT450|AppMag = 40|MPP = 0.262968|ScannerType = GT450"
		mag, mpp, okMag, okMPP := aperioL0Resolution(nil, desc)
		if !okMag || mag != 40 || !okMPP || mpp != 0.262968 {
			t.Errorf("banner parse = %v/%v (ok %v/%v), want 40 / 0.262968", mag, mpp, okMag, okMPP)
		}
	})
	t.Run("discrete keys win over banner", func(t *testing.T) {
		kvs := map[string]string{"aperio.AppMag": "20", "aperio.MPP": "0.5"}
		mag, mpp, _, _ := aperioL0Resolution(kvs, "AppMag = 40|MPP = 0.25")
		if mag != 20 || mpp != 0.5 {
			t.Errorf("= %v/%v, want discrete 20/0.5", mag, mpp)
		}
	})
	t.Run("no authoritative source", func(t *testing.T) {
		_, _, okMag, okMPP := aperioL0Resolution(map[string]string{"foo": "bar"}, "no fields here")
		if okMag || okMPP {
			t.Errorf("want all-false when no AppMag/MPP present, got ok %v/%v", okMag, okMPP)
		}
	})
	t.Run("rejects zero/garbage", func(t *testing.T) {
		kvs := map[string]string{"aperio.AppMag": "0", "aperio.MPP": "abc"}
		_, _, okMag, okMPP := aperioL0Resolution(kvs, "")
		if okMag || okMPP {
			t.Errorf("want all-false for 0/garbage, got ok %v/%v", okMag, okMPP)
		}
	})
}
