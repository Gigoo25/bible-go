package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type model struct {
	multiBibleData     *MultiBibleData
	currentTranslation string
	currentBook        string
	currentChapter     int
	verses             []Verse
	searchQuery        string
	searchResults      []Verse
	searchIndex        int
	bookmarks          []Bookmark
	statusMsg          string
	mode               mode
	selected           int
	scrollOffset       int
	savedSelected      int // reading position stashed while in a menu
	savedScroll        int
	height             int
	width              int
	config             Config
	bookStyle          lipgloss.Style
	verseNumStyle      lipgloss.Style
	textStyle          lipgloss.Style
	dimStyle           lipgloss.Style
	markStyle          lipgloss.Style
	footerStyle        lipgloss.Style
	bookmarkSet        map[Bookmark]bool
	zenMode            bool
}

type Bookmark struct {
	Book    string `json:"book"`
	Chapter int    `json:"chapter"`
	Verse   int    `json:"verse"`
}

func (m *model) getBibleData() *BibleData {
	return m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
}

type mode int

const (
	navigationMode mode = iota
	searchMode
	bookmarksMode
)

type AppState struct {
	CurrentTranslation string     `json:"currentTranslation"`
	CurrentBook        string     `json:"currentBook"`
	CurrentChapter     int        `json:"currentChapter"`
	Selected           int        `json:"selected"`
	ScrollOffset       int        `json:"scrollOffset"`
	ZenMode            bool       `json:"zenMode"`
	Bookmarks          []Bookmark `json:"bookmarks,omitempty"`
}

type Config struct {
	Theme          string `json:"theme,omitempty"`
	HighlightColor string `json:"highlightColor"`
	VerseNumColor  string `json:"verseNumColor"`
	TextColor      string `json:"textColor"`
	DimColor       string `json:"dimColor"`
	MaxWidth       int    `json:"maxWidth,omitempty"` // reading column cap; 0 = default (80)
}

// Built-in palettes, selected via "theme" in config.json.
// Explicitly set color fields override the theme's values.
var themes = map[string]Config{
	"catppuccin-mocha": {HighlightColor: "#cba6f7", VerseNumColor: "#89b4fa", TextColor: "#cdd6f4", DimColor: "#313244"},
	"catppuccin-latte": {HighlightColor: "#8839ef", VerseNumColor: "#1e66f5", TextColor: "#4c4f69", DimColor: "#ccd0da"},
	"gruvbox":          {HighlightColor: "#d3869b", VerseNumColor: "#83a598", TextColor: "#ebdbb2", DimColor: "#504945"},
	"nord":             {HighlightColor: "#b48ead", VerseNumColor: "#81a1c1", TextColor: "#d8dee9", DimColor: "#4c566a"},
	"dracula":          {HighlightColor: "#bd93f9", VerseNumColor: "#8be9fd", TextColor: "#f8f8f2", DimColor: "#6272a4"},
	"everforest":       {HighlightColor: "#a7c080", VerseNumColor: "#dbbc7f", TextColor: "#d3c6aa", DimColor: "#475258"},
	"tokyonight":       {HighlightColor: "#bb9af7", VerseNumColor: "#7aa2f7", TextColor: "#c0caf5", DimColor: "#414868"},
}

const (
	stateFile  = "state.json"
	configFile = "config.json"
)

func getFilePath(filename string) (string, error) {
	dir, err := ensureConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

func saveJSON(filename string, data interface{}) error {
	path, err := getFilePath(filename)
	if err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, jsonData, 0o644)
}

func loadJSON(filename string, target interface{}) error {
	path, err := getFilePath(filename)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}

func saveState(state AppState) error {
	return saveJSON(stateFile, state)
}

func loadState() AppState {
	var state AppState
	if err := loadJSON(stateFile, &state); err != nil || state.CurrentTranslation == "" {
		return getDefaultAppState()
	}
	return state
}

func saveConfig(config Config) error {
	return saveJSON(configFile, config)
}

func loadConfig() (Config, error) {
	var config Config
	err := loadJSON(configFile, &config)
	if os.IsNotExist(err) {
		// First run: write defaults. On a parse error, keep the user's
		// file intact and just run with defaults.
		saveConfig(getDefaultConfig())
		return getDefaultConfig(), nil
	} else if err != nil {
		return getDefaultConfig(), nil
	}

	// Theme provides the base palette; explicit colors override it.
	base := getDefaultConfig()
	if t, ok := themes[strings.ToLower(config.Theme)]; ok {
		base = t
	}
	if config.HighlightColor == "" {
		config.HighlightColor = base.HighlightColor
	}
	if config.VerseNumColor == "" {
		config.VerseNumColor = base.VerseNumColor
	}
	if config.TextColor == "" {
		config.TextColor = base.TextColor
	}
	if config.DimColor == "" {
		config.DimColor = base.DimColor
	}
	return config, nil
}

