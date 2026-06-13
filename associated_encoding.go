package opentile

// AssociatedEncoding is the on-disk encoded form of an associated image: the
// source IFD's strips plus the TIFF tags a consumer must set to re-emit them
// faithfully into a new standalone single-IFD TIFF — a byte-identical copy
// with NO re-encode (unlike AssociatedImage.Decode, which decodes to pixels,
// or Bytes, whose JPEG output is abbreviated and depends on the source IFD's
// JPEGTables).
//
// To write a faithful standalone TIFF, set on a fresh IFD: ImageWidth/Length
// from a.Size(), BitsPerSample=8, SamplesPerPixel=Samples,
// PhotometricInterpretation=Photometric, Compression, RowsPerStrip,
// Predictor (tag 317, only if >1), JPEGTables (tag 347, only if non-nil), and
// StripOffsets/StripByteCounts pointing at the Strips written verbatim.
type AssociatedEncoding struct {
	Strips       [][]byte    // source strip bytes, in document order (written verbatim)
	Compression  Compression // tag 259
	Predictor    int         // tag 317 (1 none / 2 horizontal differencing)
	JPEGTables   []byte      // tag 347 (DQT/DHT); nil when absent or inline
	RowsPerStrip int         // tag 278
	Samples      int         // tag 277 SamplesPerPixel
	Photometric  int         // tag 262 PhotometricInterpretation
}

// associatedEncoder is the optional capability implemented by associated
// images that can describe their faithful on-disk encoded form. It is NOT part
// of the AssociatedImage interface (no breaking change); AssociatedEncoding
// discovers it by type assertion.
type associatedEncoder interface {
	AssociatedEncoding() (AssociatedEncoding, bool)
}

// AssociatedEncoding returns the encoded source strips + TIFF tags for
// associated image a, for byte-identical re-emission into a new standalone
// TIFF with no re-encode (GH #22). ok=false for non-TIFF slides, formats that
// haven't opted in, and synthesized or non-strip associated images that have
// no faithful single-IFD strip representation (e.g. NDPI's synthesized label,
// DICOM frames, OME planar pages, tiled associated images).
func (s *Slide) AssociatedEncoding(a AssociatedImage) (AssociatedEncoding, bool) {
	if p, ok := a.(associatedEncoder); ok {
		return p.AssociatedEncoding()
	}
	return AssociatedEncoding{}, false
}
