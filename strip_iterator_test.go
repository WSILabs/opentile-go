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

