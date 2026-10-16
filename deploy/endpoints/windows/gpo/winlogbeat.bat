@echo off
rem GPO computer startup script: install or update Winlogbeat from the deployment share.
rem Prepared by deploy/endpoints/windows/prepare-share.ps1, which replaces FILESERVER_SHARE.
rem Idempotent: installs when missing, otherwise copies the config only when it changed.
setlocal

set "SHARE=FILESERVER_SHARE\winlogbeat"
set "MSI=%SHARE%\winlogbeat.msi"
set "CFG=%SHARE%\winlogbeat.yml"
rem The Beats MSI keeps its configuration under ProgramData
set "CFG_DIR=C:\ProgramData\Elastic\Beats\winlogbeat"
set "SERVICE=winlogbeat"
rem TLS to Logstash: prepare-share.ps1 -CaCert puts the stack's CA on the share
set "CA=FILESERVER_SHARE\esm-ca.crt"
set "CA_DST=C:\ProgramData\Elastic\Beats\esm-ca.crt"
set "CA_CHANGED="
if exist "%CA%" (
  fc /b "%CA%" "%CA_DST%" >nul 2>&1
  if errorlevel 1 (
    if not exist "C:\ProgramData\Elastic\Beats" mkdir "C:\ProgramData\Elastic\Beats"
    copy /Y "%CA%" "%CA_DST%" >nul
    set "CA_CHANGED=1"
  )
)

if not exist "%MSI%" (echo [ERR] missing %MSI% & exit /b 1)
if not exist "%CFG%" (echo [ERR] missing %CFG% & exit /b 2)

sc query %SERVICE% >nul 2>&1
if errorlevel 1 (
  echo [INFO] installing Winlogbeat
  msiexec /i "%MSI%" /quiet /norestart
  if errorlevel 1 (echo [ERR] msiexec failed & exit /b 3)
  if not exist "%CFG_DIR%" mkdir "%CFG_DIR%"
  copy /Y "%CFG%" "%CFG_DIR%\winlogbeat.yml" >nul
  sc config %SERVICE% start= auto >nul
  sc start %SERVICE% >nul
  echo [OK] Winlogbeat installed
  exit /b 0
)

fc /b "%CFG%" "%CFG_DIR%\winlogbeat.yml" >nul 2>&1
if errorlevel 1 (
  echo [INFO] Winlogbeat configuration changed, restarting the service
  if not exist "%CFG_DIR%" mkdir "%CFG_DIR%"
  copy /Y "%CFG%" "%CFG_DIR%\winlogbeat.yml" >nul
  sc stop %SERVICE% >nul
  timeout /t 5 /nobreak >nul
  sc start %SERVICE% >nul
) else if defined CA_CHANGED (
  echo [INFO] CA changed, restarting the service
  sc stop %SERVICE% >nul
  timeout /t 5 /nobreak >nul
  sc start %SERVICE% >nul
) else (
  echo [OK] Winlogbeat is up to date
)
exit /b 0
