#!/usr/bin/env bash
# Local development build of the opentile-go c-shared FFI lib. Links the codec
# libraries dynamically against the system (brew/apt) install — NOT the vcpkg
# static build used for distributable wheels (that is Phase 2). Produces
# opentile_go/_opentilego.so next to the Python package.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
CGO_ENABLED=1 go build -buildmode=c-shared \
  -o "$here/opentile_go/_opentilego.so" \
  "$here/cshim"
echo "built: $here/opentile_go/_opentilego.so"
