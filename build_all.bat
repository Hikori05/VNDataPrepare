@echo off
echo Building Projects...

echo Building Launcher (Console Menu)...
set GOARCH=amd64
go build -o VNLauncher.exe launcher.go

echo Building Capture UI (Fyne - No Console Window)...
cd capture_ui
set GOARCH=386
set CGO_ENABLED=1
go build -ldflags "-H windowsgui" -o capture_ui.exe capture_ui.go
cd ..

echo Building Capture UI (Legacy)...
cd capture_ui_old
go build -ldflags "-H windowsgui" -o capture_ui_old.exe capture_ui.go
cd ..

echo Building Capture Auto (No Console Window)...
cd capture_auto
go build -ldflags "-H windowsgui" -o capture_auto.exe capture_auto.go
cd ..

echo Building Server...
cd server
set GOARCH=amd64
go build -o server.exe server.go
cd ..

echo Done.
pause