func getDefaultAppState() AppState {
	return AppState{
		CurrentTranslation: "",
		CurrentBook:        "",
		CurrentChapter:     1,
		Selected:           0,
		ScrollOffset:       0,
	}
}

func getDefaultConfig() Config {
	return themes["catppuccin-mocha"]
}

func initialModel() tea.Model {
	multiBibleData, err := NewMultiBibleData()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading Bible data: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please ensure translation files exist in ~/.config/bible-go/translations/\n")
		os.Exit(1)
	}

	savedState := loadState()

	config, err := loadConfig()
	if err != nil {
		config = getDefaultConfig()
	}

	if savedState.CurrentTranslation == "" || !contains(multiBibleData.translationNames, savedState.CurrentTranslation) {
		savedState.CurrentTranslation = multiBibleData.translationNames[0]
	}

	bibleData := multiBibleData.GetCurrentBibleData(savedState.CurrentTranslation)
	if bibleData == nil {
		fmt.Fprintf(os.Stderr, "Error: Could not load translation '%s'\n", savedState.CurrentTranslation)
		os.Exit(1)
	}

	books := bibleData.GetBooks()
	if len(books) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No books found in Bible data\n")
		os.Exit(1)
	}

	if savedState.CurrentBook == "" || !contains(books, savedState.CurrentBook) {
		savedState.CurrentBook = books[0]
		savedState.CurrentChapter = 1
	}

	// Recover from a stale or invalid saved chapter (e.g. state written
	// with another translation) instead of showing an empty chapter.
	chapters := bibleData.chapterIndex[savedState.CurrentBook]
	if _, ok := chapters[savedState.CurrentChapter]; !ok {
		if len(chapters) == 0 {
			savedState.CurrentChapter = 1
		} else {
			savedState.CurrentChapter = nearestChapter(chapters, savedState.CurrentChapter)
		}
		savedState.Selected = 0
		savedState.ScrollOffset = 0
	}

	verses := bibleData.GetVerses(savedState.CurrentBook, savedState.CurrentChapter)

	if savedState.Selected >= len(verses) {
		savedState.Selected = 0
		savedState.ScrollOffset = 0
	}

	bookmarkSet := make(map[Bookmark]bool, len(savedState.Bookmarks))
	for _, b := range savedState.Bookmarks {
		bookmarkSet[b] = true
	}

	return model{
		multiBibleData:     multiBibleData,
		currentTranslation: savedState.CurrentTranslation,
		currentBook:        savedState.CurrentBook,
		currentChapter:     savedState.CurrentChapter,
		verses:             verses,
		mode:               navigationMode,
		selected:           savedState.Selected,
		scrollOffset:       savedState.ScrollOffset,
		height:             24,
		width:              80,
		config:             config,
		bookStyle:          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(config.HighlightColor)),
		verseNumStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color(config.VerseNumColor)).Bold(true),
		textStyle:          lipgloss.NewStyle().Foreground(lipgloss.Color(config.TextColor)),
		dimStyle:           lipgloss.NewStyle().Foreground(lipgloss.Color(config.DimColor)),
		markStyle:          lipgloss.NewStyle().Foreground(lipgloss.Color(config.HighlightColor)).Bold(true),
		footerStyle:        lipgloss.NewStyle().Foreground(lipgloss.Color(config.VerseNumColor)),
		bookmarkSet:        bookmarkSet,
		zenMode:            savedState.ZenMode,
		bookmarks:          savedState.Bookmarks,
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (m model) saveCurrentState() {
	state := AppState{
		CurrentTranslation: m.currentTranslation,
		CurrentBook:        m.currentBook,
		CurrentChapter:     m.currentChapter,
		Selected:           m.selected,
		ScrollOffset:       m.scrollOffset,
		ZenMode:            m.zenMode,
		Bookmarks:          m.bookmarks,
	}
	saveState(state)
}

func (m *model) goToPreviousBook() {
	if m.mode != navigationMode {
		return
	}
	m.navigateToBook(-1)
}

func (m *model) goToNextBook() {
	if m.mode != navigationMode {
		return
	}
	m.navigateToBook(1)
}

func (m *model) navigateToBook(direction int) {
	bibleData := m.getBibleData()
	books := bibleData.GetBooks()
	for i, book := range books {
		if book == m.currentBook {
			newIndex := i + direction
			if newIndex >= 0 && newIndex < len(books) {
				m.currentBook = books[newIndex]
				m.currentChapter = 1
				m.resetVerseView(bibleData)
			}
			break
		}
	}
}

func (m *model) resetVerseView(bibleData *BibleData) {
	m.verses = bibleData.GetVerses(m.currentBook, m.currentChapter)
	m.selected = 0
	m.scrollOffset = 0
}

// readingWidth is the wrapped text column. It fills the pane by default;
// set "maxWidth" in config.json to cap it for a narrower reading column.
func (m model) readingWidth(paddingWidth int) int {
	full := max(20, m.width-paddingWidth)
	if m.config.MaxWidth <= 0 {
		return full
	}
	return min(full, m.config.MaxWidth)
}

