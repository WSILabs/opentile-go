package tiff

import (
	"fmt"
	"math"
	"sort"
)

// Well-known TIFF tag IDs used by opentile-go.
const (
	TagNewSubfileType    uint16 = 254
	TagImageWidth        uint16 = 256
	TagImageLength       uint16 = 257
	TagBitsPerSample     uint16 = 258
	TagCompression       uint16 = 259
	TagPhotometric       uint16 = 262
	TagPredictor         uint16 = 317
	TagImageDescription  uint16 = 270
	TagStripOffsets      uint16 = 273
	TagSamplesPerPixel   uint16 = 277
	TagRowsPerStrip      uint16 = 278
	TagStripByteCounts   uint16 = 279
	TagXResolution       uint16 = 282
	TagYResolution       uint16 = 283
	TagResolutionUnit    uint16 = 296
	TagSoftware          uint16 = 305
	TagDateTime          uint16 = 306
	TagTileWidth         uint16 = 322
	TagTileLength        uint16 = 323
	TagTileOffsets       uint16 = 324
	TagTileByteCounts    uint16 = 325
	TagSubIFDs           uint16 = 330
	TagJPEGTables        uint16 = 347
	TagYCbCrSubSampling  uint16 = 530
	TagXMP               uint16 = 700
	TagImageDepth        uint16 = 32997
	TagInterColorProfile uint16 = 34675
)

// Page wraps a parsed IFD and exposes typed accessors for the tags opentile-go
// needs. Missing tags return (zero, false) — callers decide whether the
// absence is fatal.
type Page struct {
	ifd *ifd
	br  *byteReader
}

func newPage(i *ifd, br *byteReader) *Page { return &Page{ifd: i, br: br} }

// scalarU32 returns the first value of a tag, or (0, false) if missing.
//
// Tries Values() first (uint32 path, handles SHORT / LONG / IFD); falls back
// to Values64 (handles LONG8 / IFD8) for BigTIFF entries that store small
// scalars in 8-byte fields. Narrowing to uint32 is safe for the tags this
// helper is meant to back (ImageWidth / Length / TileWidth / Length /
// Compression / Photometric / SamplesPerPixel / BitsPerSample /
// ResolutionUnit) — none of which legitimately exceeds uint32 range on
// real slides.
func (p *Page) scalarU32(tag uint16) (uint32, bool) {
	e, ok := p.ifd.get(tag)
	if !ok {
		return 0, false
	}
	if vals, err := e.Values(p.br); err == nil && len(vals) > 0 {
		return vals[0], true
	}
	vals64, err := e.Values64(p.br)
	if err != nil || len(vals64) == 0 {
		return 0, false
	}
	if vals64[0] > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(vals64[0]), true
}

// ScalarU32 returns the first value of an arbitrary tag as uint32, or
// (0, false) if the tag is absent. Exposed so format packages can read
// vendor-private tags without the internal helpers gaining per-tag
// accessors.
func (p *Page) ScalarU32(tag uint16) (uint32, bool) { return p.scalarU32(tag) }

// IFDOffset returns the byte offset of this page's backing IFD in the
// file. Populated during the IFD walk; 0 for synthetic pages with no
// on-disk IFD (e.g. test-constructed pages).
func (p *Page) IFDOffset() int64 { return p.ifd.offset }

func (p *Page) ImageWidth() (uint32, bool)      { return p.scalarU32(TagImageWidth) }
func (p *Page) ImageLength() (uint32, bool)     { return p.scalarU32(TagImageLength) }
func (p *Page) TileWidth() (uint32, bool)       { return p.scalarU32(TagTileWidth) }
func (p *Page) TileLength() (uint32, bool)      { return p.scalarU32(TagTileLength) }
func (p *Page) Compression() (uint32, bool)     { return p.scalarU32(TagCompression) }
func (p *Page) Photometric() (uint32, bool)     { return p.scalarU32(TagPhotometric) }
func (p *Page) Predictor() (uint32, bool)       { return p.scalarU32(TagPredictor) }
func (p *Page) SamplesPerPixel() (uint32, bool) { return p.scalarU32(TagSamplesPerPixel) }
func (p *Page) BitsPerSample() (uint32, bool)   { return p.scalarU32(TagBitsPerSample) }
func (p *Page) ResolutionUnit() (uint32, bool)  { return p.scalarU32(TagResolutionUnit) }

// ImageDepth returns the ImageDepth tag (32997, SGI private) used by
// Ventana BIF to store Z-stack depth, or (1, false) if absent or zero.
func (p *Page) ImageDepth() (int, bool) {
	val, ok := p.scalarU32(TagImageDepth)
	if !ok || val < 1 {
		return 1, false
	}
	return int(val), true
}

