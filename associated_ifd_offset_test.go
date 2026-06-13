package opentile_test

import (
	"encoding/binary"
	"os"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/decoder/all"
	_ "github.com/wsilabs/opentile-go/formats/all"
)

// ifdHasImageWidth re-parses the TIFF IFD at byteOffset and reports whether
// it carries tag 256 (ImageWidth) — i.e. the offset points at a real image
// IFD, not arbitrary bytes. Handles classic (magic 42) and BigTIFF (magic
// 43), little- and big-endian.
func ifdHasImageWidth(t *testing.T, path string, byteOffset int64) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(data) < 8 || byteOffset < 8 || byteOffset >= int64(len(data)) {
		t.Fatalf("offset %d out of range for %s (size %d)", byteOffset, path, len(data))
	}
	var order binary.ByteOrder
	switch string(data[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		t.Fatalf("%s: not a TIFF (bad byte-order mark %q)", path, data[0:2])
	}
	magic := order.Uint16(data[2:4])
	off := int(byteOffset)
	switch magic {
	case 42: // classic: uint16 count, 12-byte entries
		count := int(order.Uint16(data[off : off+2]))
		base := off + 2
		for i := 0; i < count; i++ {
			tag := order.Uint16(data[base+i*12 : base+i*12+2])
			if tag == 256 {
				return true
			}
		}
	case 43: // BigTIFF: uint64 count, 20-byte entries
		count := int(order.Uint64(data[off : off+8]))
		base := off + 8
		for i := 0; i < count; i++ {
			tag := order.Uint16(data[base+i*20 : base+i*20+2])
			if tag == 256 {
				return true
			}
		}
	default:
		t.Fatalf("%s: unexpected TIFF magic %d", path, magic)
	}
	return false
}

// TestAssociatedIFDOffset_SVSAndGeneric covers the #15 happy path: for SVS
// and generic-TIFF, every associated image maps back to its source IFD byte
// offset, and that offset points at a real image IFD. Distinct associated
// images get distinct offsets.
func TestAssociatedIFDOffset_SVSAndGeneric(t *testing.T) {
	// CMU-1.stripped.tiff is the generic-TIFF fixture that carries
	// associated images (thumbnail / label / macro); CMU-1.tiff is a
	// bare pyramid with none.
	for _, rel := range []string{"svs/CMU-1.svs", "generic-tiff/CMU-1.stripped.tiff"} {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			path := crossFixture(t, rel) // t.Skip if missing
			s, err := opentile.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			assoc := s.AssociatedImages()
			if len(assoc) == 0 {
				t.Fatalf("%s: no associated images to exercise", rel)
			}
			seen := map[int64]opentile.AssociatedType{}
			for _, a := range assoc {
				off, ok := s.AssociatedIFDOffset(a)
				if !ok {
					t.Errorf("AssociatedIFDOffset(%q): ok=false, want true", a.Type())
					continue
				}
				if off < 8 {
					t.Errorf("%q: offset %d is inside the 8-byte header", a.Type(), off)
				}
				if prev, dup := seen[off]; dup {
					t.Errorf("%q and %q share IFD offset %d", a.Type(), prev, off)
				}
				seen[off] = a.Type()
				if !ifdHasImageWidth(t, path, off) {
					t.Errorf("%q: offset %d does not point at an IFD with ImageWidth (tag 256)", a.Type(), off)
				}
			}
		})
	}
}

// TestAssociatedIFDOffset_OkFalse covers the ok=false paths: non-TIFF slides
// (SZI) and TIFF formats that haven't opted into the provider (Philips).
func TestAssociatedIFDOffset_OkFalse(t *testing.T) {
	// szi: non-TIFF → ok=false. ndpi: TIFF-backed but NOT opted into the
	// provider (and its synthesized label has no source IFD) → ok=false.
	for _, rel := range []string{"szi/CMU-1.szi", "ndpi/CMU-1.ndpi"} {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			s, err := opentile.OpenFile(crossFixture(t, rel))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			assoc := s.AssociatedImages()
			if len(assoc) == 0 {
				t.Skipf("%s: no associated images on this fixture", rel)
			}
			for _, a := range assoc {
				if off, ok := s.AssociatedIFDOffset(a); ok {
					t.Errorf("%s %q: got (offset=%d, ok=true), want ok=false", rel, a.Type(), off)
				}
			}
		})
	}
}
