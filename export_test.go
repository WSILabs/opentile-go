package opentile

import (
	"errors"

	"github.com/wsilabs/opentile-go/internal/tiff"
)

// Test-only accessors. Exposed via export_test.go so external test
// packages (opentile_test) can verify Slide's internal handle cache
// shape without making the fields public.

// HandleCountForTest returns the number of currently-cached decoder
// pools on this Slide. Used by slide_handle_test.go to assert pool
// reuse and drain-on-Close behavior.
func (s *Slide) HandleCountForTest() int {
	s.handlesMu.Lock()
	defer s.handlesMu.Unlock()
	return len(s.handles)
}

// IDCTScaleForTest returns the codec-domain scale factor the iterator
// selected (1 when no codec downscale is used). Used to assert the
// codec-scale gate engages for JP2K/HTJ2K sources.
func (it *StripIterator) IDCTScaleForTest() int { return it.idctScale }

// FailMmapForTest makes every OpenFile mapping attempt fail, as on a
// filesystem without mmap support, until the returned restore is called.
func FailMmapForTest() (restore func()) {
	prev := openMmap
	openMmap = func(string) (*tiff.MmapFile, error) {
		return nil, errors.New("mmap: operation not supported (test)")
	}
	return func() { openMmap = prev }
}

// BackingForTest reports which backing an OpenFile Slide was opened with.
func (s *Slide) BackingForTest() string {
	switch s.r.(type) {
	case *mmapCloser:
		return "mmap"
	case *fileCloser:
		return "pread"
	}
	return "other"
}

// SetSRGBHookForTest replaces the registered ICC -> sRGB transform hook
// (nil acts as a build without color management) until restore is called.
// Only slides opened after the call see it.
func SetSRGBHookForTest(fn func(profile []byte, rgba bool) (apply func(pix []byte, width, height, stride int), free func(), ok bool)) (restore func()) {
	prev := srgbHook
	srgbHook = fn
	return func() { srgbHook = prev }
}
