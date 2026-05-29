package decoderhandle_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wsilabs/opentile-go/decoder"
	"github.com/wsilabs/opentile-go/internal/decoderhandle"
)

// fakeDecoder is an instrumented decoder.Decoder for Pool tests. Counts
// Decode and Close calls; never touches libjpeg-turbo.
type fakeDecoder struct {
	mu       sync.Mutex
	decoded  int
	closed   bool
	closeErr error
}

func (d *fakeDecoder) Decode(src []byte, opts decoder.DecodeOptions) (*decoder.Image, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errors.New("fakeDecoder: closed")
	}
	d.decoded++
	return &decoder.Image{
		Width: 1, Height: 1,
		Format: decoder.PixelFormatRGB,
		Stride: 3,
		Pix:    []byte{0, 0, 0},
	}, nil
}

func (d *fakeDecoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	return d.closeErr
}

// fakeFactory is an instrumented decoder.Factory. Counts New() calls;
// returns a fresh fakeDecoder each time.
type fakeFactory struct {
	news    atomic.Int32
	makeNil bool
}

func (f *fakeFactory) Name() string                { return "decoderhandle-test-fake" }
func (f *fakeFactory) TIFFCompressionTags() []uint16 { return []uint16{0xFFFF} }

func (f *fakeFactory) New() decoder.Decoder {
	f.news.Add(1)
	if f.makeNil {
		return nil
	}
	return &fakeDecoder{}
}

func TestPoolSequentialReuse(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	defer p.Close()
	for i := 0; i < 10; i++ {
		d, err := p.Borrow()
		if err != nil {
			t.Fatalf("Borrow #%d: %v", i, err)
		}
		if _, err := d.Decode(nil, decoder.DecodeOptions{}); err != nil {
			t.Fatalf("Decode #%d: %v", i, err)
		}
		p.Return(d)
	}
	if got := fac.news.Load(); got != 1 {
		t.Fatalf("factory.New() called %d times; want 1 (single member reused)", got)
	}
}

func TestPoolConcurrentBounded(t *testing.T) {
	fac := &fakeFactory{}
	const cap = 4
	p := decoderhandle.New(fac, cap)
	defer p.Close()

	const N = 32
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := p.Borrow()
			if err != nil {
				t.Errorf("Borrow: %v", err)
				return
			}
			defer p.Return(d)
			if _, err := d.Decode(nil, decoder.DecodeOptions{}); err != nil {
				t.Errorf("Decode: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := fac.news.Load(); got > cap {
		t.Fatalf("factory.New() called %d times; want <= %d (capacity)", got, cap)
	}
}

func TestPoolLazyCreation(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 8)
	defer p.Close()
	// Borrow 3 distinct concurrent members; never reaches capacity=8.
	var bs []decoder.Decoder
	for i := 0; i < 3; i++ {
		d, err := p.Borrow()
		if err != nil {
			t.Fatal(err)
		}
		bs = append(bs, d)
	}
	for _, d := range bs {
		p.Return(d)
	}
	if got := fac.news.Load(); got != 3 {
		t.Fatalf("factory.New() called %d times; want 3 (lazy)", got)
	}
}

func TestPoolBorrowAfterClose(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := p.Borrow()
	if !errors.Is(err, decoderhandle.ErrClosed) {
		t.Fatalf("got %v; want ErrClosed", err)
	}
}

func TestPoolReturnAfterClose(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	d, err := p.Borrow()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p.Return(d) // closed-pool branch: closes Decoder directly
	fd := d.(*fakeDecoder)
	if !fd.closed {
		t.Fatal("Decoder not Closed after Return-on-closed-pool")
	}
}

func TestPoolCloseRacesWithBorrow(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 1)
	d, err := p.Borrow()
	if err != nil {
		t.Fatal(err)
	}
	// One goroutine sits in Borrow waiting for capacity.
	got := make(chan error, 1)
	go func() {
		_, err := p.Borrow()
		got <- err
	}()
	// Give the goroutine time to block. 50 ms is enough on every
	// machine we care about; bench/CI flakiness would indicate
	// a deeper scheduler problem.
	time.Sleep(50 * time.Millisecond)
	// Close while a Borrow is in flight.
	p.Close()
	select {
	case err := <-got:
		if !errors.Is(err, decoderhandle.ErrClosed) {
			t.Fatalf("waiting Borrow got %v; want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting Borrow did not return after Close")
	}
	p.Return(d) // closes Decoder directly via closed-pool branch
}

func TestPoolDoubleClose(t *testing.T) {
	fac := &fakeFactory{}
	p := decoderhandle.New(fac, 4)
	if err := p.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}

func TestPoolFactoryReturnsNil(t *testing.T) {
	fac := &fakeFactory{makeNil: true}
	p := decoderhandle.New(fac, 2)
	defer p.Close()
	_, err := p.Borrow()
	if err == nil {
		t.Fatal("Borrow with nil-returning factory returned nil error")
	}
	if !errors.Is(err, decoderhandle.ErrFactoryReturnedNil) {
		t.Fatalf("got %v; want ErrFactoryReturnedNil", err)
	}
}
