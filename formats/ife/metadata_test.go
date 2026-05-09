package ife

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	opentile "github.com/cornish/opentile-go"
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
	const ttOff = uint64(fileHeaderSize)
	leOff := ttOff + uint64(tileTableSize)
	leSize := uint64(blockHeaderValidation) + uint64(layerExtentEntrySize)
	toOff := leOff + leSize
	toSize := uint64(blockHeaderValidation) + uint64(tileEntrySize) // 1 tile
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

	// LAYER_EXTENTS — one 1×1 layer, scale 1.
	le := out[leOff : leOff+leSize]
	binary.LittleEndian.PutUint64(le[0:8], leOff)
	binary.LittleEndian.PutUint16(le[10:12], layerExtentEntrySize)
	binary.LittleEndian.PutUint32(le[12:16], 1)
	base := blockHeaderValidation
	binary.LittleEndian.PutUint32(le[base:base+4], 1)
	binary.LittleEndian.PutUint32(le[base+4:base+8], 1)
	binary.LittleEndian.PutUint32(le[base+8:base+12], math.Float32bits(1))

	// TILE_OFFSETS.
	to := out[toOff : toOff+toSize]
	binary.LittleEndian.PutUint64(to[0:8], toOff)
	binary.LittleEndian.PutUint16(to[10:12], tileEntrySize)
	binary.LittleEndian.PutUint32(to[12:16], 1)
	body := to[blockHeaderValidation:]
	body[0] = byte(tileBytesOff)
	body[1] = byte(tileBytesOff >> 8)
	body[2] = byte(tileBytesOff >> 16)
	body[3] = byte(tileBytesOff >> 24)
	body[4] = byte(tileBytesOff >> 32)
	body[5] = byte(len(tileBytes))
	body[6] = byte(len(tileBytes) >> 8)
	body[7] = byte(len(tileBytes) >> 16)

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
		codecMajor:    2025, codecMinor: 2, codecBuild: 0,
		mpp:           0.5,
		magnification: 20,
		attrs: []kv{
			{"foo", "bar"},
			{"aperio.AppMag", "40"},
			{"empty.value", ""},
		},
		images: []synthImage{
			{W: 100, H: 50, Encoding: 2, Title: "thumbnail", ImageBytes: []byte("\xff\xd8\xff\xe0FAKE_JPEG")},
			{W: 4096, H: 3000, Encoding: 1, Title: "macro", ImageBytes: []byte("\x89PNG\r\n\x1a\nFAKE_PNG")},
			{W: 2000, H: 800, Encoding: 3, Title: "OVERVIEW", ImageBytes: []byte("FAKE_AVIF")},
		},
		icc: []byte("ICC_PROFILE_BYTES"),
	}
	data := mb.build()
	tiler, err := openIFE(bytes.NewReader(data), int64(len(data)), &opentile.Config{})
	if err != nil {
		t.Fatalf("openIFE: %v", err)
	}
	defer tiler.Close()

	// Cross-format Metadata.
	cm := tiler.Metadata()
	if cm.Magnification != 20 {
		t.Errorf("Magnification = %v, want 20", cm.Magnification)
	}

	// ICC profile.
	icc := tiler.ICCProfile()
	if string(icc) != "ICC_PROFILE_BYTES" {
		t.Errorf("ICC = %q", icc)
	}

	// Associated images.
	assoc := tiler.Associated()
	if len(assoc) != 3 {
		t.Fatalf("associated count = %d, want 3", len(assoc))
	}
	wantKinds := []string{"thumbnail", "macro", "overview"}
	for i, a := range assoc {
		if a.Type() != wantKinds[i] {
			t.Errorf("assoc[%d] kind = %q, want %q", i, a.Type(), wantKinds[i])
		}
	}
	// Sizes + compression.
	if assoc[0].Size() != (opentile.Size{W: 100, H: 50}) {
		t.Errorf("assoc[0] size = %v", assoc[0].Size())
	}
	if assoc[0].Compression() != opentile.CompressionJPEG {
		t.Errorf("assoc[0] compression = %v", assoc[0].Compression())
	}
	if assoc[1].Compression() != opentile.CompressionUnknown {
		t.Errorf("assoc[1] (PNG) compression = %v, want unknown (no CompressionPNG yet)", assoc[1].Compression())
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
	if ifeMD.MicronsPerPixel != 0.5 {
		t.Errorf("MPP = %v", ifeMD.MicronsPerPixel)
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
	if got, want := len(ifeMD.Attributes), 3; got != want {
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
	tiler, err := openIFE(bytes.NewReader(data), int64(len(data)), &opentile.Config{})
	if err != nil {
		t.Fatalf("openIFE: %v", err)
	}
	defer tiler.Close()
	if tiler.Metadata().Magnification != 0 {
		t.Errorf("Mag = %v", tiler.Metadata().Magnification)
	}
	if len(tiler.Associated()) != 0 {
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

	_, err := openIFE(bytes.NewReader(data), int64(len(data)), &opentile.Config{})
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
	_, err := openIFE(bytes.NewReader(data), int64(len(data)), &opentile.Config{})
	if err == nil || !strings.Contains(err.Error(), "recovery") {
		t.Errorf("got %v, want recovery error", err)
	}
}

func TestNormaliseAssociatedKind(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"thumbnail", "thumbnail"},
		{"Thumbnail", "thumbnail"},
		{"THUMBNAIL", "thumbnail"},
		{"label", "label"},
		{"overview", "overview"},
		{"macro", "macro"},
		{"map", "map"},
		{"probability", "probability"},
		{"freetext", "freetext"},
		{"Custom Label", "custom label"},
	} {
		if got := normaliseAssociatedKind(tc.in); got != tc.want {
			t.Errorf("normaliseAssociatedKind(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMetadataOfWalksWrappers(t *testing.T) {
	// Pin the unwrap behaviour: opentile.OpenFile returns a
	// *fileCloser wrapper; MetadataOf must walk through it.
	// Synthetic test via a minimal wrapper type.
	mb := &metadataBuilder{codecMajor: 1, magnification: 5}
	data := mb.build()
	t1, err := openIFE(bytes.NewReader(data), int64(len(data)), &opentile.Config{})
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &testWrapper{Tiler: t1}
	md, ok := MetadataOf(wrapper)
	if !ok {
		t.Fatal("MetadataOf through wrapper: !ok")
	}
	if md.MagnificationFromHeader != 5 {
		t.Errorf("Mag(hdr) = %v", md.MagnificationFromHeader)
	}

	// Non-IFE Tiler returns false.
	type notIFE struct{ opentile.Tiler }
	_, ok = MetadataOf(notIFE{})
	if ok {
		t.Error("MetadataOf on non-IFE Tiler returned true")
	}

	// nil → false (don't panic).
	_, ok = MetadataOf(nil)
	if ok {
		t.Error("MetadataOf(nil) returned true")
	}
}

// testWrapper satisfies the unwrap interface that fileCloser uses.
type testWrapper struct{ opentile.Tiler }

func (w *testWrapper) UnwrapTiler() opentile.Tiler { return w.Tiler }

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
	_, err := openIFE(bytes.NewReader(data), int64(len(data)), &opentile.Config{})
	if errors.Is(err, opentile.ErrUnsupportedFormat) {
		t.Errorf("validation error wrongly aliased to ErrUnsupportedFormat")
	}
}
