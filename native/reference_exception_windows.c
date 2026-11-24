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
#include <tlhelp32.h>

uintptr_t reference_target;
static volatile LONG caught,bad;
static DWORD observed_exception;
static BOOL worker_mode;
static DWORD measurement_thread;
static volatile LONG typed_exceptions;
static HANDLE reference_server;
static HANDLE *reference_client;
static uintptr_t reference_image;
static uint32_t reference_pair;
static BOOL reference_malformed;
static BOOL reference_exception_class(const EXCEPTION_RECORD *record,char name[64]){
 if(record->NumberParameters!=7 || !record->ExceptionInformation[1])return FALSE;
 HANDLE self=GetCurrentProcess();
 uintptr_t vmt=0,short_string=0;
 unsigned char length=0;
 if(!ReadProcessMemory(self,(const void *)record->ExceptionInformation[1],&vmt,sizeof(vmt),NULL) || vmt<136 ||
    !ReadProcessMemory(self,(const void *)(vmt-136),&short_string,sizeof(short_string),NULL) ||
    !ReadProcessMemory(self,(const void *)short_string,&length,sizeof(length),NULL) || !length || length>=64 ||
    !ReadProcessMemory(self,(const void *)(short_string+1),name,length,NULL))return FALSE;
 name[length]='\0';return TRUE;
}
static LONG CALLBACK reference_typed_exception(EXCEPTION_POINTERS *p){
 const EXCEPTION_RECORD *r=p->ExceptionRecord;
 DWORD error=GetLastError();
 if(r->ExceptionCode==EXCEPTION_ACCESS_VIOLATION){
  uintptr_t address=r->NumberParameters>=2?(uintptr_t)r->ExceptionInformation[1]:0;
  fprintf(stderr,"typed reference access violation: pair=%u malformed=%d tid=%lu rip=%llx address=%llx image=%llx target=%llx rsp=%llx rbp=%llx\n",
      reference_pair,reference_malformed,GetCurrentThreadId(),(unsigned long long)p->ContextRecord->Rip,(unsigned long long)address,
      (unsigned long long)reference_image,(unsigned long long)reference_target,(unsigned long long)p->ContextRecord->Rsp,(unsigned long long)p->ContextRecord->Rbp);
 }
 if(GetCurrentThreadId()==measurement_thread && r->ExceptionCode==0x0eedfadeu){
  char name[64];
  BOOL known=reference_exception_class(r,name);
  if(!known || strcmp(name,"EReadError")){
   fprintf(stderr,"unexpected Delphi exception: pair=%u malformed=%d class=%s params=%lu flags=%lx raise_site=%llx object=%llx image=%llx target=%llx\n",
       reference_pair,reference_malformed,known?name:"unreadable",r->NumberParameters,r->ExceptionFlags,
       r->NumberParameters?(unsigned long long)r->ExceptionInformation[0]:0,
       r->NumberParameters>=2?(unsigned long long)r->ExceptionInformation[1]:0,
       (unsigned long long)reference_image,(unsigned long long)reference_target);
   InterlockedExchange(&bad,1);
  }
  InterlockedIncrement(&typed_exceptions);
 }
 SetLastError(error);return EXCEPTION_CONTINUE_SEARCH;
}
static HANDLE original_worker(unsigned char *image){
 union {FARPROC proc;LONG (WINAPI *query)(HANDLE,ULONG,PVOID,ULONG,PULONG);} nt;
 nt.proc=GetProcAddress(GetModuleHandleW(L"ntdll.dll"),"NtQueryInformationThread");if(!nt.proc)return NULL;
 HANDLE snapshot=CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD,0);if(snapshot==INVALID_HANDLE_VALUE)return NULL;
 THREADENTRY32 entry={0};entry.dwSize=sizeof(entry);HANDLE found=NULL;
 for(BOOL more=Thread32First(snapshot,&entry);more;more=Thread32Next(snapshot,&entry)){
  if(entry.th32OwnerProcessID!=GetCurrentProcessId() || entry.th32ThreadID==GetCurrentThreadId())continue;
  HANDLE h=OpenThread(THREAD_QUERY_INFORMATION|SYNCHRONIZE,FALSE,entry.th32ThreadID);if(!h)continue;
  uintptr_t start=0;
  if(nt.query(h,9,&start,sizeof(start),NULL)>=0 && start==(uintptr_t)(image+0x191700)){found=h;break;}
  CloseHandle(h);
 }
 CloseHandle(snapshot);return found;
}