// ASCII returns an ASCII-typed tag's string value (NUL-stripped), or
// ("", false) if missing.
func (p *Page) ASCII(tag uint16) (string, bool) {
	e, ok := p.ifd.get(tag)
	if !ok {
		return "", false
	}
	s, err := e.decodeASCII(p.br, e.valueBytes[:])
	if err != nil {
		return "", false
	}
	return s, true
}

// ImageDescription returns the ASCII ImageDescription tag if present.
func (p *Page) ImageDescription() (string, bool) {
	return p.ASCII(TagImageDescription)
}

// Software returns the ASCII Software tag (305) if present. Used by
// Philips TIFF detection: tifffile's `is_philips` checks
// `software[:10] == 'Philips DP'`.
func (p *Page) Software() (string, bool) {
	return p.ASCII(TagSoftware)
}

// SubIFDOffsets returns the SubIFDs tag (330) value as a slice of
// uint64 offsets, or (nil, false) if absent. Used by OME TIFF where
// reduced-resolution pyramid levels live in SubIFDs of the base page
// rather than as top-level IFDs. Decodes both classic-TIFF LONG/IFD
// and BigTIFF LONG8/IFD8 type widths via the existing arrayU64 helper.
func (p *Page) SubIFDOffsets() ([]uint64, bool) {
	if !p.HasTag(TagSubIFDs) {
		return nil, false
	}
	offs, err := p.arrayU64(TagSubIFDs)
	if err != nil {
		return nil, false
	}
	return offs, true
}

// JPEGTables returns the JPEG tables blob used as a prefix for tiles, if present.
//
// Note: e.Count is uint64. On 32-bit platforms int(e.Count) truncates if
// Count > 2 GiB. In practice JPEGTables is bounded by real-world tag
// values (<1 MB on every Aperio / Hamamatsu slide we've seen), so the
// truncation cannot fire on the 64-bit targets we support. If 32-bit
// support is ever in scope, audit this site (and ICCProfile + Entry.Values64
// in tag.go) and add explicit bounds checks.
func (p *Page) JPEGTables() ([]byte, bool) {
	e, ok := p.ifd.get(TagJPEGTables)
	if !ok {
		return nil, false
	}
	// Tables are UNDEFINED bytes; read the payload.
	if e.fitsInline() {
		return append([]byte(nil), e.valueBytes[:e.Count]...), true
	}
	buf, err := p.br.bytes(int64(e.valueOrOffset), int(e.Count))
	if err != nil {
		return nil, false
	}
	return buf, true
}

// XMP returns the XMP packet (tag 700) raw bytes if present, or
// (nil, false) otherwise. Used by formats whose detection or metadata
// surface depends on XMP content (Ventana BIF carries `<iScan>` and
// `<EncodeInfo>` blocks here).
func (p *Page) XMP() ([]byte, bool) {
	e, ok := p.ifd.get(TagXMP)
	if !ok {
		return nil, false
	}
	if e.fitsInline() {
		return append([]byte(nil), e.valueBytes[:e.Count]...), true
	}
	buf, err := p.br.bytes(int64(e.valueOrOffset), int(e.Count))
	if err != nil {
		return nil, false
	}
	return buf, true
}

// ICCProfile returns the InterColorProfile tag bytes if present.
func (p *Page) ICCProfile() ([]byte, bool) {
	e, ok := p.ifd.get(TagInterColorProfile)
	if !ok {
		return nil, false
	}
	if e.fitsInline() {
		return append([]byte(nil), e.valueBytes[:e.Count]...), true
	}
	buf, err := p.br.bytes(int64(e.valueOrOffset), int(e.Count))
	if err != nil {
		return nil, false
	}
	return buf, true
}

// TileOffsets returns the TileOffsets array.
func (p *Page) TileOffsets() ([]uint32, error) {
	return p.arrayU32(TagTileOffsets)
}

// TileByteCounts returns the TileByteCounts array.
func (p *Page) TileByteCounts() ([]uint32, error) {
	return p.arrayU32(TagTileByteCounts)
}


func (p *Page) arrayU32(tag uint16) ([]uint32, error) {
	e, ok := p.ifd.get(tag)
	if !ok {
		return nil, fmt.Errorf("tiff: tag %d missing", tag)
	}
	return e.Values(p.br)
}

// TileOffsets64 returns the TileOffsets array as uint64 values; supports both
// LONG (classic TIFF) and LONG8 (BigTIFF) encodings.
func (p *Page) TileOffsets64() ([]uint64, error) {
	return p.arrayU64(TagTileOffsets)
}

