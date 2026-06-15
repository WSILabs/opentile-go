package decoder

// Prober is implemented by codec Factories that can return codec-domain
// metadata about an encoded codestream from its header alone — without fully
// decoding the frame (GH #41). Not every Factory implements it; consumers
// type-assert:
//
//	f, _ := decoder.GetByCompressionTag(tag)
//	if p, ok := f.(decoder.Prober); ok {
//		info, err := p.Probe(compressed)
//	}
//
// Implemented by jpeg, jpeg2000, htj2k, and jpegxl. Codecs without a meaningful
// codestream header (none / lzw / deflate / webp / avif) do not implement it,
// so the type assertion reports ok == false.
type Prober interface {
	// Probe parses src's codestream header and returns codec-domain metadata
	// without decoding the pixels. Returns ErrCorruptInput if the header can't
	// be parsed. Header-only is the contract: it must not decode the full frame.
	Probe(src []byte) (CodestreamInfo, error)
}

// CodestreamInfo is header-only metadata about an encoded tile/frame, obtained
// without a full decode (GH #41). It carries the codec-domain facts a consumer
// needs to frame-copy a tile verbatim into a target container (e.g. a DICOM
// TransferSyntax + PhotometricInterpretation) without re-decoding.
//
// Mapping the codec-domain fields to a target container's vocabulary (e.g.
// DICOM PhotometricInterpretation) is the consumer's job; this struct is
// codec-domain only.
type CodestreamInfo struct {
	// Components is the sample/channel count (1 = grayscale, 3 = color,
	// 4 = e.g. CMYK or color+alpha).
	Components int

	// BitDepth is the bits per component (typically 8; J2K/JXL may be higher).
	BitDepth int

	// Lossless reports whether the codestream is reversible/lossless. It is a
	// tri-state because not every codec exposes a header-only reversibility
	// signal: JPEG 2000 / HTJ2K report it from the COD transform and JPEG
	// baseline is always lossy, but JPEG XL's JxlBasicInfo carries no lossless
	// flag, so JXL reports LosslessUnknown.
	Lossless Lossless

	// ColorEncoding is the codec-domain color encoding of the samples.
	ColorEncoding ColorEncoding

	// Boxed reports whether src is a boxed container (JP2 / JPH / JXL container)
	// rather than a raw codestream (J2K / raw JXL). Targets such as DICOM
	// require a specific encapsulated form (e.g. HTJ2K wants the raw .j2c
	// codestream, not the .jph box), so consumers need this before frame-copy.
	Boxed bool
}

// Lossless is the tri-state reversibility of a codestream.
type Lossless uint8

const (
	// LosslessUnknown means the codec exposes no header-only reversibility
	// signal (JPEG XL).
	LosslessUnknown Lossless = iota
	// LosslessYes means the codestream is reversible/lossless (J2K 5/3,
	// HTJ2K reversible).
	LosslessYes
	// LosslessNo means the codestream is lossy (J2K 9/7, JPEG baseline).
	LosslessNo
)

func (l Lossless) String() string {
	switch l {
	case LosslessYes:
		return "lossless"
	case LosslessNo:
		return "lossy"
	default:
		return "unknown"
	}
}

// ColorEncoding is the codec-domain color encoding of a codestream. The
// YBR_ICT / YBR_RCT values are the JPEG 2000 multiple-component transforms
// (irreversible / reversible); the String() forms match the DICOM
// PhotometricInterpretation spelling for convenience.
type ColorEncoding uint8

const (
	ColorUnknown   ColorEncoding = iota
	ColorGrayscale               // single luminance channel
	ColorRGB                     // RGB components, no decorrelating transform
	ColorYCbCr                   // JPEG (JFIF) luma/chroma
	ColorYBRICT                  // JPEG 2000 irreversible MCT (lossy)
	ColorYBRRCT                  // JPEG 2000 reversible MCT (lossless)
)

func (c ColorEncoding) String() string {
	switch c {
	case ColorGrayscale:
		return "grayscale"
	case ColorRGB:
		return "RGB"
	case ColorYCbCr:
		return "YCbCr"
	case ColorYBRICT:
		return "YBR_ICT"
	case ColorYBRRCT:
		return "YBR_RCT"
	default:
		return "unknown"
	}
}
