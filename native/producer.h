#ifndef ANTIPESSIMIZER_NATIVE_PRODUCER_H
#define ANTIPESSIMIZER_NATIVE_PRODUCER_H

#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0a00
#endif
#include <windows.h>
#include <stdint.h>

#ifdef AP_BUILD_DLL
#define AP_API __declspec(dllexport)
#else
#define AP_API __declspec(dllimport)
#endif

typedef struct { uint32_t id, rva; } APProcedure;
typedef struct { uint32_t id, rva, size; } APHookProcedure;

/* Init/shutdown are owned by a cooperating host, outside DllMain. The selected
 * callers must be quiescent before shutdown. This API does not install hooks.
 * Events use per-thread bounded SPSC rings; only the drain thread performs I/O.
 * A future Delphi relay must separately preserve the interrupted native ABI. */
AP_API BOOL APNativeInit(const wchar_t *pipe_name, const APProcedure *procedures,
                        uint32_t count, uint32_t ring_capacity);
/* Startup-only installation negotiated with the Go controller. No selected
 * callers may run until this returns, or during Shutdown. Leaf subset only. */
AP_API BOOL APNativeInitHooks(const wchar_t *pipe_name, const APHookProcedure *procedures,
                             uint32_t count, uint32_t ring_capacity);
AP_API DWORD WINAPI APNativeBootstrap(void *request);
AP_API void APNativeEnter(uint32_t procedure);
AP_API void APNativeExit(uint32_t procedure);
AP_API void APNativeUnwind(uint32_t keep_depth);
AP_API void APNativeThreadEnd(void);
AP_API BOOL APNativeShutdown(DWORD timeout_ms);

#endif
