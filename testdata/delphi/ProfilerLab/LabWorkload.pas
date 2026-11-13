unit LabWorkload;

interface

procedure RunLab;

implementation

{$O+}
{$INLINE OFF}
{$Q+}
{$R+}

uses
  Winapi.Windows,
  System.SysUtils,
  System.Classes,
  System.SyncObjs;

const
  DefaultIterations = 50000000;

type
  TWorkKind = (wkShift, wkModulo);

  TLabLog = class
  private
    FLock: TCriticalSection;
    FFrequency: Int64;
  public
    constructor Create;
    destructor Destroy; override;
    procedure Emit(const EventName, Phase, Worker: string;
      Units, Checksum: UInt64; const Detail: string = '');
    property Frequency: Int64 read FFrequency;
  end;

  TCPUWorker = class(TThread)
  private
    FLog: TLabLog;
    FGate: TEvent;
    FPhase: string;
    FKind: TWorkKind;
    FIterations: Integer;
    FChecksum: UInt64;
  protected
    procedure Execute; override;
  public
    constructor Create(Log: TLabLog; Gate: TEvent; const Phase: string;
      Kind: TWorkKind; Iterations: Integer);
    property Checksum: UInt64 read FChecksum;
  end;

// No inline and no I/O in these loops. Returned state makes the work observable.
function CPUHotspotA(Iterations: Integer): UInt64;
var
  I: Integer;
  State: Cardinal;
begin
  State := 1;
  for I := 1 to Iterations do
  begin
    State := State xor (State shl 13);
    State := State xor (State shr 17);
    State := State xor (State shl 5);
  end;
  Result := State;
end;

function CPUHotspotB(Iterations: Integer): UInt64;
var
  I: Integer;
  State: UInt64;
begin
  State := 1;
  for I := 1 to Iterations do
    State := (State * 48271) mod 2147483647;
  Result := State;
end;

procedure WaitHotspot(Milliseconds: Cardinal);
begin
  Sleep(Milliseconds);
end;

