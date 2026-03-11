package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
)

// --- Configuration ---
const (
	Port         = ":8080"
	LMStudioURL  = "http://localhost:1234/v1/chat/completions"
	OllamaURL    = "http://localhost:11434/api/chat"
	SystemPrompt = `You are an OCR tool for Visual Novels.
Extract the text from the dialogue box exactly as it appears.
1. If the speaker name is "???", output "Unknown" on the first line.
2. If there is no speaker name (narration), output "Narration" on the first line, and put the narration text between double asterisks (e.g. *The wind blows.*) on the second line.
3. If there are actions or sounds in the text (e.g. *sigh*), ensure they are enclosed in double asterisks *like this*.
4. Ignore strange symbols at the end of the sentence (e.g. paw prints, hearts, symbols). Do NOT transcribe them.
5. If the text consists solely of ellipses (e.g. "..." or "……"), transcribe them exactly.
6. Put the speaker name on the first line and the text on the second line.
7. Preserve original newlines in the text if possible, but keep it as one block of text.
8. Do NOT describe the visual scene or characters (e.g. "blonde girl"). Extract ONLY text from the text box.
9. If the extracted dialogue is wrapped in quotation marks (e.g. "Hello"), remove the outer quotes (output: Hello). Keep quotes if they are used inside the sentence.
10. Output ONLY the clean text. Do NOT use JSON.
If there is no text, output exactly: NONE`
)

// --- State ---
var (
	isProcessing   bool
	stopSignal     bool
	mu             sync.Mutex
	currentSession []ConversationEntry
	stats          ProcessingStats
)

type ProcessingStats struct {
	TotalImages     int     `json:"total_images"`
	ProcessedImages int     `json:"processed_images"`
	AvgTimePerImg   float64 `json:"avg_time_per_img"` // Seconds
	ETA             float64 `json:"eta"`              // Seconds
	CurrentFolder   string  `json:"current_folder"`   // Current actively processed folder
}

// --- Data Structures ---

type ProcessRequest struct {
	InputFolder  string  `json:"input_folder"`
	OutputFolder string  `json:"output_folder"`
	ResizeFactor float64 `json:"resize_factor"` // 0.1 to 1.0
	ModelID      string  `json:"model_id"`      // Model name for Ollama/LM Studio
	AIBackend    string  `json:"ai_backend"`    // "lmstudio" or "ollama"
	BatchMode    bool    `json:"batch_mode"`    // New flag
}

type AIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type OllamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

type ConversationEntry struct {
	ImageFile string `json:"image_file"`
	Speaker   string `json:"speaker"`
	Text      string `json:"text"`
	Meta      string `json:"meta,omitempty"`
}

func main() {
	r := gin.Default()

	// Static & Templates
	r.Static("/static", "./static")
	r.Static("/screenshots_view", "./screenshots")
	r.LoadHTMLGlob("templates/*")

	// Routes
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"DefaultPath": "./screenshots",
		})
	})

	r.GET("/api/folders", listFolders)
	r.GET("/api/files", listFiles)
	r.POST("/api/process/start", startProcessing)
	r.POST("/api/process/stop", stopProcessing)
	r.GET("/api/status", getStatus)
	r.GET("/api/session", getSession)

	r.GET("/review", func(c *gin.Context) {
		c.HTML(http.StatusOK, "review.html", nil)
	})

	r.GET("/api/conversations", listConversations)
	r.GET("/api/conversation/*filename", getConversation)
	r.POST("/api/conversation/*filename", saveConversation)

	log.Println("Server running on http://localhost" + Port)
	r.Run(Port)
}

func listConversations(c *gin.Context) {
	root := "conversation"
	var files []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // or nil to ignore
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
			rel, err := filepath.Rel(root, path)
			if err == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusOK, []string{})
		return
	}

	// Create "conversation" dir if not exists (first run)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		os.Mkdir(root, 0755)
	}

	sort.Strings(files)
	c.JSON(http.StatusOK, files)
}

