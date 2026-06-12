package dicom

import (
	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
	"github.com/wsilabs/opentile-go/internal/tiffstrip"
)

// associatedImage serves a single-frame WSM instance (label/overview/
// thumbnail) as raw compressed bytes.
type associatedImage struct {
	typ         string
	size        opentile.Size
	compression opentile.Compression
	data        []byte // the single frame's bytes (already extracted at open time)
	samples     int    // SamplesPerPixel (0028,0002); authoritative for native decode
	photometric string // PhotometricInterpretation (0028,0004)
}

func (a *associatedImage) Type() string                      { return a.typ }
func (a *associatedImage) Size() opentile.Size               { return a.size }
func (a *associatedImage) Compression() opentile.Compression { return a.compression }
func (a *associatedImage) Bytes() ([]byte, error) {
	out := make([]byte, len(a.data))
	copy(out, a.data)
	return out, nil
}

// Decode returns the decoded associated-image pixels (GH #20). Encapsulated
// frames (JPEG / JPEG 2000 / HTJ2K) decode via the registry; native/
// uncompressed frames decode via the strip path using the instance's
// authoritative SamplesPerPixel / PhotometricInterpretation. DICOM PixelData
// is padded to even length (PS3.5 §7.1.1), so the buffer can be one byte
// longer than w*h*samples — that pad byte is tolerated and trimmed (GH #21).
func (a *associatedImage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	if a.compression != opentile.CompressionNone {
		return assocdecode.ViaCodec(a.compression, a.data, opts)
	}
	w, h := a.size.W, a.size.H
	samples := a.samples
	if samples != 1 && samples != 3 && samples != 4 {
		// No usable SamplesPerPixel — infer from length, tolerating the ≤1-byte
		// even-length pad (len == w*h*s or w*h*s+1).
		samples = 0
		if w > 0 && h > 0 {
			for _, s := range []int{3, 4, 1} {
				if len(a.data) == w*h*s || len(a.data) == w*h*s+1 {
					samples = s
					break
				}
			}
		}
		if samples == 0 {
			samples = 1
		}
	}
	photo := 1 // BlackIsZero
	switch a.photometric {
	case "MONOCHROME1":
		photo = 0 // WhiteIsZero
	case "MONOCHROME2":
		photo = 1
	default:
		if samples >= 3 {
			photo = 2 // RGB
		}
	}
	data := a.data
	if need := w * h * samples; need > 0 && len(data) > need {
		data = data[:need] // drop the even-length pad byte
	}
	return tiffstrip.Decode(tiffstrip.Params{
		Width:       w,
		Height:      h,
		Samples:     samples,
		Photometric: photo,
		Compression: tiffstrip.CompNone,
		Strips:      [][]byte{data},
	}, opts)
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
// frame is extracted (encapsulated or native) into a heap []byte, then
// the mmap is closed. Instances that cannot be read or have no frames are
// silently skipped.
func buildAssociated(s series, open instanceBytes) []opentile.AssociatedImage {
	var out []opentile.AssociatedImage
	for _, a := range s.associated {
		data, closeFn, err := open(a.inst.Path)
		if err != nil {
			continue
		}
		frameData, _, err := extractFirstFrame(data)
		if err != nil {
			closeFn()
			continue
		}
		frame := make([]byte, len(frameData))
		copy(frame, frameData)
		closeFn() // associated bytes are copied out; the mmap is no longer needed
		out = append(out, &associatedImage{
			typ:         dicomTypeToOpentile(a.role),
			size:        opentile.Size{W: a.inst.TotalCols, H: a.inst.TotalRows},
			compression: compressionForSyntax(a.inst.TransferSyntax),
			data:        frame,
			samples:     a.inst.SamplesPerPixel,
			photometric: a.inst.Photometric,
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
	case "1.2.840.10008.1.2.4.90", // JPEG 2000 Image Compression (Lossless Only)
		"1.2.840.10008.1.2.4.91": // JPEG 2000 Image Compression
		return opentile.CompressionJP2K
	case "1.2.840.10008.1.2.4.201", // HTJ2K (Lossless Only)
		"1.2.840.10008.1.2.4.202", // HTJ2K with RPCL Options (Lossless Only)
		"1.2.840.10008.1.2.4.203": // HTJ2K
		return opentile.CompressionHTJ2K
	default:
		return opentile.CompressionJPEG // best-effort; all v1 fixture associated images are JPEG
	}
}
