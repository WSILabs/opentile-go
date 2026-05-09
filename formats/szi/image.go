package szi

import opentile "github.com/cornish/opentile-go"

// image is the single-image opentile.Image implementation for SZI.
// SZI files always carry exactly one image (no DZC collections per
// spec page 4).
type image struct {
	t      *Tiler
	levels []opentile.Level
}

// Index always returns 0 — SZI carries exactly one image.
func (i *image) Index() int { return 0 }

// Name always returns "" — SZI does not carry per-image names.
func (i *image) Name() string { return "" }

// Levels returns a fresh copy of the level slice.
func (i *image) Levels() []opentile.Level {
	return append([]opentile.Level(nil), i.levels...)
}

// Level returns the level at idx or ErrLevelOutOfRange.
func (i *image) Level(idx int) (opentile.Level, error) {
	if idx < 0 || idx >= len(i.levels) {
		return nil, opentile.ErrLevelOutOfRange
	}
	return i.levels[idx], nil
}

// MPP returns the base-level MPP. T4 wires the metadata-derived
// value through here; until then, we delegate to the base level
// (which itself returns zero in T3).
func (i *image) MPP() opentile.SizeMm {
	if len(i.levels) == 0 {
		return opentile.SizeMm{}
	}
	return i.levels[0].MPP()
}

// SizeZ — SZI is 2D brightfield: one focal plane.
func (i *image) SizeZ() int { return 1 }

// SizeC — SZI is brightfield RGB: one composite channel.
func (i *image) SizeC() int { return 1 }

// SizeT — SZI carries no time dimension.
func (i *image) SizeT() int { return 1 }

// ChannelName — implicit RGB on the single brightfield channel;
// no name surfaced.
func (i *image) ChannelName(c int) string { return "" }

// ZPlaneFocus — single Z plane at nominal focus.
func (i *image) ZPlaneFocus(z int) float64 { return 0 }
