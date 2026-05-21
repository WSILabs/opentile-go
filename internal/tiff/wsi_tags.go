package tiff

import "math"

// COG-WSI private tag IDs per the v0.1 spec §5.2. These are part
// of the wsitools writer's namespace (range >= 65000); they are
// not registered TIFF tags.
const (
	TagWSIImageType     = 65080 // ASCII; every IFD
	TagWSILevelIndex    = 65081 // LONG; pyramid only
	TagWSILevelCount    = 65082 // LONG; pyramid only
	TagWSISourceFormat  = 65083 // ASCII; L0 only
	TagWSIToolsVersion  = 65084 // ASCII; L0 only
	TagWSIMPPX          = 65085 // DOUBLE; L0 only
	TagWSIMPPY          = 65086 // DOUBLE; L0 only
	TagWSIMagnification = 65087 // DOUBLE; L0 only
)

// WSIImageType returns the WSIImageType tag value (e.g., "pyramid",
// "label", "macro", "thumbnail", "overview") and a presence flag.
// Returns (empty, false) when the tag is absent — readers should
// treat absence as "this IFD doesn't carry WSI classification."
func (p *Page) WSIImageType() (string, bool) {
	return p.ASCII(TagWSIImageType)
}

// WSILevelIndex returns the 0-based pyramid level index declared by
// the IFD's WSILevelIndex tag (COG-WSI spec §5.2). Returns
// (0, false) when absent.
func (p *Page) WSILevelIndex() (uint32, bool) {
	return p.scalarU32(TagWSILevelIndex)
}

// WSILevelCount returns the total pyramid level count declared by
// the IFD's WSILevelCount tag (COG-WSI spec §5.2). Returns
// (0, false) when absent.
func (p *Page) WSILevelCount() (uint32, bool) {
	return p.scalarU32(TagWSILevelCount)
}

// WSISourceFormat returns the original source container identifier
// (e.g., "svs", "philips") and a presence flag. Per spec, populated
// on the L0 IFD only.
func (p *Page) WSISourceFormat() (string, bool) {
	return p.ASCII(TagWSISourceFormat)
}

// WSIToolsVersion returns the wsitools version that wrote the file.
// Per spec, populated on the L0 IFD only.
func (p *Page) WSIToolsVersion() (string, bool) {
	return p.ASCII(TagWSIToolsVersion)
}

// WSIMPPX returns the per-X-axis microns-per-pixel and a presence
// flag. Per spec, populated on the L0 IFD only.
func (p *Page) WSIMPPX() (float64, bool) {
	return p.doubleTag(TagWSIMPPX)
}

// WSIMPPY returns the per-Y-axis microns-per-pixel and a presence
// flag.
func (p *Page) WSIMPPY() (float64, bool) {
	return p.doubleTag(TagWSIMPPY)
}

// WSIMagnification returns the optical magnification (e.g., 40.0)
// and a presence flag. Per spec, populated on the L0 IFD only.
func (p *Page) WSIMagnification() (float64, bool) {
	return p.doubleTag(TagWSIMagnification)
}

// doubleTag returns the first value of a DOUBLE-typed tag (TIFF data
// type 12, IEEE 754 double-precision) as float64. Returns (0, false)
// if the tag is missing or not a DOUBLE type. COG-WSI spec uses
// DOUBLE for the microns-per-pixel and magnification fields.
func (p *Page) doubleTag(tag uint16) (float64, bool) {
	e, ok := p.ifd.get(tag)
	if !ok {
		return 0, false
	}
	if e.Type != DTDouble {
		return 0, false
	}
	// DOUBLE is 8 bytes per value. Count must be ≥ 1; read the first.
	var bits uint64
	if e.fitsInline() {
		bits = p.br.order.Uint64(e.valueBytes[:8])
	} else {
		buf, err := p.br.bytes(int64(e.valueOrOffset), 8)
		if err != nil {
			return 0, false
		}
		bits = p.br.order.Uint64(buf)
	}
	return math.Float64frombits(bits), true
}
