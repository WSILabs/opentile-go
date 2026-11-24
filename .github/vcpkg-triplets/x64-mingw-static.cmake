# Overlay for vcpkg's built-in community x64-mingw-static triplet (identical
# settings) plus one flag. Used by CI's integration-windows job and the Windows
# wheel build via --overlay-triplets=.github/vcpkg-triplets.
set(VCPKG_TARGET_ARCHITECTURE x64)
set(VCPKG_CRT_LINKAGE dynamic)
set(VCPKG_LIBRARY_LINKAGE static)
set(VCPKG_ENV_PASSTHROUGH_UNTRACKED PATH)

set(VCPKG_CMAKE_SYSTEM_NAME MinGW)

# GCC bug 54412: MinGW GCC assumes a 32-byte-aligned stack for AVX spills and
# emits aligned stores like `vmovaps %ymm4,0x3b0(%rsp)`, but Win64 only
# guarantees 16-byte stack alignment. Whenever %rsp isn't also 32-byte aligned
# the store faults (reported as access violation at 0xffffffffffffffff) —
# intermittently, depending on call depth. Hit in libjxl's AVX2 decode
# (jxl::N_AVX2::DecodeGroupImpl) and openjph's AVX2 encoder with winlibs
# GCC 15.3. This assembler option rewrites aligned vector moves as unaligned
# ones (same speed on aligned data on modern CPUs).
set(VCPKG_C_FLAGS "-Wa,-muse-unaligned-vector-move")
set(VCPKG_CXX_FLAGS "-Wa,-muse-unaligned-vector-move")