// jumpToVerse switches to the given verse's chapter and selects it.
func (m *model) jumpToVerse(target Verse) {
	m.currentBook = target.Book
	m.currentChapter = target.Chapter
	m.verses = m.getBibleData().GetVerses(target.Book, target.Chapter)
	m.selected = 0
	m.scrollOffset = 0
	for i, v := range m.verses {
		if v.Verse == target.Verse {
			m.selected = i
			break
		}
	}
	m.adjustScrollOffset(len(m.verses), m.getVisibleVerses())
}

func (m *model) bookmarkIndex(book string, chapter, verse int) int {
	for i, b := range m.bookmarks {
		if b.Book == book && b.Chapter == chapter && b.Verse == verse {
			return i
		}
	}
	return -1
}

func (m model) isBookmarked(v Verse) bool {
	return m.bookmarkSet[Bookmark{Book: v.Book, Chapter: v.Chapter, Verse: v.Verse}]
}

// nearestChapter returns the chapter of chapters closest to ch.
func nearestChapter(chapters map[int][]Verse, ch int) int {
	best, bestDist := 0, 0
	for c := range chapters {
		dist := c - ch
		if dist < 0 {
			dist = -dist
		}
		if best == 0 || dist < bestDist || (dist == bestDist && c < best) {
			best, bestDist = c, dist
		}
	}
	return best
}

// toggleBookmark adds or removes the current verse from bookmarks.
func (m *model) toggleBookmark() {
	if m.mode != navigationMode || m.selected >= len(m.verses) {
		return
	}
	v := m.verses[m.selected]
	key := Bookmark{Book: v.Book, Chapter: v.Chapter, Verse: v.Verse}
	if m.bookmarkSet[key] {
		delete(m.bookmarkSet, key)
		if i := m.bookmarkIndex(v.Book, v.Chapter, v.Verse); i >= 0 {
			m.bookmarks = append(m.bookmarks[:i], m.bookmarks[i+1:]...)
		}
		m.statusMsg = fmt.Sprintf("Removed bookmark %s %d:%d", v.Book, v.Chapter, v.Verse)
	} else {
		m.bookmarkSet[key] = true
		m.bookmarks = append(m.bookmarks, key)
		m.statusMsg = fmt.Sprintf("Bookmarked %s %d:%d", v.Book, v.Chapter, v.Verse)
	}
	m.saveCurrentState()
}

// bookmarkVerses materializes bookmarks into full verses (text from the
// current translation), sorted in biblical order for the menu.
func (m model) bookmarkVerses() []Verse {
	bd := m.getBibleData()
	order := bd.GetBooks()
	rank := func(b string) int {
		for i, x := range order {
			if x == b {
				return i
			}
		}
		return len(order)
	}
	bms := append([]Bookmark(nil), m.bookmarks...)
	sort.Slice(bms, func(i, j int) bool {
		a, b := bms[i], bms[j]
		if ra, rb := rank(a.Book), rank(b.Book); ra != rb {
			return ra < rb
		}
		if a.Chapter != b.Chapter {
			return a.Chapter < b.Chapter
		}
		return a.Verse < b.Verse
	})
	out := make([]Verse, 0, len(bms))
	for _, bm := range bms {
		text := ""
		for _, v := range bd.GetVerses(bm.Book, bm.Chapter) {
			if v.Verse == bm.Verse {
				text = v.Text
				break
			}
		}
		out = append(out, Verse{Book: bm.Book, Chapter: bm.Chapter, Verse: bm.Verse, Text: text})
	}
	return out
}

// yankVerse copies the selected verse (reference + text) to the system
// clipboard via OSC52, which works over SSH and inside tmux.
func (m *model) yankVerse() tea.Cmd {
	var v Verse
	switch {
	case m.mode == searchMode && m.selected < len(m.searchResults):
		v = m.searchResults[m.selected]
	case m.mode == bookmarksMode:
		if bms := m.bookmarkVerses(); m.selected < len(bms) {
			v = bms[m.selected]
		} else {
			return nil
		}
	case m.selected < len(m.verses):
		v = m.verses[m.selected]
	default:
		return nil
	}
	text := fmt.Sprintf("%s %d:%d (%s) %s", v.Book, v.Chapter, v.Verse, m.currentTranslation, v.Text)
	m.statusMsg = fmt.Sprintf("Copied %s %d:%d", v.Book, v.Chapter, v.Verse)
	return func() tea.Msg {
		// ponytail: single atomic Write; terminals parse OSC52 out-of-band.
		os.Stdout.WriteString(ansi.SetSystemClipboard(text))
		return nil
	}
}

func (m *model) goToPreviousChapter() {
	if m.mode != navigationMode {
		return
	}
	bibleData := m.getBibleData()
	if m.currentChapter > 1 {
		m.currentChapter--
		m.resetVerseView(bibleData)
	} else {
		m.goToPreviousBookLastChapter(bibleData)
	}
}

