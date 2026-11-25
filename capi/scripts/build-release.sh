#!/usr/bin/env bash
# Build and stage one libopentile release (#120 step 3): a self-contained
# shared library with the six codecs and lcms2 linked in statically.
#
# Usage: capi/scripts/build-release.sh VERSION PLATFORM TRIPLET_DIR OUT_DIR
#   VERSION      stamped into ot_library_version, e.g. v0.64.0
#   PLATFORM     linux-x86_64 | linux-aarch64 | macos-arm64 | macos-x86_64 |
#                windows-x86_64
#   TRIPLET_DIR  vcpkg_installed/<triplet>, installed with --x-feature=capi
#   OUT_DIR      the release is staged in OUT_DIR/libopentile-VERSION-PLATFORM
#
# Needs Go, a C/C++ toolchain and pkg-config on PATH. For a cross build
# (macos-x86_64 on arm64) set GOARCH and the CGO_*FLAGS first. Windows needs
# MinGW-w64 GCC; the MSVC import library is made afterwards from the staged
# .def (`lib /def:`), because that step needs MSVC.
#
# Staged layout:
#   include/opentile.h
#   lib/libopentile.so | lib/libopentile.dylib | bin/libopentile.dll + lib/libopentile.def
#   LICENSE, NOTICE, README.md, licenses/ (third-party notices for what is linked in)
#
# It first runs capi's Go tests (header/export sync), and fails if the library
# links anything outside the OS, does not export every function in
# libopentile.def, or carries an AV1 encoder (AVIF decodes through dav1d).
#
# OT_SRC_ROOT builds a different checkout (e.g. an older release tag) with
# this script and its tools; it defaults to the repo holding this script.
set -euo pipefail

if [ $# -ne 4 ]; then
    sed -n '2,29p' "$0" >&2
    exit 2
fi
version=$1 platform=$2 triplet_dir=$3 out_dir=$4

# No VCS stamping for any go command here (vet, test, build, list): inside
# build containers git may not trust the checkout ("dubious ownership"), and
# the library's version comes from -X main.version.
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false"

here=$(cd "$(dirname "$0")/.." && pwd) # capi/ holding this script and tools/
root=$(cd "${OT_SRC_ROOT:-$here/..}" && pwd)
capi="$root/capi"
triplet_dir=$(cd "$triplet_dir" && pwd)
mkdir -p "$out_dir"
out_dir=$(cd "$out_dir" && pwd)
stage="$out_dir/libopentile-$version-$platform"

case "$platform" in
    linux-*) os=linux; lib=lib/libopentile.so; ext="-Wl,-soname,libopentile.so" ;;
    macos-*) os=macos; lib=lib/libopentile.dylib; ext="-Wl,-install_name,@rpath/libopentile.dylib" ;;
    windows-*) os=windows; lib=bin/libopentile.dll; ext="-static" ;;
    *) echo "unknown platform: $platform" >&2; exit 2 ;;
esac

rm -rf "$stage"
mkdir -p "$stage/include" "$stage/lib" "$stage/licenses/vcpkg" "$stage/licenses/go"
[ "$os" = windows ] && mkdir -p "$stage/bin"

# --- pkg-config --static wrapper (built for the host, not the target) ------
tools=$(mktemp -d)
trap 'rm -rf "$tools"' EXIT
wrapper="$tools/pkg-config-static"
[ "$os" = windows ] && wrapper="$wrapper.exe"
(cd "$here" && env -u GOARCH -u GOOS CGO_ENABLED=0 go build -buildvcs=false -o "$wrapper" ./tools/pkgconfig-static)
command -v cygpath >/dev/null 2>&1 && wrapper=$(cygpath -w "$wrapper")
export OT_REAL_PKG_CONFIG="${PKG_CONFIG:-pkg-config}"
export PKG_CONFIG="$wrapper"
export PKG_CONFIG_PATH="$triplet_dir/lib/pkgconfig"

modules="libturbojpeg libopenjp2 libjxl libjxl_threads libwebp libavif openjph lcms2"
for m in $modules; do
    if ! "$OT_REAL_PKG_CONFIG" --static --exists "$m"; then
        echo "pkg-config can't find $m under $PKG_CONFIG_PATH (vcpkg install --x-feature=capi?)" >&2
        exit 1
    fi
done

