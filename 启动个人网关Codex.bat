@echo off
setlocal
for /f "usebackq delims=" %%K in (`powershell -NoProfile -Command "[Environment]::GetEnvironmentVariable('CODEX_GATEWAY_API_KEY','User')"`) do set "CODEX_GATEWAY_API_KEY=%%K"
if not defined CODEX_GATEWAY_API_KEY (
  echo CODEX_GATEWAY_API_KEY is missing.
  pause
  exit /b 1
)
codex --profile personal-gateway