func (m *model) goToNextChapter() {
	if m.mode != navigationMode {
		return
	}
	bibleData := m.getBibleData()
	m.currentChapter++
	m.verses = bibleData.GetVerses(m.currentBook, m.currentChapter)
	if len(m.verses) == 0 {
		m.goToNextBookFirstChapter(bibleData)
	} else {
		m.selected = 0
		m.scrollOffset = 0
	}
}

func (m *model) goToPreviousBookLastChapter(bibleData *BibleData) {
	books := bibleData.GetBooks()
	for i, book := range books {
		if book == m.currentBook && i > 0 {
			m.currentBook = books[i-1]
			m.currentChapter = m.findLastChapter(bibleData, m.currentBook)
			m.resetVerseView(bibleData)
			break
		}
	}
}

func (m *model) goToNextBookFirstChapter(bibleData *BibleData) {
	books := bibleData.GetBooks()
	for i, book := range books {
		if book == m.currentBook && i < len(books)-1 {
			m.currentBook = books[i+1]
			m.currentChapter = 1
			m.resetVerseView(bibleData)
			return
		}
	}
	m.currentChapter--
	m.resetVerseView(bibleData)
}

func (m *model) findLastChapter(bibleData *BibleData, book string) int {
	last := 0
	for ch := range bibleData.chapterIndex[book] {
		last = max(last, ch)
	}
	return last
}

func (m *model) moveUp(listLen int) {
	if m.selected > 0 {
		m.selected--
		if !m.zenMode {
			m.adjustScrollOffset(listLen, m.getVisibleVerses())
		}
	}
}

func (m *model) moveDown(listLen int) {
	if m.selected < listLen-1 {
		m.selected++
		if !m.zenMode {
			m.adjustScrollOffset(listLen, m.getVisibleVerses())
		}
	}
}

func (m *model) getActiveList() (int, bool) {
	switch {
	case m.mode == searchMode && len(m.searchResults) > 0:
		return len(m.searchResults), true
	case m.mode == bookmarksMode:
		return len(m.bookmarks), true
	case m.mode == navigationMode:
		return len(m.verses), true
	}
	return 0, false
}

func (m *model) handleMovement(direction string) {
	listLen, ok := m.getActiveList()
	if !ok {
		return
	}

	switch direction {
	case "up":
		m.moveUp(listLen)
	case "down":
		m.moveDown(listLen)
	case "pageUp":
		m.pageUp(listLen)
	case "pageDown":
		m.pageDown(listLen)
	}
}

func (m *model) pageDown(listLen int) {
	visibleVerses := m.getVisibleVerses()
	halfPage := max(1, visibleVerses/2)
	m.selected = min(listLen-1, m.selected+halfPage)
	m.adjustScrollOffset(listLen, visibleVerses)
}

