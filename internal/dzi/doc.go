// Package dzi parses the Microsoft Deep Zoom Image (DZI) manifest
// XML format and computes per-level / per-tile coordinate
// information.
//
// Per-image DZI manifests look like:
//
//	<?xml version="1.0" encoding="UTF-8"?>
//	<Image xmlns="http://schemas.microsoft.com/deepzoom/2008"
//	       Format="jpeg" Overlap="0" TileSize="256">
//	  <Size Width="2220" Height="2967"/>
//	</Image>
//
// Tile naming convention (per Microsoft spec, verbatim):
//
//	"The tiles are named as column_row.format, where row is the row
//	 number of the tile (starting from 0 at top) and column is the
//	 column number of the tile (starting from 0 at left). format is
//	 the appropriate extension for the image format used – either
//	 JPEG or PNG."
//
// Pyramid level numbering: level 0 is 1×1 pixel; each level doubles
// the previous. Total levels = ceil(log2(max(W, H))) + 1.
//
// This package is pure: no I/O, no allocation contracts beyond
// returning parsed values. Storage backend selection (ZIP for SZI,
// filesystem for bare DZI) lives in format packages.
package dzi
