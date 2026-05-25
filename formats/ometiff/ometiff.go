package ometiff

import (
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
	// TODO(v0.23): remove old opentile.Register once tiler.go deletion lands.
	opentile.Register(&Factory{})
	format.Register("ometiff", matchOMETIFF, openOMETIFFFormat)
}

// matchOMETIFF returns nil iff r is an OME TIFF (first page's
// ImageDescription ends with "OME>" after trimming whitespace from
// the last 10 bytes). Direct port of tifffile's is_ome predicate.
func matchOMETIFF(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("ometiff: not a TIFF: %w", err)
	}
	pages := file.Pages()
	if len(pages) == 0 {
		return fmt.Errorf("ometiff: TIFF has no pages")
	}
	desc, ok := pages[0].ImageDescription()
	if !ok || desc == "" {
		return fmt.Errorf("ometiff: page 0 missing or empty ImageDescription")
	}
	tail := desc
	if len(tail) > 10 {
		tail = tail[len(tail)-10:]
	}
	if !strings.HasSuffix(strings.TrimSpace(tail), omeDescriptionSuffix) {
		return fmt.Errorf("ometiff: ImageDescription does not end with %q", omeDescriptionSuffix)
	}
	return nil
}

// openOMETIFFFormat constructs a format.Reader from a raw reader.
func openOMETIFFFormat(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("ometiff: %w", err)
	}
	return openFromTIFFFile(file, cfg)
}

// openFromTIFFFile is the shared construction path used by both
// openOMETIFFFormat and Factory.Open.
func openFromTIFFFile(file *tiff.File, cfg *format.Config) (format.Reader, error) {
	pages := file.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("ome: file has no pages")
	}
	desc, ok := pages[0].ImageDescription()
	if !ok {
		return nil, fmt.Errorf("ome: page 0 missing ImageDescription")
	}
	md, err := parseOMEMetadata(desc)
	if err != nil {
		return nil, err
	}
	cls, err := classifyImages(md.Images)
	if err != nil {
		return nil, err
	}

	// Default OneFrame tile size: derive from first main pyramid's base page.
	// cfg is ignored for OME (mirrors upstream Python opentile behaviour).
	oneFrameTileSize, err := defaultOneFrameTileSizeFromFile(pages, cls.LevelImages)
	if err != nil {
		return nil, err
	}

	images := make([]opentile.Image, 0, len(cls.LevelImages))
	for k, omeIdx := range cls.LevelImages {
		if omeIdx >= len(pages) {
			return nil, fmt.Errorf("ome: OME Image %d has no corresponding TIFF page (only %d top-level pages)", omeIdx, len(pages))
		}
		basePage := pages[omeIdx]
		baseSize, err := pageDims(basePage)
		if err != nil {
			return nil, fmt.Errorf("ome: image %d base page: %w", omeIdx, err)
		}
		baseMPP := opentile.SizeMm{
			W: md.Images[omeIdx].PhysicalSizeX,
			H: md.Images[omeIdx].PhysicalSizeY,
		}
		levels, err := buildLevels(file, basePage, baseSize, baseMPP, oneFrameTileSize)
		if err != nil {
			return nil, fmt.Errorf("ome: image %d: %w", omeIdx, err)
		}
		omeImg := md.Images[omeIdx]
		sizeC := omeImg.Channels
		if sizeC < 1 {
			sizeC = 1
		}
		images = append(images, &pyramidImage{
			index:        k,
			name:         omeImg.Name,
			levels:       levels,
			mpp:          baseMPP,
			sizeZ:        omeImg.SizeZ,
			sizeC:        sizeC,
			sizeT:        omeImg.SizeT,
			channelNames: append([]string(nil), omeImg.ChannelNames...),
		})
	}

	var associated []opentile.AssociatedImage
	for _, spec := range []struct {
		kind   string
		omeIdx int
	}{
		{"thumbnail", cls.Thumbnail},
		{"label", cls.Label},
		{"overview", cls.Macro},
	} {
		if spec.omeIdx < 0 {
			continue
		}
		if spec.omeIdx >= len(pages) {
			return nil, fmt.Errorf("ome: associated %s OME Image %d has no corresponding TIFF page", spec.kind, spec.omeIdx)
		}
		a, err := newAssociatedImage(spec.kind, pages[spec.omeIdx], file.ReaderAt())
		if err != nil {
			return nil, fmt.Errorf("ome: associated %s: %w", spec.kind, err)
		}
		associated = append(associated, a)
	}

	icc, _ := pages[0].ICCProfile()
	cross := crossMetadata(md, cls)
	return &tiler{
		md:         md,
		cross:      cross,
		images:     images,
		associated: associated,
		icc:        icc,
	}, nil
}

// defaultOneFrameTileSizeFromFile picks the tile size for non-tiled
// (OneFrame) levels from the first main pyramid's base page. Mirrors
// defaultOneFrameTileSize but takes a plain pages slice + indices.
func defaultOneFrameTileSizeFromFile(pages []*tiff.Page, levelImageIndices []int) (opentile.Size, error) {
	if len(levelImageIndices) == 0 {
		return opentile.Size{}, fmt.Errorf("ome: cannot derive tile size — no main pyramids")
	}
	first := pages[levelImageIndices[0]]
	tw, ok := first.TileWidth()
	if !ok || tw == 0 {
		return opentile.Size{}, fmt.Errorf("ome: first main pyramid base page has no TileWidth — cannot default OneFrame tile size")
	}
	tl, ok := first.TileLength()
	if !ok || tl == 0 {
		return opentile.Size{}, fmt.Errorf("ome: first main pyramid base page has no TileLength")
	}
	return opentile.Size{W: int(tw), H: int(tl)}, nil
}

// opentileConfigToFormatConfig translates the opaque opentile.Config wrapper
// into a format.Config. Called from Factory.Open during the dual-registration
// transition; the new openOMETIFFFormat path receives *format.Config directly.
func opentileConfigToFormatConfig(cfg *opentile.Config) *format.Config {
	if cfg == nil {
		return &format.Config{}
	}
	ts, hasTS := cfg.TileSize()
	return &format.Config{
		TileSize:             ts,
		HasTileSize:          hasTS,
		CorruptTilePolicy:    cfg.CorruptTilePolicy(),
		NDPISynthesizedLabel: cfg.NDPISynthesizedLabel(),
		Backing:              cfg.Backing(),
	}
}
