// Package decoderhandle provides a small fixed-size pool of long-lived
// decoder.Decoder instances. Replaces the fac.New() / dec.Close()
// per-tile pattern with Borrow/Return, eliminating per-tile tjInit +
// tjDestroy churn (~290 µs/call dominated by tjDestroy).
//
// Concurrency: Borrow blocks when all pool members are in use; Return
// is non-blocking. Pool members are not concurrent-safe (libjpeg-turbo
// tjhandle is single-threaded), so Borrow grants exclusive access for
// the lifetime of the borrow.
//
// Lifetime: members are lazy-initialised on first Borrow. Close drains
// the pool and tears down every member. After Close, Borrow returns
// ErrClosed; Return on a closed pool tears the member down directly.
package decoderhandle

import (
	"errors"
	"sync"

	"github.com/wsilabs/opentile-go/decoder"
)

// ErrClosed is returned by Borrow if Close has been called.
var ErrClosed = errors.New("decoderhandle: pool closed")

// ErrFactoryReturnedNil is returned by Borrow if factory.New() returned
// nil. Surfaces what would otherwise be a nil-decoder panic in Decode.
var ErrFactoryReturnedNil = errors.New("decoderhandle: factory.New() returned nil")

// Pool is a fixed-size pool of decoder.Decoder instances.
type Pool struct {
	factory  decoder.Factory
	capacity int // max concurrent Borrows; immutable post-New

	initMu      sync.Mutex // guards lazy-create + outstanding + closed
	outstanding int        // count of members held by callers (issued via
	// Borrow's lazy-create branch and not yet returned);
	// ensures factory.New() is called at most `capacity` times total.
	items  chan decoder.Decoder // buffered, cap = capacity
	closed bool
}

// New constructs a pool of capacity members for the given factory.
// capacity must be > 0; smaller values are clamped to 1. Members are
// NOT created up-front; the first Borrow that needs to grow the pool
// invokes factory.New().
func New(fac decoder.Factory, capacity int) *Pool {
	if capacity < 1 {
		capacity = 1
	}
	return &Pool{
		factory:  fac,
		capacity: capacity,
		items:    make(chan decoder.Decoder, capacity),
	}
}

// Borrow acquires a Decoder. Blocks if all members are in use AND
// the pool is at capacity. Returns ErrClosed if Close has been called.
// Caller must call Return when done.
func (p *Pool) Borrow() (decoder.Decoder, error) {
	// Fast path: try to grab an existing returned member without
	// holding initMu.
	select {
	case d, ok := <-p.items:
		if !ok {
			return nil, ErrClosed
		}
		return d, nil
	default:
	}

	// Slow path: try to lazy-create under initMu.
	p.initMu.Lock()
	if p.closed {
		p.initMu.Unlock()
		return nil, ErrClosed
	}
	if p.outstanding < p.capacity {
		d := p.factory.New()
		if d == nil {
			p.initMu.Unlock()
			return nil, ErrFactoryReturnedNil
		}
		p.outstanding++
		p.initMu.Unlock()
		return d, nil
	}
	p.initMu.Unlock()

	// At capacity, all members busy — block waiting for Return.
	// We must NOT hold initMu while blocking; another goroutine's
	// Return needs to write to p.items, and Close needs to close it.
	d, ok := <-p.items
	if !ok {
		return nil, ErrClosed
	}
	return d, nil
}

// Return puts the Decoder back into the pool. Safe after Close
// (closes the Decoder directly in that case). Safe with d == nil.
func (p *Pool) Return(d decoder.Decoder) {
	if d == nil {
		return
	}
	p.initMu.Lock()
	if p.closed {
		p.initMu.Unlock()
		_ = d.Close()
		return
	}
	p.initMu.Unlock()

	select {
	case p.items <- d:
		// Returned to pool. outstanding stays incremented; the member
		// is "available" but still counted against capacity. The next
		// Borrow that picks it up doesn't increment outstanding (it's
		// a hit on the channel, not a new factory.New() call).
	default:
		// Pool channel full — should not happen if Borrow/Return are
		// balanced and capacity is respected. Close defensively.
		_ = d.Close()
		p.initMu.Lock()
		p.outstanding--
		p.initMu.Unlock()
	}
}

// Close drains and closes every member. Safe to call multiple times.
// In-flight Borrows blocked on the channel see channel-closed and
// return ErrClosed. Returns the first Decoder.Close error encountered
// during drain.
func (p *Pool) Close() error {
	p.initMu.Lock()
	if p.closed {
		p.initMu.Unlock()
		return nil
	}
	p.closed = true
	close(p.items)
	p.initMu.Unlock()

	var firstErr error
	for d := range p.items {
		if err := d.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
