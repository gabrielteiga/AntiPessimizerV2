#ifndef AP_LEAF_LAB_ASM_H
#define AP_LEAF_LAB_ASM_H
#include <stdint.h>
extern uint64_t APHookSum(uint64_t,uint64_t,uint64_t,uint64_t,uint64_t,uint64_t);
extern const unsigned char APHookSumEnd[];

__asm__(
 ".text\n"
 ".p2align 3\n .LAPHookConstant: .quad 35\n"
 ".globl APHookSum\n"
 ".def APHookSum; .scl 2; .type 32; .endef\n"
 "APHookSum:\n"
 ".rept 16\n nop\n .endr\n"
 "movl $4096,%r10d\n"
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

#endif
