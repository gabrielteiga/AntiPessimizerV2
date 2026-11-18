/* Unmodified program with ordinary framed calls/recursion and real Win64 SEH.
 * No profiler imports or callbacks. The outer handler is intentionally outside
 * the selected methods, so Windows unwinds their dynamically registered frames. */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>
#include <wchar.h>
#include <xmmintrin.h>

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
extern uint64_t APFrameTiny(void);
extern uint64_t APLeafMath(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t,uint64_t);
extern double APLeafConvert(float);
extern uint64_t APFrameSignedAlloc(uint64_t);
extern uint32_t APLeafSetControl(const uint32_t *);
extern uint32_t APLeafGetControl(void);
extern void *APFrameCopy(void *,const void *,uint64_t);
extern uint64_t APFrameFill(void *,uint64_t,uint64_t);
extern uint64_t APLeafAtomic(volatile uint64_t *,uint64_t);
extern uint64_t APLeafCompare(uint64_t *,uint64_t,uint64_t);
static volatile uint64_t atomic_total;
enum { AP_CLEANUP_EXCEPTION=0xe0424243u,AP_COLLISION_EXCEPTION=0xe0424244u };
static volatile LONG cleanup_count,cleanup_bad,collision_seen;
static BOOL cleanup_throws;
extern uint64_t APUnwindParent(uint64_t);
extern unsigned char APUnwindParentEnd[];
extern uint64_t APUnwindCatch(void);
METHOD uint64_t APUnwindThrower(uint64_t n){
 if(!n)RaiseException(AP_CLEANUP_EXCEPTION,0,0,NULL);
 return n+3;
}
METHOD uint64_t APUnwindWork(uint64_t n){
 InterlockedIncrement(&cleanup_count);
 if(!n)RaiseException(AP_COLLISION_EXCEPTION,0,0,NULL);
 return n;
}
void APUnwindFinally(BOOL abnormal,void *frame){
 uint64_t n=*(uint64_t *)((unsigned char *)frame+32);
 if((abnormal && n!=0)||(!abnormal && n!=7))InterlockedExchange(&cleanup_bad,1);
 (void)APUnwindWork(abnormal && cleanup_throws?0:1);
}
EXCEPTION_DISPOSITION APUnwindLanguageHandler(PEXCEPTION_RECORD record,PVOID frame,PCONTEXT context,PDISPATCHER_CONTEXT dispatcher){
 DWORD64 image=(DWORD64)(uintptr_t)GetModuleHandleW(NULL);
 DWORD64 begin=(DWORD64)(uintptr_t)APUnwindParent,end=(DWORD64)(uintptr_t)APUnwindParentEnd;
 if(dispatcher->ImageBase!=image || image+dispatcher->FunctionEntry->BeginAddress!=begin || image+dispatcher->FunctionEntry->EndAddress!=end || dispatcher->ControlPc<begin || dispatcher->ControlPc>=end || *(uint32_t *)dispatcher->HandlerData!=1)InterlockedExchange(&cleanup_bad,1);
 return __C_specific_handler(record,frame,context,dispatcher);
}
LONG APUnwindFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 if(!p)return EXCEPTION_CONTINUE_SEARCH;
 if(cleanup_throws && p->ExceptionRecord->ExceptionCode==AP_COLLISION_EXCEPTION){InterlockedIncrement(&collision_seen);return EXCEPTION_EXECUTE_HANDLER;}
 return p->ExceptionRecord->ExceptionCode==AP_CLEANUP_EXCEPTION?EXCEPTION_EXECUTE_HANDLER:EXCEPTION_CONTINUE_SEARCH;
}
/* The early RET expands before the protected region. Successful cleanup thus
 * requires translating both source PCs and opaque original scope metadata. */
