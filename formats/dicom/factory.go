package dicom

import (
	opentile "github.com/wsilabs/opentile-go"
)

func init() {
	opentile.SetDICOMPathOpenHook(openForHook)
}
