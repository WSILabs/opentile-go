package main

import (
	"errors"
	"io/fs"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/decoder"
)

// Status codes; must match OT_* in opentile.h (checked by header_test.go).
const (
	statusOK       = 0
	errGeneric     = -1
	errHandle      = -2
	errArg         = -3
	errBuffer      = -4
	errRange       = -5
	errFormat      = -6
	errIO          = -7
	errCodec       = -8
	errCorrupt     = -9
	errUnsupported = -10
	errSparse      = -11
)

// errorCodes maps Go errors to status codes; first errors.Is match wins.
var errorCodes = []struct {
	err  error
	code int
}{
	{opentile.ErrLevelOutOfRange, errRange},
	{opentile.ErrImageIndexOutOfRange, errRange},
	{opentile.ErrTileOutOfBounds, errRange},
	{opentile.ErrRegionEmpty, errRange},
	{opentile.ErrUnsupportedFormat, errFormat},
	{opentile.ErrInvalidTIFF, errFormat},
	{decoder.ErrCodecUnavailable, errCodec}, // also opentile.ErrCGORequired (same value)
	{opentile.ErrCodecNotRegistered, errCodec},
	{opentile.ErrUnsupportedCompression, errCodec},
	{opentile.ErrCorruptTile, errCorrupt},
	{decoder.ErrCorruptInput, errCorrupt},
	{opentile.ErrBadJPEGBitstream, errCorrupt},
	{opentile.ErrDimensionUnavailable, errUnsupported},
	{decoder.ErrUnsupportedScale, errUnsupported},
	{decoder.ErrUnsupportedFormat, errUnsupported},
	{opentile.ErrTileSizeRequired, errUnsupported},
	{opentile.ErrColorManagementUnavailable, errUnsupported},
	{opentile.ErrICCProfileUnusable, errUnsupported},
	{opentile.ErrSparseTile, errSparse},
}

// codeOf classifies err into a status code.
func codeOf(err error) int {
	for _, m := range errorCodes {
		if errors.Is(err, m.err) {
			return m.code
		}
	}
	var pe *fs.PathError
	if errors.As(err, &pe) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return errIO
	}
	return errGeneric
}