func (m *model) pageUp(listLen int) {
	visibleVerses := m.getVisibleVerses()
	halfPage := max(1, visibleVerses/2)
	m.selected = max(0, m.selected-halfPage)
	m.adjustScrollOffset(listLen, visibleVerses)
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevBook, prevChapter, prevTranslation := m.currentBook, m.currentChapter, m.currentTranslation
	prevZen := m.zenMode
	// Persist position and display mode on change so they survive a killed
	// terminal, not just a clean quit.
	defer func() {
		if m.currentBook != prevBook || m.currentChapter != prevChapter || m.currentTranslation != prevTranslation || m.zenMode != prevZen {
			m.saveCurrentState()
		}
	}()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		if m.scrollOffset > 0 && len(m.verses) > 0 {
			m.adjustScrollOffset(len(m.verses), m.getVisibleVerses())
		}
		return m, nil
	case tea.KeyMsg:
		m.statusMsg = "" // transient; cleared on the next keypress
		switch msg.Type {
		case tea.KeyCtrlC:
			m.saveCurrentState()
			return m, tea.Quit
		case tea.KeyEsc:
			if m.mode == searchMode {
				m.mode = navigationMode
				m.searchQuery = ""
				m.searchResults = nil
				return m, nil
			}
			if m.mode == bookmarksMode {
				m.mode = navigationMode
				m.selected = min(m.savedSelected, max(0, len(m.verses)-1))
				m.scrollOffset = m.savedScroll
				return m, nil
			}
			m.saveCurrentState()
			return m, tea.Quit

		case tea.KeyEnter:
			if m.mode == searchMode {
				if len(m.searchResults) == 0 && m.searchQuery != "" {
					bibleData := m.getBibleData()
					m.searchResults = bibleData.Search(m.searchQuery)
					m.selected = 0
					m.scrollOffset = 0
				} else if len(m.searchResults) > 0 && m.selected < len(m.searchResults) {
					m.searchIndex = m.selected
					result := m.searchResults[m.selected]
					m.mode = navigationMode
					m.jumpToVerse(result)
				}
			} else if m.mode == bookmarksMode {
				bms := m.bookmarkVerses()
				if m.selected < len(bms) {
					m.mode = navigationMode
					m.jumpToVerse(bms[m.selected])
				}
			}

		case tea.KeyBackspace:
			if m.mode == searchMode && len(m.searchResults) == 0 && len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			}

		case tea.KeySpace:
			if m.mode == searchMode && len(m.searchResults) == 0 {
				m.searchQuery += " "
			}

		case tea.KeyRunes:
			if len(msg.Runes) > 0 {
				r := msg.Runes[0]

				if m.mode == searchMode && len(m.searchResults) == 0 {
					switch r {
					case '/':
						m.searchQuery = ""
						m.searchResults = nil
						m.selected = 0
						m.scrollOffset = 0
					default:
						m.searchQuery += string(msg.Runes)
					}
					return m, nil
				}

				switch r {
				case '/':
					if m.mode == navigationMode {
						m.mode = searchMode
						m.searchQuery = ""
						m.searchResults = nil
						m.selected = 0
					} else if m.mode == searchMode {
						m.searchQuery = ""
						m.searchResults = nil
						m.selected = 0
						m.scrollOffset = 0
					}
				case 'g':
					if _, ok := m.getActiveList(); ok {
						if m.selected > 0 {
							m.selected = 0
							m.scrollOffset = 0
						}
					}
				case 'G':
					listLen, ok := m.getActiveList()
					if ok {
						m.selected = listLen - 1
						visibleVerses := m.getVisibleVerses()
						if m.selected >= visibleVerses {
							m.scrollOffset = m.selected - visibleVerses + 1
						}
					}
				case 'b':
					if m.mode == navigationMode {
						m.goToPreviousBook()
					}
				case 'w':
					if m.mode == navigationMode {
						m.goToNextBook()
					}
				case 'k':
					m.handleMovement("up")
				case 'j':
					m.handleMovement("down")
				case 'h':
					if m.mode == navigationMode {
						m.goToPreviousChapter()
					}
				case 'l':
					if m.mode == navigationMode {
						m.goToNextChapter()
					}
				case 't', 'T':
					if m.mode == navigationMode {
						currentIndex := -1
						for i, trans := range m.multiBibleData.translationNames {
							if trans == m.currentTranslation {
								currentIndex = i
								break
							}
						}

						var nextIndex int
						if msg.Runes[0] == 't' {
							nextIndex = (currentIndex + 1) % len(m.multiBibleData.translationNames)
						} else {
							nextIndex = currentIndex - 1
							if nextIndex < 0 {
								nextIndex = len(m.multiBibleData.translationNames) - 1
							}
						}

						m.currentTranslation = m.multiBibleData.translationNames[nextIndex]
						bibleData := m.getBibleData()
						books := bibleData.GetBooks()
						if !contains(books, m.currentBook) {
							m.currentBook = books[0]
							m.currentChapter = 1
						}
						prevSelected := m.selected
						m.resetVerseView(bibleData)
						if len(m.verses) == 0 {
							// New translation's book has fewer chapters.
							m.currentChapter = 1
							m.resetVerseView(bibleData)
						}
						// Stay on the same verse when comparing translations.
						if prevSelected < len(m.verses) {
							m.selected = prevSelected
							m.adjustScrollOffset(len(m.verses), m.getVisibleVerses())
						}
					}
				case 'z':
					if m.mode == navigationMode {
						m.zenMode = !m.zenMode
					}
				case 'y':
					return m, m.yankVerse()
				case 'm':
					m.toggleBookmark()
				case '\'':
					if m.mode == navigationMode {
						m.savedSelected = m.selected
						m.savedScroll = m.scrollOffset
						m.mode = bookmarksMode
						m.selected = 0
						m.scrollOffset = 0
					}
				case 'd':
					if m.mode == bookmarksMode {
						bms := m.bookmarkVerses()
						if m.selected < len(bms) {
							t := bms[m.selected]
							delete(m.bookmarkSet, Bookmark{Book: t.Book, Chapter: t.Chapter, Verse: t.Verse})
							if i := m.bookmarkIndex(t.Book, t.Chapter, t.Verse); i >= 0 {
								m.bookmarks = append(m.bookmarks[:i], m.bookmarks[i+1:]...)
								m.saveCurrentState()
							}
							if m.selected >= len(m.bookmarks) && m.selected > 0 {
								m.selected--
							}
						}
					}
				case 'n':
					if m.mode == navigationMode && len(m.searchResults) > 0 {
						m.searchIndex = (m.searchIndex + 1) % len(m.searchResults)
						m.jumpToVerse(m.searchResults[m.searchIndex])
						m.statusMsg = fmt.Sprintf("Match %d/%d", m.searchIndex+1, len(m.searchResults))
					}
				case 'N':
					if m.mode == navigationMode && len(m.searchResults) > 0 {
						m.searchIndex = (m.searchIndex - 1 + len(m.searchResults)) % len(m.searchResults)
						m.jumpToVerse(m.searchResults[m.searchIndex])
						m.statusMsg = fmt.Sprintf("Match %d/%d", m.searchIndex+1, len(m.searchResults))
					}
				case 'q':
					m.saveCurrentState()
					return m, tea.Quit
				}
			}

		case tea.KeyUp:
			m.handleMovement("up")

		case tea.KeyDown:
			m.handleMovement("down")

		case tea.KeyLeft:
			m.goToPreviousChapter()

		case tea.KeyRight:
			m.goToNextChapter()

		case tea.KeyPgUp:
			if m.mode == navigationMode {
				m.goToPreviousBook()
			}

		case tea.KeyPgDown:
			if m.mode == navigationMode {
				m.goToNextBook()
			}

		case tea.KeyCtrlD:
			m.handleMovement("pageDown")

		case tea.KeyCtrlU:
			m.handleMovement("pageUp")
		}
	}

	return m, nil
}

