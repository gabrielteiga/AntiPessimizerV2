/* Native boundary adapter only. Accounting and presentation remain in Go.
 * The wire format is versioned little endian, not compiler structs or pointers. */
#define AP_BUILD_DLL
#define WIN32_LEAN_AND_MEAN
#include "producer.h"
#include <string.h>
#include <wchar.h>
#include "hooks_windows.h"

enum { AP_THREADS = 64, AP_CAPACITY = 1024, AP_BATCH = 256, AP_SELECTION = 64 };
_Static_assert((int)AP_HOOK_LIMIT==(int)AP_SELECTION,"native installer and event catalogs must have the same bound");
enum { AP_START = 1, AP_ENTER = 2, AP_EXIT = 3, AP_END = 4, AP_UNWIND = 5, AP_UNWIND_EXIT = 6, AP_HEARTBEAT = 7, AP_CLEAR = 8 };
typedef struct { uint32_t type, tid, procedure, depth; uint64_t tick; } Event;
_Static_assert(sizeof(void *) == 8, "Windows x64 is required");
_Static_assert(sizeof(Event) == 24, "event layout changed");
typedef struct {
    _Alignas(64) volatile LONG64 head;
    _Alignas(64) volatile LONG64 tail;
    volatile LONG tid, ended, started;
    _Alignas(8) volatile LONG64 birth;
    Event events[AP_CAPACITY];
} Ring;
static struct {
    volatile LONG running, stopping, next_ring, io_error, writers, frozen, wake_pending;
    BOOL hook_mode;
    _Alignas(8) volatile LONG64 lost;
    DWORD tls;
    HANDLE pipe, worker, ready, wake;
    Ring *rings;
    APProcedure procedures[AP_SELECTION];
    uint32_t count, capacity, image_size;
    uint64_t frequency, start;
} state;

static uint64_t load64(volatile LONG64 *p) { return (uint64_t)InterlockedCompareExchange64(p, 0, 0); }
static LONG load32(volatile LONG *p) { return InterlockedCompareExchange(p, 0, 0); }
static void lost(void) { InterlockedIncrement64(&state.lost); }
/* Coalesce publication notifications while the worker is active. The worker
 * clears this flag and rescans every ring before waiting, closing the race
 * between an empty scan and a producer publishing its next head. */
static void notify_drain(void) {
    if(InterlockedCompareExchange(&state.wake_pending,1,0)==0 && !SetEvent(state.wake)) {
        InterlockedExchange(&state.io_error,(LONG)GetLastError());
        lost();
    }
}
static uint64_t ticks(void) {
    LARGE_INTEGER n;
    if (!QueryPerformanceCounter(&n) || n.QuadPart <= 0) { lost(); return 0; }
    return (uint64_t)n.QuadPart;
}
static void put32(unsigned char *p, uint32_t n) { for (unsigned i=0; i<4; i++) p[i]=(unsigned char)(n >> (i*8)); }
static void put64(unsigned char *p, uint64_t n) { for (unsigned i=0; i<8; i++) p[i]=(unsigned char)(n >> (i*8)); }
static BOOL write_all(const void *buffer, DWORD size) {
    const unsigned char *p = buffer;
    while (size) {
        DWORD n = 0;
        if (!WriteFile(state.pipe, p, size, &n, NULL) || !n) {
            DWORD error = GetLastError();
            InterlockedExchange(&state.io_error, (LONG)(error ? error : ERROR_WRITE_FAULT));
            return FALSE;
        }
        p += n; size -= n;
    }
    return TRUE;
}
static HANDLE open_pipe(const wchar_t *name) {
    /* ListenPipe reserves the name before Accept publishes a connectable
       instance. A same-process host can reach Init first. Also tolerate another
       client winning the instance between WaitNamedPipe and CreateFile. */
    ULONGLONG deadline = GetTickCount64() + 5000;
    for (;;) {
        HANDLE pipe = CreateFileW(name, GENERIC_READ | GENERIC_WRITE, 0, NULL, OPEN_EXISTING, 0, NULL);
        if (pipe != INVALID_HANDLE_VALUE || GetLastError() != ERROR_PIPE_BUSY) return pipe;
        ULONGLONG now = GetTickCount64();
        if (now >= deadline) { SetLastError(ERROR_SEM_TIMEOUT); return INVALID_HANDLE_VALUE; }
        if (!WaitNamedPipeW(name, (DWORD)(deadline-now))) return INVALID_HANDLE_VALUE;
    }
}
static BOOL batch(const Event *events, uint32_t count, uint64_t sequence, uint64_t final) {
    unsigned char header[32] = {0}, payload[AP_BATCH*24];
    put32(header, 0x31425041); /* APB1 */
    put32(header+4, count); put64(header+8, sequence);
    put64(header+16, load64(&state.lost)); put64(header+24, final);
    for (uint32_t i=0; i<count; i++) {
        unsigned char *p = payload+i*24;
        put32(p, events[i].type); put32(p+4, events[i].tid);
        put32(p+8, events[i].procedure); put32(p+12, events[i].depth);
        put64(p+16, events[i].tick);
    }
    return write_all(header, sizeof(header)) && (!count || write_all(payload, count*24));
}
/* Only the drain owner consumes rings. With writers gated, repeat until every
 * event before a Clear boundary has been published, across all thread rings. */