func getConversation(c *gin.Context) {
	// *filename includes leading slash, e.g. "/v1/Chapter1.json"
	filename := strings.TrimPrefix(c.Param("filename"), "/")
	path := filepath.Join("conversation", filename)

	data, err := os.ReadFile(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	c.Data(http.StatusOK, "application/json", data)
}

func saveConversation(c *gin.Context) {
	filename := strings.TrimPrefix(c.Param("filename"), "/")
	path := filepath.Join("conversation", filename)

	var entries []ConversationEntry
	if err := c.BindJSON(&entries); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ensure dir exists (e.g. if saving new file directly via API, though rare)
	os.MkdirAll(filepath.Dir(path), 0755)

	saveJSON(path, entries)
	c.JSON(http.StatusOK, gin.H{"status": "Saved"})
}

type FolderInfo struct {
	Path       string `json:"path"`
	Processed  bool   `json:"processed"`
	ImageCount int    `json:"image_count"`
	EntryCount int    `json:"entry_count"`
}

func listFolders(c *gin.Context) {
	root := "./screenshots"
	var folders []FolderInfo

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root {
			rel, err := filepath.Rel(root, path)
			if err == nil {
				slashPath := filepath.ToSlash(rel)

				// 1. Count Images
				dirEntries, _ := os.ReadDir(path)
				imageCount := 0
				for _, de := range dirEntries {
					if !de.IsDir() && strings.HasSuffix(strings.ToLower(de.Name()), ".png") {
						imageCount++
					}
				}

				// 2. Check Processing Status & Count Entries
				jsonPath := filepath.Join("conversation", rel+".json")
				processed := false
				entryCount := 0

				if _, err := os.Stat(jsonPath); err == nil {
					processed = true
					// Parse JSON to count entries
					// Optimization: ReadFile is okay for small text files
					if data, err := os.ReadFile(jsonPath); err == nil {
						var items []ConversationEntry // We just need length
						if err := json.Unmarshal(data, &items); err == nil {
							entryCount = len(items)
						}
					}
				}

				folders = append(folders, FolderInfo{
					Path:       slashPath,
					Processed:  processed,
					ImageCount: imageCount,
					EntryCount: entryCount,
				})
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("List folders error: %v", err)
	}

	sort.Slice(folders, func(i, j int) bool {
		return folders[i].Path < folders[j].Path
	})
	c.JSON(http.StatusOK, folders)
}

func listFiles(c *gin.Context) {
	folder := c.Query("folder")
	if folder == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "folder required"})
		return
	}

	entries, err := os.ReadDir(folder)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var filenames []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".png") || strings.HasSuffix(e.Name(), ".jpg")) {
			filenames = append(filenames, e.Name())
		}
	}
	sort.Strings(filenames)
	c.JSON(http.StatusOK, filenames)
}

func startProcessing(c *gin.Context) {
	var req ProcessRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mu.Lock()
	if isProcessing {
		mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "Already processing"})
		return
	}
	isProcessing = true
	stopSignal = false
	currentSession = []ConversationEntry{}
	stats = ProcessingStats{}
	mu.Unlock()

	go processLoop(req)

	c.JSON(http.StatusOK, gin.H{"status": "Started"})
}

func stopProcessing(c *gin.Context) {
	mu.Lock()
	stopSignal = true
	mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"status": "Stopping..."})
}

func getStatus(c *gin.Context) {
	mu.Lock()
	defer mu.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"processing": isProcessing,
		"stats":      stats,
	})
}

func getSession(c *gin.Context) {
	mu.Lock()
	defer mu.Unlock()
	c.JSON(http.StatusOK, currentSession)
}

