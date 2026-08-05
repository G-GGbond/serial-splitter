@echo off
title SerialSplitter (Python)
cd /d "%~dp0"

net session >nul 2>&1
if %errorlevel% equ 0 goto :admin

rem non-admin, request elevation
powershell -NoProfile -Command "Start-Process -FilePath 'pythonw.exe' -ArgumentList 'serial_splitter_gui.py' -WorkingDirectory '%~dp0' -Verb RunAs"
exit /b

:admin
start "" pythonw.exe serial_splitter_gui.py
exit /b
