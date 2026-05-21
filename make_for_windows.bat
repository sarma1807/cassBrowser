@echo off
setlocal EnableDelayedExpansion

:: Change to script directory
cd /d "%~dp0"
set "SCRIPT_NAME=%~nx0"

:: ============================================================
:: Load configuration from app.properties
:: ============================================================
set "BINARY_NAME=cassBrowser"
set "APP_DISPLAY=Cassandra Browser"
set "APP_VER=1.0"
set "APP_DATE=20-May-2026"
set "ENCRYPT_CONN=yes"

if exist "app.properties" (
    for /f "usebackq eol=# tokens=1,* delims==" %%A in ("app.properties") do (
        if "%%A"=="APP_COMPILE_NAME"           set "BINARY_NAME=%%B"
        if "%%A"=="APP_DISPLAY_NAME"           set "APP_DISPLAY=%%B"
        if "%%A"=="APP_VERSION"                set "APP_VER=%%B"
        if "%%A"=="APP_VERSION_DATE"           set "APP_DATE=%%B"
        if "%%A"=="ENCRYPT_CONNECTION_DETAILS" set "ENCRYPT_CONN=%%B"
    )
    set "BINARY_NAME=!BINARY_NAME: =!"
    set "APP_VER=!APP_VER: =!"
    set "APP_DATE=!APP_DATE: =!"
    set "ENCRYPT_CONN=!ENCRYPT_CONN: =!"
) else (
    echo WARNING : app.properties not found, using defaults
)

:: ============================================================
:: Command dispatch
:: ============================================================
set "COMMAND=%~1"
if "!COMMAND!"=="" set "COMMAND=rebuild"

if /i "!COMMAND!"=="build"   ( call :build   & exit /b !errorlevel! )
if /i "!COMMAND!"=="clean"   ( call :clean   & exit /b !errorlevel! )
if /i "!COMMAND!"=="run"     ( call :run     & exit /b !errorlevel! )
if /i "!COMMAND!"=="rebuild" ( call :rebuild & exit /b !errorlevel! )
if /i "!COMMAND!"=="tidy"    ( call :tidy    & exit /b !errorlevel! )
if /i "!COMMAND!"=="update"  ( call :update  & exit /b !errorlevel! )
if /i "!COMMAND!"=="fmt"     ( call :fmt     & exit /b !errorlevel! )
if /i "!COMMAND!"=="vet"     ( call :vet     & exit /b !errorlevel! )
if /i "!COMMAND!"=="help"    ( call :help    & exit /b 0 )
if /i "!COMMAND!"=="-h"      ( call :help    & exit /b 0 )
if /i "!COMMAND!"=="--help"  ( call :help    & exit /b 0 )

echo ERROR : Unknown command : !COMMAND!
echo.
call :help
exit /b 1

:: ============================================================
:check_go
:: ============================================================
where go >nul 2>&1
if errorlevel 1 (
    echo ERROR : Go is not installed or not in PATH
    echo.
    echo Install Go from : https://go.dev/dl/
    echo.
    exit /b 1
)
exit /b 0

:: ============================================================
:check_build_reqs
:: ============================================================
call :check_go
if errorlevel 1 exit /b 1

where gcc >nul 2>&1
if errorlevel 1 (
    echo ERROR : GCC is not installed or not in PATH
    echo.
    echo Fyne and DuckDB require CGO which needs a C/C++ compiler.
    echo The required compiler is MSYS2 ucrt64 GCC :
    echo.
    echo   Step 1 - Download and install MSYS2 :
    echo              https://www.msys2.org/
    echo   Step 2 - In the MSYS2 terminal, install ucrt64 GCC :
    echo              pacman -S mingw-w64-ucrt-x86_64-gcc
    echo   Step 3 - Add ucrt64\bin to your Windows PATH :
    echo              C:\msys2\ucrt64\bin
    echo   Step 4 - Close and reopen this terminal, then retry.
    echo.
    echo   MSYS2 environments reference : https://www.msys2.org/docs/environments/
    echo.
    exit /b 1
)

call :check_gcc_variant
if errorlevel 1 exit /b 1

for /f "tokens=*" %%V in ('go env CGO_ENABLED 2^>nul') do set "CGO_STATUS=%%V"
if "!CGO_STATUS!"=="0" (
    echo ERROR : CGO_ENABLED=0 but CGO is required ^(Fyne + DuckDB^)
    echo.
    echo Set CGO_ENABLED=1 in your environment before building.
    echo.
    exit /b 1
)
exit /b 0

