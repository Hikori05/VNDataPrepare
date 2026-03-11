package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/kbinani/screenshot"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.design/x/hotkey"
)

// --- KONFIGURACJA ---

type Config struct {
	OutputDir  string `json:"output_dir"`
	FilePrefix string `json:"file_prefix"`
	Rect       Rect   `json:"rect"`
}

type Rect struct {
	X, Y, Width, Height int
}

// --- ZMIENNE GLOBALNE ---

var (
	calibrationStage int = 0
	calibX, calibY   int
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32.NewProc("GetCursorPos")

	// GUI
	mw          *walk.MainWindow
	imgView     *walk.ImageView
	consoleEdit *walk.TextEdit
	autoChk     *walk.CheckBox
	cfg         *Config

	// Stan Auto-Capture i Deduplikacja
	isAutoRunning   bool
	mutex           sync.Mutex
	templateImg     image.Image
	lastAutoCapture time.Time

	// Przechowujemy ostatni zapisany obraz w pamięci, aby porównać go z nowym
	lastSavedImage *image.RGBA
)

// --- MAIN ---

func main() {
	// 1. Config
	var err error
	cfg, err = loadConfig("config.json")
	if err != nil {
		cfg = &Config{
			OutputDir:  "screenshots",
			FilePrefix: "vn_capture",
			Rect:       Rect{0, 0, 800, 200},
		}
	}
	os.MkdirAll(cfg.OutputDir, 0755)

	// 2. Ładowanie 'łapki'
	templateImg, err = loadPngImage("img.png")
	if err != nil {
		log.Println("BRAK PLIKU image_6e98e0.png - Auto-capture nie zadziała.")
	}

	// 3. Wątki tła
	go hotkeyLoop()
	go autoDetectionLoop()

	// 4. GUI
	font := Font{Family: "Segoe UI", PointSize: 9}

	if _, err := (MainWindow{
		AssignTo: &mw,
		Title:    "VN Smart Capture",
		MinSize:  Size{Width: 600, Height: 500},
		Size:     Size{Width: 900, Height: 650},
		Layout:   VBox{},
		Font:     font,
		Children: []Widget{
			// SEKCJA INSTRUKCJI (Nowość)
			GroupBox{
				Title:  "Instrukcja Sterowania",
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "F8: Kalibracja obszaru (Lewy-Góra -> Prawy-Dół)", MinSize: Size{Width: 250, Height: 0}},
					Label{Text: "F9: Ręczny Zrzut"},
					Label{Text: "Checkbox: Auto-wykrywanie"},
				},
			},

			// SEKCJA STEROWANIA
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Status Auto:"},
					CheckBox{
						AssignTo: &autoChk,
						Text:     "WŁĄCZ WYKRYWANIE IKONY",
						OnCheckedChanged: func() {
							mutex.Lock()
							isAutoRunning = autoChk.Checked()
							// Reset licznika czasu, żeby nie strzelił od razu po włączeniu
							lastAutoCapture = time.Now()
							mutex.Unlock()
							logToConsole(fmt.Sprintf("Auto-Capture: %v", autoChk.Checked()))
						},
					},
					HSpacer{},
					PushButton{
						Text: "Zapisz Ustawienia",
						OnClicked: func() {
							saveConfig("config.json", cfg)
							logToConsole("Config zapisany.")
						},
					},
				},
			},

			// PODGLĄD ZDJĘCIA
			ImageView{
				AssignTo: &imgView,
				Mode:     ImageViewModeZoom,
			},

			// KONSOLA LOGÓW
			TextEdit{
				AssignTo: &consoleEdit,
				ReadOnly: true,
				VScroll:  true,
				MaxSize:  Size{Width: 0, Height: 120},
			},
		},
	}.Run()); err != nil {
		log.Fatal(err)
	}
}

// --- LOGIKA DETEKCJI I ANTY-DUPLIKATY ---

