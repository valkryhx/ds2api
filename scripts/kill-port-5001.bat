@echo off
setlocal EnableDelayedExpansion

set "PORT=5001"
set "FOUND="

for /f "tokens=5" %%P in ('netstat -ano ^| findstr /R /C:":%PORT% .*LISTENING"') do (
    if not defined SEEN_%%P (
        set "SEEN_%%P=1"
        set "FOUND=1"
        echo Killing PID %%P on port %PORT%...
        taskkill /PID %%P /F
    )
)

if not defined FOUND (
    echo No LISTENING process found on port %PORT%.
)

endlocal
