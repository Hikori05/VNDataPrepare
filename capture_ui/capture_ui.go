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

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/kbinani/screenshot"
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
	// mw          *walk.MainWindow -> w fyne.Window
	// imgView     *walk.ImageView -> imgWidget *canvas.Image
	// consoleEdit *walk.TextEdit -> consoleWidget *widget.Entry

	w                   fyne.Window
	imgWidget           *canvas.Image
	consoleWidget       *widget.Label
	consoleScrollWidget *container.Scroll
	cfg                 *Config
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
	log.Println(msg) // Log to stdout immediately

	if consoleWidget != nil {
		// Queue update to main thread to be safe and avoid race conditions or crashes
		// We use the global window 'w' to access the queue if possible?
		// fyne.CurrentApp() is the standard way.

		// Since we didn't store 'a' (App), we can use fyne.CurrentApp() if initialized.
		// Or better, just protect the widget access.

		// Correct Fyne 2.4+ pattern for background updates:
		if a := fyne.CurrentApp(); a != nil {
			a.SendNotification(fyne.NewNotification("Log", msg)) // Optional: system notification? No.
			// RunOnMain is not directly exposed on App interface without casting?
			// Actually it is on the Driver.

			a.Driver().RunOnMain(func() {
				current := consoleWidget.Text
				if len(current) > 5000 {
					current = current[len(current)-4000:]
				}
				consoleWidget.SetText(current + msg + "\n")
				if consoleScrollWidget != nil {
					consoleScrollWidget.ScrollToBottom()
				}
			})
		}
	}
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

	// App
	a := app.New()
	// Force Dark Theme for white text visibility
	a.Settings().SetTheme(theme.DarkTheme())

	w = a.NewWindow("VN Capture UI (Fyne)")
	w.Resize(fyne.NewSize(800, 500))

	// Image Area
	// Placeholder image
	imgWidget = canvas.NewImageFromFile("")
	imgWidget.FillMode = canvas.ImageFillContain

	// Wrap image in container
	imgContainer := container.NewMax(imgWidget)

	// Console Area
	// Use Label inside ScrollContainer to keep text white (Entry disabled is grey)
	consoleLabel := widget.NewLabel("")
	consoleLabel.Wrapping = fyne.TextWrapWord

	consoleScroll := container.NewVScroll(consoleLabel)

	// Assign to global for logging
	consoleWidget = consoleLabel
	consoleScrollWidget = consoleScroll

	// Use a fixed width container for console (Right sidebar)
	// Border layout: Top, Bottom, Left, Right, Center
	// We put console on Left or Right? User had Hbox, usually Right.

	// To enable resizing between image and console, HSplit is best
	split := container.NewHSplit(imgContainer, consoleScrollWidget)
	split.SetOffset(0.7) // 70% image, 30% console

	w.SetContent(split)

	// Setup Hotkey Functionality in a Goroutine
	go hotkeyLoop()

	logToConsole("Welcome to VN Capture UI (Fyne)!")
	logToConsole("Press F9 to Capture")
	logToConsole("Press F8 to Calibrate")
	logToConsole(fmt.Sprintf("Save Dir: %s", cfg.OutputDir))

	w.ShowAndRun()
}

func hotkeyLoop() {
	// F9 for Capture
	hkCapture := hotkey.New(nil, hotkey.KeyF9)
	if err := hkCapture.Register(); err != nil {
		log.Printf("WARNING: Failed to register F9: %v", err)
		logToConsole("WARNING: F9 Hotkey failed.")
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
				// RUN ASYNC to prevent UI freeze
				go func() {
					logToConsole("Capturing...")

					// Capture
					img, path, err := captureAsync(cfg)
					if err != nil {
						logToConsole(fmt.Sprintf("Error: %v", err))
						return
					}

					// Immediate UI Update (Memory Image)
					updateImageFromMem(img)

					logToConsole(fmt.Sprintf("Saved: %s", filepath.Base(path)))
				}()
			} else {
				logToConsole("Finish calibration first!")
			}
		case <-hkCalib.Keydown():
			// Calibration needs cursor pos, keep sync or fast
			handleCalibration(cfg)
		}
	}
}

func updateImage(path string) {
	// Legacy file loading
	if imgWidget == nil {
		return
	}
	imgWidget.File = path
	imgWidget.Refresh()
}

func updateImageFromMem(img image.Image) {
	if imgWidget == nil {
		return
	}

	// Ensure thread safety
	if a := fyne.CurrentApp(); a != nil {
		a.Driver().RunOnMain(func() {
			// Set image directly from memory - FAST
			imgWidget.Image = img
			imgWidget.File = "" // Clear file path so it uses Image field
			imgWidget.Refresh()
		})
	}
}

func handleCalibration(cfg *Config) {
	x, y := getCursorPos()

	switch calibrationStage {
	case 0:
		logToConsole("--- Calibration Mode ---")
		logToConsole("TOP-LEFT -> F8")
		calibrationStage = 1
	case 1:
		calibX = x
		calibY = y
		logToConsole(fmt.Sprintf("TL: %d, %d. Now BOTTOM-RIGHT -> F8", x, y))
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

		logToConsole(fmt.Sprintf("Region: %+v", cfg.Rect))
		saveConfig("config.json", cfg)
		logToConsole("Saved. Done.")
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

// Modified capture to return Image object + Path, and do saving internally
func captureAsync(cfg *Config) (image.Image, string, error) {
	bounds := image.Rect(cfg.Rect.X, cfg.Rect.Y, cfg.Rect.X+cfg.Rect.Width, cfg.Rect.Y+cfg.Rect.Height)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, "", err
	}

	timestamp := time.Now().Format("20060102_150405_000000")
	filename := fmt.Sprintf("%s_%s.png", cfg.FilePrefix, timestamp)
	fullPath := filepath.Join(cfg.OutputDir, filename)

	// Save in yet another goroutine? No, we are already in one.
	f, err := os.Create(fullPath)
	if err != nil {
		return img, "", err
	}
	defer f.Close()

	// Using LevelBestCompression is slow, assume DefaultCompression is fine/default
	if err := png.Encode(f, img); err != nil {
		return img, "", err
	}
	return img, fullPath, nil
}

// Deprecated synchronous capture
func capture(cfg *Config) (string, error) {
	_, path, err := captureAsync(cfg)
	return path, err
}
