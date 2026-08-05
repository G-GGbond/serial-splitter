@echo off
title SerialSplitter (Web)
cd /d "%~dp0"

net session >nul 2>&1
if %errorlevel% equ 0 goto :admin

rem non-admin, request elevation
powershell -NoProfile -Command "Start-Process -FilePath 'serial-splitter.exe' -WorkingDirectory '%~dp0' -Verb RunAs"
exit /b

:admin
start "" serial-splitter.exe
exit /b
