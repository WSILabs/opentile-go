package dzi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	opentile "github.com/wsilabs/opentile-go"
)

func init() {
	opentile.SetDZIPathOpenHook(openForHook)
}

// openForHook is the entry point installed as the root's dziPathOpenHook. It
// accepts a .dzi file path, or a directory containing exactly one *.dzi. Returns
// opentile.ErrUnsupportedFormat (so OpenFile falls through to normal dispatch)
// when the path is neither.
func openForHook(path string) (any, error) {
	dziPath, err := resolveDZIPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(dziPath)
	if err != nil {
		return nil, fmt.Errorf("dzi: read manifest %s: %w", dziPath, err)
	}
	base := strings.TrimSuffix(filepath.Base(dziPath), filepath.Ext(dziPath))
	filesDir := filepath.Join(filepath.Dir(dziPath), base+"_files")
	return openBareDZI(dziPath, data, filesDir)
}

// resolveDZIPath returns the .dzi manifest path for a path that is either a
// .dzi file or a directory containing exactly one *.dzi. Non-DZI inputs return
// opentile.ErrUnsupportedFormat.
func resolveDZIPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		// Non-existent / unreadable path: not our concern — fall through.
		return "", opentile.ErrUnsupportedFormat
	}
	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(path), ".dzi") {
			return path, nil
		}
		return "", opentile.ErrUnsupportedFormat
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.dzi"))
	if err != nil || len(matches) != 1 {
		// Zero or multiple .dzi in the dir → ambiguous → fall through.
		return "", opentile.ErrUnsupportedFormat
	}
	return matches[0], nil
}