func (m model) View() string {
	var content strings.Builder

	matchesHint := ""
	if len(m.searchResults) > 0 {
		matchesHint = "n/N: Matches • "
	}
	helpText := "hjkl: Navigate • b/w: Book • t/T: Translation • /: Search • " + matchesHint + "m: Mark • ': Bookmarks • y: Copy • z: Zen • q: Quit"
	switch {
	case m.mode == searchMode && len(m.searchResults) > 0:
		helpText = "j/k: Navigate • Enter: Select • y: Copy • /: New search • Esc: Back"
	case m.mode == searchMode:
		helpText = "Type to search • Enter: Execute • Esc: Back"
	case m.mode == bookmarksMode:
		helpText = "j/k: Navigate • Enter: Open • d: Delete • y: Copy • Esc: Back"
	}

	if m.mode == navigationMode {
		if m.zenMode {
			header := m.navHeader()
			content.WriteString(m.centerText(header))
			content.WriteString("\n\n")

			versesAbove := 1
			versesBelow := 2

			headerLines := 1
			headerSpacing := 1
			helpLines := 1

			// Keep the vertical centering accurate even when a verse wraps.
			const (
				zenSideMargin   = 6
				zenMaxTextWidth = 80
			)
			zenTextWidth := max(20, m.width-(zenSideMargin*2))
			zenTextWidth = min(zenTextWidth, zenMaxTextWidth)

			startIdx := m.selected - versesAbove
			endIdx := m.selected + versesBelow + 1

			// Wrap each verse once and reuse the lines for both height
			// accounting and rendering.
			type zenEntry struct {
				valid bool
				verse Verse
				lines []string
			}
			entries := make([]zenEntry, 0, endIdx-startIdx)
			verseLinesTotal := 0
			for i := startIdx; i < endIdx; i++ {
				entry := zenEntry{}
				if i >= 0 && i < len(m.verses) {
					entry.valid = true
					entry.verse = m.verses[i]
					entry.lines = wrapVerseText(entry.verse.Text, zenTextWidth)
				}
				verseLinesTotal += max(1, len(entry.lines))
				if i < endIdx-1 {
					verseLinesTotal++ // blank line between verses
				}
				entries = append(entries, entry)
			}

			availableHeight := m.height - headerLines - headerSpacing - helpLines

			topPadding := max(0, (availableHeight-verseLinesTotal)/2)

			for i := 0; i < topPadding; i++ {
				content.WriteString("\n")
			}

			for idx, entry := range entries {
				if entry.valid {
					m.renderVerseZen(&content, entry.verse, entry.lines, startIdx+idx == m.selected)
				} else {
					content.WriteString("\n")
				}
				if idx < len(entries)-1 {
					content.WriteString("\n")
				}
			}

			linesUsed := headerLines + headerSpacing + topPadding + verseLinesTotal
			bottomPadding := max(0, m.height-linesUsed-helpLines)
			for i := 0; i < bottomPadding; i++ {
				content.WriteString("\n")
			}

			m.writeFooter(&content, helpText)
		} else {
			header := m.navHeader()
			content.WriteString(m.centerText(header))
			content.WriteString("\n\n")

			start := m.scrollOffset
			wrapped, visibleVerses := m.wrappedVisibleVerses(start)
			m.adjustScrollOffset(len(m.verses), visibleVerses)
			if m.scrollOffset != start {
				wrapped, visibleVerses = m.wrappedVisibleVerses(m.scrollOffset)
			}
			end := min(len(m.verses), m.scrollOffset+visibleVerses)

			linesUsed := 3
			for i := m.scrollOffset; i < end; i++ {
				verse := m.verses[i]
				marker := " "
				switch {
				case i == m.selected:
					marker = m.markStyle.Render(">")
				case m.isBookmarked(verse):
					marker = m.markStyle.Render("*")
				}
				verseNumStr := m.verseNumStyle.Render(fmt.Sprintf("%3d", verse.Verse))
				linesUsed += m.renderVerse(&content, wrapped[i-m.scrollOffset], marker, verseNumStr, verseTextPadding)
			}

			remainingLines := m.height - linesUsed
			if remainingLines > 0 {
				content.WriteString(strings.Repeat("\n", remainingLines))
			}

			m.writeFooter(&content, helpText)
		}
	} else if m.mode == bookmarksMode {
		bms := m.bookmarkVerses()
		if len(bms) == 0 {
			content.WriteString(m.centerText(m.bookStyle.Render("Bookmarks")))
			content.WriteString("\n\n")
			content.WriteString(m.centerText("No bookmarks yet — press m on a verse to add one."))
			if r := m.height - 3; r > 0 {
				content.WriteString(strings.Repeat("\n", r))
			}
			m.writeFooter(&content, helpText)
		} else {
			m.clampSelectedIndex(len(bms))
			m.renderList(&content, bms, fmt.Sprintf("Bookmarks (%d)", len(bms)), helpText)
		}
	} else {
		if len(m.searchResults) > 0 {
			m.clampSelectedIndex(len(m.searchResults))
			m.renderList(&content, m.searchResults,
				fmt.Sprintf("Search: %s (%d results)", m.searchQuery, len(m.searchResults)), helpText)
		} else {
			header := m.bookStyle.Render(fmt.Sprintf("Search: %s", m.searchQuery))
			content.WriteString(m.centerText(header))
			content.WriteString("\n\n")

			var promptText string
			if m.searchQuery != "" {
				promptText = "Press Enter to search"
			} else {
				promptText = "Type to search..."
			}
			content.WriteString(m.centerText(promptText))

			remainingLines := m.height - 3
			if remainingLines > 0 {
				content.WriteString(strings.Repeat("\n", remainingLines))
			}

			m.writeFooter(&content, helpText)
		}
	}

	return content.String()
}

