package opentile

import (
	"context"
	"io"
	"iter"
	"testing"
)

type bestLevelTestReader struct {
	images []Pyramid
}

func (r *bestLevelTestReader) Format() Format                                  { return "test" }
func (r *bestLevelTestReader) Pyramids() []Pyramid                             { return r.images }
func (r *bestLevelTestReader) Level(image, level int) (Level, error)           { return r.images[image].Levels[level], nil }
func (r *bestLevelTestReader) Associated() []AssociatedImage                   { return nil }
func (r *bestLevelTestReader) Metadata() Metadata                              { return Metadata{} }
func (r *bestLevelTestReader) ICCProfile() []byte                              { return nil }
func (r *bestLevelTestReader) WarmLevel(_, _ int) error                        { return nil }
func (r *bestLevelTestReader) ImageRawTile(_, _, _, _ int) ([]byte, error)     { return nil, nil }
func (r *bestLevelTestReader) ImageRawTileInto(_, _, _, _ int, _ []byte) (int, error) {
	return 0, nil
}
func (r *bestLevelTestReader) ImageTileMaxSize(_, _ int) int    { return 0 }
func (r *bestLevelTestReader) ImageTilePrefix(_, _ int) []byte  { return nil }
func (r *bestLevelTestReader) ImageTileBodyMaxSize(_, _ int) int { return 0 }
func (r *bestLevelTestReader) ImageTileBodyInto(_, _, _, _ int, _ []byte) (int, error) {
	return 0, nil
}
func (r *bestLevelTestReader) ImageTileReader(_, _, _, _ int) (io.ReadCloser, error) { return nil, nil }
func (r *bestLevelTestReader) ImageRangeTiles(_ context.Context, _, _ int) iter.Seq2[TilePos, TileResult] {
	return nil
}
func (r *bestLevelTestReader) Close() error { return nil }

func TestBestLevelForDownsample(t *testing.T) {
	slide := &Slide{r: &bestLevelTestReader{
		images: []Pyramid{
			{Index: 0, Levels: []Level{
				{Index: 0, Downsample: 1},
				{Index: 1, Downsample: 4},
				{Index: 2, Downsample: 16},
				{Index: 3, Downsample: 64},
			}},
		},
	}}

	cases := []struct {
		downsample float64
		want       int
	}{
		{0.5, 0},
		{1.0, 0},
		{3.9, 0},
		{4.0, 1},
		{8.0, 1},
		{16.0, 2},
		{50.0, 2},
		{64.0, 3},
		{100.0, 3},
	}
	for _, c := range cases {
		got := slide.BestLevelForDownsample(c.downsample)
		if got != c.want {
			t.Errorf("BestLevelForDownsample(%v): got %d, want %d", c.downsample, got, c.want)
		}
	}
}

func TestImageBestLevelForDownsample(t *testing.T) {
	slide := &Slide{r: &bestLevelTestReader{
		images: []Pyramid{
			{Index: 0, Levels: []Level{{Index: 0, Downsample: 1}, {Index: 1, Downsample: 8}}},
			{Index: 1, Levels: []Level{{Index: 0, Downsample: 2}, {Index: 1, Downsample: 16}}},
		},
	}}

	if got := slide.ImageBestLevelForDownsample(0, 4); got != 0 {
		t.Errorf("image=0 ds=4: got %d, want 0", got)
	}
	if got := slide.ImageBestLevelForDownsample(1, 4); got != 0 {
		t.Errorf("image=1 ds=4: got %d, want 0", got)
	}
	if got := slide.ImageBestLevelForDownsample(1, 16); got != 1 {
		t.Errorf("image=1 ds=16: got %d, want 1", got)
	}
	// Out-of-range image index → 0 (defensive).
	if got := slide.ImageBestLevelForDownsample(99, 4); got != 0 {
		t.Errorf("image=99: got %d, want 0", got)
	}
}
