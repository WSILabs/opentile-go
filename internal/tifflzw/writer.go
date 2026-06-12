// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file at
// https://github.com/golang/image/blob/master/LICENSE.
//
// Adapted from the standard library's compress/lzw writer with the TIFF
// "off by one" code-width transition applied (matching golang.org/x/image/
// tiff/lzw on the read side). x/image ships only a TIFF-LZW reader; the
// writer here completes the round-trip so opentile-go can re-encode
// reconstructed multi-strip labels as a single TIFF-conforming LZW stream.
//
// The single algorithmic difference vs stdlib compress/lzw: the encoder
// switches from N-bit to (N+1)-bit codes one code earlier (when the next
// code to assign is 2^N - 1, not 2^N). This matches what TIFF decoders
// (libtiff, imagecodecs, x/image) expect.

package tifflzw

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

type bufWriter interface {
	io.ByteWriter
	Flush() error
}

const (
	maxCode      = 1<<12 - 1
	invalidCode  = 1<<32 - 1
	tableSize    = 4 * 1 << 12
	tableMask    = tableSize - 1
	invalidEntry = 0
)

// Writer is a TIFF-LZW compressor.
type Writer struct {
	w            bufWriter
	litWidth     uint
	order        Order
	write        func(*Writer, uint32) error
	nBits        uint
	width        uint
	bits         uint32
	hi, overflow uint32
	savedCode    uint32
	err          error
	table        [tableSize]uint32
}

func (w *Writer) writeLSB(c uint32) error {
	w.bits |= c << w.nBits
	w.nBits += w.width
	for w.nBits >= 8 {
		if err := w.w.WriteByte(uint8(w.bits)); err != nil {
			return err
		}
		w.bits >>= 8
		w.nBits -= 8
	}
	return nil
}

func (w *Writer) writeMSB(c uint32) error {
	w.bits |= c << (32 - w.width - w.nBits)
	w.nBits += w.width
	for w.nBits >= 8 {
		if err := w.w.WriteByte(uint8(w.bits >> 24)); err != nil {
			return err
		}
		w.bits <<= 8
		w.nBits -= 8
	}
	return nil
}

var errOutOfCodes = errors.New("tifflzw: out of codes")

// incHi increments w.hi and checks for both overflow and running out of
// unused codes. The TIFF "off by one" lives here: width transitions
// happen when hi+1 == overflow, one code earlier than the stdlib variant
// (which transitions on hi == overflow). Reader and writer must agree on
// this transition exactly or the decoded code stream desyncs.
func (w *Writer) incHi() error {
	w.hi++
	// Table full: emit Clear and reset BEFORE any width transition. This
	// MUST precede the off-by-one width bump below — otherwise at hi==maxCode
	// the bump pushes width to 13 and the Clear is written at width 13, while
	// the reader caps at maxWidth (12) and reads it at 12, desyncing the
	// stream (it truncates a few KB later). Reproduced as the large-LZW
	// round-trip truncation (GH #20).
	if w.hi == maxCode {
		clear := uint32(1) << w.litWidth
		if err := w.write(w, clear); err != nil {
			return err
		}
		w.width = w.litWidth + 1
		w.hi = clear + 1
		w.overflow = clear << 1
		for i := range w.table {
			w.table[i] = invalidEntry
		}
		return errOutOfCodes
	}
	// NOTE: TIFF "off by one" — switch width one code earlier than stdlib.
	if w.hi+1 == w.overflow {
		w.width++
		w.overflow <<= 1
	}
	return nil
}

// Write writes a compressed representation of p to w's underlying writer.
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if maxLit := uint8(1<<w.litWidth - 1); maxLit != 0xff {
		for _, x := range p {
			if x > maxLit {
				w.err = errors.New("tifflzw: input byte too large for the litWidth")
				return 0, w.err
			}
		}
	}
	n = len(p)
	code := w.savedCode
	if code == invalidCode {
		clear := uint32(1) << w.litWidth
		if err := w.write(w, clear); err != nil {
			return 0, err
		}
		code, p = uint32(p[0]), p[1:]
	}
loop:
	for _, x := range p {
		literal := uint32(x)
		key := code<<8 | literal
		hash := (key>>12 ^ key) & tableMask
		for h, t := hash, w.table[hash]; t != invalidEntry; {
			if key == t>>12 {
				code = t & maxCode
				continue loop
			}
			h = (h + 1) & tableMask
			t = w.table[h]
		}
		if w.err = w.write(w, code); w.err != nil {
			return 0, w.err
		}
		code = literal
		if err1 := w.incHi(); err1 != nil {
			if err1 == errOutOfCodes {
				continue
			}
			w.err = err1
			return 0, w.err
		}
		for {
			if w.table[hash] == invalidEntry {
				w.table[hash] = (key << 12) | w.hi
				break
			}
			hash = (hash + 1) & tableMask
		}
	}
	w.savedCode = code
	return n, nil
}

// Close closes the Writer, flushing any pending output. It does not
// close w's underlying writer.
func (w *Writer) Close() error {
	if w.err != nil {
		if w.err == errClosed {
			return nil
		}
		return w.err
	}
	w.err = errClosed
	if w.savedCode != invalidCode {
		if err := w.write(w, w.savedCode); err != nil {
			return err
		}
		if err := w.incHi(); err != nil && err != errOutOfCodes {
			return err
		}
	} else {
		clear := uint32(1) << w.litWidth
		if err := w.write(w, clear); err != nil {
			return err
		}
	}
	eof := uint32(1)<<w.litWidth + 1
	if err := w.write(w, eof); err != nil {
		return err
	}
	if w.nBits > 0 {
		if w.order == MSB {
			w.bits >>= 24
		}
		if err := w.w.WriteByte(uint8(w.bits)); err != nil {
			return err
		}
	}
	return w.w.Flush()
}

// NewWriter creates a TIFF-LZW writer. litWidth must be in [2,8] and is
// typically 8.
func NewWriter(dst io.Writer, order Order, litWidth int) io.WriteCloser {
	w := new(Writer)
	w.init(dst, order, litWidth)
	return w
}

func (w *Writer) init(dst io.Writer, order Order, litWidth int) {
	switch order {
	case LSB:
		w.write = (*Writer).writeLSB
	case MSB:
		w.write = (*Writer).writeMSB
	default:
		w.err = errors.New("tifflzw: unknown order")
		return
	}
	if litWidth < 2 || 8 < litWidth {
		w.err = fmt.Errorf("tifflzw: litWidth %d out of range", litWidth)
		return
	}
	bw, ok := dst.(bufWriter)
	if !ok && dst != nil {
		bw = bufio.NewWriter(dst)
	}
	w.w = bw
	lw := uint(litWidth)
	w.order = order
	w.width = 1 + lw
	w.litWidth = lw
	w.hi = 1<<lw + 1
	w.overflow = 1 << (lw + 1)
	w.savedCode = invalidCode
}