# --- Go tests, then build --------------------------------------------------
echo "testing capi ($root)"
(cd "$capi" && CGO_ENABLED=1 go vet . && CGO_ENABLED=1 go test -buildvcs=false -count=1 .)
echo "building $lib ($version, $platform)"
(cd "$capi" && CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$version -extldflags '$ext'" \
    -o "$stage/$lib" .)
rm -f "$stage/${lib%.*}.h" # Go's generated header; opentile.h is the public one

# --- headers, docs, notices -----------------------------------------------
cp "$capi/opentile.h" "$stage/include/"
[ "$os" = windows ] && cp "$capi/libopentile.def" "$stage/lib/"
cp "$root/LICENSE" "$root/NOTICE" "$stage/"
cp "$capi/README.md" "$stage/README.md"

n=0
for d in "$triplet_dir"/share/*/; do
    p=$(basename "$d")
    if [ -f "$d/copyright" ]; then
        cp "$d/copyright" "$stage/licenses/vcpkg/$p.txt"
        n=$((n + 1))
    fi
done
[ "$n" -gt 0 ] || { echo "no vcpkg copyright files under $triplet_dir/share" >&2; exit 1; }
cp "$(go env GOROOT)/LICENSE" "$stage/licenses/go/go.txt"
# Go modules linked into the library (opentile-go's own LICENSE is at the top).
(cd "$capi" && go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Dir}}{{end}}{{end}}' .) |
    sort -u | while IFS='|' read -r path dir; do
        [ -n "$path" ] || continue
        [ "$path" = github.com/wsilabs/opentile-go ] && continue
        f=$(ls "$dir"/LICENSE* "$dir"/COPYING* 2>/dev/null | head -1 || true)
        [ -n "$f" ] || { echo "no license file for Go module $path ($dir)" >&2; exit 1; }
        cp "$f" "$stage/licenses/go/$(echo "$path" | tr '/' '_').txt"
    done

# --- checks: only OS dependencies, every .def function exported ----------
libpath="$stage/$lib"
want=$(awk '/^EXPORTS/ { f = 1; next } f && NF { print $1 }' "$capi/libopentile.def")
case "$os" in
    linux)
        deps=$(readelf -d "$libpath" | sed -n 's/.*(NEEDED).*\[\(.*\)\]/\1/p')
        bad=$(echo "$deps" | grep -Ev '^(libc|libm|libpthread|libdl|librt|libstdc\+\+|libgcc_s)\.so(\.[0-9]+)*$|^ld-linux' || true)
        have=$(nm -D --defined-only "$libpath" | awk '{ print $NF }')
        ;;
    macos)
        deps=$(otool -L "$libpath" | tail -n +2 | awk '{ print $1 }')
        bad=$(echo "$deps" | grep -Ev '^(/usr/lib/|/System/Library/|@rpath/libopentile\.dylib$)' || true)
        have=$(nm -gU "$libpath" | awk '{ print $NF }' | sed 's/^_//')
        ;;
    windows)
        deps=$(objdump -p "$libpath" | sed -n 's/^[[:space:]]*DLL Name: //p' | tr -d '\r')
        bad=""
        for d in $deps; do
            case "$d" in api-ms-win-*) continue ;; esac
            [ -f "/c/Windows/System32/$d" ] || bad="$bad $d"
        done
        have=$(objdump -p "$libpath" | tr -d '\r' |
            awk '/\[Ordinal\/Name Pointer\] Table/ { f = 1; next } f && /^[[:space:]]*\[/ { print $NF } f && !NF { f = 0 }')
        ;;
esac
echo "linked:" $deps
if [ -n "$(echo "$bad" | tr -d '[:space:]')" ]; then
    echo "FAIL: links non-system libraries:" $bad >&2
    exit 1
fi
missing=""
for s in $want; do
    # A here-string, not a pipe: grep -q exits on the first match, and with
    # pipefail the killed echo (SIGPIPE) would count a found symbol missing.
    grep -qx "$s" <<< "$have" || missing="$missing $s"
done
if [ -n "$missing" ]; then
    echo "FAIL: not exported:$missing" >&2
    exit 1
fi
# AVIF must decode through dav1d with no AV1 encoder linked in (#120): some
# downstream release checks reject libraries that carry AV1 encoder code.
# These are the encoders' interface/version strings (aom, SVT-AV1, rav1e);
# libavif's codec table names only the codecs compiled in.
for s in 'AOMedia Project AV1 Encoder' 'SVT-AV1' 'rav1e'; do
    if grep -aq "$s" "$libpath"; then
        echo "FAIL: AV1 encoder linked in (found \"$s\"); build libavif with dav1d only" >&2
        exit 1
    fi
done
grep -aq dav1d "$libpath" || { echo "FAIL: dav1d (AVIF decode) not linked in" >&2; exit 1; }
echo "ok: $(echo "$want" | wc -w | tr -d ' ') functions exported, OS libraries only, AVIF via dav1d with no AV1 encoder"
ls -la "$stage/$(dirname "$lib")"
