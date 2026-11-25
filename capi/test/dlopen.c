/* dlopen test for libopentile (Linux/macOS): load the library at run time,
 * as Python's ctypes and plugin loaders do, then use the per-thread last
 * error. A library that needs more static TLS than glibc reserves for
 * dlopen'd modules fails here with "cannot allocate memory in static TLS
 * block"; linking at startup (smoke.c) hides that.
 *
 * Usage: dlopen /path/to/libopentile.{so,dylib} */
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define OT_NO_PROTOTYPES
#include "opentile.h"

typedef int (*api_version_fn)(int32_t*, int32_t*);
typedef int (*open_fn)(const char*, const ot_open_opts*, ot_slide*);
typedef int (*last_error_fn)(char*, size_t, size_t*);
typedef int (*last_error_code_fn)(void);

int main(int argc, char** argv) {
    if (argc != 2) {
        fprintf(stderr, "usage: %s LIBRARY\n", argv[0]);
        return 2;
    }
    void* h = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    if (!h) {
        fprintf(stderr, "FAIL: dlopen: %s\n", dlerror());
        return 1;
    }
    api_version_fn api_version = (api_version_fn)dlsym(h, "ot_api_version");
    open_fn open = (open_fn)dlsym(h, "ot_open");
    last_error_fn last_error = (last_error_fn)dlsym(h, "ot_last_error");
    last_error_code_fn last_error_code = (last_error_code_fn)dlsym(h, "ot_last_error_code");
    if (!api_version || !open || !last_error || !last_error_code) {
        fprintf(stderr, "FAIL: dlsym: %s\n", dlerror());
        return 1;
    }
    int32_t major = 0, minor = 0;
    if (api_version(&major, &minor) != OT_OK || major != OT_API_VERSION_MAJOR) {
        fprintf(stderr, "FAIL: api version %d.%d\n", major, minor);
        return 1;
    }
    /* A failing call sets the per-thread error. */
    ot_slide s = 0;
    int rc = open("/nonexistent/slide.svs", NULL, &s);
    char msg[512];
    size_t n = 0;
    if (rc == OT_OK || last_error_code() != rc || last_error(msg, sizeof msg, &n) != OT_OK || n == 0) {
        fprintf(stderr, "FAIL: error state after a failed open (rc %d)\n", rc);
        return 1;
    }
    printf("dlopen OK: api %d.%d, last error \"%s\"\n", major, minor, msg);
    return 0;
}
