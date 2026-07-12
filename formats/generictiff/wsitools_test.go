package generictiff

import (
	"testing"
	"time"
)

func TestParseWSIToolsDescription_HappyPath(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode source=svs codec=avif mpp=0.499 mag=20x scanner="Aperio" date=2009-12-29`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true on wsi-tools-prefixed input")
	}
	if !md.hasMag || md.magnification != 20.0 {
		t.Errorf("magnification = %v, want 20.0", md.magnification)
	}
	if !md.hasScanner || md.scannerManufacturer != "Aperio" {
		t.Errorf("scanner = %q, want %q", md.scannerManufacturer, "Aperio")
	}
	if !md.hasMPP || md.micronsPerPixel != 0.499 {
		t.Errorf("mpp = %v, want 0.499", md.micronsPerPixel)
	}
	if !md.hasDate {
		t.Fatal("date not parsed")
	}
	want := time.Date(2009, 12, 29, 0, 0, 0, 0, time.UTC)
	if !md.acquisitionDate.Equal(want) {
		t.Errorf("date = %v, want %v", md.acquisitionDate, want)
	}
	if md.dateHasTime {
		t.Error("dateHasTime = true for a date-only value, want false (#108)")
	}
	// v0.20: Version extracted for Writer population.
	if md.Version != "0.2.0-dev" {
		t.Errorf("Version = %q, want 0.2.0-dev", md.Version)
	}
}

// TestParseWSIToolsDescription_FullTimestampDate: a full-timestamp provenance
// date= must round-trip the HH:MM:SS (#108), not be truncated to midnight.
func TestParseWSIToolsDescription_FullTimestampDate(t *testing.T) {
	desc := `wsi-tools/0.26.0 transcode source=svs date=2009-12-29T09:59:15Z`
	md, ok := parseWSIToolsDescription(desc)
	if !ok || !md.hasDate {
		t.Fatal("expected date parsed")
	}
	if !md.dateHasTime {
		t.Error("dateHasTime = false, want true for an RFC3339 value")
	}
	want := time.Date(2009, 12, 29, 9, 59, 15, 0, time.UTC)
	if !md.acquisitionDate.Equal(want) {
		t.Errorf("date = %v, want %v (time component lost)", md.acquisitionDate, want)
	}
}

func TestParseProvenanceDate(t *testing.T) {
	cases := []struct {
		in      string
		wantHMS bool
		want    time.Time
	}{
		{"2009-12-29T09:59:15Z", true, time.Date(2009, 12, 29, 9, 59, 15, 0, time.UTC)},
		{"2009-12-29T09:59:15", true, time.Date(2009, 12, 29, 9, 59, 15, 0, time.UTC)},
		{"2009-12-29 09:59:15", true, time.Date(2009, 12, 29, 9, 59, 15, 0, time.UTC)},
		{"2009-12-29", false, time.Date(2009, 12, 29, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		ts, hasTime, ok := parseProvenanceDate(c.in)
		if !ok {
			t.Errorf("%q: ok=false, want parseable", c.in)
			continue
		}
		if hasTime != c.wantHMS || !ts.UTC().Equal(c.want) {
			t.Errorf("%q: got (%v, hasTime=%v), want (%v, hasTime=%v)", c.in, ts.UTC(), hasTime, c.want, c.wantHMS)
		}
	}
	if _, _, ok := parseProvenanceDate("not-a-date"); ok {
		t.Error("garbage input: ok=true, want false")
	}
}

func TestParseWSIToolsDescription_QuotedScannerWithSpace(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode scanner="Acme WSI Scanner X100" mpp=0.25`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if md.scannerManufacturer != "Acme WSI Scanner X100" {
		t.Errorf("scanner = %q, want %q", md.scannerManufacturer, "Acme WSI Scanner X100")
	}
	if md.micronsPerPixel != 0.25 {
		t.Errorf("mpp = %v, want 0.25", md.micronsPerPixel)
	}
}

func TestParseWSIToolsDescription_MissingFields(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode codec=webp`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if md.hasMag {
		t.Error("hasMag should be false (no mag in description)")
	}
	if md.hasMPP {
		t.Error("hasMPP should be false")
	}
	if md.hasScanner {
		t.Error("hasScanner should be false")
	}
	if md.hasDate {
		t.Error("hasDate should be false")
	}
}

func TestParseWSIToolsDescription_MalformedMPP(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode mpp=not-a-number`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true (parse is lenient on bad values)")
	}
	if md.hasMPP {
		t.Error("hasMPP should be false on malformed mpp value")
	}
	if md.micronsPerPixel != 0 {
		t.Errorf("mpp = %v, want 0", md.micronsPerPixel)
	}
}

func TestParseWSIToolsDescription_NonWSIToolsInput(t *testing.T) {
	for _, desc := range []string{
		"",
		"Aperio Image Library v11.2.1",
		"some random text",
		"wsi-toolsx/0.2.0", // close prefix but not exact
	} {
		_, ok := parseWSIToolsDescription(desc)
		if ok {
			t.Errorf("parseWSIToolsDescription(%q) = ok=true, want false", desc)
		}
	}
}

func TestParseWSIToolsDescription_UnknownKeys(t *testing.T) {
	desc := `wsi-tools/0.3.0-dev transcode source=svs unknownkey=somevalue mag=40x newfield="future use"`
	md, ok := parseWSIToolsDescription(desc)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !md.hasMag || md.magnification != 40.0 {
		t.Errorf("magnification = %v, want 40.0 (known keys parsed even with unknown keys present)", md.magnification)
	}
}

func TestPopulateWSIToolsProperties_AllFields(t *testing.T) {
	desc := `wsi-tools/0.2.0-dev transcode source=svs codec=avif mpp=0.499 mag=20x scanner="Aperio" date=2009-12-29`
	var md Metadata
	populateWSIToolsProperties(&md, desc)
	want := map[string]string{
		"wsi-tools.version": "0.2.0-dev",
		"wsi-tools.source":  "svs",
		"wsi-tools.codec":   "avif",
	}
	for k, v := range want {
		if got := md.Properties[k]; got != v {
			t.Errorf("Properties[%q] = %q, want %q", k, got, v)
		}
	}
}

func TestPopulateWSIToolsProperties_MissingKeysAbsent(t *testing.T) {
	// Per Q6: missing source-data → key absent (NOT empty string).
	desc := `wsi-tools/0.2.0 transcode mpp=0.25`
	var md Metadata
	populateWSIToolsProperties(&md, desc)
	if got := md.Properties["wsi-tools.version"]; got != "0.2.0" {
		t.Errorf("version = %q, want 0.2.0", got)
	}
	if _, ok := md.Properties["wsi-tools.source"]; ok {
		t.Errorf("wsi-tools.source should be absent when source key not in desc")
	}
	if _, ok := md.Properties["wsi-tools.codec"]; ok {
		t.Errorf("wsi-tools.codec should be absent when codec key not in desc")
	}
}
