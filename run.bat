@echo off
setlocal
cd /d "%~dp0"

echo ==========================================
echo        Personal Cloud v0.5.0
echo ==========================================

echo.
echo Checking required tools...
where node >nul 2>&1
if errorlevel 1 (
  echo ERROR: Node.js/npm was not found in PATH.
  echo Install Node.js LTS and reopen PowerShell.
  exit /b 1
)
where npm >nul 2>&1
if errorlevel 1 (
  echo ERROR: npm was not found in PATH.
  echo Install Node.js LTS and reopen PowerShell.
  exit /b 1
)
where go >nul 2>&1
if errorlevel 1 (
  echo ERROR: Go was not found in PATH.
  echo Install Go 1.27+ and reopen PowerShell.
  exit /b 1
)

echo.
echo Checking frontend build...
if not exist "web\dist\index.html" (
  echo Frontend build not found. Building now...
  pushd web
  call npm install
  if errorlevel 1 (
    echo.
    echo ERROR: npm install failed.
    popd
    exit /b 1
  )
  call npm run build
  if errorlevel 1 (
    echo.
    echo ERROR: npm run build failed.
    popd
    exit /b 1
  )
  popd
)

echo.
echo Preparing Go dependencies...
pushd server
call go mod download
if errorlevel 1 (
  echo.
  echo ERROR: Go dependencies could not be downloaded.
  echo Check your Internet connection and Go installation.
  popd
  exit /b 1
)

call go mod tidy
if errorlevel 1 (
  echo.
  echo ERROR: Go module setup failed.
  popd
  exit /b 1
)

echo.
echo Starting server...
call go run ./cmd/server
set "SERVER_EXIT=%errorlevel%"
popd
exit /b %SERVER_EXIT%