func processLoop(req ProcessRequest) {
	defer func() {
		mu.Lock()
		isProcessing = false
		mu.Unlock()
	}()

	type Job struct {
		InputPath  string // Absolute or relative to root
		OutputJSON string // Path to json
		RelPath    string // Relative to screenshots/
	}

	var jobs []Job
	root := "./screenshots"

	// UNIFIED LOGIC: Always recursive.
	startDir := root // Default "./screenshots"
	if req.InputFolder != "" {
		startDir = req.InputFolder
	}

	log.Printf("Scanning start dir: %s", startDir)

	err := filepath.WalkDir(startDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// If we are scanning a specific folder (Single Mode), we might encounter the folder itself.
		// We should skip the scan ROOT if it's just the container, UNLESS it has images directly.
		// But WalkDir processes the root first.

		if d.IsDir() {
			// Check if this folder has images (Leaf folder logic)
			entries, _ := os.ReadDir(path)
			hasImages := false
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
					hasImages = true
					break
				}
			}

			if hasImages {
				// Determine RELATIVE path from the Global Root (./screenshots)
				// This ensures output JSON structure is always mirrored correctly.
				rel, err := filepath.Rel(root, path)
				if err != nil {
					// Fallback if outside root (shouldn't happen nicely)
					rel = filepath.Base(path)
				}

				// If path is root itself, Rel is "."
				if rel == "." {
					// Handle case where images are at root of screenshots (rare/messy)
					// or usually unlikely.
					// But if startDir is subfolder, Rel will be "Subfolder". Correct.
				}

				outputJSON := filepath.Join("conversation", rel+".json")
				if rel == "." {
					outputJSON = filepath.Join("conversation", "root.json") // Safety fallback
				}

				// Check existence
				if _, err := os.Stat(outputJSON); err == nil {
					log.Printf("Skipping %s (already exists)", rel)
					return nil
				}

				jobs = append(jobs, Job{
					InputPath:  path,
					OutputJSON: outputJSON,
					RelPath:    rel,
				})
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Scan error: %v", err)
	}

	// Stats Init for Batch
	mu.Lock()
	stats.TotalImages = 0
	stats.ProcessedImages = 0
	mu.Unlock()

	startTime := time.Now()
	processedCountGlobal := 0

	for _, job := range jobs {
		mu.Lock()
		if stopSignal {
			mu.Unlock()
			break
		}
		mu.Unlock()

		log.Printf("Starting job: %s -> %s", job.RelPath, job.OutputJSON)

		// Update CurrentFolder in stats
		mu.Lock()
		stats.CurrentFolder = job.RelPath
		mu.Unlock()

		// 1. Get files for this job
		var filePaths []string
		filepath.WalkDir(job.InputPath, func(path string, d fs.DirEntry, err error) error {
			if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".png") {
				rel, _ := filepath.Rel(job.InputPath, path)
				// We want relative to the job folder for the JSON entry?
				// The previous code stored relative path from input folder.
				// e.g. "image01.png"
				filePaths = append(filePaths, filepath.ToSlash(rel))
			}
			return nil
		})
		sort.Strings(filePaths)

		// Create output dir structure
		os.MkdirAll(filepath.Dir(job.OutputJSON), 0755)

		jobSession := []ConversationEntry{}

		// Update global stats expectation
		mu.Lock()
		stats.TotalImages += len(filePaths)
		mu.Unlock()

		for _, imgRel := range filePaths {
			imgStartTime := time.Now()

			mu.Lock()
			if stopSignal {
				mu.Unlock()
				break
			}
			mu.Unlock()

			fullPath := filepath.Join(job.InputPath, imgRel)

			// Process
			log.Printf("Processing %s...", fullPath)
			extracted, err := processImage(fullPath, req.ResizeFactor, req.AIBackend, req.ModelID)

			entry := ConversationEntry{
				ImageFile: imgRel,
				Speaker:   "",
				Text:      "",
			}

			if err == nil {
				entry.Speaker = extracted.Speaker
				entry.Text = extracted.Text
			} else {
				log.Printf("Failed: %v", err)
				entry.Text = "ERROR: " + err.Error()
			}

			// Calc stats
			processedCountGlobal++
			imgDuration := time.Since(imgStartTime).Seconds()
			runningTime := time.Since(startTime).Seconds()

			mu.Lock()
			stats.ProcessedImages = processedCountGlobal
			stats.AvgTimePerImg = runningTime / float64(stats.ProcessedImages)
			remaining := stats.TotalImages - stats.ProcessedImages
			stats.ETA = float64(remaining) * stats.AvgTimePerImg

			// Update current session view (shows currently processing foler's items)
			currentSession = append(jobSession, entry)
			jobSession = currentSession // keep in sync
			mu.Unlock()

			log.Printf("Done in %.2fs. ETA: %.2fs", imgDuration, stats.ETA)

			// Save incremental
			saveJSON(job.OutputJSON, jobSession)
			time.Sleep(100 * time.Millisecond)
		}

		// Job done. Clear session?
		mu.Lock()
		currentSession = []ConversationEntry{} // Clear for next job
		mu.Unlock()
	}
}