// writeFooter renders the transient status message if set, otherwise the
// help line, centered at the bottom.
func (m model) writeFooter(content *strings.Builder, helpText string) {
	text := helpText
	if m.statusMsg != "" {
		text = m.statusMsg
	}
	styled := m.footerStyle.Render(text)
	content.WriteString(m.centerText(styled))
}

func (m model) navHeader() string {
	totalCh := len(m.getBibleData().chapterIndex[m.currentBook])
	h := fmt.Sprintf("%s %s · c%d/%d", m.currentTranslation, m.currentBook, m.currentChapter, totalCh)
	if m.selected < len(m.verses) {
		h += fmt.Sprintf(" · v%d/%d", m.selected+1, len(m.verses))
		if v := m.verses[m.selected]; m.isBookmarked(v) {
			h += " ★"
		}
	}
	return m.bookStyle.Render(h)
}

func (m *model) clampSelectedIndex(maxLen int) {
	m.selected = max(0, min(maxLen-1, m.selected))
}

func (m *model) adjustScrollOffset(listLen int, visibleItems int) {
	maxScroll := max(0, listLen-visibleItems)
	m.scrollOffset = min(maxScroll, max(m.selected, m.scrollOffset))
	if m.selected >= m.scrollOffset+visibleItems {
		m.scrollOffset = m.selected - visibleItems + 1
	}
	if m.selected < m.scrollOffset {
		m.scrollOffset = m.selected
	}
}

func (m model) getVisibleVerses() int {
	if m.mode != navigationMode {
		available := m.height - 4
		if available < 3 {
			return 3
		}
		return available
	}

	_, count := m.wrappedVisibleVerses(m.scrollOffset)
	return max(1, count)
}

// wrappedVisibleVerses wraps the verses starting at start that fit in the
// reading pane. Returned lines align with m.verses[start:].
func (m model) wrappedVisibleVerses(start int) ([][]string, int) {
	availableHeight := max(5, m.height-6)
	width := m.readingWidth(verseTextPadding)

	var lines [][]string
	height := 0
	for i := start; i < len(m.verses); i++ {
		wrapped := wrapVerseText(m.verses[i].Text, width)
		verseHeight := max(2, len(wrapped)+1)
		if height+verseHeight > availableHeight {
			break
		}
		height += verseHeight
		lines = append(lines, wrapped)
	}
	return lines, len(lines)
}

func (m model) calculateTextHeight(text string, paddingWidth int) int {
	return max(2, len(wrapVerseText(text, m.readingWidth(paddingWidth)))+1)
}

const (
	verseTextPadding  = 6
	searchTextPadding = 23
)

func (m model) calculateSearchResultHeight(result Verse) int {
	return m.calculateTextHeight(result.Text, searchTextPadding)
}

func wrapVerseText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	lines := make([]string, 0, (len(words)+3)/4)
	var currentLine strings.Builder
	currentLine.Grow(maxWidth)

	for i, word := range words {
		if currentLine.Len() > 0 {
			if currentLine.Len()+1+len(word) > maxWidth {
				lines = append(lines, currentLine.String())
				currentLine.Reset()
				currentLine.Grow(maxWidth)
				currentLine.WriteString(word)
			} else {
				currentLine.WriteByte(' ')
				currentLine.WriteString(word)
			}
		} else {
			currentLine.WriteString(word)
		}

		if i == len(words)-1 && currentLine.Len() > 0 {
			lines = append(lines, currentLine.String())
		}
	}

	return lines
}

