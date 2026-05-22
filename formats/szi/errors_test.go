package szi

import (
	"archive/zip"
	"bytes"
	"testing"
)

// TestOpenRaw_MissingManifest tests that OpenRaw fails when .dzi is absent.
func TestOpenRaw_MissingManifest(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/")
	// Missing .dzi manifest
	zw.Create("root/scan-properties.xml")
	zw.Close()

	data := buf.Bytes()
	_, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with missing manifest: got nil error, want error")
	}
}

// TestOpenRaw_MissingScanProperties tests that OpenRaw fails when scan-properties.xml is absent.
func TestOpenRaw_MissingScanProperties(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/")

	manifest := `<?xml version="1.0"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="0" TileSize="256" MaxLevel="0">
  <Size Height="256" Width="256"/>
</Image>`
	w, _ := zw.Create("root/root.dzi")
	w.Write([]byte(manifest))

	// Missing scan-properties.xml
	zw.Close()

	data := buf.Bytes()
	_, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with missing scan-properties.xml: got nil error, want error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("scan-properties")) {
		t.Errorf("error should mention scan-properties: %v", err)
	}
}

// TestOpenRaw_MalformedDZIManifest tests that OpenRaw fails with malformed DZI XML.
func TestOpenRaw_MalformedDZIManifest(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/")

	// Malformed XML
	w, _ := zw.Create("root/root.dzi")
	w.Write([]byte("not valid XML at all"))

	w2, _ := zw.Create("root/scan-properties.xml")
	w2.Write([]byte(`<?xml version="1.0"?>
<image xmlns="http://www.pathozoom.com/SZI" date="2024-01-01" version="1.0">
  <properties>
    <property><name>ObjectiveMagnification</name><value>10</value></property>
  </properties>
</image>`))

	zw.Close()

	data := buf.Bytes()
	_, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with malformed DZI: got nil error, want error")
	}
}

// TestOpenRaw_MalformedScanPropertiesXML tests that OpenRaw fails with malformed properties XML.
func TestOpenRaw_MalformedScanPropertiesXML(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/")

	manifest := `<?xml version="1.0"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="0" TileSize="256" MaxLevel="0">
  <Size Height="256" Width="256"/>
</Image>`
	w, _ := zw.Create("root/root.dzi")
	w.Write([]byte(manifest))

	// Malformed properties XML
	w2, _ := zw.Create("root/scan-properties.xml")
	w2.Write([]byte("malformed xml {"))

	zw.Close()

	data := buf.Bytes()
	_, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with malformed scan-properties.xml: got nil error, want error")
	}
}

// TestOpenRaw_DZIWithAltcaseExtension tests that .DZI (uppercase) is recognized.
func TestOpenRaw_DZIWithAltcaseExtension(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/")

	// Use uppercase .DZI extension (allowed per spec page 5).
	manifest := `<?xml version="1.0"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="0" TileSize="256" MaxLevel="0">
  <Size Height="256" Width="256"/>
</Image>`
	w, _ := zw.Create("root/root.DZI")
	w.Write([]byte(manifest))

	w2, _ := zw.Create("root/scan-properties.xml")
	w2.Write([]byte(`<?xml version="1.0"?>
<image xmlns="http://www.pathozoom.com/SZI" date="2024-01-01" version="1.0">
  <properties>
    <property><name>ObjectiveMagnification</name><value>10</value></property>
  </properties>
</image>`))

	// Create at least one tile to satisfy the spec.
	// Use CreateHeader to specify Store (uncompressed) method.
	h := &zip.FileHeader{
		Name:   "root/root_files/0/0_0.jpeg",
		Method: zip.Store,
	}
	w3, _ := zw.CreateHeader(h)
	// Minimal valid JPEG SOI marker.
	w3.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00})

	zw.Close()

	data := buf.Bytes()
	_, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	// Should work with uppercase extension.
	if err != nil {
		t.Errorf("OpenRaw with uppercase .DZI: got error %v, want success", err)
	}
}

// TestOpenRaw_MissingTilesInGrid tests that missing tiles are detected on fetch.
func TestOpenRaw_MissingTilesInGrid(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root/")

	// 256×256 image with 256 tile size = 1×1 grid at L0.
	manifest := `<?xml version="1.0"?>
<Image xmlns="http://schemas.microsoft.com/deepzoom/2008" Format="jpeg" Overlap="0" TileSize="256" MaxLevel="0">
  <Size Height="256" Width="256"/>
</Image>`
	w, _ := zw.Create("root/root.dzi")
	w.Write([]byte(manifest))

	w2, _ := zw.Create("root/scan-properties.xml")
	w2.Write([]byte(`<?xml version="1.0"?>
<image xmlns="http://www.pathozoom.com/SZI" date="2024-01-01" version="1.0">
  <properties>
    <property><name>ObjectiveMagnification</name><value>10</value></property>
  </properties>
</image>`))

	// Create the directory but no tiles — this violates the spec
	// (all tiles in the addressable grid must be present).
	zw.Create("root/root_files/")
	zw.Create("root/root_files/0/")

	zw.Close()

	data := buf.Bytes()
	tlr, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	// This should succeed at open time (no actual tile fetch yet).
	if err != nil {
		t.Fatalf("OpenRaw with grid but no tiles: got error %v at open, want deferred", err)
	}

	// However, fetching a tile should fail.
	level := tlr.Levels()[0]
	_, err = level.Tile(0, 0)
	if err == nil {
		t.Errorf("Tile(0, 0) with missing entry: got nil error, want error")
	}
}

// TestOpenRaw_MultipleRoots tests that a ZIP with multiple root folders fails.
func TestOpenRaw_MultipleRoots(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Create("root1/file.txt")
	zw.Create("root2/file.txt")
	zw.Close()

	data := buf.Bytes()
	_, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with multiple roots: got nil error, want error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("root")) {
		t.Errorf("error should mention root folders: %v", err)
	}
}

// TestOpenRaw_EmptyZIP tests that an empty ZIP fails.
func TestOpenRaw_EmptyZIP(t *testing.T) {
	f := New()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	zw.Close() // Empty ZIP

	data := buf.Bytes()
	_, err := f.OpenRaw(bytes.NewReader(data), int64(len(data)), nil)
	if err == nil {
		t.Errorf("OpenRaw with empty ZIP: got nil error, want error")
	}
}
