package ife

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	opentile "github.com/cornish/opentile-go"
)

// Block recovery magic values per upstream IrisCodecExtension.hpp.
// The full list is in upstream's `RecoverableValue` enum; we name
// only the ones the v0.8 reader actually validates.
const (
	recoverMetadata        uint16 = 0x5504
	recoverAttributes      uint16 = 0x5505
	recoverImageArray      uint16 = 0x550A
	recoverImageBytes      uint16 = 0x550B
	recoverICCProfile      uint16 = 0x550C
	recoverAttributesSizes uint16 = 0x5508
	recoverAttributesBytes uint16 = 0x5509
)

// MetadataBlock fields per upstream IrisCodecExtension.hpp's
// METADATA struct (56 bytes total):
//
//	off  0  u64 validation       == self offset
//	off  8  u16 recovery         (0x5504)
//	off 10  u16 codec_major
//	off 12  u16 codec_minor
//	off 14  u16 codec_build
//	off 16  u64 attributes_offset    (-> ATTRIBUTES, or NULL)
//	off 24  u64 images_offset        (-> IMAGE_ARRAY, or NULL)
//	off 32  u64 icc_color_offset     (-> ICC_PROFILE, or NULL)
//	off 40  u64 annotations_offset   (-> ANNOTATIONS, or NULL)
//	off 48  f32 microns_per_pixel    (0.0 == unknown)
//	off 52  f32 magnification        (0.0 == unknown)
const metadataBlockSize = 56

// AttributesFormat identifies the encoding scheme used for keys and
// values in the ATTRIBUTES block. Mirrors upstream's MetadataType
// enum: FREE_TEXT (1) is the I2S key/value convention; DICOM (2) is
// reserved for DICOM-style tag/value pairs (no fixture exercises it
// yet); UNDEFINED (0) is invalid.
type AttributesFormat uint8

const (
	AttributesFormatUndefined AttributesFormat = 0
	AttributesFormatFreeText  AttributesFormat = 1 // I2S, free-form UTF-8 key/value
	AttributesFormatDICOM     AttributesFormat = 2
)

// String returns a stable identifier for AttributesFormat suitable
// for log output and JSON encoding.
func (a AttributesFormat) String() string {
	switch a {
	case AttributesFormatFreeText:
		return "free-text"
	case AttributesFormatDICOM:
		return "dicom"
	case AttributesFormatUndefined:
		return "undefined"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(a))
	}
}

// Metadata is the IFE-specific metadata struct. Embeds
// [opentile.Metadata] so the cross-format common fields are
// type-asserted off the same value.
//
// v0.17 (Q4 Option B): the format-specific MicronsPerPixel (f32)
// outer field was removed because it shadowed the cross-format
// MicronsPerPixel (f64); consumers that previously read the f32 raw
// value now read the (lossless-widened) f64 from the embedded struct.
type Metadata struct {
	opentile.Metadata

	// MagnificationFromHeader is the level-0 magnification from the
	// METADATA block's f32 magnification field. 0 when unset.
	// Available separately from opentile.Metadata.Magnification so
	// callers that prefer the header value over an Attributes-derived
	// override can reach it directly.
	MagnificationFromHeader float32

	// CodecMajor / CodecMinor / CodecBuild identify the version of
	// the Iris encoder that wrote this file. Distinct from the
	// scanner software (which lives in Attributes when the encoder
	// preserves source-vendor metadata, e.g. cervix's "aperio.*"
	// keys).
	CodecMajor uint16
	CodecMinor uint16
	CodecBuild uint16

	// AttributesFormat identifies the encoding of the Attributes map
	// (FREE_TEXT or DICOM); UNDEFINED if the file has no ATTRIBUTES
	// block at all.
	AttributesFormat AttributesFormat
	// AttributesVersion is the format-version sub-field on the
	// ATTRIBUTES block header. Currently always 0 in v1.0 files.
	AttributesVersion uint16
	// Attributes is the free-form key/value map. Empty when the
	// file has no ATTRIBUTES block. Encoder-set; opentile-go does
	// not normalise keys across vendors.
	Attributes map[string]string
}

