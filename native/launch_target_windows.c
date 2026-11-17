/* A plain Windows program: no profiler imports, SDK, initialization or event
 * calls. The collector must load and initialize everything before this runs. */
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>
#include <wchar.h>
#include <stdio.h>
#include "leaf_lab_asm.h"

static uint32_t iterations=10;
static HANDLE barrier;
static const wchar_t *marker;
static void finalize(void){
 if(APHookSum(0,11,13,17,19,23)!=777) ExitProcess(9);
 if(marker){FILE *f=_wfopen(marker,L"wb");if(!f)ExitProcess(9);fputs("finalized",f);fclose(f);}
}
static DWORD WINAPI worker(void *unused){
 (void)unused;
 if(WaitForSingleObject(barrier,5000)!=WAIT_OBJECT_0) return 1;
 for(uint32_t i=0;i<iterations;i++){
  SetLastError(0x12345678);
  if(APHookSum(i%2?0:7+i,11,13,17,19,23)!=(i%2?777:125+i)||GetLastError()!=0x12345678)return 2;
  Sleep(1);
 }
 return 0;
}
int wmain(int argc,wchar_t **argv){
 uint32_t threads=3;
 for(int i=1;i<argc;i+=2){
  if(i+1>=argc)return 2;
  if(!wcscmp(argv[i],L"--iterations"))iterations=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--threads"))threads=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--marker"))marker=argv[i+1];
  else return 2;
 }
 if(!iterations||iterations>100000||!threads||threads>16)return 2;
 if(atexit(finalize))return 3;
 barrier=CreateEventW(NULL,TRUE,FALSE,NULL);if(!barrier)return 3;
 HANDLE handles[16];DWORD result=0;
 for(uint32_t i=0;i<threads;i++){handles[i]=CreateThread(NULL,0,worker,NULL,0,NULL);if(!handles[i])return 3;}
 SetEvent(barrier);
 for(uint32_t i=0;i<threads;i++){
  DWORD code=1;if(WaitForSingleObject(handles[i],20000)!=WAIT_OBJECT_0)return 4;
  GetExitCodeThread(handles[i],&code);CloseHandle(handles[i]);result|=code;
 }
 CloseHandle(barrier);
 return result?5:0;
}
