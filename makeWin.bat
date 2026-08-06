@echo off
setlocal EnableExtensions EnableDelayedExpansion

rem ============================================================================
rem  Apogee - Windows build script.
rem
rem  The cmd.exe counterpart to the Makefile, for machines without GNU make.
rem  The source of truth for the build is still `go build`; this just gives the
rem  common invocations one-word names.
rem
rem  Usage:  makeWin.bat [target] [args...]
rem  Run    `makeWin.bat help`    to list the targets.
rem ============================================================================

set "BINARY=apogee.exe"
set "PKG=./cmd/apogee"
set "MODULE=github.com/airiclenz/apogee"

rem The 6 release targets the cross-build invariant must stay green on.
set "CROSS_TARGETS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

pushd "%~dp0" || exit /b 1

set "TARGETS=build release run install test fmt vet cross check clean help"

set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=build"
if not "%~1"=="" shift

rem A CALL to a missing label aborts the whole script, so vet the target first.
set "VALID="
for %%T in (%TARGETS%) do if /i "%%T"=="%TARGET%" set "VALID=1"
if not defined VALID (
    echo ERROR: unknown target "%TARGET%".
    call :target_help
    popd & exit /b 1
)

rem A shell opened before Go was installed still has the old PATH, so fall back
rem to the default install location before giving up.
where go >nul 2>nul
if errorlevel 1 if exist "%ProgramFiles%\Go\bin\go.exe" set "PATH=%ProgramFiles%\Go\bin;%PATH%"
where go >nul 2>nul
if errorlevel 1 if exist "%LOCALAPPDATA%\Programs\Go\bin\go.exe" set "PATH=%LOCALAPPDATA%\Programs\Go\bin;%PATH%"

where go >nul 2>nul
if errorlevel 1 if /i not "%TARGET%"=="help" (
    echo ERROR: the Go toolchain is not on PATH.
    echo        Install Go 1.26+ ^(winget install --id GoLang.Go -e^) and reopen the shell.
    popd & exit /b 1
)

call :target_%TARGET% %1 %2 %3 %4 %5 %6 %7 %8 %9
set "RC=%ERRORLEVEL%"
popd
exit /b %RC%


rem ---------------------------------------------------------------- help -----
:target_help
echo Apogee - makeWin.bat targets:
echo    build     compile the binary to .\%BINARY%, then offer to install it ^(default^)
echo                 -y ^| -n   install without asking ^| never ask
echo    release   compile a trimmed, stripped, CGO-free binary
echo    run       build-and-run the binary; extra args are passed through
echo    install   build, then put %BINARY% on PATH so any shell can run it
echo                 optional: makeWin.bat install "D:\tools"
echo    test      run the full test suite ^(with -race when a C toolchain exists^)
echo    fmt       format all Go source in place
echo    vet       run go vet over the module
echo    cross     build every release target ^(CGO off^)
echo    check     the acceptance gate: fmt, vet, build, tests, ADR-0010, cross, --help
echo    clean     remove the built binary
exit /b 0


rem --------------------------------------------------------------- build -----
rem Build, then offer to install. `-y` installs without asking, `-n` skips the
rem question; with neither, an empty answer (or no console on stdin, as in CI)
rem means "no", so an unattended `makeWin.bat` still only builds.
:target_build
call :do_build || exit /b 1
call :ask_install %1 %2
exit /b %ERRORLEVEL%


rem ------------------------------------------------------------- release -----
:target_release
echo ==^> release build ^(CGO off, trimmed, stripped^)
set "CGO_ENABLED=0"
go build -trimpath -ldflags "-s -w" -o "%BINARY%" %PKG% || exit /b 1
echo built .\%BINARY%
exit /b 0


rem ----------------------------------------------------------------- run -----
:target_run
go run %PKG% %*
exit /b %ERRORLEVEL%


rem ------------------------------------------------------------- install -----
rem `go install` would drop the binary in %GOPATH%\bin whether or not that is on
rem PATH - and it would be a *different* binary from the one just built. So this
rem copies the artifact you built and, if its directory is not yet on PATH, adds
rem it to the per-user PATH (HKCU\Environment - no admin, no elevation needed).
:target_install
call :do_build || exit /b 1
call :do_install %1
exit /b %ERRORLEVEL%


rem ------------------------------------------------------- build (shared) ----
:do_build
echo ==^> go build -o %BINARY% %PKG%
go build -o "%BINARY%" %PKG% || exit /b 1
echo built .\%BINARY%
exit /b 0


rem ------------------------------------------------- install: the question ---
rem Args: any order of -y/-n and an explicit destination directory.
:ask_install
set "AUTO="
set "SKIP="
set "IDIR="
call :ask_arg %1
call :ask_arg %2
if defined SKIP exit /b 0