// readMetadata parses a complete METADATA block + sub-blocks
// (ATTRIBUTES, IMAGE_ARRAY for associated images, ICC_PROFILE) at
// the given offset. ANNOTATIONS is read for offset validation only;
// its contents are not parsed (v0.9+; tracked as L25).
//
// Returns the IFE Metadata + the associated images slice + the ICC
// profile bytes (or nil).
func readMetadata(r io.ReaderAt, off uint64, fileSize int64) (Metadata, []opentile.AssociatedImage, []byte, error) {
	var md Metadata
	if int64(off)+metadataBlockSize > fileSize {
		return md, nil, nil, fmt.Errorf("ife: METADATA off 0x%x past EOF", off)
	}
	buf := make([]byte, metadataBlockSize)
	if _, err := r.ReadAt(buf, int64(off)); err != nil {
		return md, nil, nil, fmt.Errorf("ife: read METADATA: %w", err)
	}
	validation := binary.LittleEndian.Uint64(buf[0:8])
	if validation != off {
		return md, nil, nil, fmt.Errorf("ife: METADATA validation 0x%x != offset 0x%x",
			validation, off)
	}
	recovery := binary.LittleEndian.Uint16(buf[8:10])
	if recovery != recoverMetadata {
		return md, nil, nil, fmt.Errorf("ife: METADATA recovery 0x%04x != 0x%04x", recovery, recoverMetadata)
	}
	md.CodecMajor = binary.LittleEndian.Uint16(buf[10:12])
	md.CodecMinor = binary.LittleEndian.Uint16(buf[12:14])
	md.CodecBuild = binary.LittleEndian.Uint16(buf[14:16])
	attrsOff := binary.LittleEndian.Uint64(buf[16:24])
	imagesOff := binary.LittleEndian.Uint64(buf[24:32])
	iccOff := binary.LittleEndian.Uint64(buf[32:40])
	// annotations_offset at 40:48 — read for forward-compat but not parsed in v0.8.
	mppF32 := math.Float32frombits(binary.LittleEndian.Uint32(buf[48:52]))
	md.MagnificationFromHeader = math.Float32frombits(binary.LittleEndian.Uint32(buf[52:56]))
	md.Magnification = float64(md.MagnificationFromHeader)

	// v0.17 cross-format MPP: the IFE METADATA block carries a single
	// f32 microns_per_pixel value applied to both axes — the spec
	// doesn't allow anisotropic pixels. Widen f32→f64 (lossless) and
	// SetMPPSymmetric to populate the symmetric scalar slot.
	if mppF32 > 0 {
		md.MicronsPerPixelX = float64(mppF32)
		md.MicronsPerPixelY = float64(mppF32)
	}
	md.SetMPPSymmetric()

	if attrsOff != NullOffset && attrsOff != 0 {
		fmtType, ver, kvs, err := readAttributes(r, attrsOff, fileSize)
		if err != nil {
			return md, nil, nil, fmt.Errorf("ife: ATTRIBUTES: %w", err)
		}
		md.AttributesFormat = fmtType
		md.AttributesVersion = ver
		md.Attributes = kvs

		// v0.17 cross-format wiring from ATTRIBUTES:
		//
		//   - "tiff.ImageDescription" (the IFE encoder preserves the
		//     source slide's ImageDescription tag verbatim under this
		//     key — observed on cervix_2x's GT450-derived fixture) →
		//     cross.ImageDescription. Other IFE fixtures may store
		//     their description under a different key; consumers
		//     wanting raw access read md.Attributes directly.
		//
		//   - Every attribute (typed or untyped) → "iris.<key>"
		//     Properties for cross-format vendor passthrough so
		//     consumers can reach format-specific fields without
		//     reaching back into md.Attributes.
		populateIFECrossFields(&md.Metadata, kvs)
	} else {
		md.AttributesFormat = AttributesFormatUndefined
	}

	var assoc []opentile.AssociatedImage
	if imagesOff != NullOffset && imagesOff != 0 {
		var err error
		assoc, err = readImageArray(r, imagesOff, fileSize)
		if err != nil {
			return md, nil, nil, fmt.Errorf("ife: IMAGE_ARRAY: %w", err)
		}
	}

	var icc []byte
	if iccOff != NullOffset && iccOff != 0 {
		var err error
		icc, err = readICCProfile(r, iccOff, fileSize)
		if err != nil {
			return md, nil, nil, fmt.Errorf("ife: ICC_PROFILE: %w", err)
		}
	}

	return md, assoc, icc, nil
}