:: ============================================================
:check_gcc_variant
:: ============================================================
set "GCC_PATH="
for /f "tokens=*" %%P in ('where gcc 2^>nul') do if not defined GCC_PATH set "GCC_PATH=%%P"
if not defined GCC_PATH exit /b 0

echo !GCC_PATH! | findstr /i "ucrt64" >nul
if not errorlevel 1 exit /b 0

echo !GCC_PATH! | findstr /i "mingw64" >nul
if not errorlevel 1 (
    echo ERROR : Wrong GCC variant detected - mingw64 is incompatible with DuckDB
    echo.
    echo   Active GCC : !GCC_PATH!
    echo.
    echo   The mingw64 environment targets the legacy MSVCRT runtime.
    echo   DuckDB's pre-built Windows static libraries require the Universal
    echo   C Runtime ^(UCRT^). Linking with mingw64 produces undefined symbol
    echo   errors such as __stdio_common_vsnprintf_s.
    echo.
    echo   Fix :
    echo     1. Open the MSYS2 terminal and run :
    echo              pacman -S mingw-w64-ucrt-x86_64-gcc
    echo     2. In Windows Environment Variables, update PATH :
    echo              Remove  : ...\msys2\mingw64\bin
    echo              Add     : ...\msys2\ucrt64\bin
    echo     3. Close and reopen this terminal, then retry the build.
    echo.
    echo   MSYS2 environments : https://www.msys2.org/docs/environments/
    echo   MSYS2 download     : https://www.msys2.org/
    echo   Go download        : https://go.dev/dl/
    echo   DuckDB Go driver   : https://github.com/duckdb/duckdb-go
    echo.
    exit /b 1
)

:: Unknown toolchain (TDM-GCC, Cygwin, etc.) - warn but allow attempt
echo WARNING : Unrecognised GCC toolchain - build may fail with linker errors
echo.
echo   Active GCC : !GCC_PATH!
echo   Expected   : MSYS2 ucrt64  e.g. C:\msys2\ucrt64\bin\gcc.exe
echo.
echo   DuckDB requires a UCRT-compatible toolchain. If the build fails,
echo   switch to MSYS2 ucrt64 : https://www.msys2.org/docs/environments/
echo.
exit /b 0

:: ============================================================
:build
:: ============================================================
echo ==================================
echo   Building %BINARY_NAME%
echo ==================================
echo.
echo Configuration :
echo   App Binary Name     : %BINARY_NAME%
echo   App Display Name    : %APP_DISPLAY%
echo   App Version         : %APP_VER%
echo   App Build Date      : %APP_DATE%
echo   Encrypt Connections : %ENCRYPT_CONN%
echo.

echo Checking system requirements ...
call :check_build_reqs
if errorlevel 1 exit /b 1
echo System requirements verified
echo.

echo Go version :
go version
echo.

echo Ensuring dependencies are up to date ...
go mod tidy
if errorlevel 1 (
    echo ERROR : go mod tidy failed
    exit /b 1
)
echo Dependencies verified
echo.

echo Formatting code ...
go fmt ./...
if errorlevel 1 (
    echo ERROR : go fmt failed
    exit /b 1
)
echo Code formatted
echo.

echo Checking code ^(might take several minutes^) ...
go vet ./...
if errorlevel 1 (
    echo ERROR : go vet found issues
    exit /b 1
)
echo No issues found
echo.

echo Compiling ...
go build -ldflags "-X main.AppName=%BINARY_NAME% -X 'main.AppDisplayName=%APP_DISPLAY%' -X main.AppVersion=%APP_VER% -X main.AppVersionDate=%APP_DATE% -X main.EncryptConnectionDetails=%ENCRYPT_CONN%" -o %BINARY_NAME%.exe .
if errorlevel 1 (
    echo.
    echo ==================================
    echo   Build Failed!
    echo ==================================
    echo ERROR : Compilation errors occurred
    exit /b 1
)

echo.
echo ==================================
echo   Build Successful!
echo ==================================
echo.
dir "%BINARY_NAME%.exe"
echo.

