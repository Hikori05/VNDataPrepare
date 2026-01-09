@echo off
echo Building Capture UI (Fyne)...
cd capture_ui
set GOARCH=386
set CGO_ENABLED=1
go build -ldflags "-H windowsgui" -o capture_ui.exe capture_ui.go
cd ..
echo Done.
pause
