/* This workload has no compiler instrumentation and never calls Enter/Exit.
 * Its startup cooperates only to keep callers quiescent while Go installs hooks. */
#define WIN32_LEAN_AND_MEAN
#include "producer.h"
#include <stdio.h>
#include <stdlib.h>
#include <wchar.h>
#include <string.h>

extern uint64_t APHookSum(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t,uint64_t);
extern const unsigned char APHookSumEnd[];

__asm__(
 ".text\n"
 ".p2align 3\n .LAPHookConstant: .quad 35\n"
 ".globl APHookSum\n"
 ".def APHookSum; .scl 2; .type 32; .endef\n"
 "APHookSum:\n"
 ".rept 16\n nop\n .endr\n"
 "movl $2,%r10d\n"
 "1: subq $1,%r10\n jne 1b\n"
 "testq %rcx,%rcx\n jne 2f\n"
 "movq $777,%rax\n ret\n"
 "2: leaq (%rcx,%rdx),%rax\n"
 "addq %r8,%rax\n addq %r9,%rax\n"
 "addq 40(%rsp),%rax\n addq 48(%rsp),%rax\n"
 "addq .LAPHookConstant(%rip),%rax\n"
 "ret\n"
 ".globl APHookSumEnd\n APHookSumEnd:\n"
);
static uint32_t iterations=10;
static HANDLE start_workers;
static DWORD WINAPI worker(void *unused) {
 (void)unused;
 if(WaitForSingleObject(start_workers,5000)!=WAIT_OBJECT_0) return 1;
 for(uint32_t i=0;i<iterations;i++) {
  SetLastError(0x12345678);
  if(APHookSum(i%2?0:7+i,11,13,17,19,23)!=(i%2?777:125+i) || GetLastError()!=0x12345678) return 2;
 }
 APNativeThreadEnd();
 return 0;
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
 uint32_t size=(uint32_t)((uintptr_t)APHookSumEnd-(uintptr_t)APHookSum);
 if(size>256) return 3;
 unsigned char original[256]; memcpy(original,(const void *)APHookSum,size);
 APHookProcedure selected={1,(uint32_t)((uintptr_t)APHookSum-base),size};
 if(!APNativeInitHooks(pipe,&selected,1,capacity)) {
  if(memcmp(original,(const void *)APHookSum,size) || APHookSum(7,11,13,17,19,23)!=125) return 8;
  fputs("native hook installation failed\n",stderr);return 4;
 }
 start_workers=CreateEventW(NULL,TRUE,FALSE,NULL);
 if(!start_workers) return 5;
 HANDLE handles[16]; DWORD result=0;
 for(uint32_t i=0;i<threads;i++) {
  handles[i]=CreateThread(NULL,0,worker,NULL,0,NULL);
  if(!handles[i]) return 5;
 }
 SetEvent(start_workers);
 for(uint32_t i=0;i<threads;i++) {
  DWORD code=1;
  if(WaitForSingleObject(handles[i],10000)!=WAIT_OBJECT_0) return 6;
  GetExitCodeThread(handles[i],&code); CloseHandle(handles[i]); result|=code;
 }
 CloseHandle(start_workers);
 if(!APNativeShutdown(5000)) return 7;
 if(memcmp(original,(const void *)APHookSum,size) || APHookSum(7,11,13,17,19,23)!=125) return 8;
 return result?9:0;
}
