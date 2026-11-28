/* Internal helpers shared by the cgo files of libopentile. Not public. */
#ifndef OT_INTERNAL_H
#define OT_INTERNAL_H
#include <stddef.h>

/* errors.c: per-thread last error. */
void ot__set_error(int code, const char* msg, size_t n);
int ot__error_code(void);
const char* ot__error_msg(size_t* n);

#endif
