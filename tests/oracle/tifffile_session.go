//go:build parity

package oracle

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// TifffileSession is a long-lived Python tifffile subprocess scoped to
// one slide. Drives the tifffile_runner.py protocol — used by the v0.6
// multi-image OME parity oracle to reach pyramid series that Python
// opentile drops via its last-wins loop.
//
// Sessions are NOT safe for concurrent use — single-pipe protocol.
type TifffileSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

// tifffileRunnerScript returns the absolute path to tifffile_runner.py,
// resolved next to this source file.
func tifffileRunnerScript() string {
	if p := os.Getenv("OPENTILE_TIFFFILE_RUNNER"); p != "" {
		return p
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("tests", "oracle", "tifffile_runner.py")
	}
	return filepath.Join(filepath.Dir(thisFile), "tifffile_runner.py")
}

// NewTifffileSession spawns the tifffile runner for slide. Reuses
// OPENTILE_ORACLE_PYTHON for the interpreter, same as opentile-py
// sessions.
func NewTifffileSession(slide string) (*TifffileSession, error) {
	cmd := exec.Command(PythonBin(), tifffileRunnerScript(), slide)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("tifffile-oracle: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("tifffile-oracle: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tifffile-oracle: start runner: %w", err)
	}
	return &TifffileSession{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

// Tile returns the raw tile bytes at (image, level, x, y) via tifffile's
// dataoffsets/databytecounts arrays. Plane 0 only when
// PlanarConfiguration=2; matches our Go-side indexing.
//
// Returns (nil, nil) when the Python side emits a zero-length response
// (out-of-range, level not tiled, etc.). Returns (nil, err) on
// transport failures.
func (s *TifffileSession) Tile(imageIdx, levelIdx, x, y int) ([]byte, error) {
	if _, err := fmt.Fprintf(s.stdin, "tile %d %d %d %d\n", imageIdx, levelIdx, x, y); err != nil {
		return nil, fmt.Errorf("tifffile-oracle: send tile request: %w", err)
	}
	return s.readBlob()
}

// TileBIF returns the raw tile bytes at BIF-pyramid level (col, row)
// in image-space. The Python runner maps (col, row) row-major
// before reading dataoffsets. Pages are sorted by parsed
// ImageDescription "level=N" — non-pyramid IFDs are excluded.
//
// Useful for spec-compliant BIF fixtures (Ventana-1) that don't carry
// shared JPEGTables: opentile-go's Tile(col, row) returns the same raw
// bytes verbatim, so byte-equality is the parity bar. Fixtures with
// shared JPEGTables (OS-1) modify the bytes via jpeg.InsertTables; for
// those, prefer the openslide oracle (T20) which compares decoded
// pixels.
func (s *TifffileSession) TileBIF(levelIdx, col, row int) ([]byte, error) {
	if _, err := fmt.Fprintf(s.stdin, "tile_bif %d %d %d\n", levelIdx, col, row); err != nil {
		return nil, fmt.Errorf("tifffile-oracle: send tile_bif request: %w", err)
	}
	return s.readBlob()
}

// Close sends "quit" to the runner and waits for the subprocess to
// exit.
func (s *TifffileSession) Close() error {
	_, _ = fmt.Fprintln(s.stdin, "quit")
	_ = s.stdin.Close()
	return s.cmd.Wait()
}

func (s *TifffileSession) readBlob() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(s.stdout, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("tifffile-oracle: read response length: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s.stdout, buf); err != nil {
		return nil, fmt.Errorf("tifffile-oracle: read response body (%d bytes): %w", length, err)
	}
	return buf, nil
}
