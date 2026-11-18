/* Private startup-only installer. The cooperating host guarantees that no
 * selected caller runs before Init returns or during Shutdown. Never DllMain. */
#ifndef AP_HOOKS_WINDOWS_H
#define AP_HOOKS_WINDOWS_H
#include <string.h>

enum { AP_HOOK_LIMIT=64, AP_HOOK_STRIDE=8192, AP_HOOK_PREFIX=64 };
static struct {
    APHookProcedure selected[AP_HOOK_LIMIT];
    uint32_t count, patched, bytes;
    unsigned char *memory;
    RUNTIME_FUNCTION *table;
    BOOL registered;
    unsigned char originals[AP_HOOK_LIMIT][AP_HOOK_PREFIX];
    uint32_t lengths[AP_HOOK_LIMIT];
    DWORD protections[AP_HOOK_LIMIT];
} hooks;
static uint32_t hook_u32(const unsigned char *p) { uint32_t n=0; for(unsigned i=0;i<4;i++) n|=(uint32_t)p[i]<<(i*8); return n; }
static uint64_t hook_u64(const unsigned char *p) { uint64_t n=0; for(unsigned i=0;i<8;i++) n|=(uint64_t)p[i]<<(i*8); return n; }
static void hook_put32(unsigned char *p,uint32_t n) { for(unsigned i=0;i<4;i++) p[i]=(unsigned char)(n>>(i*8)); }
static void hook_put64(unsigned char *p,uint64_t n) { for(unsigned i=0;i<8;i++) p[i]=(unsigned char)(n>>(i*8)); }
static BOOL hook_io(HANDLE pipe,void *data,DWORD size,BOOL writing) {
    unsigned char *p=data;
    while(size) {
        DWORD n=0;
        BOOL ok=writing?WriteFile(pipe,p,size,&n,NULL):ReadFile(pipe,p,size,&n,NULL);
        if(!ok || !n) return FALSE;
        p+=n; size-=n;
    }
    return TRUE;
}
static BOOL hook_patch(unsigned char *source,const unsigned char *bytes,uint32_t size,DWORD protection) {
    DWORD old=0,ignored=0;
    if(!VirtualProtect(source,size,PAGE_READWRITE,&old)) return FALSE;
    memcpy(source,bytes,size);
    BOOL flushed=FlushInstructionCache(GetCurrentProcess(),source,size);
    BOOL protected=VirtualProtect(source,size,protection,&ignored);
    return flushed && protected;
}
static BOOL hook_retire(void) {
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    while(hooks.patched) {
        uint32_t i=hooks.patched-1;
        if(!hook_patch(image+hooks.selected[i].rva,hooks.originals[i],hooks.lengths[i],hooks.protections[i])) return FALSE;
        hooks.patched--;
    }
    if(hooks.registered && !RtlDeleteFunctionTable(hooks.table)) return FALSE;
    hooks.registered=FALSE;
    if(hooks.memory && !VirtualFree(hooks.memory,0,MEM_RELEASE)) return FALSE;
    ZeroMemory(&hooks,sizeof(hooks));
    return TRUE;
}
static BOOL hook_select(const APHookProcedure *procedures,uint32_t count) {
    if(hooks.memory || !procedures || !count || count>AP_HOOK_LIMIT) return FALSE;
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    IMAGE_NT_HEADERS64 *nt=(IMAGE_NT_HEADERS64 *)(image+((IMAGE_DOS_HEADER *)image)->e_lfanew);
    for(uint32_t i=0;i<count;i++) {
        APHookProcedure p=procedures[i];
        if(!p.id || !p.rva || p.size<5 || p.size>4096 || (uint64_t)p.rva+p.size>nt->OptionalHeader.SizeOfImage) return FALSE;
        MEMORY_BASIC_INFORMATION m;
        if(!VirtualQuery(image+p.rva,&m,sizeof(m)) || m.AllocationBase!=image || m.State!=MEM_COMMIT || (m.Protect&0xff)!=PAGE_EXECUTE_READ || (m.Protect&PAGE_GUARD) || (uintptr_t)(image+p.rva)+p.size>(uintptr_t)m.BaseAddress+m.RegionSize) return FALSE;
        hooks.protections[i]=m.Protect;
        for(uint32_t j=0;j<i;j++) {
            APHookProcedure old=procedures[j];
            if(p.id==old.id || ((uint64_t)p.rva<(uint64_t)old.rva+old.size && (uint64_t)old.rva<(uint64_t)p.rva+p.size)) return FALSE;
        }
    }
    memcpy(hooks.selected,procedures,count*sizeof(APHookProcedure));
    hooks.count=count;
    return TRUE;
}
static BOOL hook_allocate(void) {
    SYSTEM_INFO system;
    GetSystemInfo(&system);
    uintptr_t image=(uintptr_t)GetModuleHandleW(NULL);
    IMAGE_NT_HEADERS64 *nt=(IMAGE_NT_HEADERS64 *)(image+((IMAGE_DOS_HEADER *)image)->e_lfanew);
    uintptr_t granularity=system.dwAllocationGranularity;
    uintptr_t cursor=(image+nt->OptionalHeader.SizeOfImage+granularity-1)&~(granularity-1);
    uintptr_t high=image+0x70000000;
    if(high>(uintptr_t)system.lpMaximumApplicationAddress) high=(uintptr_t)system.lpMaximumApplicationAddress;
    /* Keep every code stride complete. A large selection must not consume the
       last procedure's space with the three-entry-per-function runtime table. */
    hooks.bytes=(AP_HOOK_STRIDE+3*sizeof(RUNTIME_FUNCTION))*hooks.count;
    for(unsigned attempt=0;cursor<high && attempt<4096;attempt++) {
        MEMORY_BASIC_INFORMATION m;
        if(!VirtualQuery((void *)cursor,&m,sizeof(m))) return FALSE;
        uintptr_t end=(uintptr_t)m.BaseAddress+m.RegionSize;
        if(end<=cursor) return FALSE;
        if(m.State==MEM_FREE && cursor+hooks.bytes<=end && cursor+hooks.bytes<=high) {
            hooks.memory=VirtualAlloc((void *)cursor,hooks.bytes,MEM_RESERVE|MEM_COMMIT,PAGE_READWRITE);
            if(hooks.memory) return TRUE;
            cursor+=granularity;
        } else cursor=(end+granularity-1)&~(granularity-1);
    }
    return FALSE;
}
static EXCEPTION_DISPOSITION WINAPI function_unwind(PEXCEPTION_RECORD,PVOID,PCONTEXT,PDISPATCHER_CONTEXT);
static void function_enter(uint32_t,uintptr_t);
static void function_exit(uint32_t,uintptr_t);
static BOOL hook_forward_info(const unsigned char *data,uint32_t available,RUNTIME_FUNCTION *body,uint32_t index,const unsigned char *copied_info,uint32_t info_bytes) {
    if(available<8)return FALSE;
    uint32_t kind=hook_u32(data+4);
    if(!kind)return TRUE;
    if(kind!=1 || available<36)return FALSE;
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    IMAGE_NT_HEADERS64 *nt=(IMAGE_NT_HEADERS64 *)(image+((IMAGE_DOS_HEADER *)image)->e_lfanew);
    uint32_t image_size=nt->OptionalHeader.SizeOfImage;
    uint32_t rva=hook_u32(data+8),size=hook_u32(data+12),entry_rva=hook_u32(data+16),info_rva=hook_u32(data+20),handler_rva=hook_u32(data+24),handler_data=hook_u32(data+28),count=hook_u32(data+32);
    if(rva!=hooks.selected[index].rva || size!=hooks.selected[index].size || (uint64_t)entry_rva+12>image_size || (uint64_t)info_rva+info_bytes+4>image_size || info_rva%4 || !handler_rva || handler_rva>=image_size || handler_data!=(uint64_t)info_rva+info_bytes+4 || !count || count>4097 || (uint64_t)36+count*4>available)return FALSE;
    RUNTIME_FUNCTION *original=(RUNTIME_FUNCTION *)(image+entry_rva);
    const unsigned char *info=image+info_rva;
    if(original->BeginAddress!=rva || original->EndAddress!=(uint64_t)rva+size || original->UnwindData!=info_rva || info[0]!=17 || memcmp(info+1,copied_info+1,info_bytes-1) || hook_u32(info+info_bytes)!=handler_rva)return FALSE;
    MEMORY_BASIC_INFORMATION memory;
    if(!VirtualQuery(image+handler_rva,&memory,sizeof(memory)) || memory.AllocationBase!=image || memory.State!=MEM_COMMIT || (memory.Protect&0xff)!=PAGE_EXECUTE_READ || (memory.Protect&PAGE_GUARD))return FALSE;
    uint32_t previous_source=0,previous_clone=0,delta=0,body_size=body->EndAddress-body->BeginAddress;
    if(body_size<size)return FALSE;
    for(uint32_t i=0;i<count;i++) {
        uint32_t source=data[36+i*4]|((uint32_t)data[37+i*4]<<8),clone=data[38+i*4]|((uint32_t)data[39+i*4]<<8);
        if((!i && (source || clone)) || (i && (source<=previous_source || clone<=previous_clone)) || source>size || clone>body_size || clone<source || clone-source<delta)return FALSE;
        previous_source=source;previous_clone=clone;delta=clone-source;
    }
    return delta==body_size-size;
}
static BOOL hook_exchange(HANDLE pipe,BOOL general) {
    if(!hook_allocate()) return FALSE;
    unsigned char offer[56]={'A','P','H','O','O','K','1',0};
    hook_put64(offer+8,(uintptr_t)GetModuleHandleW(NULL));
    hook_put64(offer+16,(uintptr_t)hooks.memory);
    hook_put32(offer+24,hooks.bytes); hook_put32(offer+28,hooks.count);
    hook_put64(offer+32,(uintptr_t)APNativeEnter); hook_put64(offer+40,(uintptr_t)APNativeExit);
    if(general){offer[6]='4';hook_put64(offer+32,(uintptr_t)function_enter);hook_put64(offer+40,(uintptr_t)function_exit);hook_put64(offer+48,(uintptr_t)function_unwind);}
    if(!hook_io(pipe,offer,general?56:48,TRUE)) return FALSE;
    for(uint32_t i=0;i<hooks.count;i++) {
        unsigned char extent[4]; hook_put32(extent,hooks.selected[i].size);
        if(!hook_io(pipe,extent,4,TRUE)) return FALSE;
    }
    unsigned char header[24];
    if(!hook_io(pipe,header,sizeof(header),FALSE) || memcmp(header,"APPLAN1",8)) return FALSE;
    uint32_t per=general?3:2;
    uint32_t count=hook_u32(header+8),size=hook_u32(header+12),table=hook_u32(header+16),entries=hook_u32(header+20);
    if(count!=hooks.count || size>hooks.bytes || size<16 || entries!=per*count || table%4 || (uint64_t)table+entries*12>size) return FALSE;
    if(!hook_io(pipe,hooks.memory,size,FALSE)) return FALSE;
    hooks.table=(RUNTIME_FUNCTION *)(hooks.memory+table);
    uint32_t previous=0;
    for(uint32_t i=0;i<entries;i++) {
        RUNTIME_FUNCTION *f=&hooks.table[i];
        if(f->BeginAddress<previous || f->EndAddress<=f->BeginAddress || f->EndAddress>table || f->UnwindData%4 || (uint64_t)f->UnwindData+8>table) return FALSE;
        unsigned char *info=hooks.memory+f->UnwindData;
        if(general && i%per==1) {
            uint32_t extra=4+((info[2]+1)&~1)*2;
            if(info[0]!=17 || (uint64_t)f->UnwindData+extra+12>table) return FALSE;
            uint32_t handler=hook_u32(info+extra);
            if((uint64_t)handler+14>table || memcmp(hooks.memory+handler,"\xff\x25\0\0\0\0",6) || hook_u64(hooks.memory+handler+6)!=(uintptr_t)function_unwind || hook_u32(info+extra+4)!=hooks.selected[i/per].id) return FALSE;
            uint32_t data_at=f->UnwindData+extra+4;
            if(!hook_forward_info(hooks.memory+data_at,table-data_at,f,i/per,info,extra))return FALSE;
        } else if(info[0]!=1 || info[1]!=7 || info[2]!=2 || info[3]!=0 || info[4]!=7 || info[5]!=1 || info[6]!=25 || info[7]!=0) return FALSE;
        previous=f->EndAddress;
    }
    unsigned char patches[AP_HOOK_LIMIT][AP_HOOK_PREFIX];
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    for(uint32_t i=0;i<count;i++) {
        unsigned char item[8],expected[4096];
        if(!hook_io(pipe,item,8,FALSE)) return FALSE;
        uint32_t length=hook_u32(item),prefix=hook_u32(item+4);
        if(length!=hooks.selected[i].size || prefix<5 || prefix>AP_HOOK_PREFIX || prefix>length) return FALSE;
        if(!hook_io(pipe,expected,length,FALSE) || !hook_io(pipe,patches[i],prefix,FALSE)) return FALSE;
        if(memcmp(expected,image+hooks.selected[i].rva,length)) return FALSE;
        uintptr_t target=(uintptr_t)hooks.memory+hooks.table[i*per].BeginAddress;
        uint32_t jump_size=14;
        if(patches[i][0]==0xe9){
            jump_size=5;
            uintptr_t destination=(uintptr_t)(image+hooks.selected[i].rva+5)+(intptr_t)(int32_t)hook_u32(patches[i]+1);
            if(destination!=target)return FALSE;
        }else if(prefix<14 || memcmp(patches[i],"\xff\x25\0\0\0\0",6) || hook_u64(patches[i]+6)!=target)return FALSE;
        for(uint32_t j=jump_size;j<prefix;j++) if(patches[i][j]!=0x90) return FALSE;
        memcpy(hooks.originals[i],expected,prefix);
        hooks.lengths[i]=prefix;
    }
    DWORD old=0;
    if(!VirtualProtect(hooks.memory,hooks.bytes,PAGE_EXECUTE_READ,&old) || !FlushInstructionCache(GetCurrentProcess(),hooks.memory,size)) return FALSE;
    if(!RtlAddFunctionTable(hooks.table,entries,(DWORD64)(uintptr_t)hooks.memory)) return FALSE;
    hooks.registered=TRUE;
    for(uint32_t i=0;i<count;i++) {
        /* Count the attempted patch too: even a protection/flush error after
           memcpy requires restoring its bytes before retiring the allocation. */
        hooks.patched=i+1;
        if(!hook_patch(image+hooks.selected[i].rva,patches[i],hooks.lengths[i],hooks.protections[i])) return FALSE;
    }
    unsigned char installed=1;
    return hook_io(pipe,&installed,1,TRUE);
}
#endif
