/* Test-only adapter: expose the actual production language bridge without
 * initializing a collector or publishing these entry points in the package. */
#include "producer_windows.c"
AP_API uintptr_t APReferenceUnwindAddress(void){return (uintptr_t)function_unwind;}
