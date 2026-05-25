package leicascn

import (
	"errors"
	"fmt"
	"io"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *tiler satisfies format.Reader.
var _ format.Reader = (*tiler)(nil)

func init() {
	format.Register("leicascn", matchLeicaSCN, openLeicaSCNFormat)
}

// matchLeicaSCN returns nil iff r is a Leica SCN BigTIFF (IFD 0's
// ImageDescription contains the SCN schema URN).
func matchLeicaSCN(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("leicascn: not a TIFF: %w", err)
	}
	pages := file.Pages()
	if len(pages) == 0 {
		return errors.New("leicascn: TIFF has no pages")
	}
	desc, ok := pages[0].ImageDescription()
	if !ok || !strings.Contains(desc, SchemaURN) {
		return errors.New("leicascn: ImageDescription does not contain SCN schema URN")
	}
	return nil
}

// openLeicaSCNFormat constructs a format.Reader from a raw reader.
func openLeicaSCNFormat(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("leicascn: %w", err)
	}
	return openFromTIFFFile(file, cfg)
}

// openFromTIFFFile is the shared construction path used by both
// openLeicaSCNFormat and Factory.Open.
func openFromTIFFFile(file *tiff.File, cfg *format.Config) (format.Reader, error) {
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

	// Build AssociatedImages from auxiliaries.
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

	// Compose the multi-region pyramid.
	composite, err := ComposePyramid(mains, c)
	if err != nil {
		return nil, fmt.Errorf("leicascn: %w", err)
	}

	// Build per-level compositeLevels and value-type Level metadata.
	levelImpls := make([]*compositeLevel, len(composite))
	valueLevels := make([]opentile.Level, len(composite))
	var l0Width int
	for li, cl := range composite {
		regions := make([]*tiledRegion, len(cl.Regions))
		for ri, rl := range cl.Regions {
			tr, err := newTiledRegion(rl, file, r)
			if err != nil {
				return nil, fmt.Errorf("leicascn: L%d region %d: %w", li, ri, err)
			}
			regions[ri] = tr
		}
		cmpl, err := newCompositeLevel(li, li, cl, regions)
		if err != nil {
			return nil, fmt.Errorf("leicascn: L%d composite: %w", li, err)
		}
		if li == 0 {
			l0Width = cmpl.size.W
		}
		levelImpls[li] = cmpl
		valueLevels[li] = opentile.Level{
			Index:        li,
			PyramidIndex: li,
			Size:         cmpl.size,
			TileSize:     cmpl.tileSize,
			Grid:         cmpl.grid,
			Compression:  cmpl.compression,
			Downsample:   float64(l0Width) / float64(cmpl.size.W),
		}
	}
	images := []opentile.Image{{Name: "", Index: 0, Levels: valueLevels}}

	sizeC := 1
	if len(composite) > 0 {
		sizeC = composite[0].SizeC
	}

	icc, _ := pages[0].ICCProfile()
	md := buildMetadata(c, auxs, mains, desc)

	return &tiler{
		md:         md,
		levelImpls: levelImpls,
		images:     images,
		associated: associated,
		icc:        icc,
		sizeC:      sizeC,
		channels:   md.Channels,
	}, nil
}

