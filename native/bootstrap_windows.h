/* Remote startup adapter for an exclusively owned process. Exported entry uses
 * the Windows thread ABI. Installed leaf code is never freed at process exit:
 * other application threads may still be executing it until ExitProcess kills
 * them. The ordinary cooperative Shutdown path still restores and frees it. */
#ifndef AP_BOOTSTRAP_WINDOWS_H
#define AP_BOOTSTRAP_WINDOWS_H
static VOID (WINAPI *bootstrap_exit)(UINT);
static BOOL bootstrap_mode;
static VOID WINAPI bootstrap_exit_process(UINT code) {
    InterlockedExchange(&state.running,0);
    ULONGLONG deadline=GetTickCount64()+2000;
    while(load32(&state.writers) && GetTickCount64()<deadline) Sleep(1);
    if(!load32(&state.writers)) {
        LONG count=load32(&state.next_ring);
        if(count>AP_THREADS) count=AP_THREADS;
        for(LONG i=0;i<count;i++) {
            Ring *r=&state.rings[i];
            if(load32(&r->tid) && load64(&r->head)) enqueue(r,AP_END,0,0);
        }
        InterlockedExchange(&state.stopping,1);
        WaitForSingleObject(state.worker,2000);
    }
    /* Closing the stream lets Go reject an unfinished final frame on timeout.
       Rings, native code and unwind tables remain valid until OS teardown. */
    CancelSynchronousIo(state.worker);
    CloseHandle(state.pipe);
    bootstrap_exit(code);
}
static BOOL bootstrap_hook_exit(void) {
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    IMAGE_NT_HEADERS64 *nt=(IMAGE_NT_HEADERS64 *)(image+((IMAGE_DOS_HEADER *)image)->e_lfanew);
    uint32_t size=nt->OptionalHeader.SizeOfImage;
    IMAGE_DATA_DIRECTORY directory=nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT];
    if(!directory.VirtualAddress || (uint64_t)directory.VirtualAddress+directory.Size>size) return FALSE;
    union { FARPROC raw; VOID (WINAPI *exit)(UINT); } resolved;
    resolved.raw=GetProcAddress(GetModuleHandleW(L"kernel32.dll"),"ExitProcess");
    bootstrap_exit=resolved.exit;
    if(!bootstrap_exit) return FALSE;
    for(uint32_t at=directory.VirtualAddress;at+sizeof(IMAGE_IMPORT_DESCRIPTOR)<=directory.VirtualAddress+directory.Size;at+=sizeof(IMAGE_IMPORT_DESCRIPTOR)) {
        IMAGE_IMPORT_DESCRIPTOR *d=(IMAGE_IMPORT_DESCRIPTOR *)(image+at);
        if(!d->Name) break;
        if(d->Name>=size || !memchr(image+d->Name,0,size-d->Name)) return FALSE;
        if(_stricmp((const char *)image+d->Name,"kernel32.dll")) continue;
        for(uint32_t i=0;i<4096;i++) {
            uint64_t rva=(uint64_t)d->FirstThunk+i*8;
            if(!d->FirstThunk || rva+8>size) return FALSE;
            ULONG_PTR *slot=(ULONG_PTR *)(image+rva);
            if(!*slot) break;
            BOOL match=*slot==(ULONG_PTR)bootstrap_exit;
            if(d->OriginalFirstThunk) {
                uint64_t original=(uint64_t)d->OriginalFirstThunk+i*8;
                if(original+8>size) return FALSE;
                uint64_t name=*(uint64_t *)(image+original);
                if(!(name&IMAGE_ORDINAL_FLAG64) && name+2<size && memchr(image+name+2,0,size-name-2)) match=!strcmp((const char *)image+name+2,"ExitProcess");
            }
            if(match) {
                DWORD old=0,ignored=0;
                if(!VirtualProtect(slot,8,PAGE_READWRITE,&old)) return FALSE;
                InterlockedExchangePointer((PVOID volatile *)slot,(PVOID)bootstrap_exit_process);
                return VirtualProtect(slot,8,old,&ignored);
            }
        }
    }
    return FALSE;
}
AP_API DWORD WINAPI APNativeBootstrap(void *request) {
    if(!request || state.rings || bootstrap_mode) return ERROR_ALREADY_INITIALIZED;
    unsigned char *data=request;
    if(memcmp(data,"APBOOT1",8)) return ERROR_INVALID_DATA;
    uint32_t size=hook_u32(data+8),count=hook_u32(data+12),capacity=hook_u32(data+16),chars=hook_u32(data+20);
    if(!count || count>AP_HOOK_LIMIT || !chars || chars>512 || size!=32+count*12+chars*2 || size>4096 || hook_u64(data+24)) return ERROR_INVALID_DATA;
    APHookProcedure selected[AP_HOOK_LIMIT];
    for(uint32_t i=0;i<count;i++) selected[i]=(APHookProcedure){hook_u32(data+32+i*12),hook_u32(data+36+i*12),hook_u32(data+40+i*12)};
    wchar_t *pipe=(wchar_t *)(data+32+count*12);
    if(pipe[chars-1] || wcslen(pipe)!=chars-1) return ERROR_INVALID_DATA;
    bootstrap_mode=TRUE;
    if(!APNativeInitHooks(pipe,selected,count,capacity)) return ERROR_INVALID_DATA;
    if(!bootstrap_hook_exit()) return ERROR_NOT_SUPPORTED;
    return ERROR_SUCCESS;
}
#endif