if not defined AUTO (
    echo(
    set "ANSWER="
    set /p "ANSWER=Install %BINARY% so it runs from any shell? [y/N] "
    if /i not "!ANSWER!"=="y" if /i not "!ANSWER!"=="yes" (
        echo not installed - run "makeWin.bat install" whenever you want it.
        exit /b 0
    )
)
call :do_install "%IDIR%"
exit /b %ERRORLEVEL%

:ask_arg
if "%~1"=="" exit /b 0
if /i "%~1"=="-y"   (set "AUTO=1" & exit /b 0)
if /i "%~1"=="y"    (set "AUTO=1" & exit /b 0)
if /i "%~1"=="yes"  (set "AUTO=1" & exit /b 0)
if /i "%~1"=="-n"   (set "SKIP=1" & exit /b 0)
if /i "%~1"=="n"    (set "SKIP=1" & exit /b 0)
if /i "%~1"=="no"   (set "SKIP=1" & exit /b 0)
set "IDIR=%~1"
exit /b 0


rem ----------------------------------------------- install: doing the work ---
rem Destination, in order: the directory given as %1, else GOBIN, else
rem %GOPATH%\bin (already on PATH for most Go developers), else a private
rem %LOCALAPPDATA%\Programs\apogee.
:do_install
set "DEST=%~1"
if not defined DEST for /f "delims=" %%D in ('go env GOBIN 2^>nul') do set "DEST=%%D"
if not defined DEST for /f "delims=" %%D in ('go env GOPATH 2^>nul') do set "DEST=%%D\bin"
if not defined DEST set "DEST=%LOCALAPPDATA%\Programs\apogee"
if "!DEST:~-1!"=="\" set "DEST=!DEST:~0,-1!"

if not exist "!DEST!\" (
    mkdir "!DEST!" 2>nul
    if errorlevel 1 (
        echo ERROR: cannot create !DEST!.
        echo        Pick another directory:  makeWin.bat install "D:\tools"
        exit /b 1
    )
)

echo ==^> installing to !DEST!\%BINARY%
copy /y "%BINARY%" "!DEST!\%BINARY%" >nul
if errorlevel 1 (
    echo ERROR: could not copy %BINARY% to !DEST!.
    echo        Close any running apogee ^(a running .exe is locked^) and retry,
    echo        or install elsewhere:  makeWin.bat install "D:\tools"
    exit /b 1
)

call :is_on_path "!DEST!"
if errorlevel 1 (
    call :add_to_path "!DEST!" || exit /b 1
) else (
    echo !DEST! is already on PATH.
)

"!DEST!\%BINARY%" --version >nul 2>nul
if errorlevel 1 echo WARNING: the installed binary did not run cleanly - check !DEST!\%BINARY%.
echo installed: !DEST!\%BINARY%
exit /b 0


rem Exit 0 when the directory in %1 is already an entry of this process' PATH.
:is_on_path
set "NEEDLE=%~1"
if not defined NEEDLE exit /b 1
if "!NEEDLE:~-1!"=="\" set "NEEDLE=!NEEDLE:~0,-1!"
set "HAY=;!PATH!;"
set "HAY=!HAY:\;=;!"
set "PROBE=!HAY:;%NEEDLE%;=!"
if "!PROBE!"=="!HAY!" exit /b 1
exit /b 0


rem Append %1 to the per-user PATH. Written with `reg add` rather than `setx`
rem on purpose: setx silently truncates values over 1024 characters, which is
rem how PATHs get destroyed. The existing value's type (REG_EXPAND_SZ, so that
rem entries like %%USERPROFILE%%\bin keep expanding) is preserved.
:add_to_path
set "DIR=%~1"
set "UKIND=REG_EXPAND_SZ"
set "UPATH="
for /f "tokens=1,2,*" %%A in ('reg query "HKCU\Environment" /v Path 2^>nul') do (
    if /i "%%~A"=="Path" (
        set "UKIND=%%~B"
        set "UPATH=%%~C"
    )
)

if defined UPATH (
    rem Already added by an earlier run - this shell just has not seen it yet.
    set "HAY=;!UPATH!;"
    set "HAY=!HAY:\;=;!"
    set "PROBE=!HAY:;%DIR%;=!"
    if not "!PROBE!"=="!HAY!" (
        echo !DIR! is already in your user PATH - open a new shell to pick it up.
        exit /b 0
    )
    set "NEWPATH=!UPATH!"
    if "!NEWPATH:~-1!"==";" set "NEWPATH=!NEWPATH:~0,-1!"
    set "NEWPATH=!NEWPATH!;!DIR!"
) else (
    set "NEWPATH=!DIR!"
)

