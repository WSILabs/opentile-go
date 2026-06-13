package szi

import (
	"archive/zip"
	stdimage "image"
	_ "image/jpeg" // register JPEG decoder for stdimage.DecodeConfig

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/assocdecode"
)

// associatedImage is the SZI-format opentile.AssociatedImage
// implementation. Backed by a single ZIP entry (a JPEG file under
// associated_images/).
//
// The SZI spec mandates JPEG for entries in associated_images/;
// dimensions are decoded from the JPEG header at Open() time via
// image.DecodeConfig. Bytes() reads the entire entry on demand.
type associatedImage struct {
	imgType opentile.AssociatedType // v0.15 Type() value: "label" / "overview" / "thumbnail"
	entry   *zip.File
	width   int
	height  int
}

// Type returns the v0.15-aligned associated-image type. Per the
// v0.16 spec mapping, macro.jpg surfaces as "overview" (the DICOM-
// canonical wide-field-slide-image term used by SVS / Philips /
// OME / BIF / leicascn / generictiff); label.jpg as "label"; and
// thumbnail.jpg as "thumbnail".
func (a *associatedImage) Type() opentile.AssociatedType { return a.imgType }

// Size returns the JPEG image dimensions decoded from the header.
func (a *associatedImage) Size() opentile.Size {
	return opentile.Size{W: a.width, H: a.height}
}

// Compression always returns CompressionJPEG. The SZI spec mandates
// JPEG for associated_images/ entries.
func (a *associatedImage) Compression() opentile.Compression {
	return opentile.CompressionJPEG
}

// Decode returns the decoded associated-image pixels via the registered
// JPEG decoder (GH #20).
func (a *associatedImage) Decode(opts decoder.DecodeOptions) (*decoder.Image, error) {
	data, err := a.Bytes()
	if err != nil {
		return nil, err
	}
	return assocdecode.ViaCodec(a.Compression(), data, opts)
}

// Bytes returns the raw JPEG bytes of the associated image.
func (a *associatedImage) Bytes() ([]byte, error) {
	return readZipEntry(a.entry)
}

// decodeJPEGDims reads the JPEG header to extract dimensions. Returns
// (width, height, nil) on success. Used at Open() time only; failures
// cause the image to be silently skipped (don't fail the file load).
func decodeJPEGDims(entry *zip.File) (int, int, error) {
	rc, err := entry.Open()
	if err != nil {
		return 0, 0, err
	}
	defer rc.Close()
	cfg, _, err := stdimage.DecodeConfig(rc)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// buildAssociated discovers the optional associated_images/ folder
// and populates t.associated. Filenames per spec page 5:
//
//	macro.jpg     → Type() == "overview"  (per v0.15 alignment)
//	label.jpg     → Type() == "label"
//	thumbnail.jpg → Type() == "thumbnail"
//
// Missing files are skipped (the entire folder is optional). JPEG
// decode failures also skip silently — the rest of the file load
// proceeds. Image dimensions are decoded from JPEG headers eagerly
// at Open() time; subsequent Size() calls return cached values.
func (t *Tiler) buildAssociated() {
	mapping := []struct {
		filename string
		typ      opentile.AssociatedType
	}{
		{"macro.jpg", opentile.AssociatedOverview},
		{"label.jpg", opentile.AssociatedLabel},
		{"thumbnail.jpg", opentile.AssociatedThumbnail},
	}
	for _, m := range mapping {
		p := t.rootDir + "/associated_images/" + m.filename
		entry, ok := t.entries[p]
		if !ok {
			continue
		}
		w, h, err := decodeJPEGDims(entry)
		if err != nil {
			// Malformed JPEG: skip but don't fail the file load.
			continue
		}
		t.associated = append(t.associated, &associatedImage{
			imgType: m.typ,
			entry:   entry,
			width:   w,
			height:  h,
		})
	}
}
