package opentile

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
