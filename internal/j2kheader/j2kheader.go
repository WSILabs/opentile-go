// Package j2kheader is a pure-Go, header-only parser for JPEG 2000 family
// codestreams (J2K Part 1 and HTJ2K Part 15, raw or JP2/JPH-boxed). It reads
// only the main-header SIZ + COD markers (and, for boxed inputs, the JP2 box
// structure) to recover the codec-domain facts a frame-copying consumer needs —
// component count, bit depth, reversibility, multiple-component transform, and
// boxed-vs-raw — without decoding any tile data. It deliberately avoids
// OpenJPEG / openjph so the result is library-version-independent and shareable
// between the jpeg2000 and htj2k decoders (GH #41).
package j2kheader

import (
	"encoding/binary"
	"errors"

	"github.com/wsilabs/opentile-go/decoder"
)

// ErrNotJ2K is returned when src is neither a raw J2K codestream (SOC marker)
// nor a JP2/JPH box container.
var ErrNotJ2K = errors.New("j2kheader: not a JPEG 2000 codestream")

// Info is the subset of JPEG 2000 main-header facts j2kheader recovers.
type Info struct {
	Components int  // SIZ Csiz
	BitDepth   int  // (SIZ Ssiz[0] & 0x7F) + 1, component 0
	Reversible bool // COD transformation == 1 (5/3 reversible) vs 0 (9/7 irreversible)
	MCT        bool // COD SGcod multiple-component-transform flag
	Boxed      bool // JP2/JPH box container (true) vs raw codestream (false)

	// EnumColorspace is the JP2 'colr' box enumerated colorspace when present
	// on a boxed input (16 = sRGB, 17 = grayscale, 18 = sYCC); -1 otherwise.
	EnumColorspace int

	// XRsiz / YRsiz are the per-component horizontal / vertical sub-sampling
	// factors from SIZ (component 0 = luma, typically 1/1). Used to derive
	// chroma subsampling (4:2:2 vs 4:4:4, etc.).
	XRsiz, YRsiz []int
}

// CodestreamInfo maps the codec-agnostic J2K header facts to the
// decoder-domain decoder.CodestreamInfo, shared by the jpeg2000 and htj2k
// decoders (GH #41). The color encoding reflects how the samples are stored in
// the codestream (what a frame-copy preserves): the COD multiple-component
// transform determines YBR_RCT (reversible) / YBR_ICT (irreversible); without
// it, three components are RGB unless a JP2 'colr' box declares an enumerated
// grayscale / sYCC space.
func (h Info) CodestreamInfo() decoder.CodestreamInfo {
	ci := decoder.CodestreamInfo{
		Components:        h.Components,
		BitDepth:          h.BitDepth,
		Boxed:             h.Boxed,
		ChromaSubsampling: h.chromaSubsampling(),
	}
	if h.Reversible {
		ci.Lossless = decoder.LosslessYes
	} else {
		ci.Lossless = decoder.LosslessNo
	}
	switch {
	case h.Components == 1:
		ci.ColorEncoding = decoder.ColorGrayscale
	case h.MCT && h.Reversible:
		ci.ColorEncoding = decoder.ColorYBRRCT
	case h.MCT && !h.Reversible:
		ci.ColorEncoding = decoder.ColorYBRICT
	default:
		switch h.EnumColorspace {
		case 17: // greyscale
			ci.ColorEncoding = decoder.ColorGrayscale
		case 18: // sYCC
			ci.ColorEncoding = decoder.ColorYCbCr
		default:
			ci.ColorEncoding = decoder.ColorRGB
		}
	}
	return ci
}

// chromaSubsampling derives the chroma subsampling from the SIZ per-component
// sub-sampling factors (component 1 = first chroma, relative to luma's 1/1).
func (h Info) chromaSubsampling() decoder.ChromaSubsampling {
	if h.Components == 1 {
		return decoder.SubsamplingNone
	}
	if h.Components < 3 || len(h.XRsiz) < 2 || len(h.YRsiz) < 2 {
		return decoder.SubsamplingUnknown
	}
	switch hx, vy := h.XRsiz[1], h.YRsiz[1]; {
	case hx == 1 && vy == 1:
		return decoder.Subsampling444
	case hx == 2 && vy == 1:
		return decoder.Subsampling422
	case hx == 2 && vy == 2:
		return decoder.Subsampling420
	case hx == 1 && vy == 2:
		return decoder.Subsampling440
	case hx == 4 && vy == 1:
		return decoder.Subsampling411
	default:
		return decoder.SubsamplingUnknown
	}
}

