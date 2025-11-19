package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Bible map[string]map[string]map[string]string

type Verse struct {
	Book    string
	Chapter int
	Verse   int
	Text    string
}

type BibleData struct {
	verses       []Verse
	bookList     []string
	index        map[string][]int
	chapterIndex map[string]map[int][]Verse
}

type MultiBibleData struct {
	translations     map[string]*BibleData
	translationNames []string
	filePaths        map[string]string
}

func NewBibleData(jsonData []byte) (*BibleData, error) {
	var bible Bible
	if err := json.Unmarshal(jsonData, &bible); err != nil {
		return nil, fmt.Errorf("failed to parse bible JSON: %w", err)
	}

	bd := &BibleData{
		verses:       make([]Verse, 0),
		bookList:     make([]string, 0, len(bible)),
		index:        make(map[string][]int),
		chapterIndex: make(map[string]map[int][]Verse),
	}

	bookSet := make(map[string]bool, len(bible))
	for _, bookName := range biblicalOrder {
		if _, exists := bible[bookName]; exists {
			bd.bookList = append(bd.bookList, bookName)
			bookSet[bookName] = true
		}
	}

	for bookName := range bible {
		if !bookSet[bookName] {
			bd.bookList = append(bd.bookList, bookName)
		}
	}

	for _, bookName := range bd.bookList {
		chapters := sortMapKeysAsInts(bible[bookName])

		for _, chapterNum := range chapters {
			chapter := bible[bookName][strconv.Itoa(chapterNum)]
			verses := sortMapKeysAsInts(chapter)

			for _, verseNum := range verses {
				text := chapter[strconv.Itoa(verseNum)]

				verseObj := Verse{
					Book:    bookName,
					Chapter: chapterNum,
					Verse:   verseNum,
					Text:    text,
				}
				bd.verses = append(bd.verses, verseObj)

				if bd.chapterIndex[bookName] == nil {
					bd.chapterIndex[bookName] = make(map[int][]Verse)
				}
				bd.chapterIndex[bookName][chapterNum] = append(bd.chapterIndex[bookName][chapterNum], verseObj)

				for _, word := range strings.Fields(strings.ToLower(text)) {
					if cleanWord := cleanWord(word); len(cleanWord) > minWordLength {
						bd.index[cleanWord] = append(bd.index[cleanWord], len(bd.verses)-1)
					}
				}
			}
		}
	}

	return bd, nil
}

var biblicalOrder = []string{
	"Genesis", "Exodus", "Leviticus", "Numbers", "Deuteronomy",
	"Joshua", "Judges", "Ruth", "1 Samuel", "2 Samuel", "1 Kings", "2 Kings",
	"1 Chronicles", "2 Chronicles", "Ezra", "Nehemiah", "Esther", "Job", "Psalm",
	"Proverbs", "Ecclesiastes", "Song Of Solomon", "Isaiah", "Jeremiah",
	"Lamentations", "Ezekiel", "Daniel", "Hosea", "Joel", "Amos", "Obadiah",
	"Jonah", "Micah", "Nahum", "Habakkuk", "Zephaniah", "Haggai", "Zechariah", "Malachi",
	"Matthew", "Mark", "Luke", "John", "Acts", "Romans", "1 Corinthians", "2 Corinthians",
	"Galatians", "Ephesians", "Philippians", "Colossians", "1 Thessalonians", "2 Thessalonians",
	"1 Timothy", "2 Timothy", "Titus", "Philemon", "Hebrews", "James", "1 Peter", "2 Peter",
	"1 John", "2 John", "3 John", "Jude", "Revelation",
}

func sortMapKeysAsInts[T any](m map[string]T) []int {
	numbers := make([]int, 0, len(m))
	for key := range m {
		if num, err := strconv.Atoi(key); err == nil {
			numbers = append(numbers, num)
		}
	}
	sort.Ints(numbers)
	return numbers
}

func cleanWord(word string) string {
	return strings.Trim(word, ".,;:!?\"'()[]")
}

func getConfigDir() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(configDir, "bible-go"), nil
}

func ensureConfigDir() (string, error) {
	dir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func NewMultiBibleData() (*MultiBibleData, error) {
	mbd := &MultiBibleData{
		translations:     make(map[string]*BibleData),
		translationNames: []string{},
		filePaths:        make(map[string]string),
	}

	configDir, err := getConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config dir: %w", err)
	}
	translationsDir := filepath.Join(configDir, "translations")
	files, err := filepath.Glob(filepath.Join(translationsDir, "*_bible.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob bible files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no bible JSON files found in %s (expected files like ESV_bible.json)", translationsDir)
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_bible.json") {
			transName := strings.TrimSuffix(filepath.Base(file), "_bible.json")
			mbd.filePaths[transName] = file
			mbd.translationNames = append(mbd.translationNames, transName)
		}
	}

	if len(mbd.translationNames) == 0 {
		return nil, fmt.Errorf("no valid bible translation files found")
	}

	sort.Strings(mbd.translationNames)

	return mbd, nil
}

func (mbd *MultiBibleData) GetCurrentBibleData(translation string) *BibleData {
	if bd, exists := mbd.translations[translation]; exists {
		return bd
	}

	if filePath, exists := mbd.filePaths[translation]; exists {
		if bd := mbd.loadTranslation(filePath); bd != nil {
			mbd.translations[translation] = bd
			return bd
		}
	}

	return mbd.getFallbackTranslation(translation)
}

func (mbd *MultiBibleData) loadTranslation(filePath string) *BibleData {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	bd, err := NewBibleData(data)
	if err != nil {
		return nil
	}

	return bd
}

