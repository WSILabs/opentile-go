//go:build parity

package oracle

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// PythonBin returns the Python binary to invoke. Honors $OPENTILE_ORACLE_PYTHON
// so developers can point at a pinned venv (e.g. /tmp/opentile-py/bin/python)
// without putting it on PATH. Defaults to "python3".
func PythonBin() string {
	if p := os.Getenv("OPENTILE_ORACLE_PYTHON"); p != "" {
		return p
	}
	return "python3"
}

// RunnerScript returns the absolute path to oracle_runner.py, located in the
// same directory as this source file. Resolved via runtime.Caller so it works
// regardless of the test's working directory. Falls back to
// $OPENTILE_ORACLE_RUNNER if set.
func RunnerScript() string {
	if p := os.Getenv("OPENTILE_ORACLE_RUNNER"); p != "" {
		return p
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("tests", "oracle", "oracle_runner.py")
	}
	return filepath.Join(filepath.Dir(thisFile), "oracle_runner.py")
}

// Session is a long-lived Python opentile subprocess scoped to one slide.
// Open with NewSession; call Tile / Associated for as many requests as the
// caller needs; Close terminates the subprocess. Drops the ~200ms Python
// + opentile import cost from per-request to per-slide.
//
// Sessions are NOT safe for concurrent use — the protocol is request /
// response over a single pair of pipes, so callers must serialize access.
type Session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

// NewSession spawns the batched oracle runner for slide and returns a
// Session ready to accept Tile / Associated requests. The slide is
// opened once on the Python side; tile_size is fixed via
// OPENTILE_TILE_SIZE for the lifetime of the session.
func NewSession(slide string, tileSize int) (*Session, error) {
	cmd := exec.Command(PythonBin(), RunnerScript(), slide)
	cmd.Env = append(os.Environ(), fmt.Sprintf("OPENTILE_TILE_SIZE=%d", tileSize))
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("oracle: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("oracle: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("oracle: start runner: %w", err)
	}
	return &Session{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

// Tile requests one level tile from the running session and returns the
// raw bytes Python opentile produced.
func (s *Session) Tile(level, x, y int) ([]byte, error) {
	if _, err := fmt.Fprintf(s.stdin, "level %d %d %d\n", level, x, y); err != nil {
		return nil, fmt.Errorf("oracle: send tile request: %w", err)
	}
	return s.readBlob()
}

// Associated requests an associated image of the given type ("label",
// "overview", "thumbnail"). Returns (nil, nil) if Python opentile does
// not expose that type on this slide — matches the v0.2 one-shot helper
// semantics so callers that t.Skip on empty bytes keep working.
func (s *Session) Associated(imageType string) ([]byte, error) {
	if _, err := fmt.Fprintf(s.stdin, "associated %s\n", imageType); err != nil {
		return nil, fmt.Errorf("oracle: send associated request: %w", err)
	}
	return s.readBlob()
}

// Close sends "quit" to the runner and waits for the subprocess to exit.
// Idempotent: repeated calls return the same final error.
func (s *Session) Close() error {
	_, _ = fmt.Fprintln(s.stdin, "quit")
	_ = s.stdin.Close()
	return s.cmd.Wait()
}

// readBlob reads one length-prefixed response: 4-byte big-endian length
// followed by that many bytes. A zero length is the runner's "skip /
// not-exposed" signal and is returned as (nil, nil).
func (s *Session) readBlob() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(s.stdout, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("oracle: read response length: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s.stdout, buf); err != nil {
		return nil, fmt.Errorf("oracle: read response body (%d bytes): %w", length, err)
	}
	return buf, nil
}

// Tile invokes Python opentile for a single tile and returns its raw bytes.
// The Python side is configured with OPENTILE_TILE_SIZE = tileSize.
//
// Deprecated for inner-loop use: prefer NewSession for any caller that
// fetches more than one tile from the same slide. Kept for one-shot
// diagnostic invocations that don't justify session overhead.
func Tile(slide string, level, x, y, tileSize int) ([]byte, error) {
	cmd := exec.Command(PythonBin(), RunnerScript(), slide, fmt.Sprint(level), fmt.Sprint(x), fmt.Sprint(y))
	cmd.Env = append(os.Environ(), fmt.Sprintf("OPENTILE_TILE_SIZE=%d", tileSize))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python oracle failed (%s): %w\nstderr:\n%s", cmd.Path, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// Associated invokes Python opentile for an associated image of the given
// type ("label", "overview", "thumbnail") and returns its raw bytes.
//
// Deprecated for inner-loop use: prefer Session.Associated. Kept for one-
// shot diagnostic invocations.
//
// If Python opentile does not expose that type on this slide (e.g. NDPI
// CMU-1 has no labels or thumbnails), the runner emits zero-length stdout
// with exit 0; the caller receives a nil/empty slice and should skip the
// comparison. A non-nil error means the subprocess itself failed.
//
// tileSize is passed via OPENTILE_TILE_SIZE for parity with Tile, though
// for associated images it does not affect the returned bytes — the
// associated image is always a single, fixed-size blob.
func Associated(slide, imageType string, tileSize int) ([]byte, error) {
	cmd := exec.Command(PythonBin(), RunnerScript(), slide, imageType)
	cmd.Env = append(os.Environ(), fmt.Sprintf("OPENTILE_TILE_SIZE=%d", tileSize))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python oracle associated %q failed (%s): %w\nstderr:\n%s", imageType, cmd.Path, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
