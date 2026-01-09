package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/paul-mannino/go-fuzzywuzzy"
)

// Definiujemy strukturę odpowiadającą danym w JSON
type Data struct {
	Image   string `json:"image_file"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

func isSimilar(text1 string, text2 string, threshold int) bool {
	score := fuzzy.Ratio(text1, text2)
	return score >= threshold
}

func main() {
	root := "./conversation"
	var allData []Data // Główna lista na wszystkie dane ze wszystkich plików

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && filepath.Ext(path) == ".json" {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			// Ponieważ w pliku jest tablica [{}, {}], tworzymy tymczasowy slice
			var fileEntries []Data
			if err := json.Unmarshal(content, &fileEntries); err != nil {
				fmt.Printf("Błąd w pliku %s: %v\n", path, err)
				return nil
			}

			// Używamy "...", aby dodać wszystkie elementy z fileEntries do allData
			allData = append(allData, fileEntries...)
		}
		return nil
	})

	if err != nil {
		fmt.Println("Błąd:", err)
	}

	fmt.Printf("Łącznie pobrano %d elementów z wielu plików.\n", len(allData))

	var foundWithThreshold int = 0
	var foundEqual int = 0

	for _, d := range allData {
		if isSimilar("Diederich", d.Speaker, 90) {
			foundWithThreshold++
			fmt.Printf("Speaker: %v | Text: %v\n", d.Speaker, d.Text)
		}

		if isSimilar("Diederich", d.Speaker, 90) {
			foundEqual++
		}
	}

	fmt.Printf("Łącznie znaleziono %v wypowiedzi (podobnych)\n", foundWithThreshold)
	fmt.Printf("Łącznie znaleziono %v wypowiedzi (równe)\n", foundEqual)
}