// populateIFECrossFields copies the parsed IFE ATTRIBUTES map onto
// the cross-format opentile.Metadata: surface known keys onto typed
// fields (ImageDescription) and mirror every attribute under the
// "iris." Properties namespace so cross-format consumers can reach
// vendor-specific data without parsing md.Attributes themselves.
//
// Added in v0.17. v0.20: also populates Writer from the ImageDescription
// first line when present (IFE encoder passthrough of source vendor info).
func populateIFECrossFields(cross *opentile.Metadata, kvs map[string]string) {
	for k, v := range kvs {
		// Surface every attribute, regardless of source vendor. The
		// IFE encoder may pass through arbitrary keys from the
		// originating scanner (cervix carries 24 attributes spanning
		// "tiff.*", "aperio.*"); preserving the original vendor
		// prefix under "iris." keeps the provenance visible.
		cross.SetProperty("iris."+k, v)
	}
	// "tiff.ImageDescription" carries the source slide's
	// ImageDescription tag verbatim when the IFE encoder preserves
	// it. Empty string and missing key both fall through unchanged.
	if v, ok := kvs["tiff.ImageDescription"]; ok && v != "" {
		cross.ImageDescription = v
		// v0.20: extract Writer from the first line (carries source
		// vendor info when the encoder preserves it; e.g., "Aperio
		// Leica Biosystems GT450 v1.0.1" for Aperio-scanned fixtures).
		// IFE has no native Software field, so fallback to the image
		// description passthrough.
		lines := strings.Split(v, "\n")
		if len(lines) > 0 && lines[0] != "" {
			cross.Writer = lines[0]
		}
	}
}

// ATTRIBUTES block layout (29 B header):
//
//	off  0  u64 validation
//	off  8  u16 recovery     (0x5505)
//	off 10  u8  format       (AttributesFormat)
//	off 11  u16 version
//	off 13  u64 lengths_offset    (-> ATTRIBUTES_SIZES)
//	off 21  u64 byte_array_offset (-> ATTRIBUTES_BYTES)
const attributesBlockSize = 29

// ATTRIBUTES_SIZES (16 B header + N×6 B entries):
//
//	header: validation:u64, recovery:u16, entry_size:u16, entry_n:u32
//	entry:  key_size:u16, value_size:u32
const attributesSizesEntry = 6

// ATTRIBUTES_BYTES (14 B header + concatenated key/value bytes).
//
//	header: validation:u64, recovery:u16, entry_n:u32
const attributesBytesHeaderSize = 14

