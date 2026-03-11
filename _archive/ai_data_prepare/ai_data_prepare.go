package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const fileName = "./data.jsonl"

type NavBar struct {
	Link string
	Name string
}

var navPages = []NavBar{
	{Link: "/", Name: "➕ Nowy Wpis"},
	{Link: "/view", Name: "👁️ Lista JSONL"},
}

type TrainingData struct {
	Text string `json:"text"`
}

type sysPromptStruct struct {
	System map[string]string `json:"system"`
}

func main() {
	// 1. Inicjalizacja Gina w trybie Release (mniej logów) lub Debug
	r := gin.Default()

	// 2. Ładowanie szablonów HTML
	// 2. Ładowanie szablonów HTML
	r.LoadHTMLGlob("./ai_data_prepare/templates/*")
	r.Static("/assets", "./ai_data_prepare/static")

	// 3. Trasa: Strona główna (Formularz)
	r.GET("/", indexPage)

	// 4. Trasa: Zapisywanie danych (POST)
	r.POST("/save", saveData)

	// 5. Trasa: Podgląd danych
	r.GET("/view", viewPage)

	// 6. Uruchomienie (domyślnie na porcie 8080)
	fmt.Println("🚀 Gin Server wystartował na porcie :8081")
	r.Run(":8081")
}

func indexPage(c *gin.Context) {
	sysFile, err := os.ReadFile("./ai_data_prepare/systemPrompt.json")
	if err != nil {
		// JEŚLI TO ZOBACZYSZ, TO ZNACZY ŻE GO NIE WIDZI PLIKU
		fmt.Println("❌ BŁĄD ODCZYTU:", err)
	} else {
		fmt.Println("✅ PLIK WCZYTANY POPRAWNIE")
	}

	var conf sysPromptStruct
	err = json.Unmarshal(sysFile, &conf)

	// JEŚLI TO ZOBACZYSZ, TO MASZ BŁĄD W STRUKTURZE JSON
	fmt.Printf("DEBUG: Zawartość mapy po Unmarshal: %+v\n", conf.System)

	c.HTML(http.StatusOK, "index.html", gin.H{
		"Nav":                  navPages,
		"PromptSystemsOptions": conf.System,
	})
}

func saveData(c *gin.Context) {
	user := c.PostForm("user")
	assistant := c.PostForm("assistant")
	systemChoosen := c.PostForm("system")

	sysFile, _ := os.ReadFile("./ai_data_prepare/systemPrompt.json")

	var conf sysPromptStruct
	json.Unmarshal(sysFile, &conf)

	systemPrompt := conf.System[systemChoosen]

	var chatML string
	if systemPrompt != "None" {
		chatML = fmt.Sprintf("<|im_start|>system\n%s<|im_end|>\n<|im_start|>user\n%s<|im_end|>\n<|im_start|>assistant\n%s<|im_end|>", systemPrompt, user, assistant)
	} else {
		chatML = fmt.Sprintf("<|im_start|>user\n%s<|im_end|>\n<|im_start|>assistant\n%s<|im_end|>", user, assistant)
	}

	line, _ := json.Marshal(TrainingData{Text: chatML})

	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(string(line) + "\n")
		f.Close()
	}

	c.Redirect(http.StatusSeeOther, "/")
}

func viewPage(c *gin.Context) {
	var entries []string
	file, err := os.Open(fileName)
	if err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			entries = append(entries, scanner.Text())
		}
		file.Close()
	}

	c.HTML(http.StatusOK, "list.html", gin.H{
		"Nav":     navPages,
		"Entries": entries,
	})
}
