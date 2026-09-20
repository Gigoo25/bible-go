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
	"unicode"
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
	verses       []Verse
	bookList     []string
	index        map[string][]int
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
		index:        make(map[string][]int, 16384),
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

	var wordBuf []string
	for _, bookName := range bd.bookList {
		chapters := sortMapKeysAsInts(bible[bookName])

		for _, chapterNum := range chapters {
			chapter := bible[bookName][strconv.Itoa(chapterNum)]
			verses := sortMapKeysAsInts(chapter)

			for _, verseNum := range verses {
				text := chapter[strconv.Itoa(verseNum)]
				// Lowercase once at load: both the index build and every
				// search re-use it instead of re-lowercasing per query.
				lower := strings.ToLower(text)

				verseObj := Verse{
					Book:      bookName,
					Chapter:   chapterNum,
					Verse:     verseNum,
					Text:      text,
					lowerText: lower,
				}
				bd.verses = append(bd.verses, verseObj)

				if bd.chapterIndex[bookName] == nil {
					bd.chapterIndex[bookName] = make(map[int][]Verse)
				}
				bd.chapterIndex[bookName][chapterNum] = append(bd.chapterIndex[bookName][chapterNum], verseObj)

				verseIdx := len(bd.verses) - 1
				wordBuf = splitFields(wordBuf[:0], lower)
				for _, word := range wordBuf {
					if cleanWord := cleanWord(word); len(cleanWord) > minWordLength {
						// Dedupe: a word repeated in a verse gets one posting.
						if postings := bd.index[cleanWord]; len(postings) == 0 || postings[len(postings)-1] != verseIdx {
							bd.index[cleanWord] = append(postings, verseIdx)
						}
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

// splitFields appends the whitespace-separated words of s to dst, matching
// the semantics of strings.Fields but reusing the caller's buffer.
func splitFields(dst []string, s string) []string {
	start := -1
	for i, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				dst = append(dst, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		dst = append(dst, s[start:])
	}
	return dst
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

	if !mbd.failed[translation] {
		if filePath, exists := mbd.filePaths[translation]; exists {
			if bd, err := mbd.loadTranslation(filePath); err == nil {
				mbd.translations[translation] = bd
				mbd.lastGood = bd
				return bd
			}
			mbd.failed[translation] = true
		}
	}

	// Fall back to the first translation, or to the last one that loaded.
	if len(mbd.translationNames) > 0 {
		fallback := mbd.translationNames[0]
		if fallback != translation {
			if bd := mbd.GetCurrentBibleData(fallback); bd != nil {
				return bd
			}
		}
	}
	return mbd.lastGood
}

func (mbd *MultiBibleData) loadTranslation(filePath string) (*BibleData, error) {
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

// fuzzyMatchAndScore ranks lowerText against queryLower (both lowercased).
// Multi-word queries require every word; a contiguous phrase outranks the
// same words appearing scattered. Lower score = better.
func fuzzyMatchAndScore(lowerText, queryLower string) (matches bool, score int) {
	if queryLower == "" {
		return true, noMatchScore
	}

	if idx := strings.Index(lowerText, queryLower); idx >= 0 {
		return true, idx
	}

	if !strings.ContainsAny(queryLower, " \t\n\v\f\r") {
		return false, noMatchScore
	}

	words := strings.Fields(queryLower)
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
	noMatchScore    = 1000000
	wordMatchBase   = 100000 // scattered multi-word matches rank below phrase matches
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
	slices.SortStableFunc(matches, func(a, b scoredVerse) int {
		return a.score - b.score
	})
	verses := make([]Verse, len(matches))
	for i := range matches {
		verses[i] = matches[i].verse
	}
	return verses
}

func (bd *BibleData) findBook(bookName string) string {
	bookNameLower := strings.ToLower(bookName)
	prefixMatch := ""
	for _, book := range bd.bookList {
		bookLower := strings.ToLower(book)
		if bookLower == bookNameLower {
			return book
		}
		if prefixMatch == "" && strings.HasPrefix(bookLower, bookNameLower) {
			prefixMatch = book
		}
	}
	return prefixMatch
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

	queryLower := strings.ToLower(query)
	words := strings.Fields(queryLower)
	if candidates, ok := bd.getCandidateIndices(words); ok {
		return bd.scoreAndSortCandidates(candidates, queryLower)
	}

	return bd.fullTextSearch(queryLower)
}

func (bd *BibleData) searchInBook(bookName, searchTerm string) []Verse {
	chapters := bd.chapterIndex[bookName]
	chapterNums := make([]int, 0, len(chapters))
	for ch := range chapters {
		chapterNums = append(chapterNums, ch)
	}
	sort.Ints(chapterNums)

	searchLower := strings.ToLower(searchTerm)
	var matches []scoredVerse
	for _, ch := range chapterNums {
		for _, verse := range chapters[ch] {
			if match, score := fuzzyMatchAndScore(verse.lowerText, searchLower); match {
				matches = append(matches, scoredVerse{verse: verse, score: score})
			}
		}
	}
	return sortAndExtractVerses(matches)
}

// getCandidateIndices intersects the posting lists for the query's indexed
// words. ok=false means the index cannot be used (no indexable words, or a
// word is missing from it), so the caller must fall back to a full scan.
// A non-nil, empty result is a genuine empty intersection: no fallback is
// needed because no verse can contain all the words.
func (bd *BibleData) getCandidateIndices(words []string) ([]int, bool) {
	var candidates []int
	seeded := false
	for _, word := range words {
		clean := cleanWord(word)
		if len(clean) <= minSearchLength {
			continue
		}
		indices, ok := bd.index[clean]
		if !ok {
			return nil, false
		}
		if !seeded {
			candidates = indices
			seeded = true
			continue
		}
		candidates = intersect(candidates, indices)
	}
	if !seeded {
		return nil, false
	}
	if candidates == nil {
		return []int{}, true
	}
	return candidates, true
}

func (bd *BibleData) scoreAndSortCandidates(candidates []int, queryLower string) []Verse {
	matches := make([]scoredVerse, 0, len(candidates))
	for _, idx := range candidates {
		verse := bd.verses[idx]
		if match, score := fuzzyMatchAndScore(verse.lowerText, queryLower); match {
			matches = append(matches, scoredVerse{verse: verse, score: score})
		}
	}
	return sortAndExtractVerses(matches)
}

func (bd *BibleData) fullTextSearch(queryLower string) []Verse {
	var matches []scoredVerse
	for _, verse := range bd.verses {
		if match, score := fuzzyMatchAndScore(verse.lowerText, queryLower); match {
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

	// A bare book name (no chapter or verse) is a search term, not a
	// reference; returning the whole book here would shadow text search.
	if chapterNum <= 0 && verseNum <= 0 {
		return nil
	}

	matchedBook := bd.findBook(bookName)
	if matchedBook == "" {
		return nil
	}

	var results []Verse

	chapters := bd.chapterIndex[matchedBook]
	chapterNums := make([]int, 0, len(chapters))
	for ch := range chapters {
		if chapterNum > 0 && ch != chapterNum {
			continue
		}
		chapterNums = append(chapterNums, ch)
	}
	sort.Ints(chapterNums)

	for _, ch := range chapterNums {
		for _, verse := range chapters[ch] {
			if verseNum > 0 && verse.Verse != verseNum {
				continue
			}
			results = append(results, verse)
		}
	}

	return results
}