func processImage(path string, resizeFactor float64, aiBackend string, modelID string) (*ConversationEntry, error) {
	// 1. Resize
	src, err := imaging.Open(path)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	width := float64(src.Bounds().Dx()) * resizeFactor
	dst := imaging.Resize(src, int(width), 0, imaging.Lanczos)

	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	var content string
	var parseErr error

	if aiBackend == "ollama" {
		content, parseErr = callOllama(encoded, modelID)
	} else {
		content, parseErr = callLMStudio(encoded, modelID)
	}

	if parseErr != nil {
		return nil, parseErr
	}

	// --- TEXT PARSING LOGIC ---
	if content == "NONE" {
		return &ConversationEntry{Speaker: "", Text: ""}, nil
	}

	var cleanedContent string
	if strings.HasPrefix(strings.ToLower(modelID), "glm-ocr") {
		// GLM-OCR might output text enclosed in markdown code blocks like ```markdown \n ... \n ```
		cleanedContent = strings.TrimSpace(content)
		if strings.HasPrefix(cleanedContent, "```") {
			lines := strings.Split(cleanedContent, "\n")
			if len(lines) > 2 {
				// Remove the first and last line (which are the markdown fences)
				cleanedContent = strings.Join(lines[1:len(lines)-1], "\n")
			}
		}
	} else {
		cleanedContent = content
	}

	lines := strings.Split(strings.TrimSpace(cleanedContent), "\n")
	var speaker, text string

	if strings.HasPrefix(strings.ToLower(modelID), "glm-ocr") {
		// Heuristics for GLM-OCR since we only use "Text Recognition:" prompt
		if len(lines) > 1 {
			firstLine := strings.TrimSpace(lines[0])
			isSpeaker := true

			// If it's too long, it's probably dialogue
			if len(firstLine) > 30 {
				isSpeaker = false
			}
			// If it starts with quotes or parentheses, it's dialogue
			if strings.HasPrefix(firstLine, "\"") || strings.HasPrefix(firstLine, "(") || strings.HasPrefix(firstLine, "“") || strings.HasPrefix(firstLine, "「") {
				isSpeaker = false
			}
			// If it ends with sentence punctuation, it's dialogue
			if strings.HasSuffix(firstLine, ".") || strings.HasSuffix(firstLine, "!") || strings.HasSuffix(firstLine, "?") || strings.HasSuffix(firstLine, "\"") || strings.HasSuffix(firstLine, "”") || strings.HasSuffix(firstLine, "」") {
				isSpeaker = false
			}

			if isSpeaker {
				speaker = firstLine
				text = strings.Join(lines[1:], "\n")
			} else {
				speaker = "Unknown"
				text = strings.Join(lines, "\n")
			}
		} else {
			speaker = "Unknown"
			text = lines[0]
		}

		log.Printf("=== GLM-OCR RAW ===\n%s\n===================", cleanedContent)
		log.Printf("Heuristic parsed -> Speaker: [%s], Text: [%s]", speaker, text)

		if speaker == "???" || speaker == "" {
			speaker = "Unknown"
		}

		// Clean up some known GLM-OCR emoji hallucinations
		text = strings.ReplaceAll(text, "🐺", "")
		text = strings.ReplaceAll(text, "♂", "")
		text = strings.ReplaceAll(text, "♀", "")
	} else {
		if len(lines) == 1 {
			text = lines[0]
			speaker = ""
		} else {
			speaker = strings.TrimSpace(lines[0])
			text = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}

	// Clean up surrounding quotes from text
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "\""), "\"")
	} else if strings.HasPrefix(text, "“") && strings.HasSuffix(text, "”") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "“"), "”")
	} else if strings.HasPrefix(text, "「") && strings.HasSuffix(text, "」") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "「"), "」")
	}

	return &ConversationEntry{
		Speaker: speaker,
		Text:    strings.TrimSpace(text),
	}, nil
}

func callLMStudio(encodedImage string, modelID string) (string, error) {
	var messages []interface{}

	if strings.HasPrefix(strings.ToLower(modelID), "glm-ocr") {
		// For GLM-OCR, rule 10 causes hallucinations, so we append an instruction to ignore it
		glmPrompt := SystemPrompt + "\nCRITICAL FOR GLM-OCR: Ignore rule 10. Just output the speaker name on line 1 and dialogue on line 2. Do NOT say 'Wait, the prompt says...'"
		messages = []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": glmPrompt,
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Extract conversation.",
					},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:image/jpeg;base64,%s", encodedImage),
						},
					},
				},
			},
		}
	} else {
		messages = []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": SystemPrompt,
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Extract conversation.",
					},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:image/jpeg;base64,%s", encodedImage),
						},
					},
				},
			},
		}
	}

	payload := map[string]interface{}{
		"model":       modelID, // Make sure modelID is passed, not generic "local-model"
		"messages":    messages,
		"max_tokens":  500,
		"temperature": 0.1,
	}

	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", LMStudioURL, bytes.NewBuffer(jsonPayload))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LM Studio Error: %s", string(body))
	}

	var aiResp AIResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return "", err
	}

	if len(aiResp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}

	return strings.TrimSpace(aiResp.Choices[0].Message.Content), nil
}

func callOllama(encodedImage string, modelID string) (string, error) {
	if modelID == "" {
		modelID = "gemma:latest" // Default fallback
	}

	var messages []interface{}

	if strings.HasPrefix(strings.ToLower(modelID), "glm-ocr") {
		glmPrompt := SystemPrompt + "\nCRITICAL FOR GLM-OCR: Ignore rule 10. Just output the speaker name on line 1 and dialogue on line 2. Do NOT say 'Wait, the prompt says...'"
		messages = []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": glmPrompt,
			},
			map[string]interface{}{
				"role":    "user",
				"content": "Extract conversation.",
				"images":  []string{encodedImage},
			},
		}
	} else {
		messages = []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": SystemPrompt,
			},
			map[string]interface{}{
				"role":    "user",
				"content": "Extract conversation.",
				"images":  []string{encodedImage},
			},
		}
	}

	payload := map[string]interface{}{
		"model":    modelID,
		"messages": messages,
		"options": map[string]interface{}{
			"num_gpu":     999, // Force GPU usage
			"temperature": 0.1,
		},
		"stream": false,
	}

	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", OllamaURL, bytes.NewBuffer(jsonPayload))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama Error: %s", string(body))
	}

	var aiResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return "", err
	}

	return strings.TrimSpace(aiResp.Message.Content), nil
}

func saveJSON(path string, data interface{}) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}
