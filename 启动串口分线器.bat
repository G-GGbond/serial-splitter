@echo off
title 串口分线器
cd /d "%~dp0"

net session >nul 2>&1
if %errorlevel% equ 0 goto :admin

rem 非管理员，请求提权
powershell -NoProfile -Command "Start-Process -FilePath 'pythonw.exe' -ArgumentList 'serial_splitter_gui.py' -WorkingDirectory '%~dp0' -Verb RunAs"
exit /b

:admin
rem 已是管理员，直接以隐藏窗口运行
start "" pythonw.exe serial_splitter_gui.py
exit /b
