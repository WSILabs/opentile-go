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
