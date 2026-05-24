// Package ndpi implements opentile-go format support for Hamamatsu NDPI
// files. NDPI is a TIFF variant with vendor-private tags (FileFormat,
// Magnification, ZOffsetFromSlideCenter, etc.) and pyramid levels stored as
// horizontal strips — typically 8 pixels tall — that must be reshaped into
// square output tiles at the JPEG marker level.
//
// This package detects NDPI files via the FileFormat (65420) vendor tag AND
// the Make (271) tag, ports tifffile's _series_ndpi page classification via
// tag 65421 (Magnification, FLOAT), and exposes pyramid levels as
// opentile.Level values. Stripped levels use pure-Go marker concatenation
// (internal/jpeg); one-frame levels and the label image require cgo
// (internal/jpegturbo).
package ndpi

import (
	"errors"
	"fmt"
	"io"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
	"github.com/wsilabs/opentile-go/internal/jpeg"
	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Compile-time assertion: *tiler satisfies format.Reader.
var _ format.Reader = (*tiler)(nil)

func init() {
	// TODO(v0.23): remove old opentile.Register once tiler.go deletion lands.
	opentile.Register(&Factory{})
	format.Register("ndpi", matchNDPI, openNDPI)
}

// tagMake is the standard TIFF Make tag (camera/scanner manufacturer).
const tagMake uint16 = 271

// matchNDPI returns nil iff r is an NDPI file. Per tifffile line 10608:
// NDPI requires BOTH FileFormat (65420) AND Make (271).
func matchNDPI(r io.ReaderAt, size int64) error {
	file, err := tiff.Open(r, size)
	if err != nil {
		return fmt.Errorf("ndpi: not a TIFF: %w", err)
	}
	pages := file.Pages()
	if len(pages) == 0 {
		return errors.New("ndpi: TIFF has no pages")
	}
	p := pages[0]
	if _, ok := p.ScalarU32(tagFileFormat); !ok {
		return errors.New("ndpi: missing FileFormat tag (65420)")
	}
	if _, ok := p.ASCII(tagMake); !ok {
		return errors.New("ndpi: missing Make tag (271)")
	}
	return nil
}

// openNDPI constructs a format.Reader from a raw reader. It re-parses
// the TIFF (matchNDPI already parsed it; header-only reads are cheap).
func openNDPI(r io.ReaderAt, size int64, cfg *format.Config) (format.Reader, error) {
	file, err := tiff.Open(r, size)
	if err != nil {
		return nil, fmt.Errorf("ndpi: %w", err)
	}
	return openFromTIFFFile(file, cfg)
}

// openFromTIFFFile is the shared construction path used by both openNDPI
// and Factory.Open.
func openFromTIFFFile(file *tiff.File, cfg *format.Config) (format.Reader, error) {
	pages := file.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("ndpi: file has no pages")
	}
	md, err := parseMetadata(pages[0])
	if err != nil {
		return nil, err
	}

	// Resolve the requested tile size and snap to native strip width.
	// Native strip dimensions are only discoverable by parsing the embedded
	// JPEG header (via readStripes), so we do a lightweight first pass to
	// find the smallest strip width across all pyramid-level pages.
	reqSize := opentile.Size{W: 512, H: 512}
	if cfg.HasTileSize {
		if cfg.TileSize.W != cfg.TileSize.H {
			return nil, fmt.Errorf("ndpi: tile size must be square, got %v", cfg.TileSize)
		}
		reqSize = cfg.TileSize
	}

	// Pre-read each pyramid-level page's StripInfo so we can (a) compute the
	// smallest-strip-width needed for AdjustTileSize and (b) reuse the
	// parsed header when constructing the level.
	stripInfos := make(map[*tiff.Page]*StripInfo, len(pages))
	smallestStrip := 0
	for _, p := range pages {
		if classifyPage(p) != pageLevel {
			continue
		}
		si, err := readStripes(p, file.ReaderAt())
		if err != nil {
			return nil, fmt.Errorf("ndpi: read strips for page: %w", err)
		}
		if si == nil {
			continue // non-stripped level (one-frame); doesn't constrain tile size
		}
		stripInfos[p] = si
		if smallestStrip == 0 || si.StripW < smallestStrip {
			smallestStrip = si.StripW
		}
	}
	adjusted := AdjustTileSize(reqSize.W, smallestStrip)

	var levels []opentile.Level
	var associated []opentile.AssociatedImage
	var overview *overviewImage
	levelIdx := 0
	for _, p := range pages {
		kind := classifyPage(p)
		switch kind {
		case pageLevel:
			// Stripped vs one-frame: NDPI tag 65426 (McuStarts) is the
			// authoritative discriminator — present iff the level stores
			// per-strip RSTn offsets inside the page's single JPEG.
			if si := stripInfos[p]; si != nil {
				lvl, err := newStrippedImage(levelIdx, p, adjusted, si, file.ReaderAt())
				if err != nil {
					return nil, fmt.Errorf("ndpi: level %d: %w", levelIdx, err)
				}
				levels = append(levels, lvl)
			} else {
				lvl, err := newOneFrameImage(levelIdx, p, adjusted, file.ReaderAt())
				if err != nil {
					return nil, fmt.Errorf("ndpi: level %d: %w", levelIdx, err)
				}
				levels = append(levels, lvl)
			}
			levelIdx++
		case pageMacro:
			ov, err := newOverviewImage(p, file.ReaderAt())
			if err != nil {
				return nil, fmt.Errorf("ndpi: overview: %w", err)
			}
			overview = ov
			associated = append(associated, ov)
		case pageMap:
			// L6 / R13 (v0.4): surface Map pages as AssociatedImage with
			// Type() == "map". Deliberate Go-side extension — Python
			// opentile 0.20.0 does not expose Map pages. See
			// formats/ndpi/mappage.go for the rationale.
			mp, err := newMapPage(p, file.ReaderAt())
			if err != nil {
				return nil, fmt.Errorf("ndpi: map page: %w", err)
			}
			associated = append(associated, mp)
		case pageUnknown:
			// Skip pages with no magnification tag; they're malformed or not
			// part of the standard NDPI layout.
		}
	}
	if overview != nil && cfg.NDPISynthesizedLabel {
		// Default label crop: 0 → 30% of macro width. Derive MCU pixel size
		// from the overview's actual JPEG SOF0 sampling factors rather than
		// hardcoding 16x16 (correct for the Hamamatsu YCbCr 4:2:0 default,
		// but wrong for 4:2:2 or 4:4:4 inputs).
		ovBytes, err := overview.Bytes()
		if err != nil {
			return nil, fmt.Errorf("ndpi: read overview for MCU detection: %w", err)
		}
		mcuW, _, err := jpeg.MCUSizeOf(ovBytes)
		if err != nil {
			return nil, fmt.Errorf("ndpi: derive overview MCU: %w", err)
		}
		// mcuH is no longer needed after the L17 fix — newLabelImage now
		// uses the full image height, not an MCU-floored height. See
		// formats/ndpi/associated.go::newLabelImage for the rule.
		associated = append(associated, newLabelImage(overview, 0.3, mcuW))
	}
	return &tiler{md: md, levels: levels, associated: associated}, nil
}

