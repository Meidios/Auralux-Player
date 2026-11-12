@echo off
setlocal
cd /d "%~dp0"
where go >nul 2>nul
if errorlevel 1 (
  echo [ERROR] Go was not found. Install Go 1.23 or newer first.
  pause
  exit /b 1
)
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags="-H=windowsgui -s -w" -o AuraluxPlayer.exe .
if errorlevel 1 (
  echo [ERROR] Build failed.
  pause
  exit /b 1
)
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0Embed_Windows_Icon.ps1" -ExePath "%~dp0AuraluxPlayer.exe" -IconPath "%~dp0Auralux.ico"
if errorlevel 1 (
  echo [ERROR] Icon embedding failed.
  pause
  exit /b 1
)
echo.
echo Build completed: %CD%\AuraluxPlayer.exe
pause