constructor TLabLog.Create;
begin
  inherited Create;
  FLock := TCriticalSection.Create;
  if not QueryPerformanceFrequency(FFrequency) then
    RaiseLastOSError;
  Writeln('utc', #9, 'qpc', #9, 'pid', #9, 'tid', #9, 'event', #9,
    'phase', #9, 'worker', #9, 'iterations', #9, 'checksum', #9, 'detail');
end;

destructor TLabLog.Destroy;
begin
  FLock.Free;
  inherited Destroy;
end;

procedure TLabLog.Emit(const EventName, Phase, Worker: string;
  Units, Checksum: UInt64; const Detail: string);
var
  UTC: TSystemTime;
  Counter: Int64;
  Stamp: string;
begin
  // Serialize complete rows. Logging occurs only at phase boundaries.
  FLock.Acquire;
  try
    GetSystemTime(UTC);
    if not QueryPerformanceCounter(Counter) then
      RaiseLastOSError;
    Stamp := Format('%.4d-%.2d-%.2dT%.2d:%.2d:%.2d.%.3dZ',
      [UTC.wYear, UTC.wMonth, UTC.wDay, UTC.wHour, UTC.wMinute,
       UTC.wSecond, UTC.wMilliseconds]);
    Writeln(Stamp, #9, Counter, #9, GetCurrentProcessId, #9,
      GetCurrentThreadId, #9, EventName, #9, Phase, #9, Worker, #9,
      Units, #9, Checksum, #9, Detail);
    Flush(Output);
  finally
    FLock.Release;
  end;
end;

constructor TCPUWorker.Create(Log: TLabLog; Gate: TEvent;
  const Phase: string; Kind: TWorkKind; Iterations: Integer);
begin
  inherited Create(True);
  FreeOnTerminate := False;
  FLog := Log;
  FGate := Gate;
  FPhase := Phase;
  FKind := Kind;
  FIterations := Iterations;
end;

procedure TCPUWorker.Execute;
var
  Name: string;
begin
  if FKind = wkShift then
    Name := 'A'
  else
    Name := 'B';
  if FGate.WaitFor(INFINITE) <> wrSignaled then
    raise Exception.Create('Worker start gate failed');
  FLog.Emit('work_begin', FPhase, Name, 0, 0);
  if FKind = wkShift then
    FChecksum := CPUHotspotA(FIterations)
  else
    FChecksum := CPUHotspotB(FIterations);
  FLog.Emit('work_end', FPhase, Name, UInt64(FIterations), FChecksum);
end;

procedure RunCPUPhase(Log: TLabLog; const Phase: string;
  UseA, UseB: Boolean; Iterations: Integer;
  out ChecksumA, ChecksumB: UInt64);
var
  Gate: TEvent;
  A, B: TCPUWorker;
begin
  ChecksumA := 0;
  ChecksumB := 0;
  A := nil;
  B := nil;
  Gate := TEvent.Create(nil, True, False, '');
  try
    Log.Emit('phase_begin', Phase, 'main', 0, 0);
    if UseA then
    begin
      A := TCPUWorker.Create(Log, Gate, Phase, wkShift, Iterations);
      A.Start;
    end;
    if UseB then
    begin
      B := TCPUWorker.Create(Log, Gate, Phase, wkModulo, Iterations);
      B.Start;
    end;
    Gate.SetEvent;
    if Assigned(A) then
      A.WaitFor;
    if Assigned(B) then
      B.WaitFor;
    // WaitFor does not rethrow worker exceptions. Never report a failed run as OK.
    if Assigned(A) then
    begin
      if Assigned(A.FatalException) then
        raise Exception.Create('Worker A failed; phase=' + Phase);
      ChecksumA := A.Checksum;
    end;
    if Assigned(B) then
    begin
      if Assigned(B.FatalException) then
        raise Exception.Create('Worker B failed; phase=' + Phase);
      ChecksumB := B.Checksum;
    end;
    Log.Emit('phase_end', Phase, 'main', 0, 0);
  finally
    // Release a started worker even when creation of its peer fails.
    // Each worker performs finite work; Free joins before the gate/log are freed.
    Gate.SetEvent;
    A.Free;
    B.Free;
    Gate.Free;
  end;
end;

procedure CheckEqual(const Name: string; Actual, Expected: UInt64);
begin
  if Actual <> Expected then
    raise Exception.CreateFmt('%s: expected %s, got %s',
      [Name, UIntToStr(Expected), UIntToStr(Actual)]);
end;

procedure SelfTest(Log: TLabLog);
var
  A, B: UInt64;
begin
  CheckEqual('A(0)', CPUHotspotA(0), 1);
  CheckEqual('A(1)', CPUHotspotA(1), 270369);
  CheckEqual('A(2)', CPUHotspotA(2), 67634689);
  CheckEqual('A(1000)', CPUHotspotA(1000), 269958183);
  CheckEqual('B(0)', CPUHotspotB(0), 1);
  CheckEqual('B(1)', CPUHotspotB(1), 48271);
  CheckEqual('B(2)', CPUHotspotB(2), 182605794);
  CheckEqual('B(1000)', CPUHotspotB(1000), 429183498);
  RunCPUPhase(Log, 'selftest_parallel', True, True, 1000, A, B);
  CheckEqual('worker A', A, 269958183);
  CheckEqual('worker B', B, 429183498);
  Log.Emit('selftest_pass', 'selftest', 'main', 0, 0, 'checks=10');
end;

procedure RunLab;
var
  Iterations, WaitMS, I, Value: Integer;
  Option: string;
  Log: TLabLog;
  SingleA, SingleB, ParallelA, ParallelB, Unused: UInt64;
begin
  Iterations := DefaultIterations;
  WaitMS := 10000;
  if (ParamCount = 1) and (ParamStr(1) = '--help') then
  begin
    Writeln('ProfilerLab [--iterations 1..1000000000] [--wait-ms 0..120000]');
    Writeln('ProfilerLab --self-test');
    Exit;
  end;
  if (ParamCount = 1) and (ParamStr(1) = '--self-test') then
  begin
    Log := TLabLog.Create;
    try
      SelfTest(Log);
    finally
      Log.Free;
    end;
    Exit;
  end;
  I := 1;
  while I <= ParamCount do
  begin
    Option := ParamStr(I);
    if (Option <> '--iterations') and (Option <> '--wait-ms') then
      raise Exception.Create('Unknown option: ' + Option);
    if I = ParamCount then
      raise Exception.Create('Missing value for ' + Option);
    if not TryStrToInt(ParamStr(I + 1), Value) then
      raise Exception.Create('Invalid integer for ' + Option);
    if Option = '--iterations' then
    begin
      if (Value < 1) or (Value > 1000000000) then
        raise Exception.Create('iterations must be in 1..1000000000');
      Iterations := Value;
    end
    else
    begin
      if (Value < 0) or (Value > 120000) then
        raise Exception.Create('wait-ms must be in 0..120000');
      WaitMS := Value;
    end;
    Inc(I, 2);
  end;
  Log := TLabLog.Create;
  try
    Log.Emit('session_begin', 'startup', 'main', UInt64(Iterations), 0,
      'qpc_frequency=' + IntToStr(Log.Frequency) +
      ';module_base=0x' + IntToHex(UInt64(GetModuleHandle(nil)), 16) +
      ';CPUHotspotA=0x' + IntToHex(UInt64(NativeUInt(@CPUHotspotA)), 16) +
      ';CPUHotspotB=0x' + IntToHex(UInt64(NativeUInt(@CPUHotspotB)), 16) +
      ';compiler=' + FloatToStr(CompilerVersion) + ';wait_ms=' + IntToStr(WaitMS));
    WaitHotspot(Cardinal(WaitMS));
    Log.Emit('phase_begin', 'idle', 'main', 0, 0);
    WaitHotspot(2000);
    Log.Emit('phase_end', 'idle', 'main', 0, 0);
    RunCPUPhase(Log, 'cpu_a', True, False, Iterations, SingleA, Unused);
    RunCPUPhase(Log, 'cpu_b', False, True, Iterations, Unused, SingleB);
    RunCPUPhase(Log, 'parallel', True, True, Iterations, ParallelA, ParallelB);
    CheckEqual('serial/parallel A', ParallelA, SingleA);
    CheckEqual('serial/parallel B', ParallelB, SingleB);
    Log.Emit('session_end', 'complete', 'main', 0, 0, 'checksums_match=true');
  finally
    Log.Free;
  end;
end;

end.