// TileByteCounts64 returns the TileByteCounts array as uint64 values.
func (p *Page) TileByteCounts64() ([]uint64, error) {
	return p.arrayU64(TagTileByteCounts)
}

// ScalarArrayU64 returns the value array for an arbitrary tag as uint64s.
// Generalizes TileOffsets64/TileByteCounts64 for callers that need other
// array-valued tags (e.g., SVS StripOffsets, NDPI vendor arrays).
func (p *Page) ScalarArrayU64(tag uint16) ([]uint64, error) {
	return p.arrayU64(tag)
}

// ScalarArrayU32 returns the value array for an arbitrary tag as uint32s.
// Used by format packages to read LONG-typed vendor arrays (e.g., NDPI
// McuStarts / McuStartsHighBytes). Returns an error if the tag is missing
// or cannot be decoded as uint32.
func (p *Page) ScalarArrayU32(tag uint16) ([]uint32, error) {
	return p.arrayU32(tag)
}

// HasTag reports whether the page carries the given tag at all, without
// attempting to decode its value. Useful when a format needs to branch on
// tag presence before paying the decode cost — e.g., NDPI's McuStarts
// (65426) tag determines whether a page is a stripped JPEG level.
func (p *Page) HasTag(tag uint16) bool {
	_, ok := p.ifd.get(tag)
	return ok
}

func (p *Page) arrayU64(tag uint16) ([]uint64, error) {
	e, ok := p.ifd.get(tag)
	if !ok {
		return nil, fmt.Errorf("tiff: tag %d missing", tag)
	}
	return e.Values64(p.br)
}

// XResolution returns the X resolution as a numerator/denominator rational.
func (p *Page) XResolution() (num, den uint32, ok bool) {
	return p.rationalFirst(TagXResolution)
}

// YResolution returns the Y resolution as a numerator/denominator rational.
func (p *Page) YResolution() (num, den uint32, ok bool) {
	return p.rationalFirst(TagYResolution)
}

func (p *Page) rationalFirst(tag uint16) (uint32, uint32, bool) {
	e, found := p.ifd.get(tag)
	if !found {
		return 0, 0, false
	}
	vals, err := e.decodeRational(p.br)
	if err != nil || len(vals) == 0 {
		return 0, 0, false
	}
	return vals[0][0], vals[0][1], true
}

// Float32 returns the first value of a FLOAT-typed tag (TIFF data type 11,
// IEEE 754 single-precision). Returns (0, false) if the tag is missing or
// not a FLOAT type. NDPI's Magnification tag (65421) uses this type.
func (p *Page) Float32(tag uint16) (float32, bool) {
	e, ok := p.ifd.get(tag)
	if !ok {
		return 0, false
	}
	if e.Type != DTFloat {
		return 0, false
	}
	// FLOAT is 4 bytes per value. Count must be ≥ 1; read the first.
	var bits uint32
	if e.fitsInline() {
		bits = p.br.order.Uint32(e.valueBytes[:4])
	} else {
		buf, err := p.br.bytes(int64(e.valueOrOffset), 4)
		if err != nil {
			return 0, false
		}
		bits = p.br.order.Uint32(buf)
	}
	return math.Float32frombits(bits), true
}

// TileGrid returns the tile grid dimensions (tiles in X, tiles in Y).
// Result is computed via ceil division: a partial tile at the edge counts as one.
func (p *Page) TileGrid() (int, int, error) {
	iw, ok := p.ImageWidth()
	if !ok {
		return 0, 0, fmt.Errorf("tiff: ImageWidth missing")
	}
	il, ok := p.ImageLength()
	if !ok {
		return 0, 0, fmt.Errorf("tiff: ImageLength missing")
	}
	tw, ok := p.TileWidth()
	if !ok || tw == 0 {
		return 0, 0, fmt.Errorf("tiff: TileWidth missing or zero")
	}
	tl, ok := p.TileLength()
	if !ok || tl == 0 {
		return 0, 0, fmt.Errorf("tiff: TileLength missing or zero")
	}
	gx := int(iw / tw)
	if iw%tw != 0 {
		gx++
	}
	gy := int(il / tl)
	if il%tl != 0 {
		gy++
	}
	return gx, gy, nil
}

// RawTags enumerates every tag in this page's IFD, decoded, in ascending
// tag-number order (deterministic). Used by the opentile root package to
// build the public TIFF-tag API.
func (p *Page) RawTags() []RawTag {
	nums := make([]uint16, 0, len(p.ifd.entries))
	for n := range p.ifd.entries {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	out := make([]RawTag, 0, len(nums))
	for _, n := range nums {
		out = append(out, p.ifd.entries[n].decode(p.br))
	}
	return out
}
