package opentile

import (
	"context"
	"errors"
	"io"
	"iter"
	"testing"

	"github.com/wsilabs/opentile-go/decoder"
)

// knownPixelReader is a synthetic slideReader returning every tile as
// uniform fill bytes. Used by Layer 1 tests to detect (a) whether
// fillWhite ran when it shouldn't have, and (b) whether the blit
// covered every in-bounds pixel.
type knownPixelReader struct {
	levelSize Size
	tileSize  Size
	fill      byte
}

func (r *knownPixelReader) Format() Format { return Format("test") }
func (r *knownPixelReader) Images() []Image {
	return []Image{{Levels: []Level{{
		Index: 0, Size: r.levelSize, TileSize: r.tileSize,
		Compression: CompressionJPEG,
	}}}}
}
func (r *knownPixelReader) Level(image, level int) (Level, error) {
	if image != 0 || level != 0 {
		return Level{}, ErrLevelOutOfRange
	}
	return Level{
		Index: 0, Size: r.levelSize, TileSize: r.tileSize,
		Compression: CompressionJPEG,
	}, nil
}
func (r *knownPixelReader) Associated() []AssociatedImage    { return nil }
func (r *knownPixelReader) Metadata() Metadata               { return Metadata{} }
func (r *knownPixelReader) ICCProfile() []byte               { return nil }
func (r *knownPixelReader) WarmLevel(image, level int) error { return nil }
func (r *knownPixelReader) ImageRawTile(image, level, tx, ty int) ([]byte, error) {
	return nil, errors.New("knownPixelReader: ImageRawTile unused")
}
func (r *knownPixelReader) ImageRawTileInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("knownPixelReader: ImageRawTileInto unused")
}
func (r *knownPixelReader) ImageTileMaxSize(image, level int) int     { return 1 }
func (r *knownPixelReader) ImageTilePrefix(image, level int) []byte   { return nil }
func (r *knownPixelReader) ImageTileBodyMaxSize(image, level int) int { return 1 }
func (r *knownPixelReader) ImageTileBodyInto(image, level, tx, ty int, dst []byte) (int, error) {
	return 0, errors.New("knownPixelReader: ImageTileBodyInto unused")
}
func (r *knownPixelReader) ImageTileReader(image, level, tx, ty int) (io.ReadCloser, error) {
	return nil, errors.New("knownPixelReader: ImageTileReader unused")
}
func (r *knownPixelReader) ImageRangeTiles(ctx context.Context, image, level int) iter.Seq2[TilePos, TileResult] {
	return func(yield func(TilePos, TileResult) bool) {}
}
func (r *knownPixelReader) Close() error { return nil }

// ImageDecodedTile satisfies decodedTiler. Returns a tile-sized Image
// filled with r.fill — or writes into opts.Dst if provided, per the
// v0.29 Layer 2 contract.
func (r *knownPixelReader) ImageDecodedTile(image, level, tx, ty int, opts decoder.DecodeOptions) (*decoder.Image, error) {
	format := opts.Format
	if format == 0 {
		format = decoder.PixelFormatRGB
	}
	var out *decoder.Image
	if opts.Dst != nil &&
		opts.Dst.Width == r.tileSize.W &&
		opts.Dst.Height == r.tileSize.H &&
		opts.Dst.Format == format {
		out = opts.Dst
	} else {
		out = decoder.NewImageFormat(r.tileSize.W, r.tileSize.H, format)
	}
	for i := range out.Pix {
		out.Pix[i] = r.fill
	}
	return out, nil
}

func TestReadRegionFullyInBoundsPathSkipsFillWhite(t *testing.T) {
	s := &Slide{r: &knownPixelReader{
		levelSize: Size{W: 1024, H: 1024},
		tileSize:  Size{W: 256, H: 256},
		fill:      0x42,
	}}
	defer s.Close()

	dst := decoder.NewImageFormat(512, 512, decoder.PixelFormatRGB)
	for i := range dst.Pix {
		dst.Pix[i] = 0xAA // sentinel: NOT 0xFF (fillWhite) and NOT 0x42 (blit)
	}

	if err := s.ImageReadRegionInto(0, 0, 128, 128, dst); err != nil {
		t.Fatal(err)
	}

	for i, b := range dst.Pix {
		if b != 0x42 {
			t.Fatalf("dst[%d]=0x%02x; expected 0x42 (fillWhite was unnecessarily called OR blit missed a pixel)", i, b)
		}
	}
}

func TestReadRegionEdgeRegionForceFillWhite(t *testing.T) {
	s := &Slide{r: &knownPixelReader{
		levelSize: Size{W: 1024, H: 1024},
		tileSize:  Size{W: 256, H: 256},
		fill:      0x42,
	}}
	defer s.Close()

	dst := decoder.NewImageFormat(512, 512, decoder.PixelFormatRGB)
	for i := range dst.Pix {
		dst.Pix[i] = 0xAA
	}

	// Region crossing the right edge: x=768, w=512 → right half is OOB.
	if err := s.ImageReadRegionInto(0, 0, 768, 128, dst); err != nil {
		t.Fatal(err)
	}

	stride := dst.Stride
	bpp := 3
	for row := 0; row < 512; row++ {
		for col := 0; col < 256; col++ {
			off := row*stride + col*bpp
			if dst.Pix[off] != 0x42 {
				t.Fatalf("dst[r=%d,c=%d]=0x%02x; expected 0x42 (in-bounds region)", row, col, dst.Pix[off])
			}
		}
		for col := 256; col < 512; col++ {
			off := row*stride + col*bpp
			if dst.Pix[off] != 0xFF {
				t.Fatalf("dst[r=%d,c=%d]=0x%02x; expected 0xFF (OOB needs fillWhite)", row, col, dst.Pix[off])
			}
		}
	}
}