:: Write build info file
set "BUILD_INFO_FILE=%BINARY_NAME%_BuildInfo_Windows.txt"
for /f "tokens=*" %%D in ('powershell -NoProfile -Command "Get-Date -Format \"yyyy-MM-dd HH:mm:ss\""') do set "BUILD_DATETIME=%%D"
for /f "tokens=*" %%S in ('powershell -NoProfile -Command "(Get-Item \"%BINARY_NAME%.exe\").Length"') do set "BINARY_BYTES=%%S"
for /f "tokens=*" %%G in ('go version') do set "GO_VER=%%G"
for /f "tokens=2 delims= " %%V in ('findstr "github.com/gocql/gocql" go.mod 2^>nul') do set "VER_GOCQL=%%V"
for /f "tokens=2 delims= " %%V in ('findstr "duckdb-go" go.mod 2^>nul') do set "VER_DUCKDB=%%V"
for /f "tokens=2 delims= " %%V in ('findstr "parquet-go" go.mod 2^>nul') do set "VER_PARQUET=%%V"
for /f "tokens=*" %%H in ('powershell -NoProfile -Command "(Get-FileHash \"%BINARY_NAME%.exe\" -Algorithm MD5).Hash"') do set "HASH_MD5=%%H"
for /f "tokens=*" %%H in ('powershell -NoProfile -Command "(Get-FileHash \"%BINARY_NAME%.exe\" -Algorithm SHA256).Hash"') do set "HASH_SHA256=%%H"
for /f "tokens=*" %%H in ('powershell -NoProfile -Command "(Get-FileHash \"%BINARY_NAME%.exe\" -Algorithm SHA512).Hash"') do set "HASH_SHA512=%%H"
for /f "tokens=*" %%O in ('powershell -NoProfile -Command "[System.Environment]::OSVersion.VersionString"') do set "OS_VER=%%O"
for /f "tokens=*" %%A in ('powershell -NoProfile -Command "$env:PROCESSOR_ARCHITECTURE"') do set "CPU_ARCH=%%A"

(
    echo.
    echo Build Date       : %BUILD_DATETIME%
    echo App Binary  Name : %BINARY_NAME%
    echo App Display Name : %APP_DISPLAY%
    echo App Version      : %APP_VER%
    echo App Build Date   : %APP_DATE%
    echo Binary Size      : %BINARY_BYTES% bytes
    echo.
    echo OS               : %OS_VER%
    echo Architecture     : %CPU_ARCH%
    echo.
    echo Go Version       : %GO_VER%
    echo.
    echo gocql            : %VER_GOCQL%
    echo go-duckdb        : %VER_DUCKDB%
    echo parquet-go       : %VER_PARQUET%
    echo.
    echo MD5              : %HASH_MD5%
    echo SHA256           : %HASH_SHA256%
    echo SHA512           : %HASH_SHA512%
    echo.
) > "%BUILD_INFO_FILE%"

echo Build info written to : %BUILD_INFO_FILE%
echo.
echo Run with : %BINARY_NAME%.exe
exit /b 0

:: ============================================================
:clean
:: ============================================================
echo ==================================
echo   Cleaning
echo ==================================
echo.
set "CLEANED=0"

if exist "%BINARY_NAME%.exe" (
    echo Removing binary : %BINARY_NAME%.exe
    del /f "%BINARY_NAME%.exe"
    if errorlevel 1 (
        echo ERROR : Failed to remove %BINARY_NAME%.exe
        exit /b 1
    )
    set "CLEANED=1"
)

if exist "%BINARY_NAME%.old.exe" (
    echo Removing old binary : %BINARY_NAME%.old.exe
    del /f "%BINARY_NAME%.old.exe"
    set "CLEANED=1"
)

if exist "%BINARY_NAME%_BuildInfo_Windows.txt" (
    echo Removing build info : %BINARY_NAME%_BuildInfo_Windows.txt
    del /f "%BINARY_NAME%_BuildInfo_Windows.txt"
    set "CLEANED=1"
)

if "!CLEANED!"=="0" echo Nothing to clean
echo.
echo Clean complete
exit /b 0

:: ============================================================
:run
:: ============================================================
if not exist "%BINARY_NAME%.exe" (
    echo Binary not found. Building first ...
    echo.
    call :build
    if errorlevel 1 (
        echo ERROR : Build failed, cannot run
        exit /b 1
    )
    echo.
)
echo ==================================
echo   Running %BINARY_NAME%
echo ==================================
echo.
"%BINARY_NAME%.exe"
exit /b 0

:: ============================================================
:rebuild
:: ============================================================
call :clean
if errorlevel 1 exit /b 1
echo.
call :build
exit /b !errorlevel!

:: ============================================================
:tidy
:: ============================================================
echo ==================================
echo   Running go mod tidy
echo ==================================
echo.
call :check_go
if errorlevel 1 exit /b 1

