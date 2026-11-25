#!/usr/bin/env bash
# Development build of libopentile (../capi, the opentile-go C API) into the
# Python package, linked dynamically against the codecs and lcms2 that
# pkg-config finds (brew/apt). Wheels instead bundle the static release build
# from capi/scripts/build-release.sh (see setup.py, OPENTILE_GO_STAGE).
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
case "$(uname -s)" in
    Darwin) lib=libopentile.dylib ;;
    MINGW* | MSYS* | CYGWIN*) lib=libopentile.dll ;;
    *) lib=libopentile.so ;;
esac
rm -f "$here"/opentile_go/libopentile.* "$here"/opentile_go/_opentilego.*
(cd "$here/../capi" && CGO_ENABLED=1 go build -buildmode=c-shared -buildvcs=false \
    -o "$here/opentile_go/$lib" .)
rm -f "$here/opentile_go/libopentile.h" # Go's generated header; capi/opentile.h is the public one
echo "built: $here/opentile_go/$lib"