__asm__(
 ".text\n .globl APUnwindParent\n .def APUnwindParent; .scl 2; .type 32; .endef\n"
 ".seh_proc APUnwindParent\n APUnwindParent:\n subq $56,%rsp\n .seh_stackalloc 56\n .seh_endprologue\n"
 "movq %rcx,32(%rsp)\n cmpq $999,%rcx\n jne .Lcleanup_try_begin\n movl $999,%eax\n addq $56,%rsp\n ret\n"
 ".Lcleanup_try_begin:\n call APUnwindThrower\n nop\n .Lcleanup_try_end:\n movq %rax,48(%rsp)\n"
 "xorl %ecx,%ecx\n movq %rsp,%rdx\n call APUnwindFinally\n movq 48(%rsp),%rax\n addq $56,%rsp\n ret\n"
 ".globl APUnwindParentEnd\n APUnwindParentEnd:\n"
 ".seh_handler APUnwindLanguageHandler,@unwind\n .seh_handlerdata\n .long 1\n"
 ".rva .Lcleanup_try_begin,.Lcleanup_try_end,APUnwindFinally\n .long 0\n .text\n .seh_endproc\n"
 ".globl APUnwindCatch\n .def APUnwindCatch; .scl 2; .type 32; .endef\n"
 ".seh_proc APUnwindCatch\n APUnwindCatch:\n subq $40,%rsp\n .seh_stackalloc 40\n .seh_endprologue\n"
 ".Lcleanup_outer_begin:\n xorq %rcx,%rcx\n call APUnwindParent\n nop\n .Lcleanup_outer_end:\n xorq %rax,%rax\n addq $40,%rsp\n ret\n"
 ".Lcleanup_catch:\n movl $779,%eax\n addq $40,%rsp\n ret\n"
 ".seh_handler __C_specific_handler,@except\n .seh_handlerdata\n .long 1\n"
 ".rva .Lcleanup_outer_begin,.Lcleanup_outer_end,APUnwindFilter,.Lcleanup_catch\n .text\n .seh_endproc\n"
);
/* Sixty-four separate six-byte methods exercise the complete bounded installer. */
#define AP_BATCH_METHODS(X) X(0) X(1) X(2) X(3) X(4) X(5) X(6) X(7) X(8) X(9) X(10) X(11) X(12) X(13) X(14) X(15) X(16) X(17) X(18) X(19) X(20) X(21) X(22) X(23) X(24) X(25) X(26) X(27) X(28) X(29) X(30) X(31) X(32) X(33) X(34) X(35) X(36) X(37) X(38) X(39) X(40) X(41) X(42) X(43) X(44) X(45) X(46) X(47) X(48) X(49) X(50) X(51) X(52) X(53) X(54) X(55) X(56) X(57) X(58) X(59) X(60) X(61) X(62) X(63)
#define AP_BATCH_DECLARE(n) extern uint64_t APBatch##n(void);
AP_BATCH_METHODS(AP_BATCH_DECLARE)
#define AP_BATCH_ASSEMBLY(n) __asm__( \
 ".text\n .globl APBatch" #n "\n .def APBatch" #n "; .scl 2; .type 32; .endef\n" \
 ".seh_proc APBatch" #n "\n APBatch" #n ":\n .seh_endprologue\n" \
 "movl $" #n "+1,%eax\n ret\n .seh_endproc\n");
