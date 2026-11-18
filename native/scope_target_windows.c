/* Owned Win64 SEH target. Standard CRT scopes exercise search, same-frame
 * resumption, recursive targets, collided cleanup and continuation of a fault. */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>
#include <wchar.h>

enum {AP_SCOPE_EXCEPTION=0xe0434341u,AP_SCOPE_REPLACEMENT=0xe0434342u,AP_SCOPE_ESCAPE=0xe0434343u};
static BOOL abnormal_throws,normal_throws;
static volatile LONG cleanups,continued,bad;
#define METHOD __attribute__((noinline))
extern uint64_t APScopeCombined(uint64_t),APScopeRecursive(uint64_t),APScopeContinue(void),APScopeRoot(void);
METHOD uint64_t APScopeThrower(uint64_t n){
 if(!n)RaiseException(AP_SCOPE_EXCEPTION,0,0,NULL);
 if(n==888)RaiseException(AP_SCOPE_ESCAPE,0,0,NULL);
 return n+3;
}
METHOD uint64_t APScopeCleanup(uint64_t n){
 InterlockedIncrement(&cleanups);
 if(!n)RaiseException(AP_SCOPE_REPLACEMENT,0,0,NULL);
 return n;
}
void APScopeFinally(BOOL abnormal,void *frame){
 uint64_t n=*(uint64_t *)((unsigned char *)frame+32);
 if((abnormal && n!=0 && n!=888)||(!abnormal && n!=7))InterlockedExchange(&bad,1);
 (void)APScopeCleanup(n!=888 && ((abnormal && abnormal_throws)||(!abnormal && normal_throws))?0:1);
}
LONG APScopeFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 if(!p)return EXCEPTION_CONTINUE_SEARCH;
 return p->ExceptionRecord->ExceptionCode==AP_SCOPE_EXCEPTION || p->ExceptionRecord->ExceptionCode==AP_SCOPE_REPLACEMENT?EXCEPTION_EXECUTE_HANDLER:EXCEPTION_CONTINUE_SEARCH;
}
LONG APScopeRootFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 return p && p->ExceptionRecord->ExceptionCode==AP_SCOPE_ESCAPE?EXCEPTION_EXECUTE_HANDLER:EXCEPTION_CONTINUE_SEARCH;
}
LONG APScopeContinueFilter(EXCEPTION_POINTERS *p,void *frame){
 (void)frame;
 if(!p || p->ExceptionRecord->ExceptionCode!=EXCEPTION_ACCESS_VIOLATION)return EXCEPTION_CONTINUE_SEARCH;
 p->ContextRecord->Rip+=3; /* MOV RAX,[RCX] is exactly three bytes. */
 InterlockedIncrement(&continued);
 return EXCEPTION_CONTINUE_EXECUTION;
}
__asm__(
 ".text\n .globl APScopeCombined\n .def APScopeCombined; .scl 2; .type 32; .endef\n"
 ".seh_proc APScopeCombined\n APScopeCombined:\n subq $56,%rsp\n .seh_stackalloc 56\n .seh_endprologue\n"
 "movq %rcx,32(%rsp)\n cmpq $999,%rcx\n jne .Lscope_outer_begin\n movl $999,%eax\n addq $56,%rsp\n ret\n"
 ".Lscope_outer_begin:\n .Lscope_inner_begin:\n call APScopeThrower\n nop\n .Lscope_inner_end:\n movq %rax,48(%rsp)\n"
 "xorl %ecx,%ecx\n movq %rsp,%rdx\n call APScopeFinally\n movq 48(%rsp),%rax\n addq $1,%rax\n"
 ".Lscope_outer_end:\n addq $56,%rsp\n ret\n"
 ".Lscope_combined_catch:\n movl $811,%eax\n addq $56,%rsp\n ret\n"
 ".seh_handler __C_specific_handler,@except,@unwind\n .seh_handlerdata\n .long 2\n"
 ".rva .Lscope_inner_begin,.Lscope_inner_end,APScopeFinally\n .long 0\n"
 ".rva .Lscope_outer_begin,.Lscope_outer_end,APScopeFilter,.Lscope_combined_catch\n .text\n .seh_endproc\n"
 ".globl APScopeRecursive\n .def APScopeRecursive; .scl 2; .type 32; .endef\n"
 ".seh_proc APScopeRecursive\n APScopeRecursive:\n subq $40,%rsp\n .seh_stackalloc 40\n .seh_endprologue\n"
 ".Lscope_recursive_begin:\n testq %rcx,%rcx\n je .Lscope_recursive_throw\n subq $1,%rcx\n call APScopeRecursive\n addq $1,%rax\n jmp .Lscope_recursive_end\n"
 ".Lscope_recursive_throw:\n call APScopeThrower\n nop\n .Lscope_recursive_end:\n addq $40,%rsp\n ret\n"
 ".Lscope_recursive_catch:\n movl $50,%eax\n addq $40,%rsp\n ret\n"
 ".seh_handler __C_specific_handler,@except\n .seh_handlerdata\n .long 1\n"
 ".rva .Lscope_recursive_begin,.Lscope_recursive_end,APScopeFilter,.Lscope_recursive_catch\n .text\n .seh_endproc\n"
 ".globl APScopeContinue\n .def APScopeContinue; .scl 2; .type 32; .endef\n"
 ".seh_proc APScopeContinue\n APScopeContinue:\n subq $40,%rsp\n .seh_stackalloc 40\n .seh_endprologue\n"
 ".Lscope_continue_begin:\n xorq %rcx,%rcx\n movq (%rcx),%rax\n nop\n .Lscope_continue_end:\n movl $812,%eax\n addq $40,%rsp\n ret\n"
 ".Lscope_continue_catch:\n movl $0,%eax\n addq $40,%rsp\n ret\n"
 ".seh_handler __C_specific_handler,@except\n .seh_handlerdata\n .long 1\n"
 ".rva .Lscope_continue_begin,.Lscope_continue_end,APScopeContinueFilter,.Lscope_continue_catch\n .text\n .seh_endproc\n"
 ".globl APScopeRoot\n .def APScopeRoot; .scl 2; .type 32; .endef\n"
 ".seh_proc APScopeRoot\n APScopeRoot:\n subq $40,%rsp\n .seh_stackalloc 40\n .seh_endprologue\n"
 ".Lscope_root_begin:\n movl $888,%ecx\n call APScopeCombined\n nop\n .Lscope_root_end:\n xorl %eax,%eax\n addq $40,%rsp\n ret\n"
 ".Lscope_root_catch:\n movl $813,%eax\n addq $40,%rsp\n ret\n"
 ".seh_handler __C_specific_handler,@except\n .seh_handlerdata\n .long 1\n"
 ".rva .Lscope_root_begin,.Lscope_root_end,APScopeRootFilter,.Lscope_root_catch\n .text\n .seh_endproc\n"
);
METHOD uint64_t APScopeDriver(void){
 if(APScopeCombined(7)!=(normal_throws?811:11) || APScopeCombined(0)!=811 || APScopeCombined(999)!=999)return 1;
 if(APScopeRecursive(2)!=52 || APScopeContinue()!=812 || APScopeRoot()!=813 || bad)return 2;
 return 0;
}
static DWORD WINAPI worker(void *unused){(void)unused;return (DWORD)APScopeDriver();}
int wmain(int argc,wchar_t **argv){
 uint32_t threads=3;
 for(int i=1;i<argc;i+=2){
  if(i+1>=argc)return 2;
  if(!wcscmp(argv[i],L"--threads"))threads=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--cleanup-throws"))abnormal_throws=(BOOL)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--normal-cleanup-throws"))normal_throws=(BOOL)wcstoul(argv[i+1],NULL,10);
  else return 2;
 }
 if(!threads || threads>16)return 2;
 HANDLE handles[16];DWORD result=0;
 for(uint32_t i=0;i<threads;i++){handles[i]=CreateThread(NULL,0,worker,NULL,0,NULL);if(!handles[i])return 3;}
 for(uint32_t i=0;i<threads;i++){DWORD code=1;if(WaitForSingleObject(handles[i],5000)!=WAIT_OBJECT_0)return 4;GetExitCodeThread(handles[i],&code);CloseHandle(handles[i]);result|=code;}
 return result || bad || cleanups!=(LONG)(3*threads) || continued!=(LONG)threads?5:0;
}
