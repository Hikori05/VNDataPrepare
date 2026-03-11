package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type Config struct {
	Rect struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"rect"`
	ROIPresets map[string]struct {
		X int `json:"x"`
		Y int `json:"y"`
		W int `json:"w"`
		H int `json:"h"`
	} `json:"roi_presets,omitempty"`
}

type ConversationEntry struct {
	ImageFile string `json:"image_file"`
	Speaker   string `json:"speaker"`
	Text      string `json:"text"`
}

var (
	w                 fyne.Window
	videoPath         string
	totalFrames       int
	currentFrameIdx   int
	selectedIndices   map[int]bool
	isVideoLoaded     bool
	globalConfig      *Config
	gameName          string
	
	canvasWidth       int = 540
	canvasHeight      int = 960
	
	videoWidth        int
	videoHeight       int
	
	imgWidget         *canvas.Image
	statusLabel       *widget.Label
	roiLabel          *widget.Label
	pixelResultLabel  *widget.Label
	btnToggle         *widget.Button
	btnSmart          *widget.Button
	btnExport         *widget.Button
	gameEntry         *widget.Entry
	pixelThreshSlider *widget.Slider
	listbox           *widget.List
	orderedIndices    []int
	
	// Caching logic to speed up seek
	cachedFramePath   string
	cachedFrameIdx    int = -1
)

func init() {
	cachedFramePath = filepath.Join(os.TempDir(), "vn_extractor_current.png")
}

func loadConfig() {
	globalConfig = &Config{}
	file, err := os.Open("../config.json")
	if err != nil {
		fmt.Println("No ../config.json found")
		return
	}
	defer file.Close()
	json.NewDecoder(file).Decode(globalConfig)
}

func getFrameImage(frameIdx int) (image.Image, error) {
	// Use ffmpeg to extract a single frame exactly at the index
	// Optimization: extracting by frame number natively in ffmpeg is tricky, we use accurate seek by time or just -vf select
	// -vf "select=eq(n\,100)" is slow for long videos because it decodes from start.
	// We'll calculate time: time = frameIdx / fps
	
	// 1. Get FPS
	fpsCmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=r_frame_rate", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)
	fpsOut, err := fpsCmd.Output()
	var fps float64 = 30.0
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(fpsOut)), "/")
		if len(parts) == 2 {
			num, _ := strconv.ParseFloat(parts[0], 64)
			den, _ := strconv.ParseFloat(parts[1], 64)
			if den > 0 {
				fps = num / den
			}
		} else {
			fps, _ = strconv.ParseFloat(strings.TrimSpace(string(fpsOut)), 64)
		}
	}
	
	timeSec := float64(frameIdx) / fps
	timeStr := fmt.Sprintf("%.3f", timeSec)

	// Since accurate seeking (-ss before -i) is sometimes keyframe bound, 
	// for exact frame we use -ss before -i and then -frames:v 1
	// For perfect accuracy, we can do fast seek to 1 sec before, then slow seek.
	// But let's try direct fast seek first.
	cmd := exec.Command("ffmpeg", "-y", "-ss", timeStr, "-i", videoPath, "-frames:v", "1", "-q:v", "2", cachedFramePath)
	// Hide window on windows
	cmd.SysProcAttr = &exec.SysProcAttr{HideWindow: true}
	
	err = cmd.Run()
	if err != nil {
		return nil, err
	}
	
	// Read the extracted PNG/JPG
	file, err := os.Open(cachedFramePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	img, _, err := image.Decode(file)
	return img, err
}

func loadVideo() {
	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		
		path := reader.URI().Path()
		
		// Get video length in frames via ffprobe
		cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_packets", "-show_entries", "stream=nb_read_packets", "-of", "csv=p=0", path)
		cmd.SysProcAttr = &exec.SysProcAttr{HideWindow: true}
		out, err := cmd.Output()
		if err != nil {
			dialog.ShowError(fmt.Errorf("FFMPEG/FFPROBE not found or error. Make sure ffmpeg is in PATH!"), w)
			return
		}
		
		totalFrames, err = strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil || totalFrames <= 0 {
			dialog.ShowError(fmt.Errorf("Could not determine frame count"), w)
			return
		}
		
		// Get resolution
		resCmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", path)
		resCmd.SysProcAttr = &exec.SysProcAttr{HideWindow: true}
		resOut, _ := resCmd.Output()
		resParts := strings.Split(strings.TrimSpace(string(resOut)), "x")
		if len(resParts) == 2 {
			videoWidth, _ = strconv.Atoi(resParts[0])
			videoHeight, _ = strconv.Atoi(resParts[1])
		}
		
		videoPath = path
		currentFrameIdx = 0
		selectedIndices = make(map[int]bool)
		isVideoLoaded = true
		orderedIndices = []int{}
		
		if videoWidth > videoHeight {
			canvasWidth = 1024
			canvasHeight = 576
		} else {
			canvasWidth = 540
			canvasHeight = 960
		}
		
		imgWidget.SetMinSize(fyne.NewSize(float32(canvasWidth), float32(canvasHeight)))
		
		btnExport.Enable()
		updateView()
		updateSidebar()
		
	}, w)
}

func seek(delta int) {
	if !isVideoLoaded {
		return
	}
	currentFrameIdx += delta
	if currentFrameIdx < 0 {
		currentFrameIdx = 0
	}
	if currentFrameIdx >= totalFrames {
		currentFrameIdx = totalFrames - 1
	}
	updateView()
}

func getAveragePixelLuma(img image.Image, rect image.Rectangle) float64 {
	bounds := img.Bounds()
	
	// Intersect requested rect with actual image bounds
	rect = rect.Intersect(bounds)
	if rect.Empty() {
		return 0
	}
	
	var sum float64
	var count float64
	
	// Basic luma calculation for the region
	for y := rect.Min.Y; y < rect.Max.Y; y+=2 { // Step by 2 for speed
		for x := rect.Min.X; x < rect.Max.X; x+=2 {
			r, g, b, _ := img.At(x, y).RGBA()
			// Basic luminance formula
			luma := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
			sum += luma
			count++
		}
	}
	if count == 0 { return 0 }
	return sum / count
}

func seekNextChange() {
	if !isVideoLoaded {
		return
	}
	
	if globalConfig == nil || globalConfig.Rect.Width == 0 {
		dialog.ShowInformation("Missing ROI", "Please use the Python version to draw a valid ROI first, or edit config.json manually.", w)
		return
	}
	
	btnSmart.SetText("Searching...")
	btnSmart.Disable()
	
	go func() {
		defer func() {
			btnSmart.Enable()
			btnSmart.SetText("Find Next Change 🔍")
		}()
		
		// thresholdPx is conceptual here. We use Luma difference since we do it purely in memory.
		thresholdLumaDiff := float64(pixelThreshSlider.Value) / 10.0 // Scaled down for luma diff 0-255
		
		currentImg, err := getFrameImage(currentFrameIdx)
		if err != nil { return }
		
		rect := image.Rect(
			globalConfig.Rect.X, 
			globalConfig.Rect.Y, 
			globalConfig.Rect.X+globalConfig.Rect.Width, 
			globalConfig.Rect.Y+globalConfig.Rect.Height,
		)
		
		currentLuma := getAveragePixelLuma(currentImg, rect)
		
		searchIdx := currentFrameIdx + 1
		found := false
		limit := searchIdx + 1000 // FFMPEG single frame extraction is slower than python, reduce limit
		if limit > totalFrames {
			limit = totalFrames
		}
		step := 10 // Larger step since FFMPEG spawn is slow
		
		var lastDiff float64
		
		for searchIdx < limit {
			targetImg, err := getFrameImage(searchIdx)
			if err != nil { break }
			
			targetLuma := getAveragePixelLuma(targetImg, rect)
			diff := targetLuma - currentLuma
			if diff < 0 { diff = -diff }
			
			lastDiff = diff
			
			if diff > thresholdLumaDiff {
				currentFrameIdx = searchIdx
				found = true
				break
			}
			
			searchIdx += step
			pixelResultLabel.SetText(fmt.Sprintf("Diff: %.1f", diff))
		}
		
		if found {
			pixelResultLabel.SetText(fmt.Sprintf("Diff: %.1f", lastDiff))
			updateView()
		} else {
			pixelResultLabel.SetText("No Changes Found")
		}
	}()
}

func toggleSelection() {
	if !isVideoLoaded {
		return
	}
	if selectedIndices[currentFrameIdx] {
		delete(selectedIndices, currentFrameIdx)
	} else {
		selectedIndices[currentFrameIdx] = true
	}
	updateView()
	updateSidebar()
}

func updateSidebar() {
	orderedIndices = []int{}
	for idx := range selectedIndices {
		orderedIndices = append(orderedIndices, idx)
	}
	sort.Ints(orderedIndices)
	listbox.Refresh()
}

func updateView() {
	if !isVideoLoaded {
		return
	}
	
	img, err := getFrameImage(currentFrameIdx)
	if err != nil {
		return
	}
	
	// Add red tint to image if marked? Fyne canvas image doesn't support easy overlay drawing without Custom widgets.
	// We just display it.
	imgWidget.Image = img
	imgWidget.Refresh()
	
	if selectedIndices[currentFrameIdx] {
		btnToggle.SetText("MARKED (Remove)")
	} else {
		btnToggle.SetText("MARK FRAME")
	}
	
	statusLabel.SetText(fmt.Sprintf("Frame: %d / %d", currentFrameIdx, totalFrames))
	
	if globalConfig != nil {
		// Update ROI text
		rx := globalConfig.Rect.X
		ry := globalConfig.Rect.Y
		rw := globalConfig.Rect.Width
		rh := globalConfig.Rect.Height
		if rw > 0 {
			roiLabel.SetText(fmt.Sprintf("ROI: Active (x:%d, y:%d, %dx%d)", rx, ry, rw, rh))
		}
	}
}

func exportFrames() {
	if len(selectedIndices) == 0 {
		return
	}
	
	gName := strings.TrimSpace(gameEntry.Text)
	if gName == "" {
		dialog.ShowError(fmt.Errorf("Please enter a Game / Folder name!"), w)
		return
	}
	
	workspaceRoot := ".."
	screenshotsDir := filepath.Join(workspaceRoot, "screenshots")
	conversationDir := filepath.Join(workspaceRoot, "conversation")
	
	targetImgDir := filepath.Join(screenshotsDir, gName)
	targetJsonPath := filepath.Join(conversationDir, fmt.Sprintf("%s.json", gName))
	
	os.MkdirAll(targetImgDir, 0755)
	os.MkdirAll(conversationDir, 0755)
	
	btnExport.SetText("EXPORTING...")
	btnExport.Disable()
	
	go func() {
		defer func() {
			btnExport.SetText("EXPORT TO BACKEND 🚀")
			btnExport.Enable()
		}()
		
		var jsonEntries []ConversationEntry
		
		for i, idx := range orderedIndices {
			statusLabel.SetText(fmt.Sprintf("Exporting %d/%d (FFMPEG)...", i+1, len(orderedIndices)))
			
			imgFilename := fmt.Sprintf("%s_%03d.png", gName, i+1)
			imgPath := filepath.Join(targetImgDir, imgFilename)
			
			// Use FFmpeg to dump exact frame perfectly to PNG
			// Can just calculate time again
			
			fpsCmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=r_frame_rate", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)
			fpsOut, _ := fpsCmd.Output()
			var fps float64 = 30.0
			parts := strings.Split(strings.TrimSpace(string(fpsOut)), "/")
			if len(parts) == 2 {
				num, _ := strconv.ParseFloat(parts[0], 64)
				den, _ := strconv.ParseFloat(parts[1], 64)
				if den > 0 { fps = num / den }
			}
			timeSec := float64(idx) / fps
			timeStr := fmt.Sprintf("%.3f", timeSec)

			cmd := exec.Command("ffmpeg", "-y", "-ss", timeStr, "-i", videoPath, "-frames:v", "1", imgPath)
			cmd.SysProcAttr = &exec.SysProcAttr{HideWindow: true}
			cmd.Run()
			
			relPath := filepath.Join(gName, imgFilename)
			
			jsonEntries = append(jsonEntries, ConversationEntry{
				ImageFile: filepath.ToSlash(relPath),
				Speaker:   "",
				Text:      "",
			})
		}
		
		// Json writing
		var existingEntries []ConversationEntry
		if data, err := os.ReadFile(targetJsonPath); err == nil {
			json.Unmarshal(data, &existingEntries)
		}
		
		if len(existingEntries) == 0 {
			saveJSON(targetJsonPath, jsonEntries)
		} else {
			existingMap := make(map[string]bool)
			for _, e := range existingEntries {
				existingMap[e.ImageFile] = true
			}
			for _, ne := range jsonEntries {
				if !existingMap[ne.ImageFile] {
					existingEntries = append(existingEntries, ne)
				}
			}
			saveJSON(targetJsonPath, existingEntries)
		}
		
		dialog.ShowInformation("Success", fmt.Sprintf("Exported %d frames!\nPath: %s", len(orderedIndices), targetImgDir), w)
	}()
}

func loadPresetROI() {
	gName := strings.TrimSpace(gameEntry.Text)
	if globalConfig != nil && globalConfig.ROIPresets != nil {
		if preset, exists := globalConfig.ROIPresets[gName]; exists {
			globalConfig.Rect.X = preset.X
			globalConfig.Rect.Y = preset.Y
			globalConfig.Rect.Width = preset.W
			globalConfig.Rect.Height = preset.H
			updateView()
			return
		}
	}
	dialog.ShowInformation("Info", "No ROI found for this game. Fallback to global config or empty.", w)
}

func saveJSON(path string, data interface{}) {
	f, err := os.Create(path)
	if err != nil { return }
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}

func main() {
	loadConfig()
	selectedIndices = make(map[int]bool)
	
	a := app.New()
	a.Settings().SetTheme(theme.DarkTheme())
	
	w = a.NewWindow("VN Video Extractor API (Pure Go + ffmpeg)")
	w.Resize(fyne.NewSize(1400, 950))
	
	// Layout
	statusLabel = widget.NewLabel("Select a video to begin...")
	statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	
	roiLabel = widget.NewLabel("ROI: Loaded from config.json")
	
	infoFrame := container.NewHBox(roiLabel, layoutSpacer(), statusLabel, layoutSpacer())
	
	imgWidget = canvas.NewImageFromFile("")
	imgWidget.FillMode = canvas.ImageFillContain
	imgWidget.SetMinSize(fyne.NewSize(float32(canvasWidth), float32(canvasHeight)))
	
	imgContainer := container.NewCenter(imgWidget)
	
	btnLoad := widget.NewButton("Open Video", loadVideo)
	btnToggle = widget.NewButton("MARK FRAME", toggleSelection)
	
	controlRow := container.NewHBox(
		btnLoad,
		widget.NewButton("<<", func(){ seek(-100) }),
		widget.NewButton("< 10", func(){ seek(-10) }),
		widget.NewButton("< 1", func(){ seek(-1) }),
		btnToggle,
		widget.NewButton("1 >", func(){ seek(1) }),
		widget.NewButton("10 >", func(){ seek(10) }),
		widget.NewButton(">>", func(){ seek(100) }),
	)
	
	pixelThreshSlider = widget.NewSlider(10, 500)
	pixelThreshSlider.SetValue(100)
	pixelResultLabel = widget.NewLabel("Diff: - ")
	btnSmart = widget.NewButton("Find Next Change 🔍", seekNextChange)
	
	smartRow := container.NewHBox(
		widget.NewLabel("Smart Diff (Luma based):"),
		pixelThreshSlider,
		btnSmart,
		pixelResultLabel,
	)
	
	leftPanel := container.NewBorder(
		container.NewVBox(infoFrame),
		container.NewVBox(controlRow, smartRow),
		nil, nil,
		imgContainer,
	)
	
	// Right Panel
	gameName = "my_game"
	gameEntry = widget.NewEntry()
	gameEntry.SetText(gameName)
	
	btnLoadROI := widget.NewButton("Load ROI for Game", loadPresetROI)
	
	gameConfigFrame := container.NewVBox(
		widget.NewLabelWithStyle("Game / Folder Name:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		gameEntry,
		btnLoadROI,
	)
	
	listbox = widget.NewList(
		func() int { return len(orderedIndices) },
		func() fyne.CanvasObject { return widget.NewLabel("Frame 000000") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(fmt.Sprintf("%d. Frame %d", i+1, orderedIndices[i]))
		},
	)
	listbox.OnSelected = func(id widget.ListItemID) {
		currentFrameIdx = orderedIndices[id]
		updateView()
	}
	
	btnExport = widget.NewButton("EXPORT TO BACKEND 🚀", exportFrames)
	btnExport.Disable()
	
	rightPanel := container.NewBorder(
		gameConfigFrame,
		container.NewVBox(widget.NewSeparator(), btnExport),
		nil, nil,
		listbox,
	)
	
	split := container.NewHSplit(leftPanel, rightPanel)
	split.SetOffset(0.75) // 75% left, 25% right
	
	w.SetContent(split)
	w.ShowAndRun()
}

func layoutSpacer() fyne.CanvasObject {
	return canvas.NewRectangle(color.Transparent)
}
