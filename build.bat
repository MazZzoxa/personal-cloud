@echo off
setlocal
cd /d "%~dp0"

echo ==========================================
echo     Building Personal Cloud v0.4.0
echo ==========================================

echo.
echo Preparing frontend...
pushd web
call npm install
if errorlevel 1 (
  echo npm install failed.
  popd
  exit /b 1
)
call npm run build
if errorlevel 1 (
  echo npm run build failed.
  popd
  exit /b 1
)
popd

echo.
echo Preparing Go dependencies...
pushd server
call go mod download
if errorlevel 1 (
  echo go mod download failed.
  popd
  exit /b 1
)
call go mod tidy
if errorlevel 1 (
  echo go mod tidy failed.
  popd
  exit /b 1
)

echo.
echo Building Windows executable...
call go build -o ..\personal-cloud.exe .\cmd\server
if errorlevel 1 (
  echo go build failed.
  popd
  exit /b 1
)
popd

echo.
echo Build complete:
echo   personal-cloud.exe
