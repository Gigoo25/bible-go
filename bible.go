package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type Bible map[string]map[string]map[string]string

type Verse struct {
	Book      string
	Chapter   int
	Verse     int
	Text      string
	lowerText string // precomputed lowercase Text, used by search
}

type BibleData struct {
	verses   []Verse
	bookList []string
	// bookLower holds bookList lowercased, index-aligned, for findBook.
	bookLower []string
	// chapterNums holds each book's chapter numbers in ascending order.
	chapterNums map[string][]int
	// bookVerses and chapterIndex are subslices of verses, so the text is
	// stored once however it is looked up.
	bookVerses   map[string][]Verse
	chapterIndex map[string]map[int][]Verse
}

type MultiBibleData struct {
	translations     map[string]*BibleData
	translationNames []string
	filePaths        map[string]string
	failed           map[string]bool // translations that failed to load; don't retry
	lastGood         *BibleData      // most recent successful load, used as last resort
}

func NewBibleData(jsonData []byte) (*BibleData, error) {
	var bible Bible
	if err := json.Unmarshal(jsonData, &bible); err != nil {
		return nil, fmt.Errorf("failed to parse bible JSON: %w", err)
	}

	totalVerses := 0
	for _, book := range bible {
		for _, chapter := range book {
			totalVerses += len(chapter)
		}
	}

	bd := &BibleData{
		verses:       make([]Verse, 0, totalVerses),
		bookList:     make([]string, 0, len(bible)),
		chapterNums:  make(map[string][]int, len(bible)),
		bookVerses:   make(map[string][]Verse, len(bible)),
		chapterIndex: make(map[string]map[int][]Verse, len(bible)),
	}

	bookSet := make(map[string]bool, len(bible))
	for _, bookName := range biblicalOrder {
		if _, exists := bible[bookName]; exists {
			bd.bookList = append(bd.bookList, bookName)
			bookSet[bookName] = true
		}
	}

	// Books outside the canonical list keep a stable (sorted) order.
	var extra []string
	for bookName := range bible {
		if !bookSet[bookName] {
			extra = append(extra, bookName)
		}
	}
	sort.Strings(extra)
	bd.bookList = append(bd.bookList, extra...)

	// Record index ranges first; subslices are taken once verses is complete.
	type span struct{ start, end int }
	chapterSpans := make(map[string]map[int]span, len(bible))
	bookSpans := make(map[string]span, len(bible))

	// Books without any verses are dropped so navigation never lands on one.
	books := bd.bookList[:0]
	for _, bookName := range bd.bookList {
		bookStart := len(bd.verses)
		spans := make(map[int]span)
		var nums []int

		for _, ch := range sortedNumericKeys(bible[bookName]) {
			chapterStart := len(bd.verses)
			for _, v := range sortedNumericKeys(ch.value) {
				bd.verses = append(bd.verses, Verse{
					Book:      bookName,
					Chapter:   ch.num,
					Verse:     v.num,
					Text:      v.value,
					lowerText: strings.ToLower(v.value),
				})
			}
			if len(bd.verses) > chapterStart {
				spans[ch.num] = span{chapterStart, len(bd.verses)}
				nums = append(nums, ch.num)
			}
		}

		if len(nums) == 0 {
			continue
		}
		books = append(books, bookName)
		chapterSpans[bookName] = spans
		bd.chapterNums[bookName] = nums
		bookSpans[bookName] = span{bookStart, len(bd.verses)}
	}
	bd.bookList = books

	for bookName, spans := range chapterSpans {
		chapters := make(map[int][]Verse, len(spans))
		for num, s := range spans {
			chapters[num] = bd.verses[s.start:s.end:s.end]
		}
		bd.chapterIndex[bookName] = chapters
		s := bookSpans[bookName]
		bd.bookVerses[bookName] = bd.verses[s.start:s.end:s.end]
	}

	bd.bookLower = make([]string, len(bd.bookList))
	for i, b := range bd.bookList {
		bd.bookLower[i] = strings.ToLower(b)
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

type numericKey[T any] struct {
	num   int
	value T
}

// sortedNumericKeys returns m's entries whose keys are integers, in
// ascending numeric order. Values are carried along so keys like "01" are
// never re-formatted and looked up again.
func sortedNumericKeys[T any](m map[string]T) []numericKey[T] {
	out := make([]numericKey[T], 0, len(m))
	for key, value := range m {
		if num, err := strconv.Atoi(key); err == nil {
			out = append(out, numericKey[T]{num, value})
		}
	}
	slices.SortFunc(out, func(a, b numericKey[T]) int { return a.num - b.num })
	return out
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
		failed:           make(map[string]bool),
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

	for _, file := range files {
		transName := strings.TrimSuffix(filepath.Base(file), "_bible.json")
		if transName == "" {
			continue
		}
		mbd.filePaths[transName] = file
		mbd.translationNames = append(mbd.translationNames, transName)
	}

	if len(mbd.translationNames) == 0 {
		return nil, fmt.Errorf("no bible JSON files found in %s (expected files like ESV_bible.json)", translationsDir)
	}

	sort.Strings(mbd.translationNames)

	return mbd, nil
}

// Load returns the named translation, loading it on first use. It returns
// nil if the translation is unknown or failed to load.
func (mbd *MultiBibleData) Load(translation string) *BibleData {
	if bd, exists := mbd.translations[translation]; exists {
		return bd
	}
	filePath, exists := mbd.filePaths[translation]
	if !exists || mbd.failed[translation] {
		return nil
	}
	bd, err := loadTranslation(filePath)
	if err != nil || len(bd.bookList) == 0 {
		mbd.failed[translation] = true
		return nil
	}
	mbd.translations[translation] = bd
	mbd.lastGood = bd
	return bd
}

// FirstLoadable returns the first translation, in name order, that loads.
func (mbd *MultiBibleData) FirstLoadable() (string, bool) {
	for _, name := range mbd.translationNames {
		if mbd.Load(name) != nil {
			return name, true
		}
	}
	return "", false
}

func (mbd *MultiBibleData) GetCurrentBibleData(translation string) *BibleData {
	if bd := mbd.Load(translation); bd != nil {
		return bd
	}
	return mbd.lastGood
}

func loadTranslation(filePath string) (*BibleData, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return NewBibleData(data)
}

func (bd *BibleData) GetBooks() []string {
	return bd.bookList
}

func (bd *BibleData) GetVerses(book string, chapter int) []Verse {
	return bd.chapterIndex[book][chapter]
}

// Chapters returns book's chapter numbers in ascending order.
func (bd *BibleData) Chapters(book string) []int {
	return bd.chapterNums[book]
}

type scoredVerse struct {
	verse Verse
	score int
}

// fuzzyMatchAndScore ranks lowerText against queryLower (both lowercased).
// Multi-word queries require every word; a contiguous phrase outranks the
// same words appearing scattered. Lower score = better.
func fuzzyMatchAndScore(lowerText, queryLower string, words []string) (matches bool, score int) {
	if queryLower == "" {
		return true, noMatchScore
	}

	if idx := strings.Index(lowerText, queryLower); idx >= 0 {
		return true, idx
	}

	if len(words) < 2 {
		return false, noMatchScore
	}

	total := 0
	for _, word := range words {
		idx := strings.Index(lowerText, word)
		if idx < 0 {
			return false, noMatchScore
		}
		total += idx
	}
	return true, wordMatchBase + total
}

const (
	noMatchScore  = 1000000
	wordMatchBase = 100000 // scattered multi-word matches rank below phrase matches
)

// scoreVerses returns the verses matching queryLower, best first, and
// whether any of them contains the query as a contiguous phrase.
func scoreVerses(verses []Verse, queryLower string) ([]Verse, bool) {
	words := strings.Fields(queryLower)
	var matches []scoredVerse
	phrase := false
	for _, verse := range verses {
		if match, score := fuzzyMatchAndScore(verse.lowerText, queryLower, words); match {
			matches = append(matches, scoredVerse{verse: verse, score: score})
			phrase = phrase || score < wordMatchBase
		}
	}
	return sortAndExtractVerses(matches), phrase
}

func sortAndExtractVerses(matches []scoredVerse) []Verse {
	slices.SortStableFunc(matches, func(a, b scoredVerse) int {
		return a.score - b.score
	})
	verses := make([]Verse, len(matches))
	for i := range matches {
		verses[i] = matches[i].verse
	}
	return verses
}

// findBook resolves a book name case-insensitively: an exact match wins,
// otherwise the first book (in canonical order) with that prefix.
func (bd *BibleData) findBook(bookName string) string {
	bookNameLower := strings.ToLower(strings.TrimSpace(bookName))
	if bookNameLower == "" {
		return ""
	}
	prefixMatch := ""
	for i, bookLower := range bd.bookLower {
		if bookLower == bookNameLower {
			return bd.bookList[i]
		}
		if prefixMatch == "" && strings.HasPrefix(bookLower, bookNameLower) {
			prefixMatch = bd.bookList[i]
		}
	}
	return prefixMatch
}

// Search resolves a reference ("John 3:16", "Psalm 23", "Rom 8:28-30")
// or else runs a text search. A query ending in a word, like "John love",
// is searched within that book, unless the whole query occurs as a phrase
// somewhere ("so loved" must not become a search of Song Of Solomon).
func (bd *BibleData) Search(query string) []Verse {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	if referenceResults := bd.searchByReference(query); len(referenceResults) > 0 {
		return referenceResults
	}

	queryLower := strings.ToLower(query)
	results, phrase := scoreVerses(bd.verses, queryLower)
	if phrase {
		return results
	}

	parts := strings.Fields(queryLower)
	if len(parts) >= 2 {
		bookName := strings.Join(parts[:len(parts)-1], " ")
		if matchedBook := bd.findBook(bookName); matchedBook != "" {
			if inBook, _ := scoreVerses(bd.bookVerses[matchedBook], parts[len(parts)-1]); len(inBook) > 0 {
				return inBook
			}
		}
	}

	return results
}

// searchByReference parses "Book Chapter", "Book Chapter:Verse" and
// "Book Chapter:From-To". It returns nil for anything else.
func (bd *BibleData) searchByReference(query string) []Verse {
	bookChapter, verseSpec, hasVerse := strings.Cut(query, ":")

	words := strings.Fields(bookChapter)
	if len(words) < 2 {
		return nil
	}
	chapterNum, err := strconv.Atoi(words[len(words)-1])
	if err != nil || chapterNum <= 0 {
		return nil
	}
	bookName := strings.Join(words[:len(words)-1], " ")

	verseFrom, verseTo := 0, 0 // 0 = whole chapter
	if hasVerse {
		from, to, isRange := strings.Cut(strings.TrimSpace(verseSpec), "-")
		if verseFrom, err = strconv.Atoi(strings.TrimSpace(from)); err != nil || verseFrom <= 0 {
			return nil
		}
		verseTo = verseFrom
		if isRange {
			if verseTo, err = strconv.Atoi(strings.TrimSpace(to)); err != nil || verseTo < verseFrom {
				return nil
			}
		}
	}

	matchedBook := bd.findBook(bookName)
	if matchedBook == "" {
		return nil
	}

	verses := bd.GetVerses(matchedBook, chapterNum)
	if verseFrom == 0 {
		return verses
	}
	var results []Verse
	for _, verse := range verses {
		if verse.Verse >= verseFrom && verse.Verse <= verseTo {
			results = append(results, verse)
		}
	}
	return results
}
