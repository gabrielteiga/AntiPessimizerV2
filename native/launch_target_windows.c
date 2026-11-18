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
static const wchar_t *report_path;
static DWORD delay_ms=1;
typedef struct {uint64_t ticks,checksum;} WorkerCost;
static WorkerCost costs[16];
static void finalize(void){
 if(APHookSum(0,11,13,17,19,23)!=777) ExitProcess(9);
 if(marker){FILE *f=_wfopen(marker,L"wb");if(!f)ExitProcess(9);fputs("finalized",f);fclose(f);}
}
static DWORD WINAPI worker(void *unused){
 uint32_t index=(uint32_t)(uintptr_t)unused;
 if(WaitForSingleObject(barrier,5000)!=WAIT_OBJECT_0) return 1;
 LARGE_INTEGER begin={0},end={0};
 if(report_path && !QueryPerformanceCounter(&begin))return 6;
 uint64_t checksum=0;
 for(uint32_t i=0;i<iterations;i++){
  SetLastError(0x12345678);
  uint64_t value=APHookSum(i%2?0:7+i,11,13,17,19,23);
  if(value!=(i%2?777:125+i)||GetLastError()!=0x12345678)return 2;
  checksum+=value;
  if(delay_ms)Sleep(delay_ms);
 }
 if(report_path){
  if(!QueryPerformanceCounter(&end)||end.QuadPart<=begin.QuadPart)return 6;
  costs[index]=(WorkerCost){(uint64_t)(end.QuadPart-begin.QuadPart),checksum};
 }
 return 0;
}
static BOOL report_cost(uint32_t threads,uint64_t elapsed){
 LARGE_INTEGER frequency;
 if(!QueryPerformanceFrequency(&frequency)||frequency.QuadPart<=0)return FALSE;
 FILE *f=_wfopen(report_path,L"wb");if(!f)return FALSE;
 BOOL ok=fprintf(f,"{\"version\":1,\"clock\":\"qpc\",\"frequency_hz\":%llu,\"iterations\":%u,\"threads\":%u,\"elapsed_ticks\":%llu,\"workers\":[",(unsigned long long)frequency.QuadPart,iterations,threads,(unsigned long long)elapsed)>0;
 for(uint32_t i=0;i<threads;i++)if(fprintf(f,"%s{\"ticks\":%llu,\"checksum\":%llu}",i?",":"",(unsigned long long)costs[i].ticks,(unsigned long long)costs[i].checksum)<0)ok=FALSE;
 if(fputs("]}\n",f)==EOF)ok=FALSE;
 if(fclose(f))ok=FALSE;
 return ok;
}
int wmain(int argc,wchar_t **argv){
 uint32_t threads=3;
 for(int i=1;i<argc;i+=2){
  if(i+1>=argc)return 2;
  if(!wcscmp(argv[i],L"--iterations"))iterations=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--threads"))threads=(uint32_t)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--marker"))marker=argv[i+1];
  else if(!wcscmp(argv[i],L"--delay-ms"))delay_ms=(DWORD)wcstoul(argv[i+1],NULL,10);
  else if(!wcscmp(argv[i],L"--report"))report_path=argv[i+1];
  else return 2;
 }
 if(!iterations||iterations>100000||!threads||threads>16||delay_ms>1000)return 2;
 if(atexit(finalize))return 3;
 barrier=CreateEventW(NULL,TRUE,FALSE,NULL);if(!barrier)return 3;
 HANDLE handles[16];DWORD result=0;
 for(uint32_t i=0;i<threads;i++){handles[i]=CreateThread(NULL,0,worker,(void *)(uintptr_t)i,0,NULL);if(!handles[i])return 3;}
 LARGE_INTEGER begin={0},end={0};
 if(report_path && !QueryPerformanceCounter(&begin))return 6;
 SetEvent(barrier);
 for(uint32_t i=0;i<threads;i++){
  DWORD code=1;if(WaitForSingleObject(handles[i],20000)!=WAIT_OBJECT_0)return 4;
  GetExitCodeThread(handles[i],&code);CloseHandle(handles[i]);result|=code;
 }
 if(report_path){
  if(!QueryPerformanceCounter(&end)||end.QuadPart<=begin.QuadPart||!report_cost(threads,(uint64_t)(end.QuadPart-begin.QuadPart)))result|=6;
 }
 CloseHandle(barrier);
 return result?5:0;
}
