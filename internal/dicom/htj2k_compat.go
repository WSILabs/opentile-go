package dicom

import (
	"bytes"
	"io"
	"os"

	"github.com/suyashkumar/dicom"
)

// htj2kTransferSyntaxes are the High-Throughput JPEG 2000 transfer syntax
// UIDs. suyashkumar/dicom v1.1.0 doesn't recognize them, so it can't derive
// the data-set byte order and crashes (nil ByteOrder -> SIGSEGV). They use
// the same Explicit VR Little Endian data-set encoding as JPEG 2000 (.91),
// which the library does recognize.
var htj2kTransferSyntaxes = []string{
	"1.2.840.10008.1.2.4.201", // HTJ2K (Lossless Only)
	"1.2.840.10008.1.2.4.202", // HTJ2K with RPCL Options (Lossless Only)
	"1.2.840.10008.1.2.4.203", // HTJ2K
}

// explicitVRLEProxy has the same Explicit VR Little Endian data-set encoding
// as HTJ2K and is recognized by suyashkumar/dicom. We substitute it for the
// HTJ2K UID in the meta header for the (pixel-skipping) cold-path parse only.
const explicitVRLEProxy = "1.2.840.10008.1.2.4.91" // JPEG 2000

// metaHeaderScan bytes are scanned for the meta-header transfer syntax UID.
// The DICOM meta header (group 0002) is small and at the start; 8 KiB is far
// more than enough.
const metaHeaderScan = 8192

// parseDataset parses a DICOM instance's metadata (pixel data skipped). When
// the instance uses an HTJ2K transfer syntax that suyashkumar/dicom cannot
// decode, it substitutes the meta-header UID with explicitVRLEProxy (same
// encoding) in place for the parse and returns the real transfer syntax via
// realTS so the caller can record it.
func parseDataset(path string) (ds dicom.Dataset, realTS string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return dicom.Dataset{}, "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return dicom.Dataset{}, "", err
	}
	size := fi.Size()

	n := int64(metaHeaderScan)
	if n > size {
		n = size
	}
	head := make([]byte, n)
	if _, err := io.ReadFull(f, head); err != nil {
		return dicom.Dataset{}, "", err
	}

	idx, uid := findUID(head, htj2kTransferSyntaxes)
	if idx < 0 {
		// Recognized transfer syntax: parse the file normally.
		ds, err = dicom.ParseFile(path, nil,
			dicom.SkipPixelData(), dicom.AllowMismatchPixelDataLength())
		return ds, "", err
	}

	// Overwrite the UID value in place with the proxy padded to the same
	// length (the UID is odd-length and NULL-padded to even; the proxy is
	// shorter, so it gets a trailing NULL, and the original pad byte stays).
	// Same total length -> no byte shifts; the element length field is
	// untouched. suyashkumar trims the trailing NULLs back to ".91".
	repl := make([]byte, len(uid))
	copy(repl, explicitVRLEProxy)
	patched := append([]byte(nil), head...)
	copy(patched[idx:], repl)

	r := io.MultiReader(bytes.NewReader(patched), io.NewSectionReader(f, n, size-n))
	ds, err = dicom.Parse(r, size, nil,
		dicom.SkipPixelData(), dicom.AllowMismatchPixelDataLength())
	return ds, uid, err
}

func findUID(head []byte, uids []string) (int, string) {
	for _, u := range uids {
		if i := bytes.Index(head, []byte(u)); i >= 0 {
			return i, u
		}
	}
	return -1, ""
}