func autoDetectionLoop() {
	ticker := time.NewTicker(400 * time.Millisecond) // Częstotliwość sprawdzania
	defer ticker.Stop()

	for range ticker.C {
		mutex.Lock()
		running := isAutoRunning
		tpl := templateImg
		mutex.Unlock()

		if !running || tpl == nil {
			continue
		}

		// Zrób zrzut do analizy
		bounds := image.Rect(cfg.Rect.X, cfg.Rect.Y, cfg.Rect.X+cfg.Rect.Width, cfg.Rect.Y+cfg.Rect.Height)
		screenImg, err := screenshot.CaptureRect(bounds)
		if err != nil {
			continue
		}

		// 1. Sprawdź czy jest łapka
		found, conf := findTemplateWithAlpha(screenImg, tpl, 0.82)
		if found {
			// 2. Jeśli jest łapka, sprawdź czy to nie DUPLIKAT
			// (Czy obecny ekran jest identyczny jak poprzednio zapisany?)
			if lastSavedImage != nil && imagesAreSimilar(screenImg, lastSavedImage, 0.98) {
				// To ten sam tekst, czekamy dalej
				// (0.98 to 98% podobieństwa - pozwala na minimalne zmiany jak szum czy animacja łapki)
				continue
			}

			// Jeśli minął minimalny cooldown (żeby nie spamować przy błędach detekcji)
			if time.Since(lastAutoCapture) > 1500*time.Millisecond {
				logToConsole(fmt.Sprintf("Wykryto nową scenę! (Pewność łapki: %.2f)", conf))

				path, err := saveImageToDisk(screenImg, cfg)
				if err == nil {
					logToConsole("Zapisano: " + filepath.Base(path))
					updateImagePreview(path)

					mutex.Lock()
					lastAutoCapture = time.Now()
					// Aktualizujemy wzorzec 'ostatniego zdjęcia'
					// Kopiujemy, bo screenImg zostanie nadpisany w następnej pętli
					lastSavedImage = cloneImage(screenImg)
					mutex.Unlock()
				}
			}
		}
	}
}

// imagesAreSimilar sprawdza czy dwa obrazy są "prawie" identyczne.
// threshold 0.98 oznacza, że 98% pikseli musi być bardzo podobnych.
// Używane do unikania robienia zdjęć tego samego tekstu.
func imagesAreSimilar(img1, img2 *image.RGBA, threshold float64) bool {
	b1 := img1.Bounds()
	b2 := img2.Bounds()
	if b1.Dx() != b2.Dx() || b1.Dy() != b2.Dy() {
		return false
	}

	totalPixels := 0
	similarPixels := 0

	// Sprawdzamy co 4 piksel dla wydajności (wystarczy do wykrycia zmiany tekstu)
	step := 4

	for y := 0; y < b1.Dy(); y += step {
		for x := 0; x < b1.Dx(); x += step {
			totalPixels++

			r1, g1, b1, _ := img1.At(x, y).RGBA()
			r2, g2, b2, _ := img2.At(x, y).RGBA()

			// Prosta różnica (Manhattan)
			diff := math.Abs(float64(r1)-float64(r2)) +
				math.Abs(float64(g1)-float64(g2)) +
				math.Abs(float64(b1)-float64(b2))

			// Jeśli różnica jest mała (szum kompresji / renderowania), uznajemy za taki sam
			if diff < 3000 { // Zakres uint16 to 0-65535, więc 3000 to mała różnica
				similarPixels++
			}
		}
	}

	if totalPixels == 0 {
		return false
	}
	similarity := float64(similarPixels) / float64(totalPixels)
	return similarity >= threshold
}