echo ==^> adding !DIR! to your user PATH
reg add "HKCU\Environment" /v Path /t !UKIND! /d "!NEWPATH!" /f >nul
if errorlevel 1 (
    echo ERROR: could not update the user PATH ^(HKCU\Environment^).
    echo        Add !DIR! to PATH by hand to finish the install.
    exit /b 1
)
call :broadcast_env
rem The broadcast above only reaches Explorer. A program that was already
rem running - VS Code, an open Windows Terminal - keeps the environment it
rem started with and hands that stale copy to every shell it spawns, so be
rem explicit that a new tab in the same window will not do.
echo PATH updated. Programs that are already running kept the old environment:
echo quit your terminal app completely ^(a new tab in the same window is not
echo enough^), and restart VS Code if you use its integrated terminal.
echo    for the current shell only, cmd:  set "PATH=%%PATH%%;!DIR!"
echo    for the current shell only, pwsh: $env:PATH += ";!DIR!"
exit /b 0


rem Tell Explorer the environment changed, so a terminal opened from the taskbar
rem inherits the new PATH without a sign-out. Best effort: skipped if PowerShell
rem is unavailable, and a failure here costs a restart, not the install. The C#
rem source is stitched together with [char]34 so the batch line needs no quotes.
:broadcast_env
where powershell >nul 2>nul || exit /b 0
powershell -NoProfile -Command "$s='[DllImport('+[char]34+'user32.dll'+[char]34+')]public static extern IntPtr SendMessageTimeout(IntPtr h,int m,IntPtr w,string l,int f,int t,out IntPtr r);'; $a=Add-Type -MemberDefinition $s -Name W -Namespace E -PassThru; $r=[IntPtr]::Zero; [void]$a::SendMessageTimeout([IntPtr]65535,26,[IntPtr]::Zero,'Environment',2,3000,[ref]$r)" >nul 2>nul
exit /b 0


rem ---------------------------------------------------------------- test -----
:target_test
where gcc >nul 2>nul
if errorlevel 1 (
    echo NOTE: no C toolchain on PATH - running without -race.
    echo       Install one ^(e.g. winget install --id MSYS2.MSYS2^) for the race detector.
    go test -count=1 ./...
) else (
    go test -race -count=1 ./...
)
exit /b %ERRORLEVEL%


rem ----------------------------------------------------------------- fmt -----
:target_fmt
gofmt -w .
exit /b %ERRORLEVEL%


rem ----------------------------------------------------------------- vet -----
:target_vet
go vet ./...
exit /b %ERRORLEVEL%


rem --------------------------------------------------------------- cross -----
:target_cross
set "CROSS_OUT=%TEMP%\apogee-cross"
if not exist "%CROSS_OUT%" mkdir "%CROSS_OUT%" || exit /b 1
set "CGO_ENABLED=0"
set "COUNT=0"
for %%T in (%CROSS_TARGETS%) do (
    for /f "tokens=1,2 delims=/" %%A in ("%%T") do (
        echo    -^> %%A/%%B
        set "GOOS=%%A"
        set "GOARCH=%%B"
        go build -o "%CROSS_OUT%\" ./... || (
            rmdir /s /q "%CROSS_OUT%" 2>nul
            exit /b 1
        )
        set /a COUNT+=1
    )
)
set "GOOS="
set "GOARCH="
rmdir /s /q "%CROSS_OUT%" 2>nul
echo cross-build OK ^(!COUNT! targets^)
exit /b 0


rem --------------------------------------------------------------- check -----
:target_check
echo ==^> gofmt ^(must be empty^)
set "UNFORMATTED="
for /f "delims=" %%F in ('gofmt -l .') do set "UNFORMATTED=!UNFORMATTED! %%F"
if not "!UNFORMATTED!"=="" (
    echo needs gofmt:!UNFORMATTED!
    exit /b 1
)

echo ==^> go vet
go vet ./... || exit /b 1

echo ==^> go build ./...
go build ./... || exit /b 1

echo ==^> go test ./...
call :target_test || exit /b 1

echo ==^> ADR-0010 invariant ^(internal\ must not import the root module path^)
findstr /s /m /c:"\"%MODULE%\"" internal\*.go >nul 2>nul
if not errorlevel 1 (
    findstr /s /m /c:"\"%MODULE%\"" internal\*.go
    echo ADR-0010 violation: internal\ imports the root module path
    exit /b 1
)

echo ==^> cross-build
call :target_cross || exit /b 1

echo ==^> apogee --help ^(exit 0^)
go run %PKG% --help >nul || exit /b 1

echo all gates passed
exit /b 0


rem --------------------------------------------------------------- clean -----
:target_clean
if exist "%BINARY%" del /q "%BINARY%"
echo removed .\%BINARY%
exit /b 0