// J2K marker codes (big-endian, 0xFFxx).
const (
	mrkSOC = 0x4F // start of codestream
	mrkSIZ = 0x51
	mrkCOD = 0x52
	mrkSOT = 0x90 // start of tile-part (ends the main header)
	mrkEOC = 0xD9
)

// Parse reads src's main header and returns its Info. src may be a raw J2K
// codestream or a JP2/JPH box container.
func Parse(src []byte) (Info, error) {
	info := Info{EnumColorspace: -1}

	cs := src
	if isJP2Signature(src) {
		info.Boxed = true
		codestream, enumCS, ok := walkJP2Boxes(src)
		if !ok {
			return Info{}, ErrNotJ2K
		}
		info.EnumColorspace = enumCS
		cs = codestream
	}

	// cs must start with the SOC marker (FF 4F).
	if len(cs) < 4 || cs[0] != 0xFF || cs[1] != mrkSOC {
		return Info{}, ErrNotJ2K
	}

	comps, bitDepth, xr, yr, sizEnd, err := parseSIZ(cs)
	if err != nil {
		return Info{}, err
	}
	info.Components = comps
	info.BitDepth = bitDepth
	info.XRsiz = xr
	info.YRsiz = yr

	mct, reversible, ok := findCOD(cs, sizEnd)
	if !ok {
		return Info{}, ErrNotJ2K
	}
	info.MCT = mct
	info.Reversible = reversible
	return info, nil
}

// parseSIZ reads the SIZ marker (which must immediately follow SOC) and returns
// the component count, component-0 bit depth, and the offset just past the SIZ
// segment (where main-header marker walking resumes).
func parseSIZ(cs []byte) (components, bitDepth int, xr, yr []int, sizEnd int, err error) {
	// cs[0:2] = SOC. SIZ marker begins at cs[2].
	if len(cs) < 4 || cs[2] != 0xFF || cs[3] != mrkSIZ {
		return 0, 0, nil, nil, 0, ErrNotJ2K
	}
	siz := 2 // SIZ marker offset
	if len(cs) < siz+40 {
		return 0, 0, nil, nil, 0, ErrNotJ2K
	}
	lsiz := int(binary.BigEndian.Uint16(cs[siz+2 : siz+4]))
	// Csiz (component count) sits 38 bytes into the SIZ marker (after FF51,
	// Lsiz, Rsiz, and the eight 4-byte image/tile geometry fields).
	csiz := int(binary.BigEndian.Uint16(cs[siz+38 : siz+40]))
	if csiz < 1 {
		return 0, 0, nil, nil, 0, ErrNotJ2K
	}
	// Per-component triplets [Ssiz, XRsiz, YRsiz] follow Csiz, starting at
	// siz+40 (3 bytes each).
	if len(cs) < siz+40+3*csiz {
		return 0, 0, nil, nil, 0, ErrNotJ2K
	}
	xr = make([]int, csiz)
	yr = make([]int, csiz)
	for i := 0; i < csiz; i++ {
		base := siz + 40 + 3*i
		xr[i] = int(cs[base+1])
		yr[i] = int(cs[base+2])
	}
	bitDepth = int(cs[siz+40]&0x7F) + 1 // Ssiz of component 0
	return csiz, bitDepth, xr, yr, siz + 2 + lsiz, nil
}

