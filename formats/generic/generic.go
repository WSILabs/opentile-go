package generic

import (
	"errors"
	"fmt"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/tiff"
)

// Factory is the FormatFactory implementation for generic tiled
// pyramidal TIFF — the catch-all reader registered LAST in the
// dispatch order so vendor format detectors get first crack at any
// TIFF.
type Factory struct{ opentile.RawUnsupported }

// New returns a generic-TIFF factory. Safe to call once and register
// globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier.
func (f *Factory) Format() opentile.Format { return opentile.FormatGeneric }

// Supports reports whether file looks like a generic pyramidal TIFF
// per the v0.10 spec §4 algorithm: ≥3 tiled IFDs forming a coherent
// pyramid, each carrying valid uint8 RGB/YCbCr/grayscale photometric
// + whitelisted compression. Multi-pyramid TIFFs (more than 2
// leftover tiled IFDs, or any leftover larger than 1% of baseline
// area) are rejected — those are OME's job.
//
// Detection is conservative: when in doubt, return false. The
// dispatch loop falls through to ErrUnsupportedFormat rather than
// silently misclassifying a vendor-shaped TIFF.
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	infos := make([]tiff.PyramidLevelInfo, 0, len(pages))
	for i, p := range pages {
		infos = append(infos, tiff.PyramidLevelInfoFromPage(i, p))
	}
	_, err := tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
	return err == nil
}

// Open constructs a generic-TIFF Tiler. Re-runs ClassifyPyramid to
// project the page slice into Pyramid + Others, builds a tiledImage
// per pyramid level, and runs each Other through ClassifyAssociated
// + newAssociatedImage. Associated IFDs that hit the v0.10 unsupported
// shapes (multi-strip Deflate, tiled associated) are silently dropped
// per spec §6 — the IFD is recognised but not exposed.
//
// cfg is currently unused (no tunable knobs at v0.10); accepted for
// interface symmetry with the other format factories.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	pages := file.Pages()
	infos := make([]tiff.PyramidLevelInfo, 0, len(pages))
	for i, p := range pages {
		infos = append(infos, tiff.PyramidLevelInfoFromPage(i, p))
	}
	res, err := tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
	if err != nil {
		return nil, fmt.Errorf("generic: %w", err)
	}
	if len(res.Pyramid) == 0 {
		// ClassifyPyramid would have errored already, but defend the
		// invariant — every downstream call assumes a non-empty pyramid.
		return nil, errors.New("generic: pyramid empty after classification")
	}

	r := file.ReaderAt()
	levels := make([]opentile.Level, 0, len(res.Pyramid))
	for i, info := range res.Pyramid {
		lvl, err := newTiledImage(i, i, pages[info.Index], r)
		if err != nil {
			return nil, fmt.Errorf("generic: level %d (page %d): %w", i, info.Index, err)
		}
		levels = append(levels, lvl)
	}

	baseline := res.Pyramid[0]
	var associated []opentile.AssociatedImage
	for _, info := range res.Others {
		kind := ClassifyAssociated(info, baseline)
		src := associatedSourceInfoFromClassified(pages[info.Index], info)
		a, err := newAssociatedImage(kind, src, r)
		if err != nil {
			if errors.Is(err, errUnsupportedAssociatedShape) {
				// Spec §6: silently drop unsupported IFDs (multi-strip
				// Deflate, tiled associated) — the IFD is recognised
				// but not exposed via Associated().
				continue
			}
			return nil, fmt.Errorf("generic: associated %s (page %d): %w", kind, info.Index, err)
		}
		associated = append(associated, a)
	}

	icc, _ := pages[res.Pyramid[0].Index].ICCProfile()
	md := buildMetadata(pages[res.Pyramid[0].Index])

	return &tiler{
		md:         md,
		levels:     levels,
		associated: associated,
		icc:        icc,
	}, nil
}

// associatedSourceInfoFromClassified projects the tiff.Page's tags
// into the associatedSourceInfo value newAssociatedImage needs. The
// classifier's PyramidLevelInfo carries width/height/compression/
// tile-vs-strip already; we fetch the strip tables off the Page.
//
// Errors reading the strip tables collapse to empty slices — the
// constructor will reject the IFD with a clear message.
func associatedSourceInfoFromClassified(p *tiff.Page, info tiff.PyramidLevelInfo) associatedSourceInfo {
	rps, _ := p.ScalarU32(tiff.TagRowsPerStrip)
	stripOff, _ := p.ScalarArrayU64(tiff.TagStripOffsets)
	stripCnt, _ := p.ScalarArrayU64(tiff.TagStripByteCounts)
	return associatedSourceInfo{
		tiled:        info.IsTiled(),
		width:        info.Width,
		height:       info.Height,
		rowsPerStrip: rps,
		samples:      info.SamplesPerPixel,
		compression:  info.Compression,
		stripOffsets: stripOff,
		stripCounts:  stripCnt,
	}
}
