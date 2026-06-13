package opentile

import "github.com/wsilabs/opentile-go/internal/tiff"

// pixelPointerTags are the bulk pixel-data-pointer tags excluded from the
// public API (regenerated on re-encode; not metadata): StripOffsets,
// StripByteCounts, TileOffsets, TileByteCounts.
var pixelPointerTags = map[uint16]bool{273: true, 279: true, 324: true, 325: true}

// tiffTagNames is a best-effort dictionary of well-known TIFF tag names.
// Number is always authoritative; unknown tags get Name == "".
var tiffTagNames = map[uint16]string{
	256: "ImageWidth", 257: "ImageLength", 258: "BitsPerSample",
	259: "Compression", 262: "PhotometricInterpretation", 270: "ImageDescription",
	271: "Make", 272: "Model", 274: "Orientation", 277: "SamplesPerPixel",
	282: "XResolution", 283: "YResolution", 284: "PlanarConfiguration",
	296: "ResolutionUnit", 305: "Software", 306: "DateTime",
	322: "TileWidth", 323: "TileLength", 339: "SampleFormat",
	34665: "ExifIFD", 34675: "ICCProfile",
}

// TIFFTagsFromPage builds the public TIFFTags for a parsed TIFF page. For
// use by format readers implementing the TIFF-tag provider; not part of
// the consumer-facing surface (the parameter type is internal).
func TIFFTagsFromPage(p *tiff.Page) TIFFTags {
	return tiffTagsFrom(p.RawTags())
}

// tiffTagsFrom translates internal raw tags to the public TIFFTags: maps
// types, applies the name dictionary, and drops the pixel-pointer denylist.
func tiffTagsFrom(raw []tiff.RawTag) TIFFTags {
	out := make(TIFFTags, 0, len(raw))
	for _, r := range raw {
		if pixelPointerTags[r.Number] {
			continue
		}
		t := TIFFTag{
			Number: r.Number,
			Name:   tiffTagNames[r.Number],
			Type:   TIFFType(r.Type),
			Count:  r.Count,
			Raw:    r.Raw,
			ascii:  r.ASCII,
			uints:  r.Uints,
		}
		for _, rr := range r.Rationals {
			t.rationals = append(t.rationals, Rational{Num: rr[0], Denom: rr[1]})
		}
		out = append(out, t)
	}
	return out
}

// TIFFType mirrors the TIFF field type. Named so consumers can interpret
// TIFFTag.Type without a magic-number table.
type TIFFType uint16

const (
	TIFFByte      TIFFType = 1
	TIFFASCII     TIFFType = 2
	TIFFShort     TIFFType = 3
	TIFFLong      TIFFType = 4
	TIFFRational  TIFFType = 5
	TIFFUndefined TIFFType = 7
	TIFFSShort    TIFFType = 8
	TIFFSLong     TIFFType = 9
	TIFFSRational TIFFType = 10
	TIFFLong8     TIFFType = 16
)

// Rational is an unsigned TIFF RATIONAL value.
type Rational struct{ Num, Denom uint32 }

// TIFFTag is one parsed TIFF tag, typed. Number is always set (the key for
// vendor/private tags); Name is "" when not in the known-tag dictionary.
// Raw is the verbatim payload (file byte order) for faithful re-encode.
type TIFFTag struct {
	Number uint16
	Name   string
	Type   TIFFType
	Count  int
	Raw    []byte

	// decoded forms (populated by the translator; exposed via getters)
	ascii     string
	uints     []uint64
	rationals []Rational
}

// ASCII returns the string value, ok=false unless Type==TIFFASCII.
func (t TIFFTag) ASCII() (string, bool) {
	if t.Type != TIFFASCII {
		return "", false
	}
	return t.ascii, true
}

// Uints returns unsigned integer values, ok=false unless the type is an
// unsigned integer type.
func (t TIFFTag) Uints() ([]uint64, bool) {
	switch t.Type {
	case TIFFByte, TIFFShort, TIFFLong, TIFFLong8:
		return t.uints, true
	}
	return nil, false
}

// Rationals returns rational values, ok=false unless Type==TIFFRational.
func (t TIFFTag) Rationals() ([]Rational, bool) {
	if t.Type != TIFFRational {
		return nil, false
	}
	return t.rationals, true
}

// TIFFTags is the set of tags on one IFD with lookup helpers.
type TIFFTags []TIFFTag

// Tag returns the tag with the given number, ok=false if absent.
func (ts TIFFTags) Tag(number uint16) (TIFFTag, bool) {
	for _, t := range ts {
		if t.Number == number {
			return t, true
		}
	}
	return TIFFTag{}, false
}

// ByName returns the tag with the given dictionary name, ok=false if absent.
func (ts TIFFTags) ByName(name string) (TIFFTag, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	return TIFFTag{}, false
}

// DirectoryType classifies a TIFF IFD's semantic role.
type DirectoryType uint8

const (
	DirOther      DirectoryType = iota // hidden / Map / SubIFD not surfaced elsewhere
	DirLevel                           // a pyramid level
	DirAssociated                      // an associated image
)

// TIFFDirectory is one IFD with structured identity. Image/Level are valid
// when Type==DirLevel; AssociatedType is the associated image Type() when
// Type==DirAssociated.
type TIFFDirectory struct {
	Type           DirectoryType
	Image          int
	Level          int
	AssociatedType AssociatedType
	Tags           TIFFTags
}

// Tag is a convenience for d.Tags.Tag(number).
func (d TIFFDirectory) Tag(number uint16) (TIFFTag, bool) { return d.Tags.Tag(number) }

// tiffTagProvider is implemented by TIFF-based format readers. The method
// is exported because readers live in other packages. Returns every IFD
// with structured identity; the Slide accessors derive views from it.
type tiffTagProvider interface {
	TIFFDirectories() []TIFFDirectory
}

// tiffProviderOf walks the UnwrapReader chain (like the MetadataOf helpers)
// looking for a reader that implements tiffTagProvider.
func tiffProviderOf(s *Slide) (tiffTagProvider, bool) {
	var cur any = s.r
	for cur != nil {
		if p, ok := cur.(tiffTagProvider); ok {
			return p, true
		}
		u, ok := cur.(interface{ UnwrapReader() any })
		if !ok {
			return nil, false
		}
		cur = u.UnwrapReader()
	}
	return nil, false
}

// TIFFDirectories enumerates every TIFF IFD (including orphan IFDs not
// surfaced as a level or associated image). ok=false for non-TIFF formats
// (IFE, SZI). The escape hatch for "dump all"; prefer LevelTIFFTags /
// AssociatedImage.TIFFTags() for everyday access.
func (s *Slide) TIFFDirectories() ([]TIFFDirectory, bool) {
	p, ok := tiffProviderOf(s)
	if !ok {
		return nil, false
	}
	return p.TIFFDirectories(), true
}

// imageLevelTIFFTags returns the TIFF tags of image's level's backing IFD,
// keyed by (image, level). ok=false for non-TIFF formats or an
// out-of-range (image, level). Backs (*Level).TIFFTags.
func (s *Slide) imageLevelTIFFTags(image, level int) (TIFFTags, bool) {
	dirs, ok := s.TIFFDirectories()
	if !ok {
		return nil, false
	}
	for _, d := range dirs {
		if d.Type == DirLevel && d.Image == image && d.Level == level {
			return d.Tags, true
		}
	}
	return nil, false
}

// AssociatedTIFFTags and AssociatedIFDOffset have moved onto the AssociatedImage
// interface as TIFFTags() and IFDOffset(). Callers should use a.TIFFTags() and
// a.IFDOffset() directly instead of the former Slide methods.
