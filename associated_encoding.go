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

// AssociatedEncoding is now part of the AssociatedImage interface as Encoding().
// Callers should use a.Encoding() directly instead of s.AssociatedEncoding(a).
