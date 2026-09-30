package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

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
	savedSelected      int     // reading position stashed while in a menu
	bookmarkList       []Verse // bookmarks menu contents, built on entry
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

	// Write to a temp file and rename so a crash mid-write can't truncate
	// the file (and lose bookmarks). Follow a symlinked target so the link
	// itself isn't replaced.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(jsonData); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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
	if err := loadJSON(stateFile, &state); err != nil {
		return getDefaultAppState()
	}
	// Missing or stale fields are repaired by initialModel; bookmarks are
	// kept regardless.
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
		config = getDefaultConfig()
	} else if err != nil {
		config = getDefaultConfig()
	} else {
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
	}
	// colors.json is applied last, after the returned config's only possible
	// save above, so its values never end up written into config.json.
	applyColorsFile(&config)
	return config, nil
}

// colorsFile is an optional palette managed outside the app (e.g. generated
// by a dotfiles setup from the desktop's colors). The app never writes it.
const colorsFile = "colors.json"

// colorOverrides are the colors.json fields. Each is optional and must be
// "#rrggbb"; a missing or malformed one leaves the theme/config value alone.
type colorOverrides struct {
	HighlightColor string `json:"highlightColor"`
	VerseNumColor  string `json:"verseNumColor"`
	TextColor      string `json:"textColor"`
	DimColor       string `json:"dimColor"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// applyColorsFile overrides config's colors with colors.json, which wins over
// both the theme and colors set in config.json. A missing or invalid file
// changes nothing.
func applyColorsFile(config *Config) {
	var o colorOverrides
	if err := loadJSON(colorsFile, &o); err != nil {
		return
	}
	set := func(dst *string, v string) {
		if hexColor.MatchString(v) {
			*dst = v
		}
	}
	set(&config.HighlightColor, o.HighlightColor)
	set(&config.VerseNumColor, o.VerseNumColor)
	set(&config.TextColor, o.TextColor)
	set(&config.DimColor, o.DimColor)
}

func getDefaultAppState() AppState {
	return AppState{
		CurrentTranslation: "",
		CurrentBook:        "",
		CurrentChapter:     1,
		Selected:           0,
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

	config, _ := loadConfig()

	// Fall back to the first loadable translation if the saved one is
	// missing or broken, so the header never names a translation that
	// isn't the one being shown.
	bibleData := multiBibleData.Load(savedState.CurrentTranslation)
	if bibleData == nil {
		name, ok := multiBibleData.FirstLoadable()
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: none of the translations in ~/.config/bible-go/translations/ could be loaded\n")
			os.Exit(1)
		}
		savedState.CurrentTranslation = name
		bibleData = multiBibleData.Load(name)
	}

	books := bibleData.GetBooks()
	if !slices.Contains(books, savedState.CurrentBook) {
		savedState.CurrentBook = books[0]
		savedState.CurrentChapter = 1
	}

	// Recover from a stale or invalid saved chapter (e.g. state written
	// with another translation) instead of showing an empty chapter.
	chapters := bibleData.Chapters(savedState.CurrentBook)
	if !slices.Contains(chapters, savedState.CurrentChapter) {
		savedState.CurrentChapter = nearestChapter(chapters, savedState.CurrentChapter)
		savedState.Selected = 0
	}

	verses := bibleData.GetVerses(savedState.CurrentBook, savedState.CurrentChapter)

	if savedState.Selected < 0 || savedState.Selected >= len(verses) {
		savedState.Selected = 0
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

func (m model) saveCurrentState() {
	state := AppState{
		CurrentTranslation: m.currentTranslation,
		CurrentBook:        m.currentBook,
		CurrentChapter:     m.currentChapter,
		Selected:           m.selected,
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
	newIndex := slices.Index(books, m.currentBook) + direction
	if newIndex < 0 || newIndex >= len(books) {
		return
	}
	m.currentBook = books[newIndex]
	m.currentChapter = bibleData.Chapters(m.currentBook)[0]
	m.resetVerseView(bibleData)
}

func (m *model) resetVerseView(bibleData *BibleData) {
	m.verses = bibleData.GetVerses(m.currentBook, m.currentChapter)
	m.selected = 0
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

// jumpToVerse switches to the given verse's chapter and selects it. It
// reports false, leaving the view alone, if the current translation lacks
// that chapter.
func (m *model) jumpToVerse(target Verse) bool {
	verses := m.getBibleData().GetVerses(target.Book, target.Chapter)
	if len(verses) == 0 {
		m.statusMsg = fmt.Sprintf("%s %d is not in %s", target.Book, target.Chapter, m.currentTranslation)
		return false
	}
	m.currentBook = target.Book
	m.currentChapter = target.Chapter
	m.verses = verses
	m.selected = 0
	for i, v := range m.verses {
		if v.Verse == target.Verse {
			m.selected = i
			break
		}
	}
	return true
}

func (m *model) bookmarkIndex(key Bookmark) int {
	return slices.Index(m.bookmarks, key)
}

func (m model) isBookmarked(v Verse) bool {
	return m.bookmarkSet[Bookmark{Book: v.Book, Chapter: v.Chapter, Verse: v.Verse}]
}

// nearestChapter returns the chapter of chapters (ascending) closest to ch,
// preferring the lower one on a tie, or 1 if there are none.
func nearestChapter(chapters []int, ch int) int {
	if len(chapters) == 0 {
		return 1
	}
	i, found := slices.BinarySearch(chapters, ch)
	switch {
	case found || i == 0:
		return chapters[i]
	case i == len(chapters):
		return chapters[i-1]
	case ch-chapters[i-1] <= chapters[i]-ch:
		return chapters[i-1]
	}
	return chapters[i]
}

// toggleBookmark adds or removes the current verse from bookmarks.
func (m *model) toggleBookmark() {
	if m.mode != navigationMode || m.selected >= len(m.verses) {
		return
	}
	v := m.verses[m.selected]
	key := Bookmark{Book: v.Book, Chapter: v.Chapter, Verse: v.Verse}
	if m.bookmarkSet[key] {
		m.removeBookmark(key)
		m.statusMsg = fmt.Sprintf("Removed bookmark %s %d:%d", v.Book, v.Chapter, v.Verse)
	} else {
		m.bookmarkSet[key] = true
		m.bookmarks = append(m.bookmarks, key)
		m.statusMsg = fmt.Sprintf("Bookmarked %s %d:%d", v.Book, v.Chapter, v.Verse)
	}
	m.saveCurrentState()
}

func (m *model) removeBookmark(key Bookmark) {
	delete(m.bookmarkSet, key)
	if i := m.bookmarkIndex(key); i >= 0 {
		m.bookmarks = slices.Delete(m.bookmarks, i, i+1)
	}
}

// bookmarkVerses materializes bookmarks into full verses (text from the
// current translation), sorted in biblical order for the menu.
func (m model) bookmarkVerses() []Verse {
	bd := m.getBibleData()
	rank := make(map[string]int, len(bd.GetBooks()))
	for i, b := range bd.GetBooks() {
		rank[b] = i
	}
	bookRank := func(b string) int {
		if r, ok := rank[b]; ok {
			return r
		}
		return len(rank)
	}
	out := make([]Verse, 0, len(m.bookmarks))
	for _, bm := range m.bookmarks {
		text := ""
		for _, v := range bd.GetVerses(bm.Book, bm.Chapter) {
			if v.Verse == bm.Verse {
				text = v.Text
				break
			}
		}
		out = append(out, Verse{Book: bm.Book, Chapter: bm.Chapter, Verse: bm.Verse, Text: text})
	}
	slices.SortStableFunc(out, func(a, b Verse) int {
		if ra, rb := bookRank(a.Book), bookRank(b.Book); ra != rb {
			return ra - rb
		}
		if a.Chapter != b.Chapter {
			return a.Chapter - b.Chapter
		}
		return a.Verse - b.Verse
	})
	return out
}

// activeItems returns the list shown in the current mode and the width of
// the gutter before each item's text.
func (m model) activeItems() ([]Verse, int) {
	switch m.mode {
	case searchMode:
		return m.searchResults, searchTextPadding
	case bookmarksMode:
		return m.bookmarkList, searchTextPadding
	}
	return m.verses, verseTextPadding
}

// yankVerse copies the selected verse (reference + text) to the system
// clipboard via OSC52, which works over SSH and inside tmux.
func (m *model) yankVerse() tea.Cmd {
	items, _ := m.activeItems()
	if m.selected < 0 || m.selected >= len(items) {
		return nil
	}
	v := items[m.selected]
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
	chapters := bibleData.Chapters(m.currentBook)
	if i := slices.Index(chapters, m.currentChapter); i > 0 {
		m.currentChapter = chapters[i-1]
		m.resetVerseView(bibleData)
		return
	}
	books := bibleData.GetBooks()
	if i := slices.Index(books, m.currentBook); i > 0 {
		m.currentBook = books[i-1]
		prev := bibleData.Chapters(m.currentBook)
		m.currentChapter = prev[len(prev)-1]
		m.resetVerseView(bibleData)
	}
}

func (m *model) goToNextChapter() {
	if m.mode != navigationMode {
		return
	}
	bibleData := m.getBibleData()
	chapters := bibleData.Chapters(m.currentBook)
	if i := slices.Index(chapters, m.currentChapter); i >= 0 && i < len(chapters)-1 {
		m.currentChapter = chapters[i+1]
		m.resetVerseView(bibleData)
		return
	}
	// At the last chapter of the last book this is a no-op.
	m.navigateToBook(1)
}

// switchTranslation shows the same passage in bd, falling back to the
// nearest chapter or first book when bd lacks it. Active search results
// are recomputed so n/N keep working against the new text.
func (m *model) switchTranslation(name string, bd *BibleData) {
	m.currentTranslation = name
	if !slices.Contains(bd.GetBooks(), m.currentBook) {
		m.currentBook = bd.GetBooks()[0]
		m.currentChapter = 1
	}
	m.currentChapter = nearestChapter(bd.Chapters(m.currentBook), m.currentChapter)
	prevSelected := m.selected
	m.resetVerseView(bd)
	// Stay on the same verse when comparing translations.
	if prevSelected < len(m.verses) {
		m.selected = prevSelected
	}

	if len(m.searchResults) > 0 {
		m.searchResults = bd.Search(m.searchQuery)
		m.searchIndex = 0
		for i, r := range m.searchResults {
			if r.Book == m.currentBook && r.Chapter == m.currentChapter && m.selected < len(m.verses) && r.Verse == m.verses[m.selected].Verse {
				m.searchIndex = i
				break
			}
		}
	}
}

// cycleTranslation moves to the next (dir=1) or previous (dir=-1)
// translation, skipping any that fail to load.
func (m *model) cycleTranslation(dir int) {
	names := m.multiBibleData.translationNames
	n := len(names)
	cur := slices.Index(names, m.currentTranslation)
	for step := 1; step < n || (cur < 0 && step == n); step++ {
		name := names[((cur+dir*step)%n+n)%n]
		if bd := m.multiBibleData.Load(name); bd != nil {
			m.switchTranslation(name, bd)
			return
		}
		m.statusMsg = fmt.Sprintf("Could not load %s; skipped", name)
	}
}

func (m *model) getActiveList() (int, bool) {
	if m.mode == searchMode && len(m.searchResults) == 0 {
		return 0, false // typing a query
	}
	items, _ := m.activeItems()
	return len(items), true
}

func (m *model) handleMovement(direction string) {
	listLen, ok := m.getActiveList()
	if !ok || listLen == 0 {
		return
	}

	step := 1
	if direction == "pageUp" || direction == "pageDown" {
		items, padding := m.activeItems()
		visible := len(m.visibleLines(items, m.listOffset(items, padding), padding))
		step = max(1, visible/2)
	}
	switch direction {
	case "up", "pageUp":
		m.selected = max(0, m.selected-step)
	case "down", "pageDown":
		m.selected = min(listLen-1, m.selected+step)
	}
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
		return m, nil
	case tea.KeyMsg:
		m.statusMsg = "" // transient; cleared on the next keypress
		typing := m.mode == searchMode && len(m.searchResults) == 0
		switch msg.Type {
		case tea.KeyCtrlC:
			m.saveCurrentState()
			return m, tea.Quit
		case tea.KeyEsc:
			switch m.mode {
			case searchMode:
				m.mode = navigationMode
				m.searchQuery = ""
				m.searchResults = nil
				m.selected = min(m.savedSelected, max(0, len(m.verses)-1))
				return m, nil
			case bookmarksMode:
				m.mode = navigationMode
				m.bookmarkList = nil
				m.selected = min(m.savedSelected, max(0, len(m.verses)-1))
				return m, nil
			}
			m.saveCurrentState()
			return m, tea.Quit

		case tea.KeyEnter:
			switch {
			case typing && strings.TrimSpace(m.searchQuery) != "":
				m.searchResults = m.getBibleData().Search(m.searchQuery)
				m.selected = 0
				if len(m.searchResults) == 0 {
					m.statusMsg = fmt.Sprintf("No matches for %q", m.searchQuery)
				}
			case m.mode == searchMode && m.selected < len(m.searchResults):
				if m.jumpToVerse(m.searchResults[m.selected]) {
					m.searchIndex = m.selected
					m.mode = navigationMode
				}
			case m.mode == bookmarksMode && m.selected < len(m.bookmarkList):
				if m.jumpToVerse(m.bookmarkList[m.selected]) {
					m.mode = navigationMode
					m.bookmarkList = nil
				}
			}

		case tea.KeyBackspace:
			if typing && len(m.searchQuery) > 0 {
				_, size := utf8.DecodeLastRuneInString(m.searchQuery)
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-size]
			}

		case tea.KeySpace:
			if typing {
				m.searchQuery += " "
			}

		case tea.KeyRunes:
			if len(msg.Runes) == 0 {
				break
			}
			// Fast typing can deliver several keys in one message; handle
			// each as its own keypress so none after the first is dropped.
			if len(msg.Runes) > 1 && !typing && !msg.Paste {
				var next tea.Model = m
				var cmds []tea.Cmd
				for _, r := range msg.Runes {
					var cmd tea.Cmd
					next, cmd = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
					cmds = append(cmds, cmd)
				}
				return next, tea.Batch(cmds...)
			}
			r := msg.Runes[0]

			if typing {
				if r == '/' {
					m.searchQuery = ""
				} else {
					m.searchQuery += string(msg.Runes)
				}
				return m, nil
			}

			switch r {
			case '/':
				switch m.mode {
				case navigationMode:
					m.savedSelected = m.selected
					m.mode = searchMode
				case searchMode:
					// New search from the results list.
				default:
					return m, nil
				}
				m.searchQuery = ""
				m.searchResults = nil
				m.selected = 0
			case 'g':
				if _, ok := m.getActiveList(); ok {
					m.selected = 0
				}
			case 'G':
				if listLen, ok := m.getActiveList(); ok && listLen > 0 {
					m.selected = listLen - 1
				}
			case 'b':
				m.goToPreviousBook()
			case 'w':
				m.goToNextBook()
			case 'k':
				m.handleMovement("up")
			case 'j':
				m.handleMovement("down")
			case 'h':
				m.goToPreviousChapter()
			case 'l':
				m.goToNextChapter()
			case 't', 'T':
				if m.mode == navigationMode {
					dir := 1
					if r == 'T' {
						dir = -1
					}
					m.cycleTranslation(dir)
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
					m.mode = bookmarksMode
					m.bookmarkList = m.bookmarkVerses()
					m.selected = 0
				}
			case 'd':
				if m.mode == bookmarksMode && m.selected < len(m.bookmarkList) {
					t := m.bookmarkList[m.selected]
					m.removeBookmark(Bookmark{Book: t.Book, Chapter: t.Chapter, Verse: t.Verse})
					m.bookmarkList = slices.Delete(m.bookmarkList, m.selected, m.selected+1)
					m.saveCurrentState()
					m.selected = max(0, min(m.selected, len(m.bookmarkList)-1))
				}
			case 'n', 'N':
				if m.mode == navigationMode && len(m.searchResults) > 0 {
					n := len(m.searchResults)
					next := m.searchIndex + 1
					if r == 'N' {
						next = m.searchIndex - 1
					}
					next = (next%n + n) % n
					if m.jumpToVerse(m.searchResults[next]) {
						m.searchIndex = next
						m.statusMsg = fmt.Sprintf("Match %d/%d", next+1, n)
					}
				}
			case 'q':
				m.saveCurrentState()
				return m, tea.Quit
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
			m.goToPreviousBook()

		case tea.KeyPgDown:
			m.goToNextBook()

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

	switch {
	case m.mode == navigationMode && m.zenMode:
		m.renderZen(&content, helpText)
	case m.mode == navigationMode:
		verseLabel := func(v Verse) string {
			return m.verseNumStyle.Render(fmt.Sprintf("%3d", v.Verse))
		}
		m.renderList(&content, m.navHeader(), m.verses, verseTextPadding, verseLabel, helpText)
	case m.mode == bookmarksMode && len(m.bookmarkList) == 0:
		m.renderMessage(&content, m.bookStyle.Render("Bookmarks"), "No bookmarks yet — press m on a verse to add one.", helpText)
	case m.mode == bookmarksMode:
		m.renderList(&content, m.bookStyle.Render(fmt.Sprintf("Bookmarks (%d)", len(m.bookmarkList))),
			m.bookmarkList, searchTextPadding, m.referenceLabel, helpText)
	case len(m.searchResults) > 0:
		m.renderList(&content, m.bookStyle.Render(fmt.Sprintf("Search: %s (%d results)", m.searchQuery, len(m.searchResults))),
			m.searchResults, searchTextPadding, m.referenceLabel, helpText)
	default:
		promptText := "Type to search..."
		if m.searchQuery != "" {
			promptText = "Press Enter to search"
		}
		m.renderMessage(&content, m.bookStyle.Render(fmt.Sprintf("Search: %s", m.searchQuery)), promptText, helpText)
	}

	return content.String()
}

// renderMessage draws a header, one centered line of text and the footer.
func (m model) renderMessage(content *strings.Builder, header, text, helpText string) {
	content.WriteString(m.centerText(header))
	content.WriteString("\n\n")
	content.WriteString(m.centerText(text))
	if r := m.height - 3; r > 0 {
		content.WriteString(strings.Repeat("\n", r))
	}
	m.writeFooter(content, helpText)
}

// renderZen draws the selected verse with its neighbours, vertically and
// horizontally centered.
func (m model) renderZen(content *strings.Builder, helpText string) {
	content.WriteString(m.centerText(m.navHeader()))
	content.WriteString("\n\n")

	const (
		versesAbove   = 1
		versesBelow   = 2
		headerLines   = 1
		headerSpacing = 1
		helpLines     = 1
	)

	// Keep the vertical centering accurate even when a verse wraps.
	const (
		zenSideMargin   = 6
		zenMaxTextWidth = 80
	)
	zenTextWidth := min(max(20, m.width-(zenSideMargin*2)), zenMaxTextWidth)

	startIdx := m.selected - versesAbove
	endIdx := m.selected + versesBelow + 1

	// Wrap each verse once and reuse the lines for both height accounting
	// and rendering. Slots outside the chapter stay nil and render blank.
	entries := make([][]string, 0, endIdx-startIdx)
	verseLinesTotal := 0
	for i := startIdx; i < endIdx; i++ {
		var lines []string
		if i >= 0 && i < len(m.verses) {
			lines = wrapVerseText(m.verses[i].Text, zenTextWidth)
		}
		verseLinesTotal += max(1, len(lines))
		if i < endIdx-1 {
			verseLinesTotal++ // blank line between verses
		}
		entries = append(entries, lines)
	}

	availableHeight := m.height - headerLines - headerSpacing - helpLines
	topPadding := max(0, (availableHeight-verseLinesTotal)/2)
	content.WriteString(strings.Repeat("\n", topPadding))

	for idx, lines := range entries {
		if i := startIdx + idx; lines != nil {
			m.renderVerseZen(content, m.verses[i], lines, i == m.selected)
		} else {
			content.WriteString("\n")
		}
		if idx < len(entries)-1 {
			content.WriteString("\n")
		}
	}

	linesUsed := headerLines + headerSpacing + topPadding + verseLinesTotal
	if bottomPadding := m.height - linesUsed - helpLines; bottomPadding > 0 {
		content.WriteString(strings.Repeat("\n", bottomPadding))
	}

	m.writeFooter(content, helpText)
}

// writeFooter renders the transient status message if set, otherwise the
// help line, centered at the bottom.
func (m model) writeFooter(content *strings.Builder, helpText string) {
	text := helpText
	if m.statusMsg != "" {
		text = m.statusMsg
	}
	styled := m.footerStyle.Render(ansi.Truncate(text, m.width, "…"))
	content.WriteString(m.centerText(styled))
}

func (m model) navHeader() string {
	chapters := m.getBibleData().Chapters(m.currentBook)
	totalCh := 0
	if len(chapters) > 0 {
		totalCh = chapters[len(chapters)-1]
	}
	h := fmt.Sprintf("%s %s · c%d/%d", m.currentTranslation, m.currentBook, m.currentChapter, totalCh)
	if m.selected < len(m.verses) {
		h += fmt.Sprintf(" · v%d/%d", m.selected+1, len(m.verses))
		if v := m.verses[m.selected]; m.isBookmarked(v) {
			h += " ★"
		}
	}
	return m.bookStyle.Render(h)
}

const (
	verseTextPadding  = 6  // marker, space, 3-digit verse number, space
	searchTextPadding = 23 // marker, space, 20-cell reference, space
	referenceWidth    = 20
)

// paneHeight is the number of rows available for list items.
func (m model) paneHeight() int {
	return max(5, m.height-6)
}

// itemHeight is the rows a wrapped item takes, including its blank line.
func itemHeight(lines []string) int {
	return max(2, len(lines)+1)
}

// listOffset returns the first item to draw. The selected item is kept at
// the top, except near the end, where the offset stops at the item from
// which the rest of the list exactly fills the pane.
func (m model) listOffset(items []Verse, padding int) int {
	width := m.readingWidth(padding)
	avail := m.paneHeight()
	start, used := len(items), 0
	for start > 0 {
		h := itemHeight(wrapVerseText(items[start-1].Text, width))
		if used+h > avail && used > 0 {
			break
		}
		used += h
		start--
	}
	return max(0, min(m.selected, start))
}

// visibleLines wraps the items starting at start that fit in the pane.
// Returned lines align with items[start:]. The first item is always
// included, even if it alone is taller than the pane.
func (m model) visibleLines(items []Verse, start, padding int) [][]string {
	width := m.readingWidth(padding)
	avail := m.paneHeight()
	var lines [][]string
	used := 0
	for i := start; i < len(items); i++ {
		wrapped := wrapVerseText(items[i].Text, width)
		h := itemHeight(wrapped)
		if used+h > avail && len(lines) > 0 {
			break
		}
		used += h
		lines = append(lines, wrapped)
	}
	return lines
}

// textWidth is the display width of s in terminal cells.
func textWidth(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return ansi.StringWidth(s)
		}
	}
	return len(s)
}

func wrapVerseText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	lines := make([]string, 0, len(text)/maxWidth+1)
	var currentLine strings.Builder
	currentLine.Grow(len(text))
	lineWidth := 0

	for _, word := range words {
		w := textWidth(word)
		if lineWidth > 0 && lineWidth+1+w > maxWidth {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
			lineWidth = 0
		}
		if lineWidth > 0 {
			currentLine.WriteByte(' ')
			lineWidth++
		}
		currentLine.WriteString(word)
		lineWidth += w
	}
	lines = append(lines, currentLine.String())

	return lines
}

// referenceLabel is the fixed-width "Book C:V" column used by search
// results and bookmarks.
func (m model) referenceLabel(v Verse) string {
	ref := ansi.Truncate(fmt.Sprintf("%s %d:%d", v.Book, v.Chapter, v.Verse), referenceWidth, "...")
	ref += strings.Repeat(" ", max(0, referenceWidth-textWidth(ref)))
	return m.verseNumStyle.Render(ref)
}

// renderList draws a header, a scrollable, variable-height list of verses
// (the chapter, search results or bookmarks) and the footer. label renders
// the column before each verse's text.
func (m model) renderList(content *strings.Builder, header string, items []Verse, padding int, label func(Verse) string, helpText string) {
	content.WriteString(m.centerText(header))
	content.WriteString("\n\n")

	offset := m.listOffset(items, padding)
	wrapped := m.visibleLines(items, offset, padding)
	pad := strings.Repeat(" ", padding)

	linesUsed := 3
	for j, verseLines := range wrapped {
		i := offset + j
		v := items[i]
		switch {
		case i == m.selected:
			content.WriteString(m.markStyle.Render(">"))
		case m.isBookmarked(v):
			content.WriteString(m.markStyle.Render("*"))
		default:
			content.WriteByte(' ')
		}
		content.WriteByte(' ')
		content.WriteString(label(v))
		content.WriteByte(' ')

		for k, line := range verseLines {
			if k > 0 {
				content.WriteString(pad)
			}
			content.WriteString(m.textStyle.Render(line))
			content.WriteByte('\n')
		}
		content.WriteByte('\n')
		linesUsed += itemHeight(verseLines)
	}

	if remaining := m.height - linesUsed; remaining > 0 {
		content.WriteString(strings.Repeat("\n", remaining))
	}
	m.writeFooter(content, helpText)
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
		content.WriteString(m.centerText(line))
		content.WriteString("\n")
	}
}