func readAttributes(r io.ReaderAt, off uint64, fileSize int64) (AttributesFormat, uint16, map[string]string, error) {
	if int64(off)+attributesBlockSize > fileSize {
		return 0, 0, nil, fmt.Errorf("ATTRIBUTES off 0x%x past EOF", off)
	}
	hdr := make([]byte, attributesBlockSize)
	if _, err := r.ReadAt(hdr, int64(off)); err != nil {
		return 0, 0, nil, fmt.Errorf("read header: %w", err)
	}
	validation := binary.LittleEndian.Uint64(hdr[0:8])
	if validation != off {
		return 0, 0, nil, fmt.Errorf("validation 0x%x != offset 0x%x", validation, off)
	}
	if got := binary.LittleEndian.Uint16(hdr[8:10]); got != recoverAttributes {
		return 0, 0, nil, fmt.Errorf("recovery 0x%04x != 0x%04x", got, recoverAttributes)
	}
	fmtType := AttributesFormat(hdr[10])
	version := binary.LittleEndian.Uint16(hdr[11:13])
	lenOff := binary.LittleEndian.Uint64(hdr[13:21])
	bytesOff := binary.LittleEndian.Uint64(hdr[21:29])

	if fmtType == AttributesFormatUndefined {
		return fmtType, version, map[string]string{}, nil
	}
	// v0.8 only handles FREE_TEXT; DICOM is rejected explicitly so a
	// future fixture surfaces the gap rather than silently mis-parsing.
	if fmtType == AttributesFormatDICOM {
		return fmtType, version, nil, errors.New("DICOM attributes format is not supported in v0.8")
	}
	if fmtType != AttributesFormatFreeText {
		return fmtType, version, nil, fmt.Errorf("unknown attributes format %d", uint8(fmtType))
	}

	entryN, sizes, err := readAttributesSizes(r, lenOff, fileSize)
	if err != nil {
		return fmtType, version, nil, fmt.Errorf("ATTRIBUTES_SIZES: %w", err)
	}
	if entryN == 0 {
		return fmtType, version, map[string]string{}, nil
	}
	kvs, err := readAttributesBytes(r, bytesOff, sizes, fileSize)
	if err != nil {
		return fmtType, version, nil, fmt.Errorf("ATTRIBUTES_BYTES: %w", err)
	}
	return fmtType, version, kvs, nil
}

// attrSize is one (key_size, value_size) pair from ATTRIBUTES_SIZES.
type attrSize struct {
	KeySize   uint16
	ValueSize uint32
}

func readAttributesSizes(r io.ReaderAt, off uint64, fileSize int64) (uint32, []attrSize, error) {
	if int64(off)+blockHeaderValidation > fileSize {
		return 0, nil, fmt.Errorf("hdr off 0x%x past EOF", off)
	}
	hdr := make([]byte, blockHeaderValidation)
	if _, err := r.ReadAt(hdr, int64(off)); err != nil {
		return 0, nil, fmt.Errorf("read hdr: %w", err)
	}
	validation := binary.LittleEndian.Uint64(hdr[0:8])
	if validation != off {
		return 0, nil, fmt.Errorf("validation 0x%x != offset 0x%x", validation, off)
	}
	if got := binary.LittleEndian.Uint16(hdr[8:10]); got != recoverAttributesSizes {
		return 0, nil, fmt.Errorf("recovery 0x%04x != 0x%04x", got, recoverAttributesSizes)
	}
	entrySize := binary.LittleEndian.Uint16(hdr[10:12])
	if entrySize != attributesSizesEntry {
		return 0, nil, fmt.Errorf("entry_size %d != %d", entrySize, attributesSizesEntry)
	}
	entryN := binary.LittleEndian.Uint32(hdr[12:16])
	bodySize := int64(entryN) * int64(entrySize)
	if int64(off)+blockHeaderValidation+bodySize > fileSize {
		return 0, nil, fmt.Errorf("body would extend past EOF")
	}
	body := make([]byte, bodySize)
	if _, err := r.ReadAt(body, int64(off)+blockHeaderValidation); err != nil {
		return 0, nil, fmt.Errorf("read body: %w", err)
	}
	out := make([]attrSize, entryN)
	for i := uint32(0); i < entryN; i++ {
		base := i * uint32(entrySize)
		out[i].KeySize = binary.LittleEndian.Uint16(body[base : base+2])
		out[i].ValueSize = binary.LittleEndian.Uint32(body[base+2 : base+6])
	}
	return entryN, out, nil
}

