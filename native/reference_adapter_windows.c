/* Test-only adapter: expose the actual production language bridge without
 * initializing a collector or publishing these entry points in the package. */
#include "producer_windows.c"
AP_API uintptr_t APReferenceUnwindAddress(void){return (uintptr_t)function_unwind;}

AP_API BOOL APReferenceValidatePlan(uintptr_t image,uintptr_t memory,uint32_t bytes,const RUNTIME_FUNCTION *functions,uint32_t source_rva,uint32_t source_size){
    if(hooks.memory || !image || !memory || !functions || bytes<16 || bytes>8192)return FALSE;
    if(!((source_rva==0xa930 && source_size==155)||(source_rva==0x191700 && source_size==673)))return FALSE;
    RUNTIME_FUNCTION body=functions[1];
    if(body.BeginAddress!=512 || body.EndAddress>bytes || body.EndAddress<=body.BeginAddress || body.UnwindData%4 || (uint64_t)body.UnwindData+16>bytes)return FALSE;
    unsigned char *info=(unsigned char *)memory+body.UnwindData;
    uint32_t info_bytes=4+((info[2]+1)&~1)*2;
    if(info[0]!=25 || (uint64_t)body.UnwindData+info_bytes+12>bytes)return FALSE;
    uint32_t thunk=hook_u32(info+info_bytes);
    if((uint64_t)thunk+14>bytes || memcmp((void *)(memory+thunk),"\xff\x25\0\0\0\0",6) || hook_u64((void *)(memory+thunk+6))!=(uintptr_t)function_unwind)return FALSE;
    hooks.selected[0]=(APHookProcedure){17,source_rva,source_size};hooks.count=1;
    hooks.memory=(unsigned char *)memory;hooks.bytes=16384;
    uint32_t at=body.UnwindData+info_bytes+4;
    BOOL valid=hook_forward_info_at((unsigned char *)image,(void *)(memory+at),bytes-at,&body,0,info,info_bytes,NULL,0);
    ZeroMemory(&hooks,sizeof(hooks));
    return valid;
}

/* Synchronous, single-thread test capture. It uses the production callbacks,
 * physical frame tracking, bounded rings and batch writer, without a worker or
 * installer. The public launcher continues to own only its selected EXE. */
AP_API uintptr_t APReferenceEnterAddress(void){return (uintptr_t)function_enter;}
AP_API uintptr_t APReferenceExitAddress(void){return (uintptr_t)function_exit;}
static uint64_t reference_sequence;
static uint32_t reference_entries,reference_returns,reference_unwinds;
AP_API BOOL APReferenceStartCapture(uintptr_t image,uint32_t source_rva,const wchar_t *path){
    if(state.rings || !image)return FALSE;
    LARGE_INTEGER frequency,start;
    if(!QueryPerformanceFrequency(&frequency)||!QueryPerformanceCounter(&start)||frequency.QuadPart<=0||start.QuadPart<=0)return FALSE;
    DWORD tls=TlsAlloc();
    if(tls==TLS_OUT_OF_INDEXES)return FALSE;
    if(tls>=TLS_MINIMUM_AVAILABLE){TlsFree(tls);return FALSE;}
    Ring *rings=VirtualAlloc(NULL,sizeof(Ring)*AP_THREADS,MEM_RESERVE|MEM_COMMIT,PAGE_READWRITE);
    if(!rings){TlsFree(tls);return FALSE;}
    const IMAGE_NT_HEADERS64 *nt=(const IMAGE_NT_HEADERS64 *)(image+((const IMAGE_DOS_HEADER *)image)->e_lfanew);
    state.rings=rings;state.tls=tls;state.capacity=AP_CAPACITY;state.hook_mode=2;
    APProcedure procedure={17,source_rva};
    set_procedures(&procedure,1);state.image_size=nt->OptionalHeader.SizeOfImage;
    state.frequency=(uint64_t)frequency.QuadPart;state.start=(uint64_t)start.QuadPart;
    state.pipe=CreateFileW(path,GENERIC_WRITE,0,NULL,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,NULL);
    if(state.pipe==INVALID_HANDLE_VALUE){TlsFree(tls);VirtualFree(rings,0,MEM_RELEASE);ZeroMemory(&state,sizeof(state));return FALSE;}
    unsigned char hello[48]={'A','P','N','A','T','0','1',0},catalog[8];
    put32(hello+8,4);put32(hello+12,24);put64(hello+16,state.frequency);
    put32(hello+24,GetCurrentProcessId());put32(hello+28,1);put32(hello+32,state.image_size);put64(hello+40,state.start);
    put32(catalog,17);put32(catalog+4,source_rva);
    if(!write_all(hello,sizeof(hello)) || !write_all(catalog,sizeof(catalog))){CloseHandle(state.pipe);TlsFree(tls);VirtualFree(rings,0,MEM_RELEASE);ZeroMemory(&state,sizeof(state));return FALSE;}
    reference_sequence=0;reference_entries=0;reference_returns=0;reference_unwinds=0;
    InterlockedExchange(&state.running,1);return TRUE;
}
/* The child owns this drain and calls it between complete pairs. Preserve the
 * production ring bound even when the test emits more than one ring of events. */
AP_API BOOL APReferenceFlushCapture(void){
    if(!state.rings || load32(&state.next_ring)!=1 || state.rings[0].frame_count || load64(&state.lost) || load32(&state.writers))return FALSE;
    Ring *ring=&state.rings[0];uint64_t tail=load64(&ring->tail),head=load64(&ring->head);
    if(head<tail || head-tail>state.capacity)return FALSE;
    for(uint64_t i=tail;i<head;i++){
        Event *event=&ring->events[i%state.capacity];
        if(event->type==AP_ENTER)reference_entries++;
        if(event->type==AP_EXIT)reference_returns++;
        if(event->type==AP_UNWIND_EXIT)reference_unwinds++;
    }
    int result;do{result=drain_rings(&reference_sequence);}while(result>0);
    return result==0;
}
AP_API BOOL APReferenceFinishCapture(uint32_t *entries,uint32_t *returns,uint32_t *unwinds){
    if(!state.rings || !load32(&state.running) || load32(&state.next_ring)!=1 || state.rings[0].frame_count || load64(&state.lost) || load32(&state.writers))return FALSE;
    APNativeThreadEnd();InterlockedExchange(&state.running,0);
    BOOL ok=APReferenceFlushCapture();
    *entries=reference_entries;*returns=reference_returns;*unwinds=reference_unwinds;
    if(ok)ok=batch(NULL,0,++reference_sequence,ticks());
    if(!CloseHandle(state.pipe))ok=FALSE;
    if(!TlsFree(state.tls))ok=FALSE;
    if(!VirtualFree(state.rings,0,MEM_RELEASE))ok=FALSE;
    ZeroMemory(&state,sizeof(state));return ok;
}
