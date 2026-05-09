package szi

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	opentile "github.com/cornish/opentile-go"
	"github.com/cornish/opentile-go/internal/dzi"
)

// Tiler is the SZI-format implementation of opentile.Tiler.
type Tiler struct {
	r    io.ReaderAt
	size int64
	zipR *zip.Reader

	// rootDir is the single top-level directory inside the ZIP
	// (e.g., "CMU-1" or "scan_618_"). All other paths are relative
	// to it.
	rootDir string

	// manifest is the parsed DZI XML manifest from <rootDir>/<name>.dzi.
	manifest dzi.Manifest

	// filesDir is the tile-pyramid root, "<rootDir>/<rootName>_files".
	filesDir string

	// scanPropertiesXML holds the raw bytes of scan-properties.xml.
	// T4 parses these into szi.Metadata.
	scanPropertiesXML []byte

	// entries indexes ZIP central-directory entries by full path
	// for fast tile lookup.
	entries map[string]*zip.File

	cfg *opentile.Config
}

// openSZI is the FormatFactory.OpenRaw implementation.
func openSZI(r io.ReaderAt, size int64, cfg *opentile.Config) (*Tiler, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("szi: open zip: %w", err)
	}

	t := &Tiler{
		r:       r,
		size:    size,
		zipR:    zr,
		entries: make(map[string]*zip.File, len(zr.File)),
		cfg:     cfg,
	}
	for _, f := range zr.File {
		t.entries[f.Name] = f
	}

	if err := t.discoverRoot(); err != nil {
		return nil, err
	}
	if err := t.loadManifest(); err != nil {
		return nil, err
	}
	if err := t.loadScanProperties(); err != nil {
		return nil, err
	}
	if err := t.checkTileEntriesStored(); err != nil {
		return nil, err
	}
	return t, nil
}

// discoverRoot identifies the single top-level directory inside the
// ZIP archive. SZI spec mandates exactly one root folder named
// after the image.
func (t *Tiler) discoverRoot() error {
	roots := make(map[string]struct{})
	for _, f := range t.zipR.File {
		// Take the first path component.
		name := f.Name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			roots[name[:i]] = struct{}{}
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("szi: expected exactly 1 root folder, got %d", len(roots))
	}
	for name := range roots {
		t.rootDir = name
	}
	return nil
}

// loadManifest finds and parses <rootDir>/<rootDir>.dzi (case-
// insensitive on the .dzi extension per spec).
func (t *Tiler) loadManifest() error {
	// Try canonical name first: <root>/<root>.dzi (lowercase) and
	// <root>/<root>.DZI (uppercase, per spec page 5).
	candidates := []string{
		t.rootDir + "/" + t.rootDir + ".dzi",
		t.rootDir + "/" + t.rootDir + ".DZI",
	}
	var manifestEntry *zip.File
	for _, p := range candidates {
		if e, ok := t.entries[p]; ok {
			manifestEntry = e
			break
		}
	}
	// Fallback: any file ending in .dzi/.DZI directly under rootDir.
	if manifestEntry == nil {
		for _, f := range t.zipR.File {
			lower := strings.ToLower(f.Name)
			if !strings.HasSuffix(lower, ".dzi") {
				continue
			}
			if path.Dir(f.Name) != t.rootDir {
				continue
			}
			manifestEntry = f
			break
		}
	}
	if manifestEntry == nil {
		return errors.New("szi: no .dzi manifest found in archive")
	}

	data, err := readZipEntry(manifestEntry)
	if err != nil {
		return fmt.Errorf("szi: read manifest %s: %w", manifestEntry.Name, err)
	}
	m, err := dzi.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("szi: parse manifest %s: %w", manifestEntry.Name, err)
	}
	t.manifest = m

	// _files folder: same name as the .dzi without the extension,
	// plus "_files".
	base := strings.TrimSuffix(path.Base(manifestEntry.Name), path.Ext(manifestEntry.Name))
	t.filesDir = t.rootDir + "/" + base + "_files"
	return nil
}

// loadScanProperties locates and reads <rootDir>/scan-properties.xml.
// The spec marks this file mandatory; T4 parses it into Metadata.
func (t *Tiler) loadScanProperties() error {
	p := t.rootDir + "/scan-properties.xml"
	entry, ok := t.entries[p]
	if !ok {
		return fmt.Errorf("szi: missing %s", p)
	}
	data, err := readZipEntry(entry)
	if err != nil {
		return fmt.Errorf("szi: read %s: %w", p, err)
	}
	t.scanPropertiesXML = data
	return nil
}

// checkTileEntriesStored enforces the SZI spec requirement that
// tile-pyramid entries under <filesDir>/ use compression method 0
// (zip.Store / uncompressed). The spec mandates this so that tile
// fetches can be a zero-copy SectionReader on the underlying file
// (no zlib inflate on the hot path). Manifest, scan-properties,
// and associated_images entries are unconstrained.
func (t *Tiler) checkTileEntriesStored() error {
	prefix := t.filesDir + "/"
	for _, f := range t.zipR.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		if f.Method != zip.Store {
			return fmt.Errorf("szi: tile entry %s uses compression method %d, want 0 (Store) per spec", f.Name, f.Method)
		}
	}
	return nil
}

// readZipEntry reads a complete ZIP entry into memory. Used for
// small metadata blobs (manifest, scan-properties.xml) — NOT for
// tiles, which use a SectionReader fast path.
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// Format returns opentile.FormatSZI.
func (t *Tiler) Format() opentile.Format { return opentile.FormatSZI }

// Close releases resources held by the Tiler. The underlying
// ReaderAt remains the caller's responsibility.
func (t *Tiler) Close() error {
	t.r = nil
	t.zipR = nil
	t.entries = nil
	return nil
}

// Levels is the legacy single-image shortcut accessor; SZI files
// always carry exactly one image, so this delegates to Images()[0].
//
// T3 implementation. v0.16 T2 returns nil placeholder.
func (t *Tiler) Levels() []opentile.Level { return nil }

// Level returns Levels()[i]. T3 implementation; T2 returns
// ErrLevelOutOfRange for any i.
func (t *Tiler) Level(i int) (opentile.Level, error) {
	return nil, opentile.ErrLevelOutOfRange
}

// Images returns the single Image carried by the SZI file. T3
// implementation; T2 returns nil placeholder.
func (t *Tiler) Images() []opentile.Image { return nil }

// Associated returns the associated_images/ entries. T4 implementation.
func (t *Tiler) Associated() []opentile.AssociatedImage { return nil }

// Metadata returns the cross-format metadata populated from
// scan-properties.xml. T4 implementation.
func (t *Tiler) Metadata() opentile.Metadata { return opentile.Metadata{} }

// ICCProfile returns nil — SZI does not surface ICC profiles in v0.16.
func (t *Tiler) ICCProfile() []byte { return nil }

// WarmLevel pre-warms the page cache. SZI's tile lookup is via
// SectionReader on the file; warming would touch each tile's
// uncompressed-stored bytes. T3 implementation.
func (t *Tiler) WarmLevel(i int) error {
	return opentile.ErrLevelOutOfRange
}
