package tiff

import (
	"encoding/binary"
	"testing"
)

func leOrder() binary.ByteOrder { return binary.LittleEndian }

// asciiInlineEntry builds an inline ASCII entry (value fits in the 4-byte
// classic inline cell when len(s)+1 <= 4).
func asciiInlineEntry(tag uint16, s string) Entry {
	e := Entry{Tag: tag, Type: DTASCII, Count: uint64(len(s) + 1), inlineCap: 4}
	copy(e.valueBytes[:], append([]byte(s), 0))
	return e
}

func buildSyntheticPage() *Page {
	entries := map[uint16]Entry{
		256: {Tag: 256, Type: DTShort, Count: 1, valueOrOffset: 512, inlineCap: 4},
		270: asciiInlineEntry(270, "hi"),
	}
	br := &byteReader{order: leOrder()}
	return &Page{ifd: &ifd{entries: entries}, br: br}
}

func TestRawTagsEnumeratesSorted(t *testing.T) {
	p := buildSyntheticPage()
	raw := p.RawTags()
	if len(raw) != 2 {
		t.Fatalf("RawTags len = %d, want 2", len(raw))
	}
	if raw[0].Number != 256 || raw[1].Number != 270 {
		t.Fatalf("not tag-sorted: %d, %d", raw[0].Number, raw[1].Number)
	}
	if raw[0].Type != DTShort || len(raw[0].Uints) != 1 || raw[0].Uints[0] != 512 {
		t.Fatalf("tag 256 decode wrong: %+v", raw[0])
	}
	if raw[1].ASCII != "hi" {
		t.Fatalf("tag 270 ASCII = %q, want hi", raw[1].ASCII)
	}
}