// renderVerse writes one verse from precomputed wrapped lines. marker is the
// already-styled leading gutter (">", "*" or " ").
func (m model) renderVerse(content *strings.Builder, verseLines []string, marker, verseNumStr string, paddingWidth int) int {
	content.WriteString(marker)
	content.WriteByte(' ')
	content.WriteString(verseNumStr)
	content.WriteByte(' ')

	if len(verseLines) > 0 {
		content.WriteString(m.textStyle.Render(verseLines[0]))
	}
	content.WriteByte('\n')
	linesUsed := 1

	if len(verseLines) > 1 {
		padding := strings.Repeat(" ", paddingWidth)
		for _, line := range verseLines[1:] {
			content.WriteString(padding)
			content.WriteString(m.textStyle.Render(line))
			content.WriteByte('\n')
			linesUsed++
		}
	}

	content.WriteByte('\n')
	return linesUsed + 1
}

// visibleResultLines wraps the results starting at start that fit in the
// list pane. Returned lines align with results[start:].
func (m model) visibleResultLines(results []Verse, start, availableHeight int) ([][]string, int) {
	width := m.readingWidth(searchTextPadding)
	var lines [][]string
	linesUsed := 0
	for i := start; i < len(results) && linesUsed < availableHeight; i++ {
		wrapped := wrapVerseText(results[i].Text, width)
		resultHeight := max(2, len(wrapped)+1)
		if linesUsed+resultHeight > availableHeight {
			break
		}
		linesUsed += resultHeight
		lines = append(lines, wrapped)
	}
	return lines, len(lines)
}

// renderList draws a scrollable, variable-height list of verses (used for
// both search results and the bookmarks menu).
func (m *model) renderList(content *strings.Builder, results []Verse, header, helpText string) {
	content.WriteString(m.centerText(m.bookStyle.Render(header)))
	content.WriteString("\n\n")

	availableHeight := max(5, m.height-6)
	wrapped, visibleCount := m.visibleResultLines(results, m.scrollOffset, availableHeight)

	if m.selected >= m.scrollOffset+visibleCount {
		m.scrollOffset = m.selected
		testHeight := m.calculateSearchResultHeight(results[m.selected])
		for m.scrollOffset > 0 {
			prevHeight := m.calculateSearchResultHeight(results[m.scrollOffset-1])
			if testHeight+prevHeight <= availableHeight {
				m.scrollOffset--
				testHeight += prevHeight
			} else {
				break
			}
		}
		wrapped, visibleCount = m.visibleResultLines(results, m.scrollOffset, availableHeight)
	}
	if m.selected < m.scrollOffset {
		m.scrollOffset = m.selected
		wrapped, visibleCount = m.visibleResultLines(results, m.scrollOffset, availableHeight)
	}

	end := min(len(results), m.scrollOffset+visibleCount)
	linesUsed := 3
	for i := m.scrollOffset; i < end; i++ {
		r := results[i]
		marker := " "
		switch {
		case i == m.selected:
			marker = m.markStyle.Render(">")
		case m.isBookmarked(r):
			marker = m.markStyle.Render("*")
		}
		reference := truncateText(fmt.Sprintf("%s %d:%d", r.Book, r.Chapter, r.Verse), 20)
		verseNumStr := m.verseNumStyle.Render(fmt.Sprintf("%-20s", reference))
		linesUsed += m.renderVerse(content, wrapped[i-m.scrollOffset], marker, verseNumStr, searchTextPadding)
	}

	if remaining := m.height - linesUsed; remaining > 0 {
		content.WriteString(strings.Repeat("\n", remaining))
	}
	m.writeFooter(content, helpText)
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func (m model) centerText(text string) string {
	visualWidth := lipgloss.Width(text)
	if visualWidth >= m.width {
		return text
	}
	leftPadding := (m.width - visualWidth) / 2
	return strings.Repeat(" ", leftPadding) + text
}

// renderVerseZen writes a zen-mode verse from precomputed wrapped lines,
// centered within the full terminal width.
func (m model) renderVerseZen(content *strings.Builder, verse Verse, verseLines []string, isSelected bool) {
	if len(verseLines) == 0 {
		return
	}

	style := m.dimStyle
	if isSelected {
		style = m.textStyle
	}

	// Mark bookmarked verses with the same accent star used in the header.
	bookmarked := m.isBookmarked(verse)

	for idx, rawLine := range verseLines {
		line := style.Render(rawLine)
		if idx == 0 && bookmarked {
			line = m.markStyle.Render("★ ") + line
		}
		visualWidth := lipgloss.Width(line)
		if visualWidth < m.width {
			leftPadding := (m.width - visualWidth) / 2
			content.WriteString(strings.Repeat(" ", leftPadding))
		}
		content.WriteString(line)
		content.WriteString("\n")
	}
}