func readAttributesBytes(r io.ReaderAt, off uint64, sizes []attrSize, fileSize int64) (map[string]string, error) {
	if int64(off)+attributesBytesHeaderSize > fileSize {
		return nil, fmt.Errorf("hdr off 0x%x past EOF", off)
	}
	hdr := make([]byte, attributesBytesHeaderSize)
	if _, err := r.ReadAt(hdr, int64(off)); err != nil {
		return nil, fmt.Errorf("read hdr: %w", err)
	}
	validation := binary.LittleEndian.Uint64(hdr[0:8])
	if validation != off {
		return nil, fmt.Errorf("validation 0x%x != offset 0x%x", validation, off)
	}
	if got := binary.LittleEndian.Uint16(hdr[8:10]); got != recoverAttributesBytes {
		return nil, fmt.Errorf("recovery 0x%04x != 0x%04x", got, recoverAttributesBytes)
	}
	// entry_n at hdr[10:14] is informational; the sizes array drives
	// the decode.

	// Compute the total body size + read once.
	var total int64
	for _, s := range sizes {
		total += int64(s.KeySize) + int64(s.ValueSize)
	}
	if int64(off)+attributesBytesHeaderSize+total > fileSize {
		return nil, fmt.Errorf("body would extend past EOF")
	}
	body := make([]byte, total)
	if _, err := r.ReadAt(body, int64(off)+attributesBytesHeaderSize); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	out := make(map[string]string, len(sizes))
	cursor := int64(0)
	for _, s := range sizes {
		key := string(body[cursor : cursor+int64(s.KeySize)])
		cursor += int64(s.KeySize)
		val := string(body[cursor : cursor+int64(s.ValueSize)])
		cursor += int64(s.ValueSize)
		// Last-wins on duplicate keys (per Go map semantics) — IFE
		// doesn't formally forbid duplicates but no fixture has any.
		out[key] = val
	}
	return out, nil
}

// IMAGE_ARRAY block layout: 16 B standard header + N×20 B entries.
//
//	IMAGE_ENTRY (20 B):
//	  off  0  u64 bytes_offset    (-> IMAGE_BYTES)
//	  off  8  u32 width
//	  off 12  u32 height
//	  off 16  u8  encoding   (1=PNG, 2=JPEG, 3=AVIF)
//	  off 17  u8  format     (pixel format; informational)
//	  off 18  u16 orientation (half-float; 0/90/180/270)
const imageEntrySize = 20

// IMAGE_BYTES block layout (16 B header + UTF-8 title + raw bytes):
//
//	off  0  u64 validation
//	off  8  u16 recovery     (0x550B)
//	off 10  u16 title_size
//	off 12  u32 image_size
//	off 16  ... title bytes (UTF-8) ...
//	[+title_size]  ... image bytes (encoded per IMAGE_ENTRY.encoding) ...
const imageBytesHeaderSize = 16

func readImageArray(r io.ReaderAt, off uint64, fileSize int64) ([]opentile.AssociatedImage, error) {
	if int64(off)+blockHeaderValidation > fileSize {
		return nil, fmt.Errorf("hdr off 0x%x past EOF", off)
	}
	hdr := make([]byte, blockHeaderValidation)
	if _, err := r.ReadAt(hdr, int64(off)); err != nil {
		return nil, fmt.Errorf("read hdr: %w", err)
	}
	validation := binary.LittleEndian.Uint64(hdr[0:8])
	if validation != off {
		return nil, fmt.Errorf("validation 0x%x != offset 0x%x", validation, off)
	}
	if got := binary.LittleEndian.Uint16(hdr[8:10]); got != recoverImageArray {
		return nil, fmt.Errorf("recovery 0x%04x != 0x%04x", got, recoverImageArray)
	}
	entrySize := binary.LittleEndian.Uint16(hdr[10:12])
	if entrySize != imageEntrySize {
		return nil, fmt.Errorf("entry_size %d != %d", entrySize, imageEntrySize)
	}
	entryN := binary.LittleEndian.Uint32(hdr[12:16])
	if entryN == 0 {
		return nil, nil
	}
	bodySize := int64(entryN) * int64(entrySize)
	if int64(off)+blockHeaderValidation+bodySize > fileSize {
		return nil, fmt.Errorf("body would extend past EOF")
	}
	body := make([]byte, bodySize)
	if _, err := r.ReadAt(body, int64(off)+blockHeaderValidation); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	out := make([]opentile.AssociatedImage, 0, entryN)
	for i := uint32(0); i < entryN; i++ {
		base := i * uint32(entrySize)
		bytesOff := binary.LittleEndian.Uint64(body[base : base+8])
		w := binary.LittleEndian.Uint32(body[base+8 : base+12])
		h := binary.LittleEndian.Uint32(body[base+12 : base+16])
		encoding := body[base+16]
		// body[base+17] = format (B8G8R8A8 etc.); informational only.
		// body[base+18:base+20] = orientation (half-float); ignored.

		title, imgBytes, err := readImageBytes(r, bytesOff, fileSize)
		if err != nil {
			return nil, fmt.Errorf("image[%d]: %w", i, err)
		}
		comp, err := compressionFromImageEncoding(encoding)
		if err != nil {
			return nil, fmt.Errorf("image[%d]: %w", i, err)
		}
		out = append(out, &associatedImage{
			kind:        normaliseAssociatedKind(title),
			rawTitle:    title,
			size:        opentile.Size{W: int(w), H: int(h)},
			compression: comp,
			bytes:       imgBytes,
		})
	}
	return out, nil
}

