package opentile

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

// DirectoryKind classifies a TIFF IFD's semantic role.
type DirectoryKind uint8

const (
	DirOther      DirectoryKind = iota // hidden / Map / SubIFD not surfaced elsewhere
	DirLevel                           // a pyramid level
	DirAssociated                      // an associated image
)

// TIFFDirectory is one IFD with structured identity. Image/Level are valid
// when Kind==DirLevel; Associated is the associated image Type() when
// Kind==DirAssociated.
type TIFFDirectory struct {
	Kind       DirectoryKind
	Image      int
	Level      int
	Associated string
	Tags       TIFFTags
}

// Tag is a convenience for d.Tags.Tag(number).
func (d TIFFDirectory) Tag(number uint16) (TIFFTag, bool) { return d.Tags.Tag(number) }
