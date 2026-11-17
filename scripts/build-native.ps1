param([string]$Output = "native-dist")
$ErrorActionPreference = "Stop"
$compiler = Get-Command gcc -ErrorAction SilentlyContinue
if (!$compiler) {
    foreach ($candidate in @('C:\msys64\mingw64\bin\gcc.exe', 'C:\mingw64\bin\gcc.exe')) {
        if (Test-Path $candidate) { $compiler = Get-Item $candidate; break }
    }
}
if (!$compiler) { throw 'Windows x64 GCC/MinGW is required to build the optional native producer' }
$gcc = $compiler.Source
if (!$gcc) { $gcc = $compiler.FullName }
New-Item -ItemType Directory -Force $Output | Out-Null
$Output = (Resolve-Path $Output).Path
& $gcc -m64 -std=c11 -O2 -Wall -Wextra -Werror -shared -static-libgcc -o (Join-Path $Output 'AntiPessimizerNative.dll') ./native/producer_windows.c "-Wl,--out-implib,$Output/libAPNative.a"
if ($LASTEXITCODE -ne 0) { throw 'Native producer build failed' }
& $gcc -m64 -std=c11 -O1 -Wall -Wextra -Werror -municode -finstrument-functions -finstrument-functions-exclude-file-list=include -fno-optimize-sibling-calls -static-libgcc -o (Join-Path $Output 'APNativeLab.exe') ./native/lab_windows.c (Join-Path $Output 'libAPNative.a')
if ($LASTEXITCODE -ne 0) { throw 'Controlled native workload build failed' }
& $gcc -m64 -std=c11 -O1 -Wall -Wextra -Werror -municode -static-libgcc -o (Join-Path $Output 'APHookLab.exe') ./native/hook_lab_windows.c (Join-Path $Output 'libAPNative.a')
if ($LASTEXITCODE -ne 0) { throw 'Controlled hook workload build failed' }
& $gcc -m64 -std=c11 -O1 -Wall -Wextra -Werror -municode -static-libgcc -o (Join-Path $Output 'APLaunchTarget.exe') ./native/launch_target_windows.c
if ($LASTEXITCODE -ne 0) { throw 'Unmodified launch target build failed' }
Write-Host "Native producer and controlled workload built in $Output"
