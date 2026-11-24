/* Per-thread last error for libopentile. A cgo //export call runs on the
 * calling OS thread for its whole duration, so thread-local storage here is
 * genuinely per caller thread. Fixed-size buffer: nothing to free at thread
 * exit; messages longer than the buffer are truncated. */
#include <string.h>
#include "internal.h"

#if defined(_MSC_VER)
#  define OT_TLS __declspec(thread)
#else
#  define OT_TLS __thread
#endif

#define OT_ERR_MSG_MAX 4096

static OT_TLS int ot__code;
static OT_TLS size_t ot__len;
static OT_TLS char ot__msg[OT_ERR_MSG_MAX];

void ot__set_error(int code, const char* msg, size_t n) {
    if (n >= OT_ERR_MSG_MAX) n = OT_ERR_MSG_MAX - 1;
    if (n) memcpy(ot__msg, msg, n);
    ot__msg[n] = 0;
    ot__len = n;
    ot__code = code;
}

int ot__error_code(void) { return ot__code; }

const char* ot__error_msg(size_t* n) {
    *n = ot__len;
    return ot__msg;
}
