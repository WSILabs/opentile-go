// pkgconfig-static runs `pkg-config --static <args>`. The libopentile release
// build points PKG_CONFIG at it so cgo compiles and links against the codecs'
// static variants: --static adds Libs.private (e.g. libjxl's highway and
// brotli) and the static-build defines (-DOPJ_STATIC, -DJXL_STATIC_DEFINE).
// Go runs PKG_CONFIG directly, so this must be a real executable, not a
// script (ported from openscope-native's libopentile build).
//
// The real pkg-config is OT_REAL_PKG_CONFIG if set, else "pkg-config".
package main

import (
	"errors"
	"os"
	"os/exec"
)

func main() {
	real := os.Getenv("OT_REAL_PKG_CONFIG")
	if real == "" {
		real = "pkg-config"
	}
	cmd := exec.Command(real, append([]string{"--static"}, os.Args[1:]...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		os.Stderr.WriteString("pkgconfig-static: " + err.Error() + "\n")
		os.Exit(1)
	}
}