AP_BATCH_METHODS(AP_BATCH_ASSEMBLY)
#define AP_BATCH_ADDRESS(n) APBatch##n,
static uint64_t (*const batch_methods[])(void)={AP_BATCH_METHODS(AP_BATCH_ADDRESS)};
METHOD uint64_t APFrameParent(uint64_t n){
 if(APFrameTiny()!=77)return 0;
 uint32_t original_control=_mm_getcsr();
 BOOL control_ok=TRUE;
 for(uint32_t mode=0;mode<4;mode++){
  uint32_t control=(original_control&~0x603fu)|0x1fa4u|(mode<<13);
  if(APLeafSetControl(&control)!=control||APLeafGetControl()!=control)control_ok=FALSE;
 }
 _mm_setcsr(original_control);
 if(!control_ok)return 0;
 for(uint32_t i=0;i<64;i++)if(batch_methods[i]()!=(uint64_t)i+1)return 0;
 uint64_t source[16],copied[16],filled[16];
 for(uint32_t i=0;i<16;i++){source[i]=n+i+1;copied[i]=0;filled[i]=99;}
 uint64_t bytes=n%16,words=n%8;
 if(APFrameCopy(copied,source,bytes)!=copied)return 0;
 const unsigned char *actual=(const unsigned char *)copied,*expected=(const unsigned char *)source;
 for(uint32_t i=0;i<sizeof(copied);i++)if(actual[i]!=(i<bytes?expected[i]:0))return 0;
 if(APFrameFill(filled,n+7,words)!=n+7)return 0;
 for(uint32_t i=0;i<16;i++)if(filled[i]!=(i<words?n+7:99))return 0;
 uint64_t local=n+1;
 if(APLeafCompare(&local,n+1,n+2)!=n+1||local!=n+2)return 0;
 if(APLeafCompare(&local,n+1,999)!=n+2||local!=n+2)return 0;
 if(APLeafAtomic(&atomic_total,1)>=16016000)return 0;
 if(APLeafMath(n,11,13,17,19,23)!=((((n+11)*3)^13)+60))return 0;
 if(APFrameSignedAlloc(n)!=n+5)return 0;
 float input=(float)n-2.75f;
 if(APLeafConvert(input)!=(double)((int64_t)input+3))return 0;
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
LONG APFrameMemoryFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 return p && p->ExceptionRecord->ExceptionCode==EXCEPTION_ACCESS_VIOLATION?EXCEPTION_EXECUTE_HANDLER:EXCEPTION_CONTINUE_SEARCH;
}
extern uint64_t APFrameMemoryCatch(void);
__asm__(
 ".text\n .globl APLeafSetControl\n .def APLeafSetControl; .scl 2; .type 32; .endef\n"
 ".seh_proc APLeafSetControl\n APLeafSetControl:\n .seh_endprologue\n ldmxcsr (%rcx)\n movl (%rcx),%eax\n ret\n .seh_endproc\n"
 ".text\n .globl APLeafGetControl\n .def APLeafGetControl; .scl 2; .type 32; .endef\n"
 ".seh_proc APLeafGetControl\n APLeafGetControl:\n .seh_endprologue\n stmxcsr 8(%rsp)\n movl 8(%rsp),%eax\n ret\n .seh_endproc\n"
 ".text\n .globl APFrameTiny\n .def APFrameTiny; .scl 2; .type 32; .endef\n"
 ".seh_proc APFrameTiny\n APFrameTiny:\n .seh_endprologue\n movl $77,%eax\n ret\n .seh_endproc\n"
 ".text\n .globl APLeafMath\n .def APLeafMath; .scl 2; .type 32; .endef\n"
 ".seh_proc APLeafMath\n APLeafMath:\n .seh_endprologue\n"
 "movq %rcx,%rax\n movq %rdx,8(%rsp)\n addq 8(%rsp),%rax\n imulq $3,%rax,%rax\n xorq %r8,%rax\n"
 "rorq $1,%rax\n rolq $1,%rax\n addq %r9,%rax\n addq 40(%rsp),%rax\n addq 48(%rsp),%rax\n"
 "shlq $1,%rax\n shrq $1,%rax\n cmpq $0,%rax\n setg %r10b\n movzbl %r10b,%r10d\n addq %r10,%rax\n ret\n .seh_endproc\n"
 ".text\n .globl APLeafConvert\n .def APLeafConvert; .scl 2; .type 32; .endef\n"
 ".seh_proc APLeafConvert\n APLeafConvert:\n .seh_endprologue\n"
 "cvtss2sd %xmm0,%xmm0\n cvttsd2si %xmm0,%eax\n addl $3,%eax\n cvtsi2sd %eax,%xmm0\n ret\n .seh_endproc\n"
 ".text\n .globl APFrameSignedAlloc\n .def APFrameSignedAlloc; .scl 2; .type 32; .endef\n"
 ".seh_proc APFrameSignedAlloc\n APFrameSignedAlloc:\n pushq %rbp\n .seh_pushreg %rbp\n"
 "addq $-128,%rsp\n .seh_stackalloc 128\n movq %rsp,%rbp\n .seh_setframe %rbp,0\n .seh_endprologue\n"
 "movq %rcx,48(%rsp)\n call *__imp_GetCurrentThreadId(%rip)\n movq 48(%rsp),%rax\n addq $5,%rax\n leaq 128(%rbp),%rsp\n popq %rbp\n ret\n .seh_endproc\n"
 ".text\n .globl APFrameCopy\n .def APFrameCopy; .scl 2; .type 32; .endef\n"
 ".seh_proc APFrameCopy\n APFrameCopy:\n pushq %rsi\n .seh_pushreg %rsi\n pushq %rdi\n .seh_pushreg %rdi\n .seh_endprologue\n"
 "movq %rcx,%rdi\n movq %rdx,%rsi\n movq %rcx,%rax\n movq %r8,%rcx\n shrq $3,%rcx\n rep movsq\n"
 "movq %r8,%rcx\n andq $7,%rcx\n rep movsb\n popq %rdi\n popq %rsi\n ret\n .seh_endproc\n"
 ".text\n .globl APFrameFill\n .def APFrameFill; .scl 2; .type 32; .endef\n"
 ".seh_proc APFrameFill\n APFrameFill:\n pushq %rdi\n .seh_pushreg %rdi\n .seh_endprologue\n"
 "movq %rcx,%rdi\n movq %rdx,%rax\n movq %r8,%rcx\n rep stosq\n popq %rdi\n ret\n .seh_endproc\n"
 ".text\n .globl APLeafAtomic\n .def APLeafAtomic; .scl 2; .type 32; .endef\n"
 ".seh_proc APLeafAtomic\n APLeafAtomic:\n .seh_endprologue\n movq %rdx,%rax\n lock xaddq %rax,(%rcx)\n ret\n .seh_endproc\n"
 ".text\n .globl APLeafCompare\n .def APLeafCompare; .scl 2; .type 32; .endef\n"
 ".seh_proc APLeafCompare\n APLeafCompare:\n .seh_endprologue\n movq %rdx,%rax\n lock cmpxchgq %r8,(%rcx)\n ret\n .seh_endproc\n"
 ".text\n .globl APFrameCatch\n .def APFrameCatch; .scl 2; .type 32; .endef\n"
 ".seh_proc APFrameCatch\n APFrameCatch:\n"
 "subq $40,%rsp\n .seh_stackalloc 40\n .seh_endprologue\n"
 ".Lframe_try_begin:\n xorq %rcx,%rcx\n call APFrameParent\n nop\n nop\n"
 ".Lframe_try_end:\n xorq %rax,%rax\n addq $40,%rsp\n ret\n"
 ".Lframe_catch:\n movq $777,%rax\n addq $40,%rsp\n ret\n"
 ".seh_handler __C_specific_handler, @except\n .seh_handlerdata\n"
 ".long 1\n .rva .Lframe_try_begin,.Lframe_try_end,APFrameFilter,.Lframe_catch\n"
 ".text\n .seh_endproc\n"
 ".text\n .globl APFrameMemoryCatch\n .def APFrameMemoryCatch; .scl 2; .type 32; .endef\n"
 ".seh_proc APFrameMemoryCatch\n APFrameMemoryCatch:\n"
 "subq $56,%rsp\n .seh_stackalloc 56\n .seh_endprologue\n"
 ".Lmemory_try_begin:\n leaq 32(%rsp),%rcx\n xorq %rdx,%rdx\n movq $8,%r8\n call APFrameCopy\n nop\n"
 ".Lmemory_try_end:\n xorq %rax,%rax\n addq $56,%rsp\n ret\n"
 ".Lmemory_catch:\n movq $888,%rax\n addq $56,%rsp\n ret\n"
 ".seh_handler __C_specific_handler, @except\n .seh_handlerdata\n"
 ".long 1\n .rva .Lmemory_try_begin,.Lmemory_try_end,APFrameMemoryFilter,.Lmemory_catch\n"
 ".text\n .seh_endproc\n"
);
static HANDLE barrier;
static uint32_t iterations=10;
static DWORD WINAPI worker(void *unused){
 (void)unused;
 if(WaitForSingleObject(barrier,5000)!=WAIT_OBJECT_0)return 1;
 for(uint32_t i=0;i<iterations;i++)if(APFrameParent(i+1)!=17+2*i)return 2;
 if(APFrameCatch()!=777)return 3;
 if(APFrameMemoryCatch()!=888)return 4;
 if(APUnwindParent(7)!=10 || APUnwindParent(999)!=999 || APUnwindCatch()!=779 || cleanup_bad)return 6;
 return 0;
}
int wmain(int argc,wchar_t **argv){
 uint32_t threads=3,rounds=1;
 for(int i=1;i<argc;i+=2){
  if(i+1>=argc)return 2;
  if(!wcscmp(argv[i],L"--threads"))threads=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--iterations"))iterations=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--rounds"))rounds=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--delay-ms"))child_delay_ms=(DWORD)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--cleanup-throws"))cleanup_throws=(BOOL)wcstoul(argv[i+1],NULL,10);
  else return 2;
 }
 if(!threads||threads>16||!rounds||rounds>1000||!iterations||iterations>1000||child_delay_ms>5000)return 2;
 barrier=CreateEventW(NULL,TRUE,FALSE,NULL);if(!barrier)return 3;
 HANDLE handles[16];DWORD result=0;
 for(uint32_t round=0;round<rounds;round++){
 for(uint32_t i=0;i<threads;i++){handles[i]=CreateThread(NULL,0,worker,NULL,0,NULL);if(!handles[i])return 3;}
 SetEvent(barrier);
 for(uint32_t i=0;i<threads;i++){
  DWORD code=1;if(WaitForSingleObject(handles[i],20000)!=WAIT_OBJECT_0)return 4;
  GetExitCodeThread(handles[i],&code);CloseHandle(handles[i]);result|=code;
 }
 }
 CloseHandle(barrier);return result||atomic_total!=(uint64_t)threads*rounds*(iterations+1)||cleanup_count!=(LONG)(threads*rounds*2)||(cleanup_throws && collision_seen<(LONG)(threads*rounds))?5:0;
}