// cloneImage tworzy kopię obrazu, aby zapisać go w pamięci
func cloneImage(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

// --- FUNKCJE UTILITY (Te same co wcześniej) ---

func findTemplateWithAlpha(img *image.RGBA, tpl image.Image, threshold float64) (bool, float64) {
	tplBounds := tpl.Bounds()
	w, h := tplBounds.Dx(), tplBounds.Dy()
	imgBounds := img.Bounds()
	imgW, imgH := imgBounds.Dx(), imgBounds.Dy()

	if w > imgW || h > imgH {
		return false, 0
	}

	// Szybka konwersja
	tplRGBA, ok := tpl.(*image.RGBA)
	if !ok {
		b := tpl.Bounds()
		tplRGBA = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(tplRGBA, tplRGBA.Bounds(), tpl, b.Min, draw.Src)
	}

	step := 5 // Optymalizacja skanowania
	maxScore := 0.0

	for y := 0; y < imgH-h; y += step {
		for x := 0; x < imgW-w; x += step {
			score := calculateSimilarity(img, tplRGBA, x, y)
			if score > maxScore {
				maxScore = score
			}
			if score >= threshold {
				return true, score
			}
		}
	}
	return false, maxScore
}

func calculateSimilarity(screen *image.RGBA, tpl *image.RGBA, offX, offY int) float64 {
	matches := 0
	totalChecks := 0
	w, h := tpl.Bounds().Dx(), tpl.Bounds().Dy()
	checkStep := 2

	for y := 0; y < h; y += checkStep {
		for x := 0; x < w; x += checkStep {
			tr, tg, tb, ta := tpl.At(x, y).RGBA()
			if ta < 10000 {
				continue
			} // Ignoruj przezroczystość

			totalChecks++
			sr, sg, sb, _ := screen.At(offX+x, offY+y).RGBA()

			diff := math.Abs(float64(tr>>8)-float64(sr>>8)) +
				math.Abs(float64(tg>>8)-float64(sg>>8)) +
				math.Abs(float64(tb>>8)-float64(sb>>8))

			if diff < 100.0 {
				matches++
			}
		}
	}
	if totalChecks == 0 {
		return 0
	}
	return float64(matches) / float64(totalChecks)
}

func saveImageToDisk(img *image.RGBA, cfg *Config) (string, error) {
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

// --- SYSTEM GUI HELPERY ---

type point struct{ X, Y int32 }

func getCursorPos() (int, int) {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y)
}

func logToConsole(msg string) {
	if consoleEdit != nil && mw != nil {
		mw.Synchronize(func() {
			consoleEdit.AppendText(msg + "\r\n")
		})
	}
	log.Println(msg)
}

func updateImagePreview(path string) {
	if mw == nil || imgView == nil {
		return
	}
	mw.Synchronize(func() {
		img, err := walk.NewImageFromFile(path)
		if err == nil {
			imgView.SetImage(img)
		}
	})
}

func handleCalibration() {
	x, y := getCursorPos()
	switch calibrationStage {
	case 0:
		logToConsole("--- KALIBRACJA: Wybierz lewy-górny róg ---")
		calibrationStage = 1
	case 1:
		calibX, calibY = x, y
		logToConsole("--- KALIBRACJA: Wybierz prawy-dolny róg ---")
		calibrationStage = 2
	case 2:
		w, h := x-calibX, y-calibY
		if w < 0 {
			w = -w
			calibX = x
		}
		if h < 0 {
			h = -h
			calibY = y
		}
		cfg.Rect = Rect{calibX, calibY, w, h}
		saveConfig("config.json", cfg)
		logToConsole("--- KALIBRACJA ZAKOŃCZONA ---")
		calibrationStage = 0
	}
}

func hotkeyLoop() {
	hkCapture := hotkey.New(nil, hotkey.KeyF9)
	hkCapture.Register()
	defer hkCapture.Unregister()
	hkCalib := hotkey.New(nil, hotkey.KeyF8)
	hkCalib.Register()
	defer hkCalib.Unregister()

	for {
		select {
		case <-hkCapture.Keydown():
			if calibrationStage == 0 {
				logToConsole("Manualne zdjęcie (F9)")
				// Przy ręcznym zrzucie też aktualizujemy 'lastSavedImage',
				// żeby auto-tryb nie zrobił duplikatu zaraz potem
				bounds := image.Rect(cfg.Rect.X, cfg.Rect.Y, cfg.Rect.X+cfg.Rect.Width, cfg.Rect.Y+cfg.Rect.Height)
				img, _ := screenshot.CaptureRect(bounds)
				saveImageToDisk(img, cfg)
				mutex.Lock()
				lastSavedImage = cloneImage(img)
				mutex.Unlock()
			}
		case <-hkCalib.Keydown():
			handleCalibration()
		}
	}
}

func loadConfig(path string) (*Config, error) {
	// Try local
	f, err := os.Open(path)
	if err != nil {
		// Try parent
		f, err = os.Open("../" + path)
		if err != nil {
			return nil, err
		}
	}
	defer f.Close()
	c := &Config{}
	json.NewDecoder(f).Decode(c)
	return c, nil
}
func saveConfig(path string, c *Config) {
	// Try to save to where we loaded from?
	// Default to parent if existing there, else local.
	target := path
	if _, err := os.Stat("../" + path); err == nil {
		target = "../" + path
	}

	f, err := os.Create(target)
	if err == nil {
		defer f.Close()
		json.NewEncoder(f).Encode(c)
	}
}

func loadPngImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		// Try parent
		f, err = os.Open("../" + path)
		if err != nil {
			return nil, err
		}
	}
	defer f.Close()
	return png.Decode(f)
}
