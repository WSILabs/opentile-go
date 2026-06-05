package generictiff

import (
	"errors"
	"fmt"
	"io"
	"sort"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *tiler satisfies format.Reader.
var _ format.Reader = (*tiler)(nil)

func init() {
	// generictiff is the catch-all — registered as a fallback so vendor
	// detectors win regardless of import order.
	format.RegisterFallback("generictiff", matchGenericTIFF, openGenericTIFF)
}

// matchGenericTIFF returns nil iff r is a generic pyramidal TIFF per the
// v0.10 spec §4 algorithm. It is intentionally conservative: when in
// doubt, return an error so dispatch falls through to ErrUnknownFormat.
func matchGenericTIFF(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("generictiff: not a TIFF: %w", err)
	}
	pages := file.Pages()
	infos := make([]tiff.PyramidLevelInfo, 0, len(pages))
	for i, p := range pages {
		infos = append(infos, tiff.PyramidLevelInfoFromPage(i, p))
	}
	_, classErr := tiff.ClassifyPyramid(infos, tiff.DefaultClassifyPyramidConfig())
	if classErr != nil {
		return fmt.Errorf("generictiff: %w", classErr)
	}
	return nil
}

// openGenericTIFF constructs a format.Reader from a raw reader.
func openGenericTIFF(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("generictiff: %w", err)
	}
	return openFromTIFFFile(file, cfg)
}

// openFromTIFFFile is the shared construction path used by both openGenericTIFF
// and Factory.Open.
//
// cfg is currently unused (no tunable knobs at v0.10); accepted for
// interface symmetry with the other format constructors.
func openFromTIFFFile(file *tiff.File, cfg *format.Config) (format.Reader, error) {
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
	tiledLevels := make([]*tiledImage, 0, len(res.Pyramid))
	valueLevels := make([]opentile.Level, 0, len(res.Pyramid))
	var dirSpecs []genericDirSpec
	seenPages := make(map[int]bool)
	var l0Width int
	for i, info := range res.Pyramid {
		lvl, err := newTiledImage(i, i, pages[info.Index], r)
		if err != nil {
			return nil, fmt.Errorf("generic: level %d (page %d): %w", i, info.Index, err)
		}
		if i == 0 {
			l0Width = lvl.size.W
		}
		tiledLevels = append(tiledLevels, lvl)
		valueLevels = append(valueLevels, opentile.Level{
			Index:        lvl.index,
			PyramidIndex: lvl.pyrIndex,
			Size:         lvl.size,
			TileSize:     lvl.tileSize,
			Grid:         lvl.grid,
			Compression:  lvl.compression,
			MPP:          opentile.SizeMm{},
			FocalPlane:   0,
			Downsample:   float64(l0Width) / float64(lvl.size.W),
		})
		dirSpecs = append(dirSpecs, genericDirSpec{pageIdx: info.Index, typ: opentile.DirLevel, level: i})
		seenPages[info.Index] = true
	}

	baseline := res.Pyramid[0]
	var associated []opentile.AssociatedImage
	for _, info := range res.Others {
		// v0.19: WSI-tag-aware classification. Honors COG-WSI's
		// authoritative WSIImageType tag when present; falls back to
		// the pre-v0.19 dimension/aspect heuristics otherwise.
		imageType := ClassifyAssociatedFromPage(pages[info.Index], info, baseline)
		src := associatedSourceInfoFromClassified(pages[info.Index], info)
		a, err := newAssociatedImage(imageType, src, r)
		if err != nil {
			if errors.Is(err, errUnsupportedAssociatedShape) {
				// Spec §6: silently drop unsupported IFDs (multi-strip
				// Deflate, tiled associated) — the IFD is recognised
				// but not exposed via Associated(). Route to DirOther
				// so the page is still visible via TIFFDirectories.
				continue
			}
			return nil, fmt.Errorf("generic: associated %s (page %d): %w", imageType, info.Index, err)
		}
		associated = append(associated, a)
		dirSpecs = append(dirSpecs, genericDirSpec{pageIdx: info.Index, typ: opentile.DirAssociated, assoc: imageType})
		seenPages[info.Index] = true
	}
	// Capture orphan pages (IFDs not surfaced as a level or associated image).
	for i := range pages {
		if !seenPages[i] {
			dirSpecs = append(dirSpecs, genericDirSpec{pageIdx: i, typ: opentile.DirOther})
		}
	}

	icc, _ := pages[res.Pyramid[0].Index].ICCProfile()
	md := buildMetadata(pages[res.Pyramid[0].Index])
	images := []opentile.Image{{
		Name:   "",
		Index:  0,
		Levels: valueLevels,
	}}

	return &tiler{
		md:          md,
		tiledLevels: tiledLevels,
		images:      images,
		associated:  associated,
		icc:         icc,
		file:        file,
		dirSpecs:    dirSpecs,
	}, nil
}

// OpenFromTIFF constructs a generic-TIFF format.Reader from an already-parsed
// TIFF file using a format.Config. Used by cogwsi to delegate the pyramid build.
func OpenFromTIFF(file *tiff.File, cfg *format.Config) (format.Reader, error) {
	return openFromTIFFFile(file, cfg)
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
