@echo off
cd /d "%~dp0"
serial-splitter.exe > server.log 2>&1
