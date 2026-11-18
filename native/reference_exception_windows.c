/* Isolated child for the pinned original Delphi DLL. It owns the original
 * worker's fixed pipe, initializes the real RTL and never patches source code.
 * Parent verifies the complete DLL SHA before starting this executable. */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>
#include <io.h>
#include <fcntl.h>

uintptr_t reference_target;
static volatile LONG enter_count,exit_count,caught,bad;
static DWORD observed_exception;
static void reference_enter(uint32_t id,uintptr_t sp){if(id!=17 || !sp || (sp&7))InterlockedExchange(&bad,1);InterlockedIncrement(&enter_count);}
static void reference_exit(uint32_t id,uintptr_t sp){if(id!=17 || !sp || (sp&7))InterlockedExchange(&bad,1);InterlockedIncrement(&exit_count);}
LONG APReferenceFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 if(!p)return EXCEPTION_CONTINUE_SEARCH;
 DWORD code=p->ExceptionRecord->ExceptionCode;
 if(code!=EXCEPTION_ACCESS_VIOLATION && code!=0x0eedfadeu)return EXCEPTION_CONTINUE_SEARCH;
 observed_exception=code;InterlockedIncrement(&caught);return EXCEPTION_EXECUTE_HANDLER;
}
extern uintptr_t APReferenceCall(void *);
__asm__(
 ".text\n .globl APReferenceCall\n .def APReferenceCall; .scl 2; .type 32; .endef\n"
 ".seh_proc APReferenceCall\n APReferenceCall:\n subq $40,%rsp\n .seh_stackalloc 40\n .seh_endprologue\n"
 ".Lreference_try_begin:\n xorl %edx,%edx\n call *reference_target(%rip)\n nop\n .Lreference_try_end:\n addq $40,%rsp\n ret\n"
 ".Lreference_catch:\n xorl %eax,%eax\n addq $40,%rsp\n ret\n"
 ".seh_handler __C_specific_handler,@except\n .seh_handlerdata\n .long 1\n"
 ".rva .Lreference_try_begin,.Lreference_try_end,APReferenceFilter,.Lreference_catch\n .text\n .seh_endproc\n"
);
static BOOL pairs(uint32_t count){
 for(uint32_t i=0;i<count;i++){
  _Alignas(16) unsigned char object[128]={0};
  if(APReferenceCall(object)!=(uintptr_t)object || object[8]!=1 || *(uintptr_t *)(object+16))return FALSE;
  LONG before=caught;
  if(APReferenceCall(NULL)!=0 || caught!=before+1)return FALSE;
 }
 return TRUE;
}
static void *allocate_near(unsigned char *image){
 IMAGE_NT_HEADERS64 *nt=(IMAGE_NT_HEADERS64 *)(image+((IMAGE_DOS_HEADER *)image)->e_lfanew);
 uintptr_t cursor=((uintptr_t)image+nt->OptionalHeader.SizeOfImage+0xffff)&~(uintptr_t)0xffff;
 uintptr_t end=(uintptr_t)image+0x70000000;
 for(unsigned i=0;i<4096 && cursor<end;i++){
  MEMORY_BASIC_INFORMATION m;if(!VirtualQuery((void *)cursor,&m,sizeof(m)))return NULL;
  if(m.State==MEM_FREE && m.RegionSize>=16384){void *p=VirtualAlloc((void *)cursor,16384,MEM_RESERVE|MEM_COMMIT,PAGE_READWRITE);if(p)return p;}
  uintptr_t next=((uintptr_t)m.BaseAddress+m.RegionSize+0xffff)&~(uintptr_t)0xffff;if(next<=cursor)return NULL;cursor=next;
 }
 return NULL;
}
int wmain(int argc,wchar_t **argv){
 if(argc!=3)return 2;
 if(_setmode(_fileno(stdin),_O_BINARY)==-1)return 2;
 SetErrorMode(SEM_FAILCRITICALERRORS|SEM_NOGPFAULTERRORBOX);
 /* Preclaim the original's namespace. Never connect it to an unrelated server. */
 HANDLE pipe=CreateNamedPipeW(L"\\\\.\\pipe\\AntiPessimizerPipe",PIPE_ACCESS_DUPLEX|FILE_FLAG_FIRST_PIPE_INSTANCE|FILE_FLAG_OVERLAPPED,PIPE_TYPE_BYTE|PIPE_READMODE_BYTE|PIPE_WAIT,1,4096,4096,0,NULL);
 if(pipe==INVALID_HANDLE_VALUE)return 3;
 OVERLAPPED connect={0};connect.hEvent=CreateEventW(NULL,TRUE,FALSE,NULL);if(!connect.hEvent)return 3;
 BOOL pending=ConnectNamedPipe(pipe,&connect);if(!pending && GetLastError()!=ERROR_IO_PENDING)return 3;
 HMODULE dll=LoadLibraryW(argv[1]);if(!dll){fprintf(stderr,"LoadLibrary original failed %lu\n",GetLastError());return 4;}
 if(WaitForSingleObject(connect.hEvent,5000)!=WAIT_OBJECT_0)return 5;
 DWORD transferred=0;if(!GetOverlappedResult(pipe,&connect,&transferred,FALSE))return 5;CloseHandle(connect.hEvent);
 OVERLAPPED send={0};send.hEvent=CreateEventW(NULL,TRUE,FALSE,NULL);if(!send.hEvent)return 5;
 const uint32_t end_command[2]={4,0};
 BOOL written=WriteFile(pipe,end_command,sizeof(end_command),&transferred,&send);
 if(!written && GetLastError()!=ERROR_IO_PENDING)return 5;
 if(!written && (WaitForSingleObject(send.hEvent,5000)!=WAIT_OBJECT_0 || !GetOverlappedResult(pipe,&send,&transferred,FALSE)))return 5;
 CloseHandle(send.hEvent);if(transferred!=sizeof(end_command))return 5;
 unsigned char *image=(unsigned char *)dll;
 static const unsigned char prefix[]={0x55,0x53,0x48,0x83,0xec,0x38,0x48,0x8b,0xec,0x48,0x89,0x6d,0x28};
 if(memcmp(image+0xa930,prefix,sizeof(prefix)))return 6;
 reference_target=(uintptr_t)(image+0xa930);
 fprintf(stderr,"original Delphi: valid constructor and nil-object exception\n");
 if(!pairs(4)){fprintf(stderr,"original baseline failed caught=%ld code=%lx\n",caught,observed_exception);return 7;}
 HMODULE adapter=LoadLibraryW(argv[2]);if(!adapter)return 8;
 union {FARPROC proc;uintptr_t (*address)(void);} helper;helper.proc=GetProcAddress(adapter,"APReferenceUnwindAddress");if(!helper.proc)return 8;
 unsigned char *memory=allocate_near(image);if(!memory)return 9;
 printf("{\"loaded\":%llu,\"allocation\":%llu,\"enter\":%llu,\"exit\":%llu,\"unwind\":%llu}\n",(unsigned long long)(uintptr_t)image,(unsigned long long)(uintptr_t)memory,(unsigned long long)(uintptr_t)reference_enter,(unsigned long long)(uintptr_t)reference_exit,(unsigned long long)helper.address());fflush(stdout);
 uint32_t size=0;RUNTIME_FUNCTION functions[3];
 if(fread(&size,4,1,stdin)!=1 || size==0 || size>8192 || fread(functions,sizeof(functions),1,stdin)!=1 || fread(memory,size,1,stdin)!=1)return 10;
 for(unsigned i=0;i<3;i++)if(functions[i].BeginAddress>=functions[i].EndAddress || functions[i].EndAddress>size || functions[i].UnwindData>=size || (i && functions[i].BeginAddress<functions[i-1].EndAddress))return 10;
 DWORD protection=0;
 if(!VirtualProtect(memory,16384,PAGE_EXECUTE_READ,&protection)||!FlushInstructionCache(GetCurrentProcess(),memory,size)||!RtlAddFunctionTable(functions,3,(DWORD64)(uintptr_t)memory))return 11;
 reference_target=(uintptr_t)memory;
 fprintf(stderr,"cloned Delphi: original handler, capture/resume/reraise and cleanup\n");
 if(!pairs(100) || bad || enter_count!=200 || exit_count!=100 || caught!=104){fprintf(stderr,"clone failed enter=%ld exit=%ld caught=%ld code=%lx bad=%ld\n",enter_count,exit_count,caught,observed_exception,bad);return 12;}
 if(!RtlDeleteFunctionTable(functions)||!VirtualFree(memory,0,MEM_RELEASE))return 13;
 printf("{\"baseline_pairs\":4,\"instrumented_pairs\":100,\"entries\":%ld,\"returns\":%ld,\"caught\":%ld,\"exception_code\":%lu}\n",enter_count,exit_count,caught,observed_exception);fflush(stdout);
 /* ExitProcess owns RTL/worker teardown; never unload a DLL underneath its worker. */
 return 0;
}
