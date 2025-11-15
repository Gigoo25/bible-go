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

	biblicalOrder := []string{
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

	bd := &BibleData{
		verses:       make([]Verse, 0),
		bookList:     make([]string, 0, len(bible)),
		index:        make(map[string][]int),
		chapterIndex: make(map[string]map[int][]Verse),
	}

	for _, bookName := range biblicalOrder {
		if _, exists := bible[bookName]; exists {
			bd.bookList = append(bd.bookList, bookName)
		}
	}

	for bookName := range bible {
		found := false
		for _, orderedBook := range bd.bookList {
			if bookName == orderedBook {
				found = true
				break
			}
		}
		if !found {
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

				words := strings.Fields(strings.ToLower(text))
				for _, word := range words {
					cleanWord := strings.Trim(word, ".,;:!?\"'()[]")
					if len(cleanWord) > 2 {
						bd.index[cleanWord] = append(bd.index[cleanWord], len(bd.verses)-1)
					}
				}
			}
		}
	}

	return bd, nil
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

func NewMultiBibleData() (*MultiBibleData, error) {
	mbd := &MultiBibleData{
		translations:     make(map[string]*BibleData),
		translationNames: []string{},
		filePaths:        make(map[string]string),
	}

	files, err := filepath.Glob("bible-data/*_bible.json")
	if err != nil {
		return nil, fmt.Errorf("failed to glob bible files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no bible JSON files found (expected files like ESV_bible.json)")
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
		data, err := os.ReadFile(filePath)
		if err != nil {
			return mbd.getFallbackTranslation(translation)
		}

		bd, err := NewBibleData(data)
		if err != nil {
			return mbd.getFallbackTranslation(translation)
		}

		mbd.translations[translation] = bd
		return bd
	}

	if len(mbd.translationNames) > 0 {
		return mbd.GetCurrentBibleData(mbd.translationNames[0])
	}
	return nil
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
		cleanWord := strings.Trim(word, ".,;:!?\"'()[]")
		if strings.HasPrefix(cleanWord, patternLower) {
			return true, 100 + i
		}
		if strings.Contains(cleanWord, patternLower) {
			return true, 500 + i
		}
	}

	return false, 1000000
}

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
	for i, match := range matches {
		verses[i] = match.verse
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
			var matches []scoredVerse
			for _, verse := range bd.verses {
				if verse.Book == matchedBook {
					if match, score := fuzzyMatchAndScore(verse.Text, searchTerm); match {
						matches = append(matches, scoredVerse{verse: verse, score: score})
					}
				}
			}
			if len(matches) > 0 {
				return sortAndExtractVerses(matches)
			}
		}
	}

	// Optimize search using word index
	words := strings.Fields(strings.ToLower(query))
	var candidates []int
	for _, word := range words {
		clean := strings.Trim(word, ".,;:!?\"'()[]")
		if len(clean) > 2 {
			if indices, ok := bd.index[clean]; ok {
				if candidates == nil {
					candidates = make([]int, len(indices))
					copy(candidates, indices)
				} else {
					candidates = intersect(candidates, indices)
				}
			} else {
				candidates = nil
				break
			}
		}
	}

	if candidates != nil {
		var matches []scoredVerse
		for _, idx := range candidates {
			verse := bd.verses[idx]
			if match, score := fuzzyMatchAndScore(verse.Text, query); match {
				matches = append(matches, scoredVerse{verse: verse, score: score})
			}
		}
		return sortAndExtractVerses(matches)
	}

	// Fallback to full scan
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