static int drain_rings(uint64_t *sequence) {
    BOOL any=FALSE;
    LONG count=load32(&state.next_ring);
    if(count>AP_THREADS)count=AP_THREADS;
    for(LONG i=0;i<count;i++){
        Ring *r=&state.rings[i];
        uint64_t tail=load64(&r->tail),head=load64(&r->head);
        uint32_t n=(uint32_t)(head-tail);
        if(!n)continue;
        if(n>AP_BATCH)n=AP_BATCH;
        Event copy[AP_BATCH];
        for(uint32_t j=0;j<n;j++)copy[j]=r->events[(tail+j)%state.capacity];
        InterlockedExchange64(&r->tail,(LONG64)(tail+n));
        if(!batch(copy,n,++*sequence,0))return -1;
        any=TRUE;
    }
    return any?1:0;
}
static BOOL writer_gate(BOOL require_running){
    for(;;){
        while(load32(&state.frozen)){
            if(require_running && !load32(&state.running))return FALSE;
            SwitchToThread();
        }
        InterlockedIncrement(&state.writers);
        if(!load32(&state.frozen))return TRUE;
        InterlockedDecrement(&state.writers);
    }
}
static BOOL clear_boundary(uint64_t *sequence){
    InterlockedExchange(&state.frozen,1);
    ULONGLONG deadline=GetTickCount64()+2000;
    while(load32(&state.writers)){
        if(GetTickCount64()>=deadline){lost();InterlockedExchange(&state.frozen,0);return FALSE;}
        SwitchToThread();
    }
    uint64_t at=ticks();
    int drained;
    do{drained=drain_rings(sequence);}while(drained>0);
    Event clear={AP_CLEAR,0,0,0,at};
    BOOL ok=drained==0 && at && batch(&clear,1,++*sequence,0);
    InterlockedExchange(&state.frozen,0);
    return ok;
}
/* The debugger reports actual OS thread exit, after application/TLS/DLL
 * finalizers. Publish its end before making the slot reusable by another life. */
