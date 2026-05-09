package dzi

import "testing"

func TestParseManifest_HappyPath(t *testing.T) {
	const data = `<?xml version="1.0" encoding="UTF-8"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008"
       Format="jpeg" Overlap="0" TileSize="256">
  <Size Width="2220" Height="2967"/>
</Image>`
	m, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Format != "jpeg" {
		t.Errorf("Format = %q, want jpeg", m.Format)
	}
	if m.Overlap != 0 {
		t.Errorf("Overlap = %d, want 0", m.Overlap)
	}
	if m.TileSize != 256 {
		t.Errorf("TileSize = %d, want 256", m.TileSize)
	}
	if m.Width != 2220 || m.Height != 2967 {
		t.Errorf("Size = %dx%d, want 2220x2967", m.Width, m.Height)
	}
}

func TestParseManifest_GrundiumLayout(t *testing.T) {
	// scan_618_grundium_SZI.szi manifest: TileSize=512, large dims.
	const data = `<?xml version="1.0" encoding="UTF-8"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="0" TileSize="512">
    <Size Height="81920" Width="147456"/>
</Image>`
	m, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.TileSize != 512 {
		t.Errorf("TileSize = %d, want 512", m.TileSize)
	}
	if m.Width != 147456 || m.Height != 81920 {
		t.Errorf("Size = %dx%d, want 147456x81920", m.Width, m.Height)
	}
}

func TestParseManifest_PNGFormat(t *testing.T) {
	const data = `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="png" Overlap="0" TileSize="256"><Size Width="100" Height="100"/></Image>`
	m, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Format != "png" {
		t.Errorf("Format = %q, want png", m.Format)
	}
}

func TestParseManifest_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"malformed XML", `<Image><Size`},
		{"wrong root", `<Other xmlns="http://schemas.microsoft.com/deepzoom/2008"/>`},
		{"wrong namespace", `<Image xmlns="http://example.com/foo" Format="jpeg" TileSize="256"><Size Width="100" Height="100"/></Image>`},
		{"missing Format", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" TileSize="256"><Size Width="100" Height="100"/></Image>`},
		{"zero TileSize", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" TileSize="0"><Size Width="100" Height="100"/></Image>`},
		{"zero size", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" TileSize="256"><Size Width="0" Height="0"/></Image>`},
		{"negative overlap", `<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="-1" TileSize="256"><Size Width="100" Height="100"/></Image>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.data)); err == nil {
				t.Error("ParseManifest: want error, got nil")
			}
		})
	}
}
