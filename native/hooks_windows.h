/* Private startup-only installer. The cooperating host guarantees that no
 * selected caller runs before Init returns or during Shutdown. Never DllMain. */
#ifndef AP_HOOKS_WINDOWS_H
#define AP_HOOKS_WINDOWS_H
#include <string.h>

enum { AP_HOOK_LIMIT=256, AP_HOOK_STRIDE=8192, AP_HOOK_PREFIX=64, AP_HOOK_LARGE_STRIDE=32768, AP_HOOK_SOURCE_LIMIT=8192 };
static struct {
    APHookProcedure selected[AP_HOOK_LIMIT];
    uint32_t count, patched, bytes, stride;
    unsigned char *memory;
    unsigned char *image;
    RUNTIME_FUNCTION *table;
    BOOL registered;
    void *breakpoint_handler;
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
/* Only the owned startup protocol installs breakpoint entries. Leave all
 * other first-chance exceptions for the application's own handlers. */
static LONG CALLBACK hook_breakpoint(EXCEPTION_POINTERS *exception) {
    const EXCEPTION_RECORD *record=exception->ExceptionRecord;
    if(record->ExceptionCode!=EXCEPTION_BREAKPOINT || (record->ExceptionFlags&EXCEPTION_NONCONTINUABLE))
        return EXCEPTION_CONTINUE_SEARCH;
    uintptr_t address=(uintptr_t)record->ExceptionAddress;
    uintptr_t rip=(uintptr_t)exception->ContextRecord->Rip;
    if(rip!=address && rip!=address+1) return EXCEPTION_CONTINUE_SEARCH;
    uintptr_t image=(uintptr_t)hooks.image;
    for(uint32_t i=0;i<hooks.count;i++) {
        if(hooks.lengths[i]==1 && address==image+hooks.selected[i].rva) {
            exception->ContextRecord->Rip=(uintptr_t)hooks.memory+hooks.table[i*3].BeginAddress;
            return EXCEPTION_CONTINUE_EXECUTION;
        }
    }
    return EXCEPTION_CONTINUE_SEARCH;
}
static BOOL hook_retire(void) {
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    while(hooks.patched) {
        uint32_t i=hooks.patched-1;
        if(!hook_patch(image+hooks.selected[i].rva,hooks.originals[i],hooks.lengths[i],hooks.protections[i])) return FALSE;
        hooks.patched--;
    }
    if(hooks.breakpoint_handler && !RemoveVectoredExceptionHandler(hooks.breakpoint_handler)) return FALSE;
    hooks.breakpoint_handler=NULL;
    if(hooks.registered && !RtlDeleteFunctionTable(hooks.table)) return FALSE;
    hooks.registered=FALSE;
    if(hooks.memory && !VirtualFree(hooks.memory,0,MEM_RELEASE)) return FALSE;
    ZeroMemory(&hooks,sizeof(hooks));
    return TRUE;
}
static BOOL hook_select(const APHookProcedure *procedures,uint32_t count,BOOL allow_tiny) {
    if(hooks.memory || !procedures || !count || count>AP_HOOK_LIMIT) return FALSE;
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    IMAGE_NT_HEADERS64 *nt=(IMAGE_NT_HEADERS64 *)(image+((IMAGE_DOS_HEADER *)image)->e_lfanew);
    for(uint32_t i=0;i<count;i++) {
        APHookProcedure p=procedures[i];
        if(!p.id || !p.rva || p.size<(allow_tiny?1u:5u) || p.size>AP_HOOK_SOURCE_LIMIT || (uint64_t)p.rva+p.size>nt->OptionalHeader.SizeOfImage) return FALSE;
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
    hooks.image=image;
    return TRUE;
}
static BOOL hook_allocate(BOOL general) {
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
    hooks.stride=general?AP_HOOK_LARGE_STRIDE:AP_HOOK_STRIDE;
    hooks.bytes=(hooks.stride+3*sizeof(RUNTIME_FUNCTION))*hooks.count;
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
static EXCEPTION_DISPOSITION WINAPI function_unwind(PEXCEPTION_RECORD,PVOID,PCONTEXT,PVOID);
static void function_enter(uint32_t,uintptr_t);
static void function_exit(uint32_t,uintptr_t);
static uint32_t hook_clone_pc(const unsigned char *data,uint32_t map_at,uint32_t count,uint32_t source_rva,uint32_t clone_rva,uint32_t pc){
    uint32_t offset=pc-source_rva,shift=0;
    for(uint32_t m=0;m<count;m++){
        uint32_t source=data[map_at+m*4]|((uint32_t)data[map_at+1+m*4]<<8),clone=data[map_at+2+m*4]|((uint32_t)data[map_at+3+m*4]<<8);
        if(source>offset)break;
        shift=clone-source;
    }
    return clone_rva+offset+shift;
}
static BOOL hook_forward_info_at(unsigned char *image,const unsigned char *data,uint32_t available,RUNTIME_FUNCTION *body,uint32_t index,const unsigned char *copied_info,uint32_t info_bytes) {
    if(available<8)return FALSE;
    uint32_t kind=hook_u32(data+4);
    if(!kind)return TRUE;
    if((kind!=1 && kind!=2) || available<(kind==2?56u:36u))return FALSE;
    uint32_t map_at=kind==2?56:36;
    IMAGE_NT_HEADERS64 *nt=(IMAGE_NT_HEADERS64 *)(image+((IMAGE_DOS_HEADER *)image)->e_lfanew);
    uint32_t image_size=nt->OptionalHeader.SizeOfImage;
    uint32_t rva=hook_u32(data+8),size=hook_u32(data+12),entry_rva=hook_u32(data+16),info_rva=hook_u32(data+20),handler_rva=hook_u32(data+24),handler_data=hook_u32(data+28),count=hook_u32(data+32);
    if(rva!=hooks.selected[index].rva || size!=hooks.selected[index].size || (uint64_t)entry_rva+12>image_size || (uint64_t)info_rva+info_bytes+4>image_size || info_rva%4 || !handler_rva || handler_rva>=image_size || handler_data!=(uint64_t)info_rva+info_bytes+4 || !count || count>8193 || (uint64_t)map_at+count*4>available)return FALSE;
    RUNTIME_FUNCTION *original=(RUNTIME_FUNCTION *)(image+entry_rva);
    const unsigned char *info=image+info_rva;
    if(original->BeginAddress!=rva || original->EndAddress!=(uint64_t)rva+size || original->UnwindData!=info_rva || (kind==1?info[0]!=17:(info[0]!=9 && info[0]!=25)) || memcmp(info+1,copied_info+1,info_bytes-1) || hook_u32(info+info_bytes)!=handler_rva)return FALSE;
    MEMORY_BASIC_INFORMATION memory;
    if(!VirtualQuery(image+handler_rva,&memory,sizeof(memory)) || memory.AllocationBase!=image || memory.State!=MEM_COMMIT || (memory.Protect&0xff)!=PAGE_EXECUTE_READ || (memory.Protect&PAGE_GUARD))return FALSE;
    uint32_t previous_source=0,previous_clone=0,delta=0,body_size=body->EndAddress-body->BeginAddress;
    if(body_size<size)return FALSE;
    for(uint32_t i=0;i<count;i++) {
        uint32_t source=data[map_at+i*4]|((uint32_t)data[map_at+1+i*4]<<8),clone=data[map_at+2+i*4]|((uint32_t)data[map_at+3+i*4]<<8);
        if((!i && (source || clone)) || (i && (source<=previous_source || clone<=previous_clone)) || source>size || clone>body_size || clone<source || clone-source<delta)return FALSE;
        previous_source=source;previous_clone=clone;delta=clone-source;
    }
    if(delta!=body_size-size)return FALSE;
    if(kind==1)return TRUE;
    uint32_t scope_bytes=hook_u32(data+40),shadow_at=hook_u32(data+48),scope_at=hook_u32(data+52);
    uint32_t style=hook_u32(data+36);
    if((style!=1 && style!=2 && style!=3) || (style>=2 && info[0]!=25) || hook_u32(data+44)!=info[0] || scope_at!=map_at+count*4 || scope_bytes<20 || scope_bytes>4+64*16 || (scope_bytes-4)%16 || (uint64_t)scope_at+scope_bytes+12+info_bytes+20>available || (style!=3 && shadow_at!=scope_at+scope_bytes) || (uint64_t)shadow_at+12+info_bytes+20>available || (uint64_t)handler_data+scope_bytes>image_size)return FALSE;
    const unsigned char *scope=data+scope_at,*original_scope=image+handler_data;
    uint32_t scopes=hook_u32(scope);
    if(scopes!=(scope_bytes-4)/16 || scopes!=hook_u32(original_scope))return FALSE;
    uintptr_t allocation=(uintptr_t)hooks.memory,image_base=(uintptr_t)image;
    if(allocation<image_base || allocation-image_base>UINT32_MAX-hooks.bytes)return FALSE;
    uint32_t difference=(uint32_t)(allocation-image_base);
    uint32_t typed_count=0,typed_rvas[64]={0};
    const unsigned char *typed_data[64]={0};BOOL typed_used[64]={FALSE};
    if(style==3){
        uint32_t at=scope_at+scope_bytes;
        if((uint64_t)at+4>shadow_at || shadow_at-at>2048)return FALSE;
        typed_count=hook_u32(data+at);at+=4;
        if(!typed_count || typed_count>64)return FALSE;
        for(uint32_t i=0;i<typed_count;i++){
            if((uint64_t)at+12>shadow_at)return FALSE;
            uint32_t original_rva=hook_u32(data+at),bytes=hook_u32(data+at+4);at+=8;
            if(bytes<12 || bytes>4+64*8 || (bytes-4)%8 || (uint64_t)at+bytes>shadow_at || (uint64_t)original_rva+bytes>image_size || ((uint64_t)original_rva<(uint64_t)rva+size && (uint64_t)rva<(uint64_t)original_rva+bytes))return FALSE;
            for(uint32_t j=0;j<i;j++)if(typed_rvas[j]==original_rva)return FALSE;
            typed_rvas[i]=original_rva;typed_data[i]=data+at;
            const unsigned char *a=image+original_rva,*b=data+at;
            uint32_t types=(bytes-4)/8;
            if(hook_u32(a)!=types || hook_u32(b)!=types)return FALSE;
            for(uint32_t j=0;j<types;j++){
                uint32_t class_rva=hook_u32(a+4+j*8),target=hook_u32(a+8+j*8);
                if(hook_u32(b+4+j*8)!=class_rva || (!class_rva && j+1!=types))return FALSE;
                if(class_rva && ((uint64_t)class_rva+8>image_size || ((uint64_t)class_rva<(uint64_t)rva+size && (uint64_t)rva<(uint64_t)class_rva+8)))return FALSE;
                if((uint64_t)target<(uint64_t)rva+info[1] || (uint64_t)target>=(uint64_t)rva+size || hook_u32(b+8+j*8)!=hook_clone_pc(data,map_at,count,rva,difference+body->BeginAddress,target))return FALSE;
            }
            at+=bytes;
        }
        if(at!=shadow_at)return FALSE;
    }
    const unsigned char *shadow=data+shadow_at,*alias=shadow+12;
    uint32_t info_at=(uint32_t)(alias-hooks.memory),thunk=hook_u32(copied_info+info_bytes);
    if(hook_u32(shadow)!=difference+body->BeginAddress || hook_u32(shadow+4)!=difference+body->EndAddress || hook_u32(shadow+8)!=difference+info_at || memcmp(alias,copied_info,info_bytes) || hook_u32(alias+info_bytes)!=difference+thunk || hook_u32(alias+info_bytes+4)!=hooks.selected[index].id || hook_u32(alias+info_bytes+8)!=3 || hook_u64(alias+info_bytes+12)!=(uintptr_t)data)return FALSE;
    for(uint32_t i=0;i<scopes;i++) {
        const unsigned char *a=original_scope+4+i*16,*b=scope+4+i*16;
        uint32_t begin=hook_u32(a),end=hook_u32(a+4),filter=hook_u32(a+8),target=hook_u32(a+12);
        if((uint64_t)begin<(uint64_t)rva+info[1] || begin>=end || (uint64_t)end>(uint64_t)rva+size )return FALSE;
        BOOL cleanup=style==1?!target:filter==0;
        uint32_t callback=style>=2?(cleanup?target:0):filter;
        BOOL typed=style==3 && filter>2;
        if(style>=2 && filter!=0 && filter!=2 && !typed)return FALSE;
        if(typed){
            BOOL found=FALSE;
            if(target || hook_u32(b+12))return FALSE;
            for(uint32_t j=0;j<typed_count;j++)if(typed_rvas[j]==filter){
                if(hook_u32(b+8)!=(uint64_t)difference+(uintptr_t)typed_data[j]-(uintptr_t)hooks.memory)return FALSE;
                typed_used[j]=TRUE;found=TRUE;break;
            }
            if(!found)return FALSE;
        }else if(hook_u32(b+8)!=filter)return FALSE;
        if(cleanup){if(!(info[0]&16) || !callback || (callback>=rva && (uint64_t)callback<(uint64_t)rva+size) || hook_u32(b+12)!=(style==1?0:target))return FALSE;}
        else if(!typed && ((uint64_t)target<(uint64_t)rva+info[1] || (uint64_t)target>=(uint64_t)rva+size || (style==1 && filter!=1 && (!filter || (filter>=rva && (uint64_t)filter<(uint64_t)rva+size)))))return FALSE;
        if(callback && (style>=2 || callback!=1) && (callback>=image_size || !VirtualQuery(image+callback,&memory,sizeof(memory)) || memory.AllocationBase!=image || memory.State!=MEM_COMMIT || (memory.Protect&0xff)!=PAGE_EXECUTE_READ || (memory.Protect&PAGE_GUARD)))return FALSE;
        for(uint32_t field=0;field<4;field++) {
            if(field==2 || (field==3 && (cleanup || typed)))continue;
            if(hook_u32(b+field*4)!=hook_clone_pc(data,map_at,count,rva,difference+body->BeginAddress,hook_u32(a+field*4)))return FALSE;
        }
    }
    for(uint32_t i=0;i<typed_count;i++)if(!typed_used[i])return FALSE;
    return TRUE;
}
static BOOL hook_exchange(HANDLE pipe,BOOL general) {
    if(!hook_allocate(general)) return FALSE;
    unsigned char offer[56]={'A','P','H','O','O','K','1',0};
    hook_put64(offer+8,(uintptr_t)GetModuleHandleW(NULL));
    hook_put64(offer+16,(uintptr_t)hooks.memory);
    hook_put32(offer+24,hooks.bytes); hook_put32(offer+28,hooks.count);
    hook_put64(offer+32,(uintptr_t)APNativeEnter); hook_put64(offer+40,(uintptr_t)APNativeExit);
    if(general){offer[6]='A';hook_put64(offer+32,(uintptr_t)function_enter);hook_put64(offer+40,(uintptr_t)function_exit);hook_put64(offer+48,(uintptr_t)function_unwind);}
    BOOL tiny=FALSE;
    for(uint32_t i=0;i<hooks.count;i++)if(hooks.selected[i].size<5)tiny=TRUE;
    if(tiny){
        if(!general)return FALSE;
        offer[6]='B';
    }
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
            if((info[0]!=17 && info[0]!=25) || (uint64_t)f->UnwindData+extra+12>table) return FALSE;
            uint32_t handler=hook_u32(info+extra);
            if((uint64_t)handler+14>table || memcmp(hooks.memory+handler,"\xff\x25\0\0\0\0",6) || hook_u64(hooks.memory+handler+6)!=(uintptr_t)function_unwind || hook_u32(info+extra+4)!=hooks.selected[i/per].id) return FALSE;
            uint32_t data_at=f->UnwindData+extra+4;
            if(!hook_forward_info_at((unsigned char *)GetModuleHandleW(NULL),hooks.memory+data_at,table-data_at,f,i/per,info,extra) || (info[0]==25)!=(hook_u32(hooks.memory+data_at+4)==2))return FALSE;
        } else if(info[0]!=1 || info[1]!=7 || info[2]!=2 || info[3]!=0 || info[4]!=7 || info[5]!=1 || info[6]!=25 || info[7]!=0) return FALSE;
        previous=f->EndAddress;
    }
    unsigned char patches[AP_HOOK_LIMIT][AP_HOOK_PREFIX];
    unsigned char *image=(unsigned char *)GetModuleHandleW(NULL);
    for(uint32_t i=0;i<count;i++) {
        unsigned char item[8],expected[AP_HOOK_SOURCE_LIMIT];
        if(!hook_io(pipe,item,8,FALSE)) return FALSE;
        uint32_t length=hook_u32(item),prefix=hook_u32(item+4);
        if(length!=hooks.selected[i].size || prefix==0 || prefix>AP_HOOK_PREFIX || prefix>length) return FALSE;
        if(!hook_io(pipe,expected,length,FALSE) || !hook_io(pipe,patches[i],prefix,FALSE)) return FALSE;
        if(memcmp(expected,image+hooks.selected[i].rva,length)) return FALSE;
        uintptr_t target=(uintptr_t)hooks.memory+hooks.table[i*per].BeginAddress;
        uint32_t jump_size=14;
        if(length<5){
            if(prefix!=1 || patches[i][0]!=0xcc || expected[0]==0xcc)return FALSE;
            const unsigned char *info=hooks.memory+hooks.table[i*per+1].UnwindData;
            if(info[0]!=17 || info[1]!=0 || info[2]!=0 || info[3]!=0 || hook_u32(info+12)!=0)return FALSE;
            jump_size=1;
        }else if(patches[i][0]==0xe9 && prefix>=5){
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
    if(tiny){
        hooks.breakpoint_handler=AddVectoredExceptionHandler(1,hook_breakpoint);
        if(!hooks.breakpoint_handler)return FALSE;
    }
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
