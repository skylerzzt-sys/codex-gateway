@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0codex-app-bind.ps1" -Mode gateway
if errorlevel 1 pause