LONG APReferenceFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 if(!p)return EXCEPTION_CONTINUE_SEARCH;
 DWORD code=p->ExceptionRecord->ExceptionCode;
 if(code!=0x0eedfadeu)return EXCEPTION_CONTINUE_SEARCH;
 if(worker_mode)fprintf(stderr,"Delphi exception escaped the typed handler: pair=%u malformed=%d flags=%lx\n",reference_pair,reference_malformed,p->ExceptionRecord->ExceptionFlags);
 if(p->ExceptionRecord->NumberParameters!=7 || !p->ExceptionRecord->ExceptionInformation[1])InterlockedExchange(&bad,1);
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
typedef struct {OVERLAPPED connect;uint32_t command;} WorkerRequest;
static DWORD WINAPI send_worker_command(void *arg){
 WorkerRequest *request=arg;DWORD transferred=0;
 if(WaitForSingleObject(request->connect.hEvent,5000)!=WAIT_OBJECT_0 || !GetOverlappedResult(reference_server,&request->connect,&transferred,FALSE))return 1;
 OVERLAPPED write={0};write.hEvent=CreateEventW(NULL,TRUE,FALSE,NULL);if(!write.hEvent)return 1;
 uint32_t packet[2]={4,request->command};
 BOOL ok=WriteFile(reference_server,packet,sizeof(packet),&transferred,&write);
 if(!ok && GetLastError()==ERROR_IO_PENDING)ok=WaitForSingleObject(write.hEvent,5000)==WAIT_OBJECT_0 && GetOverlappedResult(reference_server,&write,&transferred,FALSE);
 CloseHandle(write.hEvent);return ok && transferred==sizeof(packet)?0:1;
}
static BOOL worker_call(BOOL malformed){
 reference_malformed=malformed;
 if(!DisconnectNamedPipe(reference_server) && GetLastError()!=ERROR_PIPE_NOT_CONNECTED)return FALSE;
 WorkerRequest request={0};request.command=malformed?2:0;
 request.connect.hEvent=CreateEventW(NULL,TRUE,FALSE,NULL);if(!request.connect.hEvent)return FALSE;
 BOOL connected=ConnectNamedPipe(reference_server,&request.connect);
 if(!connected && GetLastError()!=ERROR_IO_PENDING){CloseHandle(request.connect.hEvent);return FALSE;}
 HANDLE sender=CreateThread(NULL,0,send_worker_command,&request,0,NULL);
 if(!sender){CloseHandle(request.connect.hEvent);return FALSE;}
 LONG before=caught,exceptions_before=typed_exceptions;
 uintptr_t result=APReferenceCall(NULL);
 DWORD status=1;
 if(WaitForSingleObject(sender,5000)!=WAIT_OBJECT_0){TerminateProcess(GetCurrentProcess(),25);return FALSE;}
 BOOL ok=GetExitCodeThread(sender,&status);CloseHandle(sender);CloseHandle(request.connect.hEvent);
 if(*reference_client==INVALID_HANDLE_VALUE || !CloseHandle(*reference_client))ok=FALSE;
 *reference_client=INVALID_HANDLE_VALUE;
 return ok && !status && !result && caught==before && typed_exceptions==exceptions_before+(malformed?1:0) && !bad;
}
static BOOL pairs(uint32_t count){
 for(uint32_t i=0;i<count;i++){
  if(worker_mode){reference_pair=i+1;if(!worker_call(FALSE)||!worker_call(TRUE))return FALSE;continue;}
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
 if(argc!=4 && argc!=5)return 2;
 worker_mode=argc==5 && !wcscmp(argv[4],L"worker");
 if(argc==5 && !worker_mode)return 2;
 uint32_t source_rva=worker_mode?0x191700:0xa930,source_size=worker_mode?673:155;
 if(_setmode(_fileno(stdin),_O_BINARY)==-1)return 2;
 SetErrorMode(SEM_FAILCRITICALERRORS|SEM_NOGPFAULTERRORBOX);
 /* Preclaim the original's namespace. Never connect it to an unrelated server. */
 HANDLE pipe=CreateNamedPipeW(L"\\\\.\\pipe\\AntiPessimizerPipe",PIPE_ACCESS_DUPLEX|FILE_FLAG_FIRST_PIPE_INSTANCE|FILE_FLAG_OVERLAPPED,PIPE_TYPE_BYTE|PIPE_READMODE_BYTE|PIPE_WAIT,1,4096,4096,0,NULL);
 if(pipe==INVALID_HANDLE_VALUE)return 3;
 OVERLAPPED connect={0};connect.hEvent=CreateEventW(NULL,TRUE,FALSE,NULL);if(!connect.hEvent)return 3;
 BOOL pending=ConnectNamedPipe(pipe,&connect);if(!pending && GetLastError()!=ERROR_IO_PENDING)return 3;
 HMODULE dll=LoadLibraryW(argv[1]);if(!dll){fprintf(stderr,"LoadLibrary original failed %lu\n",GetLastError());return 4;}
 if(WaitForSingleObject(connect.hEvent,5000)!=WAIT_OBJECT_0)return 5;
 HANDLE worker=worker_mode?original_worker((unsigned char *)dll):NULL;
 if(worker_mode && !worker)return 18;
 DWORD transferred=0;if(!GetOverlappedResult(pipe,&connect,&transferred,FALSE))return 5;CloseHandle(connect.hEvent);
 OVERLAPPED send={0};send.hEvent=CreateEventW(NULL,TRUE,FALSE,NULL);if(!send.hEvent)return 5;
 const uint32_t end_command[2]={4,0};
 BOOL written=WriteFile(pipe,end_command,sizeof(end_command),&transferred,&send);
 if(!written && GetLastError()!=ERROR_IO_PENDING)return 5;
 if(!written && (WaitForSingleObject(send.hEvent,5000)!=WAIT_OBJECT_0 || !GetOverlappedResult(pipe,&send,&transferred,FALSE)))return 5;
 CloseHandle(send.hEvent);if(transferred!=sizeof(end_command))return 5;
 unsigned char *image=(unsigned char *)dll;
 static const unsigned char prefix[]={0x55,0x53,0x48,0x83,0xec,0x38,0x48,0x8b,0xec,0x48,0x89,0x6d,0x28};
 if(!worker_mode && memcmp(image+source_rva,prefix,sizeof(prefix)))return 6;
 unsigned char original[4096];memcpy(original,image+source_rva,source_size);
 if(worker_mode){
  if(WaitForSingleObject(worker,5000)!=WAIT_OBJECT_0)return 19;
  CloseHandle(worker);
  /* The reviewed worker stores its owned client handle at this RIP operand. */
  if(memcmp(image+source_rva+0x59,"\x48\x89\x05",3))return 19;
  int32_t displacement=0;memcpy(&displacement,image+source_rva+0x5c,4);
  reference_client=(HANDLE *)(image+source_rva+0x60+displacement);
  if(*reference_client==INVALID_HANDLE_VALUE || !CloseHandle(*reference_client))return 19;
  *reference_client=INVALID_HANDLE_VALUE;reference_server=pipe;
  reference_image=(uintptr_t)image;
  measurement_thread=GetCurrentThreadId();
  if(!AddVectoredExceptionHandler(1,reference_typed_exception))return 19;
 }
 reference_target=(uintptr_t)(image+source_rva);
 fprintf(stderr,"original Delphi: %s\n",worker_mode?"worker ctEnd and typed EReadError":"valid constructor and nil-object exception");
 if(!pairs(4) || bad || (worker_mode?typed_exceptions!=4:observed_exception!=0x0eedfadeu)){fprintf(stderr,"original baseline failed caught=%ld code=%lx\n",caught,observed_exception);return 7;}
 HMODULE adapter=LoadLibraryW(argv[2]);if(!adapter)return 8;
 union {FARPROC proc;uintptr_t (*address)(void);} helper;helper.proc=GetProcAddress(adapter,"APReferenceUnwindAddress");if(!helper.proc)return 8;
 union {FARPROC proc;BOOL (*validate)(uintptr_t,uintptr_t,uint32_t,const RUNTIME_FUNCTION *,uint32_t,uint32_t);} verifier;
 verifier.proc=GetProcAddress(adapter,"APReferenceValidatePlan");if(!verifier.proc)return 8;
 union {FARPROC proc;BOOL (*start)(uintptr_t,uint32_t);} capture_start;
 capture_start.proc=GetProcAddress(adapter,"APReferenceStartCapture");if(!capture_start.proc)return 8;
 union {FARPROC proc;BOOL (*finish)(const wchar_t *,uint32_t *,uint32_t *,uint32_t *);} capture_finish;
 capture_finish.proc=GetProcAddress(adapter,"APReferenceFinishCapture");if(!capture_finish.proc)return 8;
 union {FARPROC proc;uintptr_t (*address)(void);} enter_helper,exit_helper;
 enter_helper.proc=GetProcAddress(adapter,"APReferenceEnterAddress");exit_helper.proc=GetProcAddress(adapter,"APReferenceExitAddress");if(!enter_helper.proc || !exit_helper.proc)return 8;
 FILETIME created,exited,kernel,user;
 if(!GetProcessTimes(GetCurrentProcess(),&created,&exited,&kernel,&user))return 8;
 uint64_t created_ticks=((uint64_t)created.dwHighDateTime<<32)|created.dwLowDateTime;
 unsigned char *memory=allocate_near(image);if(!memory)return 9;
 printf("{\"pid\":%lu,\"created\":%llu,\"loaded\":%llu,\"allocation\":%llu,\"enter\":%llu,\"exit\":%llu,\"unwind\":%llu}\n",GetCurrentProcessId(),(unsigned long long)created_ticks,(unsigned long long)(uintptr_t)image,(unsigned long long)(uintptr_t)memory,(unsigned long long)enter_helper.address(),(unsigned long long)exit_helper.address(),(unsigned long long)helper.address());fflush(stdout);
 uint32_t size=0;RUNTIME_FUNCTION functions[3];
 if(fread(&size,4,1,stdin)!=1 || size==0 || size>8192 || fread(functions,sizeof(functions),1,stdin)!=1 || fread(memory,size,1,stdin)!=1)return 10;
 for(unsigned i=0;i<3;i++)if(functions[i].BeginAddress>=functions[i].EndAddress || functions[i].EndAddress>size || functions[i].UnwindData>=size || (i && functions[i].BeginAddress<functions[i-1].EndAddress))return 10;
 if(!verifier.validate((uintptr_t)image,(uintptr_t)memory,size,functions,source_rva,source_size)){fprintf(stderr,"native scope validation failed\n");return 14;}
 unsigned char *info=memory+functions[1].UnwindData;
 uint32_t info_bytes=4+((info[2]+1)&~1)*2;
 unsigned char *descriptor=info+info_bytes+4;
 uint32_t scope_at=0;memcpy(&scope_at,descriptor+52,4);
 uint32_t *landing=(uint32_t *)(descriptor+scope_at+4);
 *landing^=1;
 BOOL corrupt_accepted=verifier.validate((uintptr_t)image,(uintptr_t)memory,size,functions,source_rva,source_size);
 *landing^=1;
 if(corrupt_accepted){fprintf(stderr,"native validator accepted a changed catch destination\n");return 14;}
 if(worker_mode){
  uint32_t scope_bytes=0;memcpy(&scope_bytes,descriptor+40,4);
  unsigned char *block=descriptor+scope_at+scope_bytes;
  for(uint32_t field=0;field<3;field++){
   /* First typed block: header Count, source RVA, bytes, then table Count,
      class-slot RVA and landing RVA. Each must be checked against the source. */
   uint32_t *word=(uint32_t *)(block+(field==0?12:field==1?16:20));
   *word^=1;
   BOOL accepted=verifier.validate((uintptr_t)image,(uintptr_t)memory,size,functions,source_rva,source_size);
   *word^=1;
   if(accepted){fprintf(stderr,"native validator accepted typed metadata corruption %u\n",field);return 14;}
  }
 }
 DWORD protection=0;
 if(!VirtualProtect(memory,16384,PAGE_EXECUTE_READ,&protection)||!FlushInstructionCache(GetCurrentProcess(),memory,size)||!RtlAddFunctionTable(functions,3,(DWORD64)(uintptr_t)memory))return 11;
 if(!capture_start.start((uintptr_t)image,source_rva))return 16;
 reference_target=(uintptr_t)memory;
 fprintf(stderr,"cloned Delphi: %s\n",worker_mode?"typed Exception catch, normal return and Go metrics":"original handler, capture/resume/reraise and cleanup");
 if(!pairs(100) || bad || (worker_mode?(caught!=0 || typed_exceptions!=104):caught!=104)){fprintf(stderr,"clone failed caught=%ld code=%lx bad=%ld\n",caught,observed_exception,bad);return 12;}
 uint32_t entries=0,returns=0,unwinds=0;
 if(!capture_finish.finish(argv[3],&entries,&returns,&unwinds) || entries!=200 || returns!=(worker_mode?200u:100u) || unwinds!=(worker_mode?0u:100u)){fprintf(stderr,"native capture incomplete enter=%u return=%u unwind=%u\n",entries,returns,unwinds);return 17;}
 if((!worker_mode && observed_exception!=0x0eedfadeu) || memcmp(original,image+source_rva,source_size))return 15;
 if(!RtlDeleteFunctionTable(functions)||!VirtualFree(memory,0,MEM_RELEASE))return 13;
 printf("{\"baseline_pairs\":4,\"instrumented_pairs\":100,\"entries\":%u,\"returns\":%u,\"unwinds\":%u,\"caught\":%ld,\"exception_code\":%lu,\"typed_exceptions\":%ld}\n",entries,returns,unwinds,caught,observed_exception,typed_exceptions);fflush(stdout);
 /* ExitProcess owns RTL/worker teardown; never unload a DLL underneath its worker. */
 return 0;
}