// Factory is the FormatFactory implementation for NDPI. Preserved for the
// dual-registration transition period; removed once tiler.go deletion lands.
type Factory struct{ opentile.RawUnsupported }

// New returns an NDPI factory. Safe to register globally.
func New() *Factory { return &Factory{} }

// Format reports the format identifier used by opentile.Tiler.Format().
func (f *Factory) Format() opentile.Format { return opentile.FormatNDPI }

// Supports reports whether file looks like an NDPI. Per tifffile line 10608:
// NDPI requires BOTH FileFormat (65420) AND Make (271).
func (f *Factory) Supports(file *tiff.File) bool {
	pages := file.Pages()
	if len(pages) == 0 {
		return false
	}
	p := pages[0]
	if _, ok := p.ScalarU32(tagFileFormat); !ok {
		return false
	}
	if _, ok := p.ASCII(tagMake); !ok {
		return false
	}
	return true
}

// Open constructs an NDPI Tiler from a parsed TIFF file.
func (f *Factory) Open(file *tiff.File, cfg *opentile.Config) (opentile.Tiler, error) {
	fcfg := opentileConfigToFormatConfig(cfg)
	return openFromTIFFFile(file, fcfg)
}

// opentileConfigToFormatConfig translates the opaque opentile.Config wrapper
// into a format.Config. Called from Factory.Open during the dual-registration
// transition; the new openNDPI path receives *format.Config directly.
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
