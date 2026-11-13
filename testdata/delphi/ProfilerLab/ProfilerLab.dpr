program ProfilerLab;

{$APPTYPE CONSOLE}
{$IFNDEF WIN64}
  {$MESSAGE FATAL 'Build this fixture for Windows 64-bit.'}
{$ENDIF}

uses
  System.SysUtils,
  LabWorkload in 'LabWorkload.pas';

begin
  try
    RunLab;
  except
    on E: Exception do
    begin
      Writeln(ErrOutput, E.ClassName + ': ' + E.Message);
      ExitCode := 1;
    end;
  end;
end.
