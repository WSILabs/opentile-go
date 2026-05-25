package format

import (
	"io"
	"sync"
)

type entry struct {
	name   string
	match  Match
	opener Opener
}

var (
	regMu    sync.RWMutex
	reg      []entry
	regFallback []entry
)

// Register adds a format to the global registry. Called from each
// format package's init() function. Registration order matters:
// OpenAny dispatches in registration order; first Match wins.
func Register(name string, match Match, opener Opener) {
	regMu.Lock()
	defer regMu.Unlock()
	reg = append(reg, entry{name: name, match: match, opener: opener})
}

// RegisterFallback adds a format to the fallback registry. Fallback
// formats are only considered by OpenAny when no main-registry format
// matches. Use this for catch-all detectors (e.g. generic-tiff) that
// must not shadow more-specific formats regardless of import order.
func RegisterFallback(name string, match Match, opener Opener) {
	regMu.Lock()
	defer regMu.Unlock()
	regFallback = append(regFallback, entry{name: name, match: match, opener: opener})
}

// OpenAny probes registered formats in registration order. The first
// whose Match returns nil wins; its Opener is invoked. Fallback
// formats are only considered when no main-registry format matches.
// Returns ErrUnknownFormat if no format matches.
func OpenAny(r io.ReaderAt, size int64, cfg *Config) (Reader, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	if cfg == nil {
		cfg = &Config{}
	}
	for _, e := range reg {
		if err := e.match(r, size); err == nil {
			return e.opener(r, size, cfg)
		}
	}
	for _, e := range regFallback {
		if err := e.match(r, size); err == nil {
			return e.opener(r, size, cfg)
		}
	}
	return nil, ErrUnknownFormat
}

// registryState holds both registry slices for test isolation.
type registryState struct {
	main     []entry
	fallback []entry
}

// snapshot, restore, and reset are test-only helpers visible within
// the package (white-box test convention). They allow registry tests
// to isolate the global registry state.
func snapshot() registryState {
	regMu.RLock()
	defer regMu.RUnlock()
	return registryState{
		main:     append([]entry(nil), reg...),
		fallback: append([]entry(nil), regFallback...),
	}
}

func restore(s registryState) {
	regMu.Lock()
	defer regMu.Unlock()
	reg = s.main
	regFallback = s.fallback
}

func reset() {
	regMu.Lock()
	defer regMu.Unlock()
	reg = nil
	regFallback = nil
}