// readImageBytes parses one IMAGE_BYTES block at off and returns
// the title + the encoded image bytes (raw passthrough; not decoded).
func readImageBytes(r io.ReaderAt, off uint64, fileSize int64) (string, []byte, error) {
	if int64(off)+imageBytesHeaderSize > fileSize {
		return "", nil, fmt.Errorf("IMAGE_BYTES hdr off 0x%x past EOF", off)
	}
	hdr := make([]byte, imageBytesHeaderSize)
	if _, err := r.ReadAt(hdr, int64(off)); err != nil {
		return "", nil, fmt.Errorf("read hdr: %w", err)
	}
	validation := binary.LittleEndian.Uint64(hdr[0:8])
	if validation != off {
		return "", nil, fmt.Errorf("IMAGE_BYTES validation 0x%x != offset 0x%x", validation, off)
	}
	if got := binary.LittleEndian.Uint16(hdr[8:10]); got != recoverImageBytes {
		return "", nil, fmt.Errorf("IMAGE_BYTES recovery 0x%04x != 0x%04x", got, recoverImageBytes)
	}
	titleSize := binary.LittleEndian.Uint16(hdr[10:12])
	imageSize := binary.LittleEndian.Uint32(hdr[12:16])
	body := make([]byte, int64(titleSize)+int64(imageSize))
	if int64(off)+imageBytesHeaderSize+int64(len(body)) > fileSize {
		return "", nil, fmt.Errorf("IMAGE_BYTES body past EOF")
	}
	if _, err := r.ReadAt(body, int64(off)+imageBytesHeaderSize); err != nil {
		return "", nil, fmt.Errorf("read body: %w", err)
	}
	title := string(body[:titleSize])
	imgBytes := body[titleSize:]
	return title, imgBytes, nil
}

// compressionFromImageEncoding maps the IMAGE_ENTRY.encoding byte to
// opentile.Compression. Distinct from compressionFromEncoding (which
// maps TILE_TABLE.encoding for pyramid tiles): IMAGE_ARRAY uses 1=PNG
// (not 1=IRIS as TILE_TABLE does).
func compressionFromImageEncoding(e uint8) (opentile.Compression, error) {
	switch e {
	case 1:
		// PNG associated images. opentile-go has no CompressionPNG yet
		// because no other format we read uses PNG for associated
		// images. Return CompressionUnknown so consumers know it's
		// raw passthrough but unidentified-codec; downstream code can
		// sniff the PNG signature on the bytes if needed.
		return opentile.CompressionUnknown, nil
	case 2:
		return opentile.CompressionJPEG, nil
	case 3:
		return opentile.CompressionAVIF, nil
	case 0:
		return opentile.CompressionUnknown, errors.New("ENCODING_UNDEFINED is not valid for an associated image")
	default:
		return opentile.CompressionUnknown, fmt.Errorf("unknown IMAGE_ARRAY encoding %d", e)
	}
}

