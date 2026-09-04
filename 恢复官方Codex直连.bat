@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0codex-app-bind.ps1" -Mode official
if errorlevel 1 pause
