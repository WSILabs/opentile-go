package opentile

import (
	bytes_lib "bytes"
	"context"
	"image"
	image_lib "image"
	stdjpeg "image/jpeg"
	"io"
	"iter"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
	_ "github.com/wsilabs/opentile-go/decoder/jpeg" // register JPEG decoder
)

func TestScaledStripsStripsCount(t *testing.T) {
	cases := []struct {
		outH, stripH int
		want         int
	}{
		{100, 100, 1},
		{100, 50, 2},
		{100, 33, 4}, // 33+33+33+1
		{1, 100, 1},
		{0, 100, 0},
		{100, 0, 0},
	}
	slide := newTestSlideForStrips()
	for _, c := range cases {
		it := slide.ScaledStrips(
			image.Rect(0, 0, 1000, 1000),
			image.Point{X: 200, Y: c.outH},
			c.stripH,
		)
		got := it.Strips()
		if got != c.want {
			t.Errorf("outH=%d stripH=%d: got %d strips, want %d", c.outH, c.stripH, got, c.want)
		}
		it.Close()
	}
}

func TestScaledStripsCloseIdempotent(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.ScaledStrips(image.Rect(0, 0, 100, 100), image.Point{X: 50, Y: 50}, 25)
	if err := it.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := it.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestScaledStripsNextAfterClose(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.ScaledStrips(image.Rect(0, 0, 100, 100), image.Point{X: 50, Y: 50}, 25)
	it.Close()
	_, err := it.Next()
	if err != io.ErrClosedPipe {
		t.Errorf("Next after Close: got %v, want io.ErrClosedPipe", err)
	}
}

func TestScaledStripsContextCancel(t *testing.T) {
	slide := newTestSlideForStrips()
	ctx, cancel := context.WithCancel(context.Background())
	it := slide.ScaledStrips(
		image.Rect(0, 0, 100, 100),
		image.Point{X: 50, Y: 50},
		25,
		WithStripContext(ctx),
	)
	defer it.Close()
	cancel()
	// After Phase 4 implementation lands, Next() should return
	// ctx.Err(). For now, the stub returns "not yet implemented" —
	// we just verify the cancellation context propagates.
	if it.cancelCtx == nil {
		t.Errorf("cancelCtx not set")
	}
	select {
	case <-it.cancelCtx.Done():
		// OK — parent ctx cancellation propagated.
	default:
		t.Errorf("cancelCtx not cancelled")
	}
}

// newTestSlideForStrips constructs a minimal *Slide backed by a fake
// reader. Returns a slide whose Levels()[0] reports the given
// dimensions; tile reads return synthetic gray bytes.
func newTestSlideForStrips() *Slide {
	return &Slide{r: &stripsTestReader{}}
}

type stripsTestReader struct{}

func (r *stripsTestReader) Format() Format { return "test" }
func (r *stripsTestReader) Images() []Image {
	return []Image{
		{Index: 0, Levels: []Level{
			{Index: 0, Size: Size{W: 1000, H: 1000}, TileSize: Size{W: 256, H: 256}, Grid: Size{W: 4, H: 4}, Compression: CompressionJPEG, Downsample: 1.0},
		}},
	}
}
func (r *stripsTestReader) Level(image, level int) (Level, error) {
	return r.Images()[image].Levels[level], nil
}
func (r *stripsTestReader) Associated() []AssociatedImage { return nil }
func (r *stripsTestReader) Metadata() Metadata            { return Metadata{} }
func (r *stripsTestReader) ICCProfile() []byte            { return nil }
func (r *stripsTestReader) WarmLevel(_, _ int) error      { return nil }

func (r *stripsTestReader) ImageRawTile(_, _ int, tx, ty int) ([]byte, error) {
	img := image_lib.NewYCbCr(image_lib.Rect(0, 0, 256, 256), image_lib.YCbCrSubsampleRatio444)
	val := byte((tx*16 + ty) & 0xFF)
	for i := range img.Y {
		img.Y[i] = val
	}
	var buf bytes_lib.Buffer
	if err := stdjpeg.Encode(&buf, img, &stdjpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func (r *stripsTestReader) ImageRawTileInto(_, _, _, _ int, _ []byte) (int, error) {
	return 0, nil
}
func (r *stripsTestReader) ImageTileMaxSize(_, _ int) int    { return 0 }
func (r *stripsTestReader) ImageTilePrefix(_, _ int) []byte  { return nil }
func (r *stripsTestReader) ImageTileBodyMaxSize(_, _ int) int { return 0 }
func (r *stripsTestReader) ImageTileBodyInto(_, _, _, _ int, _ []byte) (int, error) {
	return 0, nil
}
func (r *stripsTestReader) ImageTileReader(_, _, _, _ int) (io.ReadCloser, error) { return nil, nil }
func (r *stripsTestReader) ImageRangeTiles(_ context.Context, _, _ int) iter.Seq2[TilePos, TileResult] {
	return nil
}
func (r *stripsTestReader) Close() error { return nil }

func TestScaledStripsSingleStripWholeSlide(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.ScaledStrips(
		image.Rect(0, 0, 1000, 1000),
		image.Point{X: 100, Y: 100},
		100, // stripHeight = outH → 1 strip
	)
	defer it.Close()

	if it.Strips() != 1 {
		t.Fatalf("Strips: got %d, want 1", it.Strips())
	}

	img, err := it.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if img.Width != 100 || img.Height != 100 {
		t.Errorf("dimensions: got %dx%d, want 100x100", img.Width, img.Height)
	}

	_, err = it.Next()
	if err != io.EOF {
		t.Errorf("second Next: got %v, want io.EOF", err)
	}
}

func TestScaledStripsMultipleStrips(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.ScaledStrips(
		image.Rect(0, 0, 1000, 1000),
		image.Point{X: 100, Y: 200},
		50, // stripHeight = 50 → 4 strips
	)
	defer it.Close()

	if it.Strips() != 4 {
		t.Fatalf("Strips: got %d, want 4", it.Strips())
	}

	for i := 0; i < 4; i++ {
		img, err := it.Next()
		if err != nil {
			t.Fatalf("Next strip %d: %v", i, err)
		}
		if img.Width != 100 || img.Height != 50 {
			t.Errorf("strip %d: got %dx%d, want 100x50", i, img.Width, img.Height)
		}
	}

	_, err := it.Next()
	if err != io.EOF {
		t.Errorf("after final Next: got %v, want io.EOF", err)
	}
}

func TestScaledStripsShortLastStrip(t *testing.T) {
	slide := newTestSlideForStrips()
	it := slide.ScaledStrips(
		image.Rect(0, 0, 1000, 1000),
		image.Point{X: 100, Y: 130},
		50, // 130 / 50 = 2 strips of 50 + last of 30
	)
	defer it.Close()

	imgs := make([]*decoder.Image, 0, 3)
	for {
		img, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		imgs = append(imgs, img)
	}
	if len(imgs) != 3 {
		t.Fatalf("got %d strips, want 3", len(imgs))
	}
	if imgs[0].Height != 50 || imgs[1].Height != 50 || imgs[2].Height != 30 {
		t.Errorf("strip heights: %d, %d, %d (want 50, 50, 30)",
			imgs[0].Height, imgs[1].Height, imgs[2].Height)
	}
}
