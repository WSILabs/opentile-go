package main

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

var update = flag.Bool("update", false, "rewrite libopentile.def from opentile.h")

var (
	protoRe  = regexp.MustCompile(`(?s)OT_API\s+int\s+(ot_\w+)\s*\(([^)]*)\)\s*;`)
	defineRe = regexp.MustCompile(`(?m)^#define\s+(OT_\w+)\s+\(?(-?\d+)(?:u\s*<<\s*(\d+))?\)?`)
)

// headerPrototypes returns name -> parameter count for every OT_API function.
func headerPrototypes(t *testing.T) map[string]int {
	t.Helper()
	src, err := os.ReadFile("opentile.h")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, m := range protoRe.FindAllStringSubmatch(string(src), -1) {
		params := strings.TrimSpace(m[2])
		n := 0
		if params != "" && params != "void" {
			n = strings.Count(params, ",") + 1
		}
		out[m[1]] = n
	}
	return out
}

// headerDefines returns every integer #define OT_* in the header.
func headerDefines(t *testing.T) map[string]int64 {
	t.Helper()
	src, err := os.ReadFile("opentile.h")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, m := range defineRe.FindAllStringSubmatch(string(src), -1) {
		v, _ := strconv.ParseInt(m[2], 10, 64)
		if m[3] != "" {
			sh, _ := strconv.Atoi(m[3])
			v <<= sh
		}
		out[m[1]] = v
	}
	return out
}

// goExports returns name -> parameter count for every //export function.
func goExports(t *testing.T) map[string]int {
	t.Helper()
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	out := map[string]int{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Doc == nil {
				continue
			}
			for _, c := range fd.Doc.List {
				if strings.HasPrefix(c.Text, "//export ") {
					n := 0
					for _, p := range fd.Type.Params.List {
						n += max(len(p.Names), 1)
					}
					out[strings.TrimPrefix(c.Text, "//export ")] = n
				}
			}
		}
	}
	return out
}

func TestHeaderMatchesExports(t *testing.T) {
	h, g := headerPrototypes(t), goExports(t)
	if len(h) == 0 {
		t.Fatal("no prototypes parsed from opentile.h")
	}
	for name, n := range h {
		gn, ok := g[name]
		switch {
		case !ok:
			t.Errorf("%s declared in opentile.h but not //export'ed", name)
		case gn != n:
			t.Errorf("%s: header has %d params, Go export has %d", name, n, gn)
		}
	}
	for name := range g {
		if _, ok := h[name]; !ok {
			t.Errorf("%s is //export'ed but missing from opentile.h", name)
		}
	}
}

func wantDefine(t *testing.T, defs map[string]int64, name string, want int64) {
	t.Helper()
	got, ok := defs[name]
	if !ok {
		t.Errorf("%s missing from opentile.h", name)
	} else if got != want {
		t.Errorf("%s = %d in opentile.h, Go uses %d", name, got, want)
	}
}

func TestHeaderConstants(t *testing.T) {
	d := headerDefines(t)
	for name, v := range map[string]int64{
		"OT_API_VERSION_MAJOR": apiVersionMajor, "OT_API_VERSION_MINOR": apiVersionMinor,
		"OT_OK": statusOK, "OT_ERR": errGeneric, "OT_ERR_HANDLE": errHandle, "OT_ERR_ARG": errArg,
		"OT_ERR_BUFFER": errBuffer, "OT_ERR_RANGE": errRange, "OT_ERR_FORMAT": errFormat,
		"OT_ERR_IO": errIO, "OT_ERR_CODEC": errCodec, "OT_ERR_CORRUPT": errCorrupt,
		"OT_ERR_UNSUPPORTED": errUnsupported, "OT_ERR_SPARSE": errSparse,
		"OT_PIXEL_RGB8": pixelRGB8, "OT_PIXEL_RGBA8": pixelRGBA8,
		"OT_BACKING_DEFAULT": backingDefault, "OT_BACKING_MMAP": backingMmap, "OT_BACKING_PREAD": backingPread,
		"OT_CORRUPT_TILE_ERROR": corruptTileError, "OT_CORRUPT_TILE_BLANK": corruptTileBlank,
		"OT_DECODE_SRGB": decodeSRGB,
		"OT_KERNEL_DEFAULT": 0,
	} {
		wantDefine(t, d, name, v)
	}
	for f, v := range formatCodes {
		name := "OT_FORMAT_UNKNOWN"
		if f != opentile.FormatUnknown {
			name = "OT_FORMAT_" + strings.ToUpper(strings.ReplaceAll(string(f), "-", "_"))
		}
		wantDefine(t, d, name, int64(v))
	}
	for c, v := range compressionCodes {
		wantDefine(t, d, "OT_COMPRESSION_"+strings.ToUpper(strings.ReplaceAll(c.String(), "-", "")), int64(v))
	}
	for m, v := range overlapCodes {
		wantDefine(t, d, "OT_OVERLAP_"+strings.ToUpper(m.String()), int64(v))
	}
	kernelNames := map[int32]string{1: "NEAREST", 2: "BILINEAR", 3: "LANCZOS", 4: "BOX"}
	for v := range kernelCodes {
		wantDefine(t, d, "OT_KERNEL_"+kernelNames[v], int64(v))
	}
	// Every format the Go library defines must have a code.
	for _, f := range []opentile.Format{
		opentile.FormatSVS, opentile.FormatNDPI, opentile.FormatPhilipsTIFF, opentile.FormatOMETIFF,
		opentile.FormatBIF, opentile.FormatIFE, opentile.FormatGenericTIFF, opentile.FormatLeicaSCN,
		opentile.FormatSZI, opentile.FormatCOGWSI, opentile.FormatDICOM, opentile.FormatDZI,
	} {
		if _, ok := formatCodes[f]; !ok {
			t.Errorf("format %q has no OT_FORMAT_ code", f)
		}
	}
	// Every compression up to the last known one must have a code.
	for c := opentile.CompressionUnknown; c <= opentile.CompressionPNG; c++ {
		if _, ok := compressionCodes[c]; !ok {
			t.Errorf("compression %v has no OT_COMPRESSION_ code", c)
		}
	}
}

func defFile(t *testing.T) string {
	names := make([]string, 0)
	for n := range headerPrototypes(t) {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("; Generated from opentile.h by `go test -run TestDefFile -update`. Do not edit.\n")
	b.WriteString("LIBRARY libopentile.dll\nEXPORTS\n")
	for _, n := range names {
		b.WriteString("    " + n + "\n")
	}
	return b.String()
}

func TestDefFile(t *testing.T) {
	want := defFile(t)
	if *update {
		if err := os.WriteFile("libopentile.def", []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile("libopentile.def")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Error("libopentile.def is stale; run `go test -run TestDefFile -update` in capi/")
	}
}

func TestCodeOf(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{opentile.ErrLevelOutOfRange, errRange},
		{opentile.ErrSparseTile, errSparse},
		{opentile.ErrUnsupportedFormat, errFormat},
		{&os.PathError{Op: "open", Path: "x", Err: os.ErrNotExist}, errIO},
		{os.ErrClosed, errGeneric},
	}
	for _, c := range cases {
		if got := codeOf(c.err); got != c.want {
			t.Errorf("codeOf(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}