// normaliseAssociatedKind maps an IFE associated-image free-form
// title (case-insensitive) onto opentile-go's existing taxonomy
// ("label" / "overview" / "thumbnail" / "macro" / "map" /
// "probability"). Unrecognised titles surface as the raw lowercased
// string so consumers see what the encoder actually wrote.
func normaliseAssociatedKind(title string) string {
	lower := make([]byte, len(title))
	for i := 0; i < len(title); i++ {
		c := title[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		lower[i] = c
	}
	switch string(lower) {
	case "label":
		return "label"
	case "overview":
		return "overview"
	case "thumbnail":
		return "thumbnail"
	case "macro":
		return "macro"
	case "map":
		return "map"
	case "probability":
		return "probability"
	}
	return string(lower)
}

// ICC_PROFILE block layout: 14 B header + raw ICC bytes.
//
//	off  0  u64 validation
//	off  8  u16 recovery     (0x550C)
//	off 10  u32 entry_n      (== profile byte length)
const iccHeaderSize = 14

func readICCProfile(r io.ReaderAt, off uint64, fileSize int64) ([]byte, error) {
	if int64(off)+iccHeaderSize > fileSize {
		return nil, fmt.Errorf("ICC_PROFILE hdr off 0x%x past EOF", off)
	}
	hdr := make([]byte, iccHeaderSize)
	if _, err := r.ReadAt(hdr, int64(off)); err != nil {
		return nil, fmt.Errorf("read hdr: %w", err)
	}
	validation := binary.LittleEndian.Uint64(hdr[0:8])
	if validation != off {
		return nil, fmt.Errorf("ICC_PROFILE validation 0x%x != offset 0x%x", validation, off)
	}
	if got := binary.LittleEndian.Uint16(hdr[8:10]); got != recoverICCProfile {
		return nil, fmt.Errorf("ICC_PROFILE recovery 0x%04x != 0x%04x", got, recoverICCProfile)
	}
	entryN := binary.LittleEndian.Uint32(hdr[10:14])
	if entryN == 0 {
		return nil, nil
	}
	if int64(off)+iccHeaderSize+int64(entryN) > fileSize {
		return nil, fmt.Errorf("ICC_PROFILE body past EOF")
	}
	body := make([]byte, entryN)
	if _, err := r.ReadAt(body, int64(off)+iccHeaderSize); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// associatedImage is the IFE implementation of
// opentile.AssociatedImage. Bytes are populated at parse time —
// associated images are typically <1 MB, so this trades memory for
// implementation simplicity. (If a future fixture carries a 100 MB
// macro image we can revisit and read on demand.)
type associatedImage struct {
	kind        string
	rawTitle    string
	size        opentile.Size
	compression opentile.Compression
	bytes       []byte
}

func (a *associatedImage) Type() string                      { return a.kind }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }
func (a *associatedImage) Bytes() ([]byte, error) {
	out := make([]byte, len(a.bytes))
	copy(out, a.bytes)
	return out, nil
}

// MetadataOf returns the IFE-specific metadata if t is an IFE Tiler,
// otherwise (nil, false). Walks any number of opentile wrappers
// (e.g. the *fileCloser returned by opentile.OpenFile) before
// asserting on the concrete type.
//
//	if md, ok := ife.MetadataOf(tiler); ok {
//	    fmt.Println(md.MicronsPerPixel, md.Attributes["aperio.AppMag"])
//	}
func MetadataOf(t opentile.Tiler) (*Metadata, bool) {
	for i := 0; t != nil && i <= maxTilerUnwrapHops; i++ {
		if ifeT, ok := t.(*tiler); ok {
			return &ifeT.md, true
		}
		u, ok := t.(tilerUnwrapper)
		if !ok {
			return nil, false
		}
		t = u.UnwrapTiler()
	}
	return nil, false
}

// tilerUnwrapper mirrors the unexported coordination interface from
// the other format packages. opentile-go's *fileCloser implements it
// so MetadataOf works whether the caller used Open or OpenFile.
type tilerUnwrapper interface {
	UnwrapTiler() opentile.Tiler
}

const maxTilerUnwrapHops = 16
