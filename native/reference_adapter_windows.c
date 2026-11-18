/* Test-only adapter: expose the actual production language bridge without
 * initializing a collector or publishing these entry points in the package. */
#include "producer_windows.c"
AP_API uintptr_t APReferenceUnwindAddress(void){return (uintptr_t)function_unwind;}

AP_API BOOL APReferenceValidatePlan(uintptr_t image,uintptr_t memory,uint32_t bytes,const RUNTIME_FUNCTION *functions){
    if(hooks.memory || !image || !memory || !functions || bytes<16 || bytes>8192)return FALSE;
    RUNTIME_FUNCTION body=functions[1];
    if(body.BeginAddress!=512 || body.EndAddress>bytes || body.EndAddress<=body.BeginAddress || body.UnwindData%4 || (uint64_t)body.UnwindData+16>bytes)return FALSE;
    unsigned char *info=(unsigned char *)memory+body.UnwindData;
    uint32_t info_bytes=4+((info[2]+1)&~1)*2;
    if(info[0]!=25 || (uint64_t)body.UnwindData+info_bytes+12>bytes)return FALSE;
    uint32_t thunk=hook_u32(info+info_bytes);
    if((uint64_t)thunk+14>bytes || memcmp((void *)(memory+thunk),"\xff\x25\0\0\0\0",6) || hook_u64((void *)(memory+thunk+6))!=(uintptr_t)function_unwind)return FALSE;
    hooks.selected[0]=(APHookProcedure){17,0xa930,155};hooks.count=1;
    hooks.memory=(unsigned char *)memory;hooks.bytes=16384;
    uint32_t at=body.UnwindData+info_bytes+4;
    BOOL valid=hook_forward_info_at((unsigned char *)image,(void *)(memory+at),bytes-at,&body,0,info,info_bytes);
    ZeroMemory(&hooks,sizeof(hooks));
    return valid;
}
