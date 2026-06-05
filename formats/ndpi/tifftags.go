package ndpi

import (
	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// ndpiDirSpec captures the *tiff.Page pointer and semantic role of one IFD,
// recorded at Open time so TIFFDirectories() can build the public view lazily.
//
// NDPI is single-image (image == 0 always). Each pyramid level has its own
// page (strippedImage or oneframe), and the overview (Macro) and map pages
// each have their own IFD. The synthesized label image shares the overview
// page and is NOT a separate IFD; it does not appear in dirSpecs.
type ndpiDirSpec struct {
	page  *tiff.Page
	typ   opentile.DirectoryType
	level int    // valid when typ==DirLevel; equals levelIdx at construction
	assoc string // valid when typ==DirAssociated; matches AssociatedImage.Type()
}

// TIFFDirectories exposes the raw TIFF tags per IFD, lazily decoded.
// Implements opentile's (unexported) tiffTagProvider interface so that
// LevelTIFFTags, AssociatedTIFFTags, and TIFFDirectoriesOf all work for
// NDPI slides.
//
// NDPI is single-image: Image is always 0. The overview page is surfaced
// as DirAssociated with Associated=="overview"; the map page (when present)
// as DirAssociated with Associated=="map". Unknown/malformed pages appear
// as DirOther. The synthesized label image shares the overview IFD and is
// not separately listed.
func (t *tiler) TIFFDirectories() []opentile.TIFFDirectory {
	out := make([]opentile.TIFFDirectory, 0, len(t.dirSpecs))
	for _, ds := range t.dirSpecs {
		if ds.page == nil {
			continue
		}
		out = append(out, opentile.TIFFDirectory{
			Type:           ds.typ,
			Image:          0, // NDPI is single-image
			Level:          ds.level,
			AssociatedType: ds.assoc,
			Tags:           opentile.TIFFTagsFromPage(ds.page),
		})
	}
	return out
}
