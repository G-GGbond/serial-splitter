@echo off
title 串口分线器 (Web版)
cd /d "%~dp0"

net session >nul 2>&1
if %errorlevel% equ 0 goto :admin

rem 非管理员，请求提权（只弹一次 UAC）
powershell -NoProfile -Command "Start-Process -FilePath 'serial-splitter.exe' -WorkingDirectory '%~dp0' -Verb RunAs"
exit /b

:admin
rem 已是管理员，直接运行（无控制台窗口）
start "" serial-splitter.exe
exit /b
