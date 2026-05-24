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
	regMu sync.RWMutex
	reg   []entry
)

// Register adds a format to the global registry. Called from each
// format package's init() function. Registration order matters:
// OpenAny dispatches in registration order; first Match wins.
func Register(name string, match Match, opener Opener) {
	regMu.Lock()
	defer regMu.Unlock()
	reg = append(reg, entry{name: name, match: match, opener: opener})
}

// OpenAny probes registered formats in registration order. The first
// whose Match returns nil wins; its Opener is invoked. Returns
// ErrUnknownFormat if no format matches.
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
	return nil, ErrUnknownFormat
}

// snapshot, restore, and reset are test-only helpers visible within
// the package (white-box test convention). They allow registry tests
// to isolate the global registry state.
func snapshot() []entry {
	regMu.RLock()
	defer regMu.RUnlock()
	return append([]entry(nil), reg...)
}

func restore(s []entry) {
	regMu.Lock()
	defer regMu.Unlock()
	reg = s
}

func reset() {
	regMu.Lock()
	defer regMu.Unlock()
	reg = nil
}
