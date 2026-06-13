package opentile

import (
	"errors"
	"fmt"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

var (
	ErrUnsupportedFormat      = errors.New("opentile: unsupported format")
	ErrUnsupportedCompression = errors.New("opentile: unsupported compression")
	ErrTileOutOfBounds        = errors.New("opentile: tile position out of bounds")
	ErrCorruptTile            = errors.New("opentile: corrupt tile")
	ErrLevelOutOfRange        = errors.New("opentile: level index out of range")
	ErrImageIndexOutOfRange   = errors.New("opentile: image index out of range")
	ErrInvalidTIFF            = errors.New("opentile: invalid TIFF structure")

	// ErrTooManyIFDs is returned when a TIFF IFD chain exceeds the safety cap
	// (1024 IFDs) before terminating. Either the file is corrupt, presents a
	// cycle, or is malicious. Re-exports internal/tiff.ErrTooManyIFDs so
	// callers can errors.Is(err, opentile.ErrTooManyIFDs).
	ErrTooManyIFDs = tiff.ErrTooManyIFDs

	// Returned (wrapped in TileError) when internal/jpeg cannot parse a JPEG
	// bitstream or assemble a valid one from TIFF fragments.
	ErrBadJPEGBitstream = errors.New("opentile: invalid JPEG bitstream")

	// Returned when an operation requires an MCU-aligned region and the
	// computed or requested region is not. Primarily an internal invariant
	// guard; consumers encounter it only on malformed slides.
	ErrMCUAlignment = errors.New("opentile: operation requires MCU alignment")

	// Returned from NDPI one-frame levels and NDPI label on builds compiled
	// without cgo (CGO_ENABLED=0 or -tags nocgo).
	ErrCGORequired = errors.New("opentile: operation requires cgo build with libjpeg-turbo")

	// Reserved for future use; currently unfired because v0.2 defaults the
	// NDPI tile size to 512 rather than erroring. Predefined so exporting
	// it later is not a breaking change.
	ErrTileSizeRequired = errors.New("opentile: tile size not representable for this format")

	// ErrDimensionUnavailable is returned (wrapped in TileError) when a
	// caller asks for a non-zero Z, C, or T axis on a TileCoord but the
	// underlying Image's SizeZ/SizeC/SizeT is 1 (the format / slide
	// doesn't carry that dimension at all). Distinct from
	// ErrTileOutOfBounds: that error means "this axis exists but the
	// requested index is past its size"; this means "the axis itself
	// doesn't exist on this slide / format / milestone."
	//
	// Added in v0.7 alongside Level.TileAt and the multi-dim Image
	// dimension accessors.
	ErrDimensionUnavailable = errors.New("opentile: dimension not supported on this format/image")

	// ErrSparseTile is returned (wrapped in TileError) when a tile
	// position falls within the level's grid but the underlying file
	// has no compressed bytes at that cell — the format encodes
	// "absent / blank tile" as a sentinel offset rather than empty
	// content. Iris IFE uses NULL_TILE (0xFFFFFFFFFF in the 40-bit
	// offset field); other formats may add later. Consumers typically
	// translate this into an HTTP 404 or a fixed blank image. Distinct
	// from ErrTileOutOfBounds (the position itself is past the grid).
	//
	// Added in v0.8 alongside the Iris IFE format package.
	ErrSparseTile = errors.New("opentile: sparse tile (no compressed bytes at this position)")

	// ErrMmapUnavailable is returned by [OpenFile] when called with
	// [WithBacking](BackingMmap) (the default since v0.9) but the
	// underlying memory-map operation fails — typically because the
	// file is on a filesystem that doesn't support mmap (some FUSE
	// or network mounts) or the platform lacks `golang.org/x/exp/mmap`
	// support. Wraps the underlying error from `mmap.Open`.
	//
	// Callers that want automatic fallback to the os.File / pread
	// path can retry with `opts...` extended by
	// `WithBacking(BackingPread)` on this error.
	//
	// Added in v0.9 alongside the mmap-default OpenFile change.
	ErrMmapUnavailable = errors.New("opentile: memory-map unavailable for this file")

	// ErrCodecNotRegistered is returned by Level.DecodedTile and friends
	// when no decoder is registered for the level's Compression. The
	// wrapping error message names the compression and the suggested
	// blank-import that fixes it.
	//
	// Added in v0.24 alongside the DecodedTile family.
	ErrCodecNotRegistered = errors.New("opentile: codec not registered for this slide's tile compression")

	// ErrRegionEmpty is returned by Level.ReadRegion / Pyramid.ReadRegionScaled
	// when the requested rectangle has no in-bounds pixels.
	//
	// Added in v0.25 alongside the ReadRegion family.
	ErrRegionEmpty = errors.New("opentile: region has no in-bounds pixels")
)

// TileError wraps a per-tile failure with the (level, x, y) that produced it.
// Consumers use errors.As to extract the coordinates and errors.Is against the
// exported sentinels to branch on the underlying cause.
type TileError struct {
	Level int
	X, Y  int
	Err   error
}

func (e *TileError) Error() string {
	return fmt.Sprintf("opentile: tile (%d,%d) on level %d: %v", e.X, e.Y, e.Level, e.Err)
}

func (e *TileError) Unwrap() error { return e.Err }
