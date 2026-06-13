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
	seenIFDs := make(map[int]bool)
	var dirSpecs []scnDirSpec

	var associated []opentile.AssociatedImage
	for _, aux := range auxs {
		a, err := newAssociatedImage(aux, file, r)
		if err != nil {
			if errors.Is(err, errUnsupportedAuxiliary) {
				// All dimension IFDs for this skipped auxiliary become DirOther.
				for _, d := range aux.Dimensions {
					if d.IFD >= 0 && d.IFD < len(pages) && !seenIFDs[d.IFD] {
						dirSpecs = append(dirSpecs, scnDirSpec{page: pages[d.IFD], typ: opentile.DirOther})
						seenIFDs[d.IFD] = true
					}
				}
				continue
			}
			return nil, fmt.Errorf("leicascn: auxiliary %q: %w", aux.Name, err)
		}
		associated = append(associated, a)
		// Identify the representative IFD: lowest-resolution dimension
		// (smallest pixel area), mirroring newAssociatedImage's lo selection.
		lo := aux.Dimensions[0]
		for _, d := range aux.Dimensions[1:] {
			if int64(d.SizeX)*int64(d.SizeY) < int64(lo.SizeX)*int64(lo.SizeY) {
				lo = d
			}
		}
		if lo.IFD >= 0 && lo.IFD < len(pages) && !seenIFDs[lo.IFD] {
			dirSpecs = append(dirSpecs, scnDirSpec{page: pages[lo.IFD], typ: opentile.DirAssociated, assoc: "overview"})
			seenIFDs[lo.IFD] = true
		}
		// Remaining aux dimension IFDs become DirOther.
		for _, d := range aux.Dimensions {
			if d.IFD == lo.IFD {
				continue
			}
			if d.IFD >= 0 && d.IFD < len(pages) && !seenIFDs[d.IFD] {
				dirSpecs = append(dirSpecs, scnDirSpec{page: pages[d.IFD], typ: opentile.DirOther})
				seenIFDs[d.IFD] = true
			}
		}
	}

	// Compose the multi-region pyramid.
	composite, err := ComposePyramid(mains, c)
	if err != nil {
		return nil, fmt.Errorf("leicascn: %w", err)
	}

	// Build per-level compositeLevels and value-type Level metadata.
	// While doing so, capture dirSpecs: for each level, the canonical IFD
	// (region 0 / channel 0) becomes DirLevel; all other region/channel
	// IFDs at that level become DirOther.
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
			// Capture dirSpecs for this region's IFDs.
			for chi, ifdIdx := range rl.IFDPerChannel {
				if ifdIdx < 0 || ifdIdx >= len(pages) || seenIFDs[ifdIdx] {
					continue
				}
				if ri == 0 && chi == 0 {
					// Canonical page for this composite level.
					dirSpecs = append(dirSpecs, scnDirSpec{page: pages[ifdIdx], typ: opentile.DirLevel, level: li})
				} else {
					dirSpecs = append(dirSpecs, scnDirSpec{page: pages[ifdIdx], typ: opentile.DirOther})
				}
				seenIFDs[ifdIdx] = true
			}
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

	// Capture any remaining IFDs (not surfaced as a level or associated image)
	// as DirOther.
	for i, pg := range pages {
		if !seenIFDs[i] {
			dirSpecs = append(dirSpecs, scnDirSpec{page: pg, typ: opentile.DirOther})
		}
	}

	images := []opentile.Pyramid{{Name: "", Index: 0, Levels: valueLevels}}

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
		dirSpecs:   dirSpecs,
	}, nil
}