echo Go version :
go version
echo.

if not exist "go.mod" (
    echo ERROR : go.mod not found. Run 'go mod init' first.
    exit /b 1
)

echo Tidying dependencies ...
go mod tidy
if errorlevel 1 (
    echo ERROR : go mod tidy failed
    exit /b 1
)
echo.
echo go mod tidy completed successfully
echo.
echo Dependencies in go.mod :
findstr /r "^	" go.mod 2>nul
exit /b 0

:: ============================================================
:update
:: ============================================================
echo ==================================
echo   Updating Dependencies
echo ==================================
echo.
call :check_go
if errorlevel 1 exit /b 1

echo Go version :
go version
echo.

if not exist "go.mod" (
    echo ERROR : go.mod not found. Run 'go mod init' first.
    exit /b 1
)

for /f "tokens=*" %%T in ('powershell -NoProfile -Command "Get-Date -Format \"yyyyMMdd_HHmmss\""') do set "BACKUP_TS=%%T"

echo Backing up go.mod to : go.mod_%BACKUP_TS%
copy /y go.mod go.mod_%BACKUP_TS% >nul
echo Backup created : go.mod_%BACKUP_TS%

if exist "go.sum" (
    echo Backing up go.sum to : go.sum_%BACKUP_TS%
    copy /y go.sum go.sum_%BACKUP_TS% >nul
    echo Backup created : go.sum_%BACKUP_TS%
)
echo.

echo Upgrading all dependencies to latest versions ...
go get -u ./...
if errorlevel 1 (
    echo ERROR : go get -u failed
    exit /b 1
)
echo Dependencies upgraded
echo.

echo Tidying dependencies ...
go mod tidy
if errorlevel 1 (
    echo ERROR : go mod tidy failed
    exit /b 1
)
echo go mod tidy completed successfully
echo.
echo Dependencies in go.mod :
findstr /r "^	" go.mod 2>nul
echo.
echo Run '%SCRIPT_NAME% build' to rebuild with updated dependencies
exit /b 0

:: ============================================================
:fmt
:: ============================================================
echo ==================================
echo   Formatting Code
echo ==================================
echo.
call :check_go
if errorlevel 1 exit /b 1
echo Running go fmt ...
go fmt ./...
if errorlevel 1 (
    echo ERROR : go fmt failed
    exit /b 1
)
echo.
echo Code formatting complete
exit /b 0

:: ============================================================
:vet
:: ============================================================
echo ==================================
echo   Checking Code
echo ==================================
echo.
call :check_go
if errorlevel 1 exit /b 1
echo Running go vet ...
go vet ./...
if errorlevel 1 (
    echo.
    echo Issues found ^(see above^)
    exit /b 1
)
echo.
echo No issues found
exit /b 0

:: ============================================================
:help
:: ============================================================
echo %BINARY_NAME% Build Tool ^(Windows^)
echo.
echo Usage: %SCRIPT_NAME% [command]
echo.
echo Commands:
echo   build     - Compile the application
echo   clean     - Remove compiled binaries
echo   run       - Build ^(if needed^) and run the application
echo   rebuild   - Clean and build ^(default^)
echo   tidy      - Run go mod tidy to update dependencies
echo   update    - Upgrade all dependencies to latest versions
echo   fmt       - Format code with go fmt
echo   vet       - Check code with go vet
echo   help      - Show this help message
echo.
echo Examples:
echo   %SCRIPT_NAME%            - Rebuild ^(clean + build, default^)
echo   %SCRIPT_NAME% build      - Build ^(includes go mod tidy^)
echo   %SCRIPT_NAME% clean      - Clean
echo   %SCRIPT_NAME% run        - Run ^(builds if needed^)
echo   %SCRIPT_NAME% rebuild    - Clean and build
echo   %SCRIPT_NAME% tidy       - Update dependencies only
echo   %SCRIPT_NAME% update     - Upgrade all dependencies to latest versions
echo   %SCRIPT_NAME% fmt        - Format code
echo   %SCRIPT_NAME% vet        - Check for issues
echo.
echo Note : App name is read from app.properties ^(APP_COMPILE_NAME=%BINARY_NAME%^)
echo.
echo Prerequisites :
echo   Go       : https://go.dev/dl/
echo   TDM-GCC  : https://jmeubank.github.io/tdm-gcc/
echo   MSYS2    : https://www.msys2.org/
echo.
exit /b 0
