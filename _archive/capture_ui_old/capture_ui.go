package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/kbinani/screenshot"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.design/x/hotkey"
)

// --- Config ---

type Config struct {
	OutputDir    string `json:"output_dir"`
	FilePrefix   string `json:"file_prefix"`
	MonitorIndex int    `json:"monitor_index"`
	Rect         Rect   `json:"rect"`
	Hotkey       string `json:"hotkey"`
}

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

var (
	calibrationStage int = 0
	calibX, calibY   int
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32.NewProc("GetCursorPos")

	// GUI Components
	mw          *walk.MainWindow
	imgView     *walk.ImageView
	consoleEdit *walk.TextEdit
	cfg         *Config
)

type point struct {
	X, Y int32
}

func getCursorPos() (int, int) {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y)
}

func logToConsole(msg string) {
	if consoleEdit != nil {
		// Append text safely from any goroutine
		mw.Synchronize(func() {
			consoleEdit.AppendText(msg + "\r\n")
		})
	}
	log.Println(msg) // Also log to std out
}

func main() {
	// Initialize Config
	var err error
	cfg, err = loadConfig("config.json")
	if err != nil {
		cfg = &Config{
			OutputDir:  "screenshots",
			FilePrefix: "vn_capture",
			Rect:       Rect{0, 0, 1920, 1080},
		}
	}
	os.MkdirAll(cfg.OutputDir, 0755)

	// Setup Hotkey Functionality in a Goroutine
	go hotkeyLoop()

	// GUI Definition
	if _, err := (MainWindow{
		AssignTo: &mw,
		Title:    "VN Capture UI (Classic)",
		MinSize:  Size{Width: 600, Height: 400},
		Size:     Size{Width: 800, Height: 500},
		Layout:   HBox{},
		Children: []Widget{
			ImageView{
				AssignTo: &imgView,
				Mode:     ImageViewModeZoom,
			},
			TextEdit{
				AssignTo: &consoleEdit,
				ReadOnly: true,
				VScroll:  true,
				MinSize:  Size{Width: 200, Height: 0},
				MaxSize:  Size{Width: 300, Height: 0}, // Right sidebar width
			},
		},
	}.Run()); err != nil {
		log.Fatal(err)
	}
}

func hotkeyLoop() {
	// F9 for Capture
	hkCapture := hotkey.New(nil, hotkey.KeyF9)
	if err := hkCapture.Register(); err != nil {
		log.Printf("WARNING: Failed to register F9: %v", err)
		logToConsole("WARNING: F9 Hotkey failed. Check console.")
	} else {
		defer hkCapture.Unregister()
	}

	// F8 for Calibration
	hkCalib := hotkey.New(nil, hotkey.KeyF8)
	if err := hkCalib.Register(); err != nil {
		log.Printf("WARNING: Failed to register F8: %v", err)
	} else {
		defer hkCalib.Unregister()
	}

	for {
		select {
		case <-hkCapture.Keydown():
			if calibrationStage == 0 {
				logToConsole("Capturing...")
				// Execute Capture
				path, err := capture(cfg)
				if err != nil {
					logToConsole(fmt.Sprintf("Error: %v", err))
				} else {
					logToConsole(fmt.Sprintf("Saved: %s", filepath.Base(path)))
					// Update GUI image
					updateImage(path)
				}
			} else {
				logToConsole("Finish calibration first!")
			}
		case <-hkCalib.Keydown():
			handleCalibration(cfg)
		}
	}
}

func updateImage(path string) {
	if mw == nil || imgView == nil {
		return
	}

	// Walk requires operations on main thread
	mw.Synchronize(func() {
		img, err := walk.NewImageFromFile(path)
		if err != nil {
			logToConsole("Failed to load image preview")
			return
		}
		imgView.SetImage(img)
	})
}

func handleCalibration(cfg *Config) {
	x, y := getCursorPos()

	switch calibrationStage {
	case 0:
		logToConsole("--- Calibration Mode Started ---")
		logToConsole("Move mouse to TOP-LEFT corner of dialogue box and press F8 again.")
		calibrationStage = 1
	case 1:
		calibX = x
		calibY = y
		logToConsole(fmt.Sprintf("Top-Left set to: %d, %d", x, y))
		logToConsole("Now move mouse to BOTTOM-RIGHT corner and press F8.")
		calibrationStage = 2
	case 2:
		width := x - calibX
		height := y - calibY
		if width < 0 {
			width = -width
			calibX = x
		}
		if height < 0 {
			height = -height
			calibY = y
		}

		cfg.Rect.X = calibX
		cfg.Rect.Y = calibY
		cfg.Rect.Width = width
		cfg.Rect.Height = height

		logToConsole(fmt.Sprintf("Region defined: %+v", cfg.Rect))
		saveConfig("config.json", cfg)
		logToConsole("Saved to config.json. Calibration Done.")
		calibrationStage = 0
	}
}

// Helpers

func saveConfig(path string, cfg *Config) {
	target := path
	if _, err := os.Stat("../" + path); err == nil {
		target = "../" + path
	}

	f, err := os.Create(target)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(cfg)
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		// Try parent
		file, err = os.Open("../" + path)
		if err != nil {
			return nil, err
		}
	}
	defer file.Close()
	cfg := &Config{}
	if err := json.NewDecoder(file).Decode(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func capture(cfg *Config) (string, error) {
	bounds := image.Rect(cfg.Rect.X, cfg.Rect.Y, cfg.Rect.X+cfg.Rect.Width, cfg.Rect.Y+cfg.Rect.Height)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102_150405_000000")
	filename := fmt.Sprintf("%s_%s.png", cfg.FilePrefix, timestamp)
	fullPath := filepath.Join(cfg.OutputDir, filename)

	f, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return fullPath, nil
}
