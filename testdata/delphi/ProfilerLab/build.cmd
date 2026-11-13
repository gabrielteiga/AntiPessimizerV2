@echo off
setlocal
pushd "%~dp0"
if not defined BDS goto :missing
if not exist "%BDS%\bin\dcc64.exe" goto :missing
where msbuild >nul 2>nul
if errorlevel 1 goto :missing
if not exist logs mkdir logs
msbuild ProfilerLab.dproj /t:Rebuild /p:Config=Symbols /p:Platform=Win64 /nologo > logs\build-symbols.txt 2>&1
if errorlevel 1 goto :failed
msbuild ProfilerLab.dproj /t:Rebuild /p:Config=NoSymbols /p:Platform=Win64 /nologo > logs\build-nosymbols.txt 2>&1
if errorlevel 1 goto :failed
powershell -NoProfile -File "%~dp0verify.ps1"
if errorlevel 1 goto :failed
echo Builds and checks passed. Outputs: bin, logs.
popd
exit /b 0
:missing
echo Open the RAD Studio 10.4 Command Prompt, then run build.cmd.
popd
exit /b 1
:failed
echo Build or verification failed. See logs.
popd
exit /b 1
