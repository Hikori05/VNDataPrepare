package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"image"
	"syscall"
	"unsafe"

	"github.com/kbinani/screenshot"
	"golang.design/x/hotkey"
	"golang.design/x/hotkey/mainthread"
)

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
)

type point struct {
	X, Y int32
}

func getCursorPos() (int, int) {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y)
}

func main() {
	mainthread.Init(run)
}

func run() {
	// 1. Load Config
	cfg, err := loadConfig("config.json")
	if err != nil {
		// Create default if missing
		cfg = &Config{
			OutputDir:  "screenshots",
			FilePrefix: "vn_capture",
			Rect:       Rect{0, 0, 1920, 1080},
		}
	}

	// 2. Prepare Output Directory
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		log.Fatalf("Failed to create output dir: %v", err)
	}
	fmt.Printf("Save location: %s\n", cfg.OutputDir)

	// 3. Register Hotkeys
	// F9 for Capture
	hkCapture := hotkey.New(nil, hotkey.KeyF9)
	if err := hkCapture.Register(); err != nil {
		log.Fatalf("Failed to register F9: %v", err)
	}
	defer hkCapture.Unregister()

	// F8 for Calibration
	hkCalib := hotkey.New(nil, hotkey.KeyF8)
	if err := hkCalib.Register(); err != nil {
		log.Printf("Failed to register F8 (Calibration): %v", err)
	} else {
		defer hkCalib.Unregister()
	}

	fmt.Println("Capture App Running...")
	fmt.Println("[F9] Capture Screen")
	fmt.Println("[F8] Calibrate Region (Mouse based)")
	fmt.Printf("Current Rect: %+v\n", cfg.Rect)

	// 4. Listen for events
	captureChan := hkCapture.Keydown()
	calibChan := hkCalib.Keydown()

	for {
		select {
		case <-captureChan:
			if calibrationStage == 0 {
				fmt.Println("Capturing...")
				capture(cfg)
			} else {
				fmt.Println("Finish calibration first!")
			}
		case <-calibChan:
			handleCalibration(cfg)
		}
	}
}

func handleCalibration(cfg *Config) {
	x, y := getCursorPos()

	switch calibrationStage {
	case 0:
		fmt.Println("--- Calibration Mode Started ---")
		fmt.Println("Move mouse to TOP-LEFT corner of dialogue box and press F8 again.")
		calibrationStage = 1
	case 1:
		calibX = x
		calibY = y
		fmt.Printf("Top-Left set to: %d, %d\n", x, y)
		fmt.Println("Now move mouse to BOTTOM-RIGHT corner and press F8.")
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

		fmt.Printf("Region defined: %+v\n", cfg.Rect)
		saveConfig("config.json", cfg)
		fmt.Println("Saved to config.json. Calibration Done.")
		calibrationStage = 0
	}
}

func saveConfig(path string, cfg *Config) {
	f, err := os.Create(path)
	if err != nil {
		log.Println("Error saving config:", err)
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
		return nil, err
	}
	defer file.Close()

	cfg := &Config{}
	if err := json.NewDecoder(file).Decode(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func capture(cfg *Config) {
	// 1. Determine bounds
	bounds := image.Rect(cfg.Rect.X, cfg.Rect.Y, cfg.Rect.X+cfg.Rect.Width, cfg.Rect.Y+cfg.Rect.Height)

	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		log.Printf("Error capturing screen: %v", err)
		return
	}

	// 2. Generate Filename
	timestamp := time.Now().Format("20060102_150405_000000")
	filename := fmt.Sprintf("%s_%s.png", cfg.FilePrefix, timestamp)
	fullPath := filepath.Join(cfg.OutputDir, filename)

	// 3. Save File
	f, err := os.Create(fullPath)
	if err != nil {
		log.Printf("Error ensuring file: %v", err)
		return
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		log.Printf("Error encoding png: %v", err)
		return
	}

	fmt.Printf("Saved: %s\n", fullPath)
}
