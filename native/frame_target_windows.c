/* Unmodified program with ordinary framed calls/recursion and real Win64 SEH.
 * No profiler imports or callbacks. The outer handler is intentionally outside
 * the selected methods, so Windows unwinds their dynamically registered frames. */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>
#include <wchar.h>

enum { AP_FRAME_EXCEPTION=0xe0424242u };
#define METHOD __attribute__((noinline))
static DWORD child_delay_ms=1;
METHOD uint64_t APFrameChild(uint64_t n){
 volatile uint64_t value=n;
 if(!value) RaiseException(AP_FRAME_EXCEPTION,0,0,NULL);
 Sleep(child_delay_ms);
 return value+5;
}
METHOD uint64_t APFrameRecursive(uint64_t n){
 volatile uint64_t value=n;
 return value?1+APFrameRecursive(value-1):1;
}
METHOD uint64_t APFrameParent(uint64_t n){
 volatile uint64_t value=n;
 uint64_t first=APFrameChild(value);
 uint64_t second=APFrameChild(value+1);
 return first+second+APFrameRecursive(3);
}
LONG APFrameFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 return p && p->ExceptionRecord->ExceptionCode==AP_FRAME_EXCEPTION?EXCEPTION_EXECUTE_HANDLER:EXCEPTION_CONTINUE_SEARCH;
}
extern uint64_t APFrameCatch(void);
__asm__(
 ".text\n .globl APFrameCatch\n .def APFrameCatch; .scl 2; .type 32; .endef\n"
 ".seh_proc APFrameCatch\n APFrameCatch:\n"
 "subq $40,%rsp\n .seh_stackalloc 40\n .seh_endprologue\n"
 ".Lframe_try_begin:\n xorq %rcx,%rcx\n call APFrameParent\n nop\n nop\n"
 ".Lframe_try_end:\n xorq %rax,%rax\n addq $40,%rsp\n ret\n"
 ".Lframe_catch:\n movq $777,%rax\n addq $40,%rsp\n ret\n"
 ".seh_handler __C_specific_handler, @except\n .seh_handlerdata\n"
 ".long 1\n .rva .Lframe_try_begin,.Lframe_try_end,APFrameFilter,.Lframe_catch\n"
 ".text\n .seh_endproc\n"
);
static HANDLE barrier;
static uint32_t iterations=10;
static DWORD WINAPI worker(void *unused){
 (void)unused;
 if(WaitForSingleObject(barrier,5000)!=WAIT_OBJECT_0)return 1;
 for(uint32_t i=0;i<iterations;i++)if(APFrameParent(i+1)!=17+2*i)return 2;
 if(APFrameCatch()!=777)return 3;
 return 0;
}
int wmain(int argc,wchar_t **argv){
 uint32_t threads=3;
 for(int i=1;i<argc;i+=2){
  if(i+1>=argc)return 2;
  if(!wcscmp(argv[i],L"--threads"))threads=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--iterations"))iterations=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--delay-ms"))child_delay_ms=(DWORD)wcstoul(argv[i+1],NULL,10);
  else return 2;
 }
 if(!threads||threads>16||!iterations||iterations>1000||child_delay_ms>5000)return 2;
 barrier=CreateEventW(NULL,TRUE,FALSE,NULL);if(!barrier)return 3;
 HANDLE handles[16];DWORD result=0;
 for(uint32_t i=0;i<threads;i++){handles[i]=CreateThread(NULL,0,worker,NULL,0,NULL);if(!handles[i])return 3;}
 SetEvent(barrier);
 for(uint32_t i=0;i<threads;i++){
  DWORD code=1;if(WaitForSingleObject(handles[i],20000)!=WAIT_OBJECT_0)return 4;
  GetExitCodeThread(handles[i],&code);CloseHandle(handles[i]);result|=code;
 }
 CloseHandle(barrier);return result?5:0;
}
