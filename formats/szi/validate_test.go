package szi

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/format"
)

// minimalSZI builds a well-formed SZI ZIP in memory.
//
// Layout:
//   - test/test.dzi          — DZI manifest (1×1, TileSize=256, Format=jpeg)
//   - test/scan-properties.xml — minimal scan-properties
//   - test/test_files/0/0_0.jpeg — one Store-method tile
//
// A 1×1 image has MaxLevel=0 (one DZI level, one tile at 0/0_0).
// Returns the raw ZIP bytes and the reported file size.
func minimalSZI(t *testing.T) (data []byte, size int64) {
	t.Helper()

	const dzi = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" ` +
		`Format="jpeg" Overlap="0" TileSize="256">` +
		`<Size Width="1" Height="1"/></Image>`

	const scanProps = `<image version="1.0"></image>`

	// A minimal (fake) JPEG: just a few bytes. The Validate hook does
	// NOT decode tile content, so this doesn't need to be a real JPEG.
	tileBytes := []byte{0xFF, 0xD8, 0xFF, 0xD9} // SOI + EOI

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	writeStored := func(name string, body []byte) {
		t.Helper()
		h := &zip.FileHeader{
			Name:   name,
			Method: zip.Store,
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatalf("zip.CreateHeader(%q): %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}

	writeStored("test/test.dzi", []byte(dzi))
	writeStored("test/scan-properties.xml", []byte(scanProps))
	writeStored("test/test_files/0/0_0.jpeg", tileBytes)

	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	raw := buf.Bytes()
	return raw, int64(len(raw))
}

// TestValidateSZIHookExists is a compile-time assertion that *Tiler
// implements opentile.Validator. This will fail to compile until
// formats/szi/validate.go adds the Validate method.
func TestValidateSZIHookExists(t *testing.T) {
	data, size := minimalSZI(t)
	rdr, err := openSZI(bytes.NewReader(data), size, &format.Config{})
	if err != nil {
		t.Fatalf("openSZI: %v", err)
	}
	defer rdr.Close()
	// If *Tiler does not implement Validator, this line won't compile.
	var _ opentile.Validator = rdr
}

// TestValidateSZISynthValid confirms no false-positive CheckOffsetsOutOfBounds
// on a well-formed synthetic SZI.
func TestValidateSZISynthValid(t *testing.T) {
	data, size := minimalSZI(t)

	rep, err := opentile.Validate(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, f := range rep.Findings {
		if f.Code == opentile.CheckOffsetsOutOfBounds {
			t.Errorf("valid synthetic SZI got false-positive OOB finding: %+v", f)
		}
	}
	if rep.Format != opentile.FormatSZI {
		t.Errorf("Format = %q, want %q", rep.Format, opentile.FormatSZI)
	}
}

// TestValidateSZIOOBEntry is the primary TDD test. It patches the
// CompressedSize64 field of the tile entry in the ZIP central directory
// so that offset+length > fileSize, then calls opentile.Validate. The
// Validate hook must flag CheckOffsetsOutOfBounds.
//
// Archive/zip reads file metadata from the central directory and still
// opens the ZIP successfully with an inflated CompressedSize64 (the
// local-file content is not re-read at Open time). DataOffset() reads
// the local-file-header offset, which is unaffected by the patch.
func TestValidateSZIOOBEntry(t *testing.T) {
	data, size := minimalSZI(t)

	// Locate the tile entry in the central directory and patch
	// its CompressedSize64 to exceed the file.
	patched := patchFirstTileCDCompressedSize(t, data, uint32(size)+100)

	rep, err := opentile.Validate(bytes.NewReader(patched), int64(len(patched)))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var found bool
	for _, f := range rep.Findings {
		if f.Code == opentile.CheckOffsetsOutOfBounds {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("OOB tile entry not flagged; got findings: %+v", rep.Findings)
	}
}

// cdSig is the ZIP central directory entry signature.
var cdSig = []byte{0x50, 0x4B, 0x01, 0x02}

// patchFirstTileCDCompressedSize locates the first central directory entry
// whose name contains "test_files" (the tile entry) and overwrites its
// CompressedSize (4-byte LE at offset 20 from the CD entry start) with
// the given value. Panics if no such entry is found.
//
// Central directory entry layout (per PKWARE spec §4.3.12):
//
//	+0  signature       4B  PK\x01\x02
//	+4  version made    2B
//	+6  version needed  2B
//	+8  flags           2B
//	+10 method          2B
//	+12 last mod time   2B
//	+14 last mod date   2B
//	+16 crc-32          4B
//	+20 compressed sz   4B  ← patch here
//	+24 uncompressed sz 4B
//	+28 fname len       2B
//	+30 extra len       2B
//	+32 comment len     2B
//	+34 ...
//	+46 file name       fname-len bytes
func patchFirstTileCDCompressedSize(t *testing.T, data []byte, newSize uint32) []byte {
	t.Helper()
	out := bytes.Clone(data)

	for i := 0; i+46 <= len(out); {
		if !bytes.Equal(out[i:i+4], cdSig) {
			i++
			continue
		}
		fnLen := int(binary.LittleEndian.Uint16(out[i+28 : i+30]))
		if i+46+fnLen > len(out) {
			break
		}
		name := string(out[i+46 : i+46+fnLen])
		if bytes.Contains([]byte(name), []byte("test_files")) {
			binary.LittleEndian.PutUint32(out[i+20:i+24], newSize)
			return out
		}
		extraLen := int(binary.LittleEndian.Uint16(out[i+30 : i+32]))
		commentLen := int(binary.LittleEndian.Uint16(out[i+32 : i+34]))
		i += 46 + fnLen + extraLen + commentLen
	}
	t.Fatal("patchFirstTileCDCompressedSize: no tile entry found in central directory")
	return nil
}
