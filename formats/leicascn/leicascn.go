package leicascn

import (
	"errors"
	"fmt"
	"strings"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// Factory is the FormatFactory implementation for Leica SCN.
// Registered BEFORE generictiff in formats/all so vendor detection
// wins on any TIFF that smells like SCN.
type Factory struct{ opentile.RawUnsupported }

// New returns a Leica SCN factory. Safe to call once and register
// globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier.
func (f *Factory) Format() opentile.Format { return opentile.FormatLeicaSCN }

// Supports reports whether file looks like a Leica SCN BigTIFF.
// Discriminator (sealed Q1): IFD 0's ImageDescription contains the
// SCN schema URN. Cheap substring search; full XML parse happens at
// Open time.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return false
	}
	return strings.Contains(desc, SchemaURN)
}

// Open constructs a Leica SCN Tiler. Parses IFD 0's SCN XML,
// classifies <image> elements into auxiliaries (→ AssociatedImages)
// and main scans (→ composite Image levels), validates the multi-
// main composition invariants, and builds the Tiler.
//
// T6 leaves the Levels slice empty pending T7+ wiring; AssociatedImages
// and Metadata are populated end-to-end so Tiler.Format() / .Associated()
// / .Metadata() / .ICCProfile() are functional.
//
// cfg is currently unused (no SCN-specific knobs at v0.11); accepted
// for interface symmetry with the other format factories.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	pages := file.Pages()
	if len(pages) == 0 {
		return nil, errors.New("leicascn: file has no pages")
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return nil, errors.New("leicascn: missing IFD 0 ImageDescription")
	}
	c, err := ParseDescription(desc)
	if err != nil {
		return nil, fmt.Errorf("leicascn: %w", err)
	}

	// Partition <image> elements: view==collection → auxiliary, else → main.
	var auxs, mains []Image
	for _, img := range c.Images {
		if IsAuxiliary(img, c) {
			auxs = append(auxs, img)
		} else {
			mains = append(mains, img)
		}
	}
	if len(mains) == 0 {
		return nil, fmt.Errorf("leicascn: no main scan <image> elements (file has only %d auxiliaries)",
			len(auxs))
	}

	// Build AssociatedImages from auxiliaries. Multi-tile-lowest-res
	// auxiliaries silently drop per Q8 (errUnsupportedAuxiliary
	// filtered).
	r := file.ReaderAt()
	var associated []opentile.AssociatedImage
	for _, aux := range auxs {
		a, err := newAssociatedImage(aux, file, r)
		if err != nil {
			if errors.Is(err, errUnsupportedAuxiliary) {
				continue
			}
			return nil, fmt.Errorf("leicascn: auxiliary %q: %w", aux.Name, err)
		}
		associated = append(associated, a)
	}

	// Compose the multi-region pyramid (validates Q5 invariants).
	composite, err := ComposePyramid(mains, c)
	if err != nil {
		return nil, fmt.Errorf("leicascn: %w", err)
	}
	_ = composite // T7+ wires Levels; T6 leaves the slice empty.

	// Determine SizeC from first main's dimensions: max(c) + 1.
	sizeC := 1
	for _, d := range mains[0].Dimensions {
		if d.C+1 > sizeC {
			sizeC = d.C + 1
		}
	}

	icc, _ := pages[0].ICCProfile()

	md := buildMetadata(c, auxs, mains)

	return &tiler{
		md:         md,
		levels:     nil, // T7-T10 populates from composite
		associated: associated,
		icc:        icc,
		sizeC:      sizeC,
		channels:   md.Channels,
	}, nil
}
