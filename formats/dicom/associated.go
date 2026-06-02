package dicom

import (
	opentile "github.com/wsilabs/opentile-go"
)

// associatedImage serves a single-frame WSM instance (label/overview/
// thumbnail) as raw compressed bytes.
type associatedImage struct {
	typ         string
	size        opentile.Size
	compression opentile.Compression
	data        []byte // the single frame's bytes (already extracted at open time)
}

func (a *associatedImage) Type() string                      { return a.typ }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }
func (a *associatedImage) Bytes() ([]byte, error) {
	out := make([]byte, len(a.data))
	copy(out, a.data)
	return out, nil
}

// dicomTypeToOpentile maps a WSM ImageType token to an opentile Type() string.
func dicomTypeToOpentile(role string) string {
	switch role {
	case "LABEL":
		return "label"
	case "OVERVIEW":
		return "overview"
	case "THUMBNAIL":
		return "thumbnail"
	}
	return "associated"
}

// buildAssociated extracts single-frame associated images (label/overview/
// thumbnail) from the series. Each instance's bytes are read, the first
// encapsulated frame is extracted into a heap []byte, then the mmap is closed.
// Instances that cannot be read or have no frames are silently skipped.
func buildAssociated(s series, open instanceBytes) []opentile.AssociatedImage {
	var out []opentile.AssociatedImage
	for _, a := range s.associated {
		data, closeFn, err := open(a.inst.Path)
		if err != nil {
			continue
		}
		spans, err := walkEncapsulatedFrames(data)
		if err != nil || len(spans) == 0 {
			closeFn()
			continue
		}
		sp := spans[0]
		frame := make([]byte, sp.length)
		copy(frame, data[sp.off:sp.off+sp.length])
		closeFn() // associated bytes are copied out; the mmap is no longer needed
		out = append(out, &associatedImage{
			typ:         dicomTypeToOpentile(a.role),
			size:        opentile.Size{W: a.inst.TotalCols, H: a.inst.TotalRows},
			compression: compressionForSyntax(a.inst.TransferSyntax),
			data:        frame,
		})
	}
	return out
}

// compressionForSyntax maps a DICOM Transfer Syntax UID to opentile.Compression.
func compressionForSyntax(ts string) opentile.Compression {
	switch ts {
	case "1.2.840.10008.1.2.4.50": // JPEG Baseline (Process 1)
		return opentile.CompressionJPEG
	case "1.2.840.10008.1.2.1", "1.2.840.10008.1.2": // Explicit/Implicit VR Little Endian (uncompressed)
		return opentile.CompressionNone
	default:
		return opentile.CompressionJPEG // best-effort; all v1 fixture associated images are JPEG
	}
}
