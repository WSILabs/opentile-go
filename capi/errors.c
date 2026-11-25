/* Per-thread last error for libopentile. A cgo //export call runs on the
 * calling OS thread for its whole duration, so per-thread state here is
 * genuinely per caller thread. Messages longer than OT_ERR_MSG_MAX are
 * truncated.
 *
 * POSIX keeps the state in a heap block per thread behind a pthread key, not
 * in __thread storage. The Go runtime in this library uses initial-exec TLS,
 * which makes glibc place the library's whole TLS segment in the small
 * static-TLS reserve when the library is dlopen'd (ctypes, plugin loaders);
 * a 4 KB __thread buffer there fails with "cannot allocate memory in static
 * TLS block". The block is freed when the thread exits.
 *
 * Windows keeps thread-local storage: DLLs loaded with LoadLibrary get it
 * without a fixed reserve. */
#include <stdlib.h>
#include <string.h>
#include "internal.h"

#define OT_ERR_MSG_MAX 4096

typedef struct {
    int    code;
    size_t len;
    char   msg[OT_ERR_MSG_MAX];
} ot__err_state;

#if defined(_WIN32)

#  if defined(_MSC_VER)
#    define OT_TLS __declspec(thread)
#  else
#    define OT_TLS __thread
#  endif

static OT_TLS ot__err_state ot__state;

static ot__err_state* ot__get(int create) {
    (void)create;
    return &ot__state;
}

#else /* POSIX */

#  include <pthread.h>

static pthread_key_t ot__key;
static pthread_once_t ot__once = PTHREAD_ONCE_INIT;
static int ot__key_ok;

static void ot__make_key(void) { ot__key_ok = pthread_key_create(&ot__key, free) == 0; }

/* The calling thread's state; NULL if it has none yet and create is 0, or
 * if allocation fails. */
static ot__err_state* ot__get(int create) {
    pthread_once(&ot__once, ot__make_key);
    if (!ot__key_ok) return NULL;
    ot__err_state* s = pthread_getspecific(ot__key);
    if (!s && create) {
        s = calloc(1, sizeof *s);
        if (s && pthread_setspecific(ot__key, s) != 0) {
            free(s);
            s = NULL;
        }
    }
    return s;
}

#endif

void ot__set_error(int code, const char* msg, size_t n) {
    ot__err_state* s = ot__get(1);
    if (!s) return; /* out of memory: the status code still reports the failure */
    if (n >= OT_ERR_MSG_MAX) n = OT_ERR_MSG_MAX - 1;
    if (n) memcpy(s->msg, msg, n);
    s->msg[n] = 0;
    s->len = n;
    s->code = code;
}

int ot__error_code(void) {
    ot__err_state* s = ot__get(0);
    return s ? s->code : 0;
}

const char* ot__error_msg(size_t* n) {
    ot__err_state* s = ot__get(0);
    if (!s) {
        *n = 0;
        return "";
    }
    *n = s->len;
    return s->msg;
}
