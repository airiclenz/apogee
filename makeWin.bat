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
echo    build     compile the binary to .\%BINARY%  ^(default^)
echo    release   compile a trimmed, stripped, CGO-free binary
echo    run       build-and-run the binary; extra args are passed through
echo    install   install the binary into %%GOPATH%%\bin
echo    test      run the full test suite ^(with -race when a C toolchain exists^)
echo    fmt       format all Go source in place
echo    vet       run go vet over the module
echo    cross     build every release target ^(CGO off^)
echo    check     the acceptance gate: fmt, vet, build, tests, ADR-0010, cross, --help
echo    clean     remove the built binary
exit /b 0


rem --------------------------------------------------------------- build -----
:target_build
echo ==^> go build -o %BINARY% %PKG%
go build -o "%BINARY%" %PKG% || exit /b 1
echo built .\%BINARY%
exit /b 0


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
:target_install
echo ==^> go install %PKG%
go install %PKG%
exit /b %ERRORLEVEL%


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