static BOOL end_thread_boundary(uint32_t tid,uint64_t exited_at,uint64_t *sequence){
    Ring *r=NULL;
    LONG count=load32(&state.next_ring);if(count>AP_THREADS)count=AP_THREADS;
    for(LONG i=0;i<count;i++)if((uint32_t)load32(&state.rings[i].tid)==tid && load32(&state.rings[i].started) && load64(&state.rings[i].birth)<=exited_at && !load32(&state.rings[i].ended)){r=&state.rings[i];break;}
    if(!r)return TRUE;
    InterlockedExchange(&state.frozen,1);
    ULONGLONG deadline=GetTickCount64()+2000;
    while(load32(&state.writers)){
        if(GetTickCount64()>=deadline){lost();InterlockedExchange(&state.frozen,0);return FALSE;}
        SwitchToThread();
    }
    if((uint32_t)load32(&r->tid)!=tid || !load32(&r->started) || load64(&r->birth)>exited_at || load32(&r->ended)){InterlockedExchange(&state.frozen,0);return TRUE;}
    int drained;
    do{drained=drain_rings(sequence);}while(drained>0);
    BOOL ok=drained==0;
    if(ok && load32(&r->started) && !load32(&r->ended)){
        Event end={AP_END,tid,0,0,ticks()};
        ok=end.tick && batch(&end,1,++*sequence,0);
    }
    if(ok)InterlockedExchange(&r->ended,1);
    InterlockedExchange(&state.frozen,0);
    return ok;
}
static DWORD WINAPI drain(void *unused) {
    (void)unused;
    unsigned char hello[48] = { 'A','P','N','A','T','0','1',0 }, catalog[AP_SELECTION*8];
    put32(hello+8, state.hook_mode==2?4:1); put32(hello+12, 24); put64(hello+16, state.frequency);
    put32(hello+24, GetCurrentProcessId()); put32(hello+28, state.count);
    put32(hello+32, state.image_size); put64(hello+40, state.start);
    for (uint32_t i=0; i<state.count; i++) {
        put32(catalog+i*8, state.procedures[i].id); put32(catalog+i*8+4, state.procedures[i].rva);
    }
    if (!write_all(hello, sizeof(hello)) || !write_all(catalog, state.count*8)) return 1;
    /* A synchronous pipe handle serializes its I/O. Keep hello and ACK on
       this thread; reading ACK on Init's thread can block the hello write. */
    unsigned char start_signal=0; DWORD acknowledged=0;
    if (!ReadFile(state.pipe, &start_signal, 1, &acknowledged, NULL) || acknowledged!=1 || start_signal!=(state.hook_mode?2:1)) {
        InterlockedExchange(&state.io_error, ERROR_INVALID_DATA);
        return 1;
    }
    if(state.hook_mode && !hook_exchange(state.pipe,state.hook_mode==2)) {
        InterlockedExchange(&state.io_error, ERROR_INVALID_DATA);
        return 1;
    }
    InterlockedExchange(&state.running, 1);
    SetEvent(state.ready);
    uint64_t sequence = 0;
    ULONGLONG last_heartbeat=GetTickCount64();
    for (;;) {
        /* If shutdown was observed before this scan, all selected callers are
           already quiescent and every published head must be drained. */
        BOOL stopping = load32(&state.stopping);
        BOOL commanded=FALSE;
        if(state.hook_mode==2 && !stopping){
            DWORD available=0;
            if(!PeekNamedPipe(state.pipe,NULL,0,NULL,&available,NULL))return 1;
            if(available){
                unsigned char command=0;DWORD read=0;
                if(!ReadFile(state.pipe,&command,1,&read,NULL)||read!=1)return 1;
                if(command==3){if(!clear_boundary(&sequence))return 1;}
                else if(command==4){unsigned char life[12];if(!hook_io(state.pipe,life,12,FALSE)||!end_thread_boundary(hook_u32(life),hook_u64(life+4),&sequence))return 1;}
                else return 1;
                commanded=TRUE;
            }
        }
        int drained=drain_rings(&sequence);
        if(drained<0)return 1;
        BOOL any=drained!=0 || commanded;
        if (stopping && !any) break;
        /* Worker-owned liveness only: never advance a measured frame or invent
           elapsed time for an open call. V1 cooperative hosts stay unchanged. */
        ULONGLONG now=GetTickCount64();
        if(state.hook_mode==2 && now-last_heartbeat>=250) {
            Event heartbeat={AP_HEARTBEAT,0,0,0,ticks()};
            if(!batch(&heartbeat,1,++sequence,0)) return 1;
            last_heartbeat=now;
        }
        if (!any) {
            InterlockedExchange(&state.wake_pending,0);
            drained=drain_rings(&sequence);
            if(drained<0)return 1;
            if(drained>0 || load32(&state.stopping))continue;
            /* Owned captures retain their command/heartbeat polling bound.
             * Cooperative captures need no polling at all: publication and
             * shutdown both signal the event. No callback waits for space. */
            DWORD wait=WaitForSingleObject(state.wake,state.hook_mode==2?1:INFINITE);
            if(wait!=WAIT_OBJECT_0 && wait!=WAIT_TIMEOUT) {
                InterlockedExchange(&state.io_error,(LONG)GetLastError());
                return 1;
            }
        }
    }
    return batch(NULL, 0, ++sequence, ticks()) ? 0 : 1;
}
static uint64_t enqueue(Ring *r, uint32_t type, uint32_t procedure, uint32_t depth) {
    uint64_t tick = ticks();
    if (!tick) return 0;
    uint64_t head = load64(&r->head), tail = load64(&r->tail);
    if (head-tail >= state.capacity) { lost(); return 0; }
    r->events[head%state.capacity] = (Event){type, (uint32_t)load32(&r->tid), procedure, depth, tick};
    InterlockedExchange64(&r->head, (LONG64)(head+1));
    notify_drain();
    return tick;
}
/* Wait for a reusable slot outside the writer gate: the worker can drain/end
 * old lifetimes and process Clear while a new thread is waiting for capacity. */