// findCOD walks the main-header markers from offset start until it finds COD,
// returning its MCT flag and reversibility. Stops at SOT/EOC.
func findCOD(cs []byte, start int) (mct, reversible, ok bool) {
	p := start
	for p+4 <= len(cs) {
		if cs[p] != 0xFF {
			return false, false, false
		}
		marker := cs[p+1]
		if marker == mrkSOT || marker == mrkEOC {
			return false, false, false
		}
		segLen := int(binary.BigEndian.Uint16(cs[p+2 : p+4]))
		if segLen < 2 || p+2+segLen > len(cs) {
			return false, false, false
		}
		if marker == mrkCOD {
			// COD: Lcod(2) Scod(1) SGcod(4) SPcod(...). From the marker start:
			//   [4]   Scod
			//   [5]   SGcod progression order
			//   [6:8] SGcod number of layers
			//   [8]   SGcod multiple-component transform (0/1)
			//   [9]   SPcod decomposition levels
			//   [10]  SPcod code-block width
			//   [11]  SPcod code-block height
			//   [12]  SPcod code-block style
			//   [13]  SPcod transformation (0 = 9/7 irreversible, 1 = 5/3 reversible)
			if p+14 > len(cs) {
				return false, false, false
			}
			mct = cs[p+8] != 0
			reversible = cs[p+13] == 1
			return mct, reversible, true
		}
		p += 2 + segLen
	}
	return false, false, false
}

// isJP2Signature reports whether src begins with the JP2/JPH signature box
// (LBox=12, TBox="jP  ", content 0D 0A 87 0A).
func isJP2Signature(src []byte) bool {
	sig := []byte{0x00, 0x00, 0x00, 0x0C, 'j', 'P', ' ', ' ', 0x0D, 0x0A, 0x87, 0x0A}
	if len(src) < len(sig) {
		return false
	}
	for i, b := range sig {
		if src[i] != b {
			return false
		}
	}
	return true
}

// walkJP2Boxes scans the top-level JP2/JPH boxes for the contiguous codestream
// box ('jp2c') and, inside the 'jp2h' superbox, the enumerated colorspace
// ('colr'). Returns the codestream slice, the enumerated colorspace (or -1),
// and whether a codestream was found.
func walkJP2Boxes(src []byte) (codestream []byte, enumCS int, ok bool) {
	enumCS = -1
	for p := 0; p+8 <= len(src); {
		boxLen := int(binary.BigEndian.Uint32(src[p : p+4]))
		typ := string(src[p+4 : p+8])
		contentStart := p + 8
		var boxEnd int
		switch {
		case boxLen == 1: // 64-bit extended length
			if p+16 > len(src) {
				return nil, enumCS, ok
			}
			xl := int(binary.BigEndian.Uint64(src[p+8 : p+16]))
			contentStart = p + 16
			boxEnd = p + xl
		case boxLen == 0: // extends to end of file
			boxEnd = len(src)
		default:
			boxEnd = p + boxLen
		}
		if boxEnd < contentStart || boxEnd > len(src) {
			return nil, enumCS, ok
		}
		switch typ {
		case "jp2c":
			codestream = src[contentStart:boxEnd]
			ok = true
		case "jp2h":
			enumCS = findColrEnumCS(src[contentStart:boxEnd])
		}
		if boxEnd == p { // guard against zero-progress
			break
		}
		p = boxEnd
	}
	return codestream, enumCS, ok
}

// findColrEnumCS scans a jp2h superbox's children for a 'colr' box with an
// enumerated method (METH=1) and returns its EnumCS, or -1.
func findColrEnumCS(jp2h []byte) int {
	for p := 0; p+8 <= len(jp2h); {
		boxLen := int(binary.BigEndian.Uint32(jp2h[p : p+4]))
		typ := string(jp2h[p+4 : p+8])
		contentStart := p + 8
		boxEnd := p + boxLen
		if boxLen == 0 {
			boxEnd = len(jp2h)
		}
		if boxEnd < contentStart || boxEnd > len(jp2h) {
			return -1
		}
		if typ == "colr" && contentStart+7 <= boxEnd {
			// colr: METH(1) PREC(1) APPROX(1) [EnumCS(4) if METH==1].
			if jp2h[contentStart] == 1 {
				return int(binary.BigEndian.Uint32(jp2h[contentStart+3 : contentStart+7]))
			}
		}
		if boxEnd == p {
			break
		}
		p = boxEnd
	}
	return -1
}