func (mbd *MultiBibleData) getFallbackTranslation(translation string) *BibleData {
	if len(mbd.translationNames) > 0 {
		fallback := mbd.translationNames[0]
		if fallback != translation {
			return mbd.GetCurrentBibleData(fallback)
		}
	}
	return nil
}

func (bd *BibleData) GetBooks() []string {
	return bd.bookList
}

func (bd *BibleData) GetVerses(book string, chapter int) []Verse {
	if chapters, ok := bd.chapterIndex[book]; ok {
		if verses, ok := chapters[chapter]; ok {
			return verses
		}
	}
	return []Verse{}
}

type scoredVerse struct {
	verse Verse
	score int
}

func fuzzyMatchAndScore(text, pattern string) (matches bool, score int) {
	if pattern == "" {
		return true, 1000000
	}

	textLower := strings.ToLower(text)
	patternLower := strings.ToLower(pattern)

	if idx := strings.Index(textLower, patternLower); idx >= 0 {
		return true, idx
	}

	words := strings.Fields(textLower)
	for i, word := range words {
		cleaned := cleanWord(word)
		if strings.HasPrefix(cleaned, patternLower) {
			return true, 100 + i
		}
		if strings.Contains(cleaned, patternLower) {
			return true, 500 + i
		}
	}

	return false, 1000000
}

const (
	noMatchScore    = 1000000
	minWordLength   = 2
	minSearchLength = 2
)

func intersect(a, b []int) []int {
	var result []int
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

func sortAndExtractVerses(matches []scoredVerse) []Verse {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score < matches[j].score
	})
	verses := make([]Verse, len(matches))
	for i := range matches {
		verses[i] = matches[i].verse
	}
	return verses
}

func (bd *BibleData) findBook(bookName string) string {
	bookNameLower := strings.ToLower(bookName)
	for _, book := range bd.bookList {
		bookLower := strings.ToLower(book)
		if bookLower == bookNameLower || strings.HasPrefix(bookLower, bookNameLower) {
			return book
		}
	}
	return ""
}

func (bd *BibleData) Search(query string) []Verse {
	if query == "" {
		return []Verse{}
	}

	if referenceResults := bd.searchByReference(query); len(referenceResults) > 0 {
		return referenceResults
	}

	parts := strings.Fields(query)
	if len(parts) >= 2 {
		bookName := strings.Join(parts[:len(parts)-1], " ")
		searchTerm := parts[len(parts)-1]

		if matchedBook := bd.findBook(bookName); matchedBook != "" {
			results := bd.searchInBook(matchedBook, searchTerm)
			if len(results) > 0 {
				return results
			}
		}
	}

	words := strings.Fields(strings.ToLower(query))
	candidates := bd.getCandidateIndices(words)

	if candidates != nil {
		return bd.scoreAndSortCandidates(candidates, query)
	}

	return bd.fullTextSearch(query)
}

func (bd *BibleData) searchInBook(bookName, searchTerm string) []Verse {
	var matches []scoredVerse
	for _, verse := range bd.verses {
		if verse.Book == bookName {
			if match, score := fuzzyMatchAndScore(verse.Text, searchTerm); match {
				matches = append(matches, scoredVerse{verse: verse, score: score})
			}
		}
	}
	return sortAndExtractVerses(matches)
}

func (bd *BibleData) getCandidateIndices(words []string) []int {
	var candidates []int
	for _, word := range words {
		if clean := cleanWord(word); len(clean) > minSearchLength {
			if indices, ok := bd.index[clean]; ok {
				if candidates == nil {
					candidates = make([]int, len(indices))
					copy(candidates, indices)
				} else {
					candidates = intersect(candidates, indices)
				}
			} else {
				return nil
			}
		}
	}
	return candidates
}

func (bd *BibleData) scoreAndSortCandidates(candidates []int, query string) []Verse {
	matches := make([]scoredVerse, 0, len(candidates))
	for _, idx := range candidates {
		verse := bd.verses[idx]
		if match, score := fuzzyMatchAndScore(verse.Text, query); match {
			matches = append(matches, scoredVerse{verse: verse, score: score})
		}
	}
	return sortAndExtractVerses(matches)
}

func (bd *BibleData) fullTextSearch(query string) []Verse {
	var matches []scoredVerse
	for _, verse := range bd.verses {
		if match, score := fuzzyMatchAndScore(verse.Text, query); match {
			matches = append(matches, scoredVerse{verse: verse, score: score})
		}
	}
	return sortAndExtractVerses(matches)
}

func (bd *BibleData) searchByReference(query string) []Verse {
	query = strings.TrimSpace(query)

	parts := strings.Split(query, ":")
	var bookChapter string
	var verseNum int

	if len(parts) == 2 {
		bookChapter = strings.TrimSpace(parts[0])
		if num, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			verseNum = num
		}
	} else {
		bookChapter = query
		verseNum = -1
	}

	words := strings.Fields(bookChapter)
	if len(words) == 0 {
		return nil
	}

	var bookName string
	var chapterNum int

	lastWord := words[len(words)-1]
	if num, err := strconv.Atoi(lastWord); err == nil && num > 0 {
		chapterNum = num
		bookName = strings.Join(words[:len(words)-1], " ")
	} else {
		bookName = strings.Join(words, " ")
		chapterNum = -1
	}

	if bookName == "" {
		return nil
	}

	matchedBook := bd.findBook(bookName)
	if matchedBook == "" {
		return nil
	}

	var results []Verse

	for _, verse := range bd.verses {
		if verse.Book == matchedBook {
			if chapterNum > 0 && verse.Chapter != chapterNum {
				continue
			}
			if verseNum > 0 && verse.Verse != verseNum {
				continue
			}
			results = append(results, verse)
		}
	}

	return results
}