static Ring *thread_ring(void) {
    Ring *r=TlsGetValue(state.tls);
    if(r==(Ring *)(uintptr_t)1){lost();return NULL;}
    if(r)return r;
    LONG tid=(LONG)GetCurrentThreadId();
    ULONGLONG deadline=GetTickCount64()+2000;
    while(load32(&state.running)){
        LONG count=load32(&state.next_ring);if(count>AP_THREADS)count=AP_THREADS;
        BOOL pending=FALSE;
        for(LONG i=0;i<count;i++){
            Ring *old=&state.rings[i];
            if(load32(&old->tid)==tid && (!load32(&old->ended)||load64(&old->tail)!=load64(&old->head))){pending=TRUE;break;}
        }
        if(!pending){
            for(LONG i=0;i<count;i++){
                Ring *old=&state.rings[i];
                if(load32(&old->ended) && load64(&old->tail)==load64(&old->head) && InterlockedCompareExchange(&old->ended,0,1)==1){r=old;break;}
            }
            if(!r && count<AP_THREADS && InterlockedCompareExchange(&state.next_ring,count+1,count)==count)r=&state.rings[count];
            if(r){
                InterlockedExchange(&r->started,0);InterlockedExchange64(&r->birth,0);InterlockedExchange(&r->tid,tid);
                if(!TlsSetValue(state.tls,r)){InterlockedExchange(&r->ended,1);lost();return NULL;}
                return r;
            }
        }
        if(GetTickCount64()>=deadline){TlsSetValue(state.tls,(void *)(uintptr_t)1);lost();return NULL;}
        SwitchToThread();
    }
    return NULL;
}
static void record(uint32_t type,uint32_t procedure,uint32_t depth){
    DWORD error=GetLastError();
    if(load32(&state.running)){
        BOOL selected=!procedure;
        for(uint32_t i=0;i<state.count && !selected;i++)selected=state.procedures[i].id==procedure;
        if(!selected)lost();
        else{
            Ring *r=thread_ring();
            if(r && writer_gate(TRUE)){
                if(load32(&state.running)){
                    if(!load32(&r->started)){uint64_t birth=enqueue(r,AP_START,0,0);InterlockedExchange64(&r->birth,(LONG64)birth);InterlockedExchange(&r->started,1);}
                    enqueue(r,type,procedure,depth);
                }
                InterlockedDecrement(&state.writers);
            }
        }
    }
    SetLastError(error);
}
static EXCEPTION_DISPOSITION WINAPI function_unwind(PEXCEPTION_RECORD exception,PVOID frame,PCONTEXT context,PDISPATCHER_CONTEXT dispatcher) {
    (void)frame; (void)context;
    if((exception->ExceptionFlags&EXCEPTION_UNWINDING) && !(exception->ExceptionFlags&EXCEPTION_TARGET_UNWIND) && dispatcher && dispatcher->HandlerData) {
        uint32_t id=*(const uint32_t *)dispatcher->HandlerData;
        record(AP_UNWIND_EXIT,id,0);
    }
    return ExceptionContinueSearch;
}
AP_API void APNativeEnter(uint32_t procedure) { if (procedure) record(AP_ENTER, procedure, 0); else { DWORD e=GetLastError(); if(load32(&state.running)) lost(); SetLastError(e); } }
AP_API void APNativeExit(uint32_t procedure) { if (procedure) record(AP_EXIT, procedure, 0); else { DWORD e=GetLastError(); if(load32(&state.running)) lost(); SetLastError(e); } }
AP_API void APNativeUnwind(uint32_t depth) { record(AP_UNWIND, 0, depth); }
AP_API void APNativeThreadEnd(void) {
    DWORD error = GetLastError();
    if (!writer_gate(TRUE)){SetLastError(error);return;}
    if (load32(&state.running)) {
        Ring *r = TlsGetValue(state.tls);
        if(r && r!=(Ring *)(uintptr_t)1 && load32(&r->started) && !load32(&r->ended)){enqueue(r,AP_END,0,0);InterlockedExchange(&r->ended,1);}
        /* A cooperating host must call this only at actual thread end. The TLS
           sentinel rejects callbacks after explicit retirement of that life. */
        TlsSetValue(state.tls, (void *)(uintptr_t)1);
    }
    InterlockedDecrement(&state.writers);
    SetLastError(error);
}
static BOOL init_producer(const wchar_t *name, const APProcedure *procedures, uint32_t count, uint32_t capacity, BOOL hook_mode) {
    if (state.rings) { SetLastError(ERROR_ALREADY_INITIALIZED); return FALSE; }
    if (!name || wcsncmp(name, L"\\\\.\\pipe\\", 9) || wcschr(name+9, L'\\') || !name[9] || !procedures || !count || count>AP_SELECTION || !capacity || capacity>AP_CAPACITY) { SetLastError(ERROR_INVALID_PARAMETER); return FALSE; }
    LARGE_INTEGER frequency, start;
    if (!QueryPerformanceFrequency(&frequency) || frequency.QuadPart<=0 || !QueryPerformanceCounter(&start) || start.QuadPart<=0) return FALSE;
    unsigned char *image = (unsigned char *)GetModuleHandleW(NULL);
    IMAGE_DOS_HEADER *dos = (IMAGE_DOS_HEADER *)image;
    IMAGE_NT_HEADERS64 *nt = (IMAGE_NT_HEADERS64 *)(image+dos->e_lfanew);
    for (uint32_t i=0; i<count; i++) {
        if (!procedures[i].id || !procedures[i].rva || procedures[i].rva>=nt->OptionalHeader.SizeOfImage) { SetLastError(ERROR_INVALID_ADDRESS); return FALSE; }
        for (uint32_t j=0; j<i; j++) if(procedures[i].id==procedures[j].id || procedures[i].rva==procedures[j].rva) { SetLastError(ERROR_DUP_NAME); return FALSE; }
    }
    state.pipe = open_pipe(name);
    if (state.pipe == INVALID_HANDLE_VALUE) return FALSE;
    state.tls = TlsAlloc();
    /* Expanded TLS can allocate on a thread's first TlsSetValue. Require the
       primary TEB slots so event callbacks do not trigger that allocation. */
    if (state.tls == TLS_OUT_OF_INDEXES || state.tls >= TLS_MINIMUM_AVAILABLE) {
        if (state.tls != TLS_OUT_OF_INDEXES) TlsFree(state.tls);
        CloseHandle(state.pipe); SetLastError(ERROR_NO_SYSTEM_RESOURCES); return FALSE;
    }
    state.rings = (Ring *)VirtualAlloc(NULL, sizeof(Ring)*AP_THREADS, MEM_RESERVE|MEM_COMMIT, PAGE_READWRITE);
    if (!state.rings) { TlsFree(state.tls); CloseHandle(state.pipe); return FALSE; }
    state.count=count; state.capacity=capacity; state.frequency=(uint64_t)frequency.QuadPart; state.start=(uint64_t)start.QuadPart; state.hook_mode=hook_mode;
    state.image_size=nt->OptionalHeader.SizeOfImage; memcpy(state.procedures, procedures, count*sizeof(APProcedure));
    state.ready=CreateEventW(NULL, TRUE, FALSE, NULL);
    if (!state.ready) { VirtualFree(state.rings, 0, MEM_RELEASE); state.rings=NULL; TlsFree(state.tls); CloseHandle(state.pipe); return FALSE; }
    state.wake=CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!state.wake) { CloseHandle(state.ready); VirtualFree(state.rings, 0, MEM_RELEASE); state.rings=NULL; TlsFree(state.tls); CloseHandle(state.pipe); return FALSE; }
    state.worker=CreateThread(NULL, 0, drain, NULL, 0, NULL);
    if (!state.worker) { CloseHandle(state.wake); CloseHandle(state.ready); VirtualFree(state.rings, 0, MEM_RELEASE); state.rings=NULL; TlsFree(state.tls); CloseHandle(state.pipe); return FALSE; }
    HANDLE startup[2]={state.ready,state.worker};
    if (WaitForMultipleObjects(2, startup, FALSE, 5000)!=WAIT_OBJECT_0) { APNativeShutdown(5000); return FALSE; }
    return TRUE;
}
AP_API BOOL APNativeInit(const wchar_t *name,const APProcedure *procedures,uint32_t count,uint32_t capacity) {
    return init_producer(name,procedures,count,capacity,FALSE);
}
static BOOL init_selected_hooks(const wchar_t *name,const APHookProcedure *procedures,uint32_t count,uint32_t capacity,BOOL mode) {
    if(state.rings || !hook_select(procedures,count)) return FALSE;
    APProcedure selected[AP_HOOK_LIMIT];
    for(uint32_t i=0;i<count;i++) selected[i]=(APProcedure){procedures[i].id,procedures[i].rva};
    BOOL ok=init_producer(name,selected,count,capacity,mode);
    if(!ok && !state.rings) hook_retire();
    return ok;
}
AP_API BOOL APNativeInitHooks(const wchar_t *name,const APHookProcedure *procedures,uint32_t count,uint32_t capacity){return init_selected_hooks(name,procedures,count,capacity,1);}
AP_API BOOL APNativeInitFunctions(const wchar_t *name,const APHookProcedure *procedures,uint32_t count,uint32_t capacity){return init_selected_hooks(name,procedures,count,capacity,2);}
AP_API BOOL APNativeShutdown(DWORD timeout_ms) {
    if (!state.rings || !state.worker) return FALSE;
    InterlockedExchange(&state.running, 0); InterlockedExchange(&state.stopping, 1);
    SetEvent(state.wake);
    DWORD wait = WaitForSingleObject(state.worker, timeout_ms);
    if (wait != WAIT_OBJECT_0) { CancelSynchronousIo(state.worker); return FALSE; }
    DWORD result = 1;
    GetExitCodeThread(state.worker, &result);
    if(!hook_retire()) return FALSE;
    CloseHandle(state.worker); CloseHandle(state.wake); CloseHandle(state.ready); CloseHandle(state.pipe); TlsFree(state.tls);
    VirtualFree(state.rings, 0, MEM_RELEASE);
    BOOL ok = !result && !load32(&state.io_error);
    ZeroMemory(&state, sizeof(state));
    return ok;
}

#include "bootstrap_windows.h"
