/* Owned, unmodified Win64 target. Tail transfers leave the original return slot
 * and all six ABI arguments in place. No profiler imports or callbacks. */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>
#include <wchar.h>
enum { AP_TAIL_EXCEPTION=0xe0424243u };
static DWORD tail_delay_ms;
static DWORD tail_indirect_mode;
extern uint64_t APTailStart(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t,uint64_t);
extern uint64_t APTailIndirectStart(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t,uint64_t);
extern uint64_t APTailConditionalStart(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t,uint64_t);
extern uint64_t APTailFramedStack(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t,uint64_t);
extern uint64_t APTailRecursive(uint64_t,uint64_t),APTailRecursiveStep(uint64_t,uint64_t),APTailCatch(void);
__attribute__((noinline)) uint64_t APTailWork(uint64_t n,uint64_t a,uint64_t b,uint64_t c,uint64_t d,uint64_t e){
 volatile uint64_t value=n;
 if(value==1002)RaiseException(AP_TAIL_EXCEPTION,0,0,NULL);
 if(tail_delay_ms)Sleep(tail_delay_ms);
 return value+a+b+c+d+e;
}
LONG APTailFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;return p && p->ExceptionRecord->ExceptionCode==AP_TAIL_EXCEPTION?EXCEPTION_EXECUTE_HANDLER:EXCEPTION_CONTINUE_SEARCH;
}
__asm__(
 ".text\n .globl APTailFramedStack\n .def APTailFramedStack; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailFramedStack\n APTailFramedStack:\n pushq %rbx\n .seh_pushreg %rbx\n subq $32,%rsp\n .seh_stackalloc 32\n .seh_endprologue\n"
 "movq %rcx,%rbx\n addq $32,%rsp\n popq %rbx\n jmp APTailFramedRBP\n .seh_endproc\n"
 ".globl APTailFramedRBP\n .def APTailFramedRBP; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailFramedRBP\n APTailFramedRBP:\n pushq %rbp\n .seh_pushreg %rbp\n subq $64,%rsp\n .seh_stackalloc 64\n leaq 32(%rsp),%rbp\n .seh_setframe %rbp,32\n .seh_endprologue\n"
 "movq %rcx,-16(%rbp)\n movq -16(%rbp),%rcx\n leaq 32(%rbp),%rsp\n popq %rbp\n jmp APTailStart\n .seh_endproc\n"
 ".text\n .globl APTailStart\n .def APTailStart; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailStart\n APTailStart:\n .seh_endprologue\n addq $1,%rcx\n jmp APTailMiddle\n .seh_endproc\n"
 ".globl APTailIndirectStart\n .def APTailIndirectStart; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailIndirectStart\n APTailIndirectStart:\n .seh_endprologue\n"
 "addq $1,%rcx\n leaq APTailMiddle(%rip),%r11\n jmp *%r11\n .seh_endproc\n"
 ".globl APTailConditionalStart\n .def APTailConditionalStart; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailConditionalStart\n APTailConditionalStart:\n .seh_endprologue\n"
 "addq $1,%rcx\n testb $1,%cl\n leaq APTailMiddle(%rip),%r11\n leaq APTailAlternative(%rip),%r10\n"
 "cmovne %r10,%r11\n jmp *%r11\n .seh_endproc\n"
 ".globl APTailMiddle\n .def APTailMiddle; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailMiddle\n APTailMiddle:\n .seh_endprologue\n addq $2,%rcx\n jmp APTailWork\n .seh_endproc\n"
 ".globl APTailAlternative\n .def APTailAlternative; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailAlternative\n APTailAlternative:\n .seh_endprologue\n addq $2,%rcx\n jmp APTailWork\n .seh_endproc\n"
 ".globl APTailRecursive\n .def APTailRecursive; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailRecursive\n APTailRecursive:\n .seh_endprologue\n testq %rcx,%rcx\n jz .Ltail_done\n subq $1,%rcx\n jmp APTailRecursiveStep\n"
 ".Ltail_done:\n leaq 123(%rdx),%rax\n ret\n .seh_endproc\n"
 ".globl APTailRecursiveStep\n .def APTailRecursiveStep; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailRecursiveStep\n APTailRecursiveStep:\n .seh_endprologue\n addq $1,%rdx\n jmp APTailRecursive\n .seh_endproc\n"
 ".globl APTailCatch\n .def APTailCatch; .scl 2; .type 32; .endef\n"
 ".seh_proc APTailCatch\n APTailCatch:\n subq $56,%rsp\n .seh_stackalloc 56\n .seh_endprologue\n"
 ".Ltail_try:\n movl $999,%ecx\n movl $2,%edx\n movl $3,%r8d\n movl $5,%r9d\n movq $7,32(%rsp)\n movq $11,40(%rsp)\n"
 "cmpl $2,tail_indirect_mode(%rip)\n je .Ltail_conditional_call\n"
 "cmpl $0,tail_indirect_mode(%rip)\n je .Ltail_direct_call\n call APTailIndirectStart\n jmp .Ltail_after_call\n"
 ".Ltail_conditional_call:\n call APTailConditionalStart\n jmp .Ltail_after_call\n"
 ".Ltail_direct_call:\n call APTailFramedStack\n .Ltail_after_call:\n nop\n"
 ".Ltail_end:\n xorl %eax,%eax\n addq $56,%rsp\n ret\n"
 ".Ltail_catch:\n movl $903,%eax\n addq $56,%rsp\n ret\n"
 ".seh_handler __C_specific_handler,@except\n .seh_handlerdata\n .long 1\n"
 ".rva .Ltail_try,.Ltail_end,APTailFilter,.Ltail_catch\n .text\n .seh_endproc\n"
);
static HANDLE barrier;
static uint32_t iterations=10;
static DWORD WINAPI worker(void *unused){
 (void)unused;if(WaitForSingleObject(barrier,5000)!=WAIT_OBJECT_0)return 1;
 for(uint32_t i=0;i<iterations;i++){
  uint64_t result;
  if(tail_indirect_mode==2)result=APTailConditionalStart(i+10,2,3,5,7,11);
  else if(tail_indirect_mode==1)result=APTailIndirectStart(i+10,2,3,5,7,11);
  else result=APTailFramedStack(i+10,2,3,5,7,11);
  if(result!=i+41)return 2;
 }
 if(APTailRecursive(5,0)!=128)return 3;
 if(APTailCatch()!=903)return 4;
 return 0;
}
int wmain(int argc,wchar_t **argv){
 uint32_t threads=3;
 for(int i=1;i<argc;i+=2){
  if(i+1>=argc)return 2;
  if(!wcscmp(argv[i],L"--threads"))threads=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--delay-ms"))tail_delay_ms=(DWORD)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--iterations"))iterations=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--indirect"))tail_indirect_mode=(DWORD)wcstoul(argv[i+1],NULL,10);
  else return 2;
 }
 if(tail_delay_ms>5000||tail_indirect_mode>2||!threads||threads>16||!iterations||iterations>100)return 2;
 barrier=CreateEventW(NULL,TRUE,FALSE,NULL);if(!barrier)return 3;
 HANDLE handles[16];DWORD result=0;
 for(uint32_t i=0;i<threads;i++){handles[i]=CreateThread(NULL,0,worker,NULL,0,NULL);if(!handles[i])return 3;}
 SetEvent(barrier);
 for(uint32_t i=0;i<threads;i++){
  DWORD code=1;if(WaitForSingleObject(handles[i],10000)!=WAIT_OBJECT_0)return 4;
  GetExitCodeThread(handles[i],&code);CloseHandle(handles[i]);result|=code;
 }
 CloseHandle(barrier);return result?5:0;
}
