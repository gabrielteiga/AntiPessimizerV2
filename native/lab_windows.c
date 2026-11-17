/* Controlled native workload, compiled with GCC's entry/exit callbacks.
 * This tests the producer, not installation of hooks in unmodified Delphi. */
#define WIN32_LEAN_AND_MEAN
#include "producer.h"
#include <stdio.h>
#include <stdlib.h>
#include <wchar.h>

#define NOHOOK __attribute__((no_instrument_function))
#define LAB __attribute__((noinline,dllexport))
static uint32_t iterations = 10;
static volatile LONG bad_error;
static HANDLE start_workers;
static uint32_t id_for(void *function) NOHOOK;
void __cyg_profile_func_enter(void *function, void *caller) NOHOOK;
void __cyg_profile_func_exit(void *function, void *caller) NOHOOK;

LAB uint64_t APLabChild(uint64_t a,uint64_t b,uint64_t c,uint64_t d,uint64_t e,uint64_t f) {
    Sleep(1);
    return a+2*b+3*c+4*d+5*e+6*f;
}
LAB uint64_t APLabRecursive(uint64_t n) { return n ? n*APLabRecursive(n-1) : 1; }
LAB double APLabFloat(double value) { return value*2.0+0.25; }
LAB uint64_t APLabParent(void) {
    return APLabChild(1,2,3,4,5,6)+APLabChild(6,5,4,3,2,1)+APLabRecursive(3)+(uint64_t)APLabFloat(3.5);
}
static uint32_t id_for(void *function) {
    if (function==(void *)APLabParent) return 1;
    if (function==(void *)APLabChild) return 2;
    if (function==(void *)APLabRecursive) return 3;
    if (function==(void *)APLabFloat) return 4;
    return 0;
}
void __cyg_profile_func_enter(void *function, void *caller) {
    (void)caller; DWORD error=GetLastError(); uint32_t id=id_for(function); if(id) APNativeEnter(id);
    if(GetLastError()!=error) InterlockedExchange(&bad_error,1);
}
void __cyg_profile_func_exit(void *function, void *caller) {
    (void)caller; DWORD error=GetLastError(); uint32_t id=id_for(function); if(id) APNativeExit(id);
    if(GetLastError()!=error) InterlockedExchange(&bad_error,1);
}
static DWORD WINAPI worker(void *unused) {
    (void)unused;
    if (WaitForSingleObject(start_workers, 5000)!=WAIT_OBJECT_0) return 1;
    uint64_t sum=0;
    for (uint32_t i=0;i<iterations;i++) sum+=APLabParent();
    APNativeThreadEnd();
    return sum==(uint64_t)iterations*160 && !bad_error ? 0 : 1;
}
int wmain(int argc,wchar_t **argv) {
    const wchar_t *pipe=NULL;
    uint32_t threads=3,capacity=1024;
    for(int i=1;i<argc;i+=2) {
        if(i+1>=argc) return 2;
        if(!wcscmp(argv[i],L"--pipe")) pipe=argv[i+1];
        else if(!wcscmp(argv[i],L"--threads")) threads=(uint32_t)wcstoul(argv[i+1],NULL,10);
        else if(!wcscmp(argv[i],L"--iterations")) iterations=(uint32_t)wcstoul(argv[i+1],NULL,10);
        else if(!wcscmp(argv[i],L"--capacity")) capacity=(uint32_t)wcstoul(argv[i+1],NULL,10);
        else return 2;
    }
    if(!pipe || !threads || threads>16 || !iterations || iterations>100000) return 2;
    uintptr_t base=(uintptr_t)GetModuleHandleW(NULL);
    APProcedure procs[]={{1,(uint32_t)((uintptr_t)APLabParent-base)}, {2,(uint32_t)((uintptr_t)APLabChild-base)}, {3,(uint32_t)((uintptr_t)APLabRecursive-base)}, {4,(uint32_t)((uintptr_t)APLabFloat-base)}};
    if(!APNativeInit(pipe,procs,4,capacity)) {fputs("native producer initialization failed\n",stderr);return 3;}
    start_workers=CreateEventW(NULL,TRUE,FALSE,NULL);
    if(!start_workers) return 4;
    HANDLE handles[16]; DWORD status=0;
    for(uint32_t i=0;i<threads;i++) {
        handles[i]=CreateThread(NULL,0,worker,NULL,0,NULL);
        if(!handles[i]) {fputs("native worker creation failed\n",stderr);return 4;}
    }
    SetEvent(start_workers);
    for(uint32_t i=0;i<threads;i++) {
        DWORD code=1;
        if(WaitForSingleObject(handles[i],10000)!=WAIT_OBJECT_0) return 5;
        GetExitCodeThread(handles[i],&code); CloseHandle(handles[i]); status|=code;
    }
    CloseHandle(start_workers);
    if(!APNativeShutdown(5000)) return 6;
    return status ? 7 : 0;
}
