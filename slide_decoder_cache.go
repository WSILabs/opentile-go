package opentile

import (
	"fmt"
	"runtime"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/decoderhandle"
)

// decoderPoolCapacity is the per-(Slide, codec) pool size:
// min(NumCPU, 8). The 8 cap bounds memory at ~16 MB/codec/Slide
// (8 × libjpeg-turbo work buffer ≈ 2 MB) while still giving
// good multi-core throughput; cgo decode is intrinsically
// blocking, so concurrency benefit plateaus around 4-8 workers.
func decoderPoolCapacity() int {
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	return n
}

// decoderFor returns (and lazily creates) the decoder pool for the
// given TIFF compression tag. Pools are cached on the Slide; each is
// torn down by Slide.Close.
//
// Returns ErrCodecNotRegistered (wrapped with the tag) if no factory
// is registered for the compression.
//
// Added in v0.28.
func (s *Slide) decoderFor(tag uint16) (*decoderhandle.Pool, error) {
	s.handlesMu.Lock()
	defer s.handlesMu.Unlock()
	if pool, ok := s.handles[tag]; ok {
		return pool, nil
	}
	fac, ok := decoder.GetByCompressionTag(tag)
	if !ok {
		return nil, fmt.Errorf("%w: tag %d (blank-import github.com/wsilabs/opentile-go/decoder/all or decoder/<codec>)",
			ErrCodecNotRegistered, tag)
	}
	pool := decoderhandle.New(fac, decoderPoolCapacity())
	if s.handles == nil {
		s.handles = make(map[uint16]*decoderhandle.Pool)
	}
	s.handles[tag] = pool
	return pool, nil
}
