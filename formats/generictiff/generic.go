package generictiff

import (
	"errors"
	"fmt"
	"sort"

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
func (f *Factory) Format() opentile.Format { return opentile.FormatGenericTIFF }

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
	// v0.19: WSI-tag short-circuit for the pyramid build. When every
	// tiled candidate IFD carries the WSILevelIndex private tag
	// (COG-WSI spec §5.2), trust the writer's declared ordering and
	// skip ClassifyPyramid's dimension-ratio drift check. Non-tiled
	// or non-tagged IFDs flow to Others as usual. When tags are
	// absent / partial, fall through to the pre-v0.19 heuristic path.
	res, ok, err := classifyByWSITag(pages, infos)
	if err != nil {
		return nil, fmt.Errorf("generic: %w", err)
	}
	if !ok {
		res, err = tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
		if err != nil {
			return nil, fmt.Errorf("generic: %w", err)
		}
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
		// v0.19: WSI-tag-aware classification. Honors COG-WSI's
		// authoritative WSIImageType tag when present; falls back to
		// the pre-v0.19 dimension/aspect heuristics otherwise.
		kind := ClassifyAssociatedFromPage(pages[info.Index], info, baseline)
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

// classifyByWSITag builds a [tiff.ClassifyPyramidResult] directly
// from the COG-WSI private tags when every tiled candidate IFD
// carries [tiff.TagWSILevelIndex]. Returns (result, true, nil) on
// the WSI-tag path; (zero, false, nil) when the tag-set is absent
// or partial (caller falls back to dimension-ratio
// [tiff.ClassifyPyramid]); a non-nil error only on the duplicate-
// index case the writer is unambiguously responsible for.
//
// v0.19 (Issue #5 part A): a COG-WSI writer that emits
// WSIImageType=pyramid + WSILevelIndex on every pyramid IFD has
// already declared the level ordering. Trusting the writer skips
// drift-tolerance gymnastics on integer-multiple chains (handled
// by T3) and on weirder scale series (e.g., 3.33×, 2.5×) that the
// dimension-ratio heuristic would reject.
//
// Selection rule: every tiled IFD MUST carry WSILevelIndex; tiled
// IFDs with WSILevelIndex go to Pyramid (ascending by declared
// index), everything else (untiled IFDs, plus any tiled IFD whose
// WSIImageType marks it as non-pyramid) goes to Others. A tiled
// IFD that lacks WSILevelIndex but has a non-"pyramid" WSIImageType
// (e.g., a tiled overview) does not abort the short-circuit; only a
// tiled IFD that has neither tag does. This handles COG-WSI files
// whose pyramid IFDs all carry the tag but whose tiled associated
// images (rare but legal) do not.
func classifyByWSITag(pages []*tiff.Page, infos []tiff.PyramidLevelInfo) (tiff.ClassifyPyramidResult, bool, error) {
	type indexed struct {
		info tiff.PyramidLevelInfo
		lvl  uint32
	}
	var pyramid []indexed
	var others []tiff.PyramidLevelInfo
	for _, info := range infos {
		if !info.IsTiled() {
			others = append(others, info)
			continue
		}
		page := pages[info.Index]
		lvl, hasIdx := page.WSILevelIndex()
		if hasIdx {
			pyramid = append(pyramid, indexed{info, lvl})
			continue
		}
		// Tiled but no WSILevelIndex. If the writer explicitly tagged
		// it as non-pyramid (e.g., a tiled overview), route to Others
		// and keep the short-circuit alive. Otherwise, abandon — we
		// can't trust a heterogeneous tagging story.
		if wt, ok := page.WSIImageType(); ok && wt != "pyramid" {
			others = append(others, info)
			continue
		}
		return tiff.ClassifyPyramidResult{}, false, nil
	}
	if len(pyramid) == 0 {
		// No tiled candidates at all → not a generic pyramid; let the
		// dimension-ratio path produce the canonical error.
		return tiff.ClassifyPyramidResult{}, false, nil
	}
	// Ascending by declared level index; ties (writer bug) → error.
	sort.SliceStable(pyramid, func(i, j int) bool {
		return pyramid[i].lvl < pyramid[j].lvl
	})
	for i := 1; i < len(pyramid); i++ {
		if pyramid[i].lvl == pyramid[i-1].lvl {
			return tiff.ClassifyPyramidResult{}, false,
				fmt.Errorf("duplicate WSILevelIndex %d on IFDs %d and %d",
					pyramid[i].lvl, pyramid[i-1].info.Index, pyramid[i].info.Index)
		}
	}
	res := tiff.ClassifyPyramidResult{
		Pyramid: make([]tiff.PyramidLevelInfo, 0, len(pyramid)),
		Others:  others,
	}
	for _, p := range pyramid {
		res.Pyramid = append(res.Pyramid, p.info)
	}
	return res, true, nil
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
