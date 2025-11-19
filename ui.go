package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	multiBibleData     *MultiBibleData
	currentTranslation string
	currentBook        string
	currentChapter     int
	verses             []Verse
	searchQuery        string
	searchResults      []Verse
	mode               mode
	selected           int
	scrollOffset       int
	height             int
	width              int
	config             Config
	bookStyle          lipgloss.Style
	verseNumStyle      lipgloss.Style
	textStyle          lipgloss.Style
	dimStyle           lipgloss.Style
	zenMode            bool
}

func (m *model) getBibleData() *BibleData {
	return m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
}

type mode int

const (
	navigationMode mode = iota
	searchMode
)

type AppState struct {
	CurrentTranslation string `json:"currentTranslation"`
	CurrentBook        string `json:"currentBook"`
	CurrentChapter     int    `json:"currentChapter"`
	Selected           int    `json:"selected"`
	ScrollOffset       int    `json:"scrollOffset"`
}

type Config struct {
	HighlightColor string `json:"highlightColor"`
	VerseNumColor  string `json:"verseNumColor"`
	TextColor      string `json:"textColor"`
	DimColor       string `json:"dimColor"`
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

func loadState() (AppState, error) {
	var state AppState
	err := loadJSON(stateFile, &state)
	if err != nil || state.CurrentTranslation == "" {
		return getDefaultAppState(), nil
	}
	return state, nil
}

func saveConfig(config Config) error {
	return saveJSON(configFile, config)
}

func loadConfig() (Config, error) {
	config := getDefaultConfig()
	err := loadJSON(configFile, &config)
	if err != nil {
		saveConfig(config)
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
	return Config{
		HighlightColor: "#cba6f7",
		VerseNumColor:  "#89b4fa",
		TextColor:      "#cdd6f4",
		DimColor:       "#313244",
	}
}

var (
	verseStyle = lipgloss.NewStyle().
			MarginBottom(2)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			MarginTop(1).
			PaddingLeft(1)
)

func initialModel() tea.Model {
	multiBibleData, err := NewMultiBibleData()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading Bible data: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please ensure translation files exist in ~/.config/bible-go/translations/\n")
		os.Exit(1)
	}

	savedState, err := loadState()
	if err != nil {
		savedState = getDefaultAppState()
		savedState.CurrentBook = "Genesis"
	}

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

	verses := bibleData.GetVerses(savedState.CurrentBook, savedState.CurrentChapter)

	if savedState.Selected >= len(verses) {
		savedState.Selected = 0
		savedState.ScrollOffset = 0
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
		zenMode:            false,
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
	for ch := 1; ; ch++ {
		verses := bibleData.GetVerses(book, ch)
		if len(verses) == 0 {
			return ch - 1
		}
	}
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
	if m.mode == searchMode && len(m.searchResults) > 0 {
		return len(m.searchResults), true
	}
	if m.mode == navigationMode {
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		if m.scrollOffset > 0 && len(m.verses) > 0 {
			m.adjustScrollOffset(len(m.verses), m.getVisibleVerses())
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			if m.mode == searchMode {
				m.mode = navigationMode
				m.searchQuery = ""
				m.searchResults = nil
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
					result := m.searchResults[m.selected]
					m.currentBook = result.Book
					m.currentChapter = result.Chapter
					bibleData := m.getBibleData()
					m.verses = bibleData.GetVerses(result.Book, result.Chapter)
					m.mode = navigationMode
					m.selected = 0
					m.scrollOffset = 0

					for i, verse := range m.verses {
						if verse.Verse == result.Verse {
							m.selected = i
							break
						}
					}
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
					if m.mode == navigationMode || (m.mode == searchMode && len(m.searchResults) > 0) {
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
						m.resetVerseView(bibleData)
					}
				case 'z':
					if m.mode == navigationMode {
						m.zenMode = !m.zenMode
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

	helpText := "j/k: Navigate • h/l: Chapter • b/w: Book • t/T: Translation • g/G: Top/Bottom • Ctrl+d/u: Half page • /: Search • z: Zen mode • q: Quit"
	if m.mode == searchMode {
		if len(m.searchResults) > 0 {
			helpText = "j/k: Navigate • g/G: Top/Bottom • Ctrl+d/u: Half page • Enter: Select • /: New search • Esc: Back"
		} else {
			helpText = "Type to search • Enter: Execute • Esc: Back • q: Quit"
		}
	}

	if m.mode == navigationMode {
		if m.zenMode {
			header := m.bookStyle.Render(fmt.Sprintf("%s %s %d", m.currentTranslation, m.currentBook, m.currentChapter))
			content.WriteString(m.centerText(header))
			content.WriteString("\n\n")

			versesAbove := 2
			versesBelow := 2

			headerLines := 1
			headerSpacing := 1
			helpLines := 1
			verseCount := versesAbove + 1 + versesBelow
			verseLinesTotal := verseCount + (verseCount - 1)

			availableHeight := m.height - headerLines - headerSpacing - helpLines

			topPadding := max(0, (availableHeight-verseLinesTotal)/2)

			for i := 0; i < topPadding; i++ {
				content.WriteString("\n")
			}

			startIdx := m.selected - versesAbove
			endIdx := m.selected + versesBelow + 1

			for i := startIdx; i < endIdx; i++ {
				if i < 0 || i >= len(m.verses) {
					content.WriteString("\n")
					if i < endIdx-1 {
						content.WriteString("\n")
					}
				} else {
					verse := m.verses[i]
					verseNumStr := m.verseNumStyle.Render(fmt.Sprintf("%3d", verse.Verse))
					paddingWidth := 6

					m.renderVerseZen(&content, verse, i == m.selected, verseNumStr, paddingWidth)

					if i < endIdx-1 {
						content.WriteString("\n")
					}
				}
			}

			linesUsed := headerLines + headerSpacing + topPadding + verseLinesTotal
			bottomPadding := max(0, m.height-linesUsed-helpLines)
			for i := 0; i < bottomPadding; i++ {
				content.WriteString("\n")
			}

			helpStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(m.config.VerseNumColor)).Render(helpText)
			content.WriteString(m.centerText(helpStyled))
		} else {
			header := m.bookStyle.Render(fmt.Sprintf("%s %s %d", m.currentTranslation, m.currentBook, m.currentChapter))
			content.WriteString(m.centerText(header))
			content.WriteString("\n\n")

			visibleVerses := m.getVisibleVerses()
			m.adjustScrollOffset(len(m.verses), visibleVerses)
			end := min(len(m.verses), m.scrollOffset+visibleVerses)

			linesUsed := 3
			for i := m.scrollOffset; i < end; i++ {
				verse := m.verses[i]
				verseNumStr := m.verseNumStyle.Render(fmt.Sprintf("%3d", verse.Verse))
				linesUsed += m.renderVerse(&content, verse, i == m.selected, verseNumStr, verseTextPadding)
			}

			remainingLines := m.height - linesUsed
			if remainingLines > 0 {
				content.WriteString(strings.Repeat("\n", remainingLines))
			}

			helpStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(m.config.VerseNumColor)).Render(helpText)
			content.WriteString(m.centerText(helpStyled))
		}
	} else {
		if len(m.searchResults) > 0 {
			header := m.bookStyle.Render(fmt.Sprintf("Search: %s (%d results)", m.searchQuery, len(m.searchResults)))
			content.WriteString(m.centerText(header))
			content.WriteString("\n\n")

			availableHeight := m.height - 6

			if availableHeight < 5 {
				availableHeight = 5
			}

			m.clampSelectedIndex(len(m.searchResults))

			_, visibleCount := m.calculateVisibleSearchResults(availableHeight)

			if m.selected >= m.scrollOffset+visibleCount {
				m.scrollOffset = m.selected
				testHeight := m.calculateSearchResultHeight(m.searchResults[m.selected])

				for m.scrollOffset > 0 && testHeight < availableHeight {
					prevHeight := m.calculateSearchResultHeight(m.searchResults[m.scrollOffset-1])
					if testHeight+prevHeight <= availableHeight {
						m.scrollOffset--
						testHeight += prevHeight
					} else {
						break
					}
				}

				_, visibleCount = m.calculateVisibleSearchResults(availableHeight)
			}

			if m.selected < m.scrollOffset {
				m.scrollOffset = m.selected
				_, visibleCount = m.calculateVisibleSearchResults(availableHeight)
			}

			end := min(len(m.searchResults), m.scrollOffset+visibleCount)

			linesUsed := 3
			for i := m.scrollOffset; i < end; i++ {
				result := m.searchResults[i]
				reference := truncateText(fmt.Sprintf("%s %d:%d", result.Book, result.Chapter, result.Verse), 20)
				verseNumStr := m.verseNumStyle.Render(fmt.Sprintf("%-20s", reference))
				linesUsed += m.renderVerse(&content, result, i == m.selected, verseNumStr, searchTextPadding)
			}

			remainingLines := m.height - linesUsed
			if remainingLines > 0 {
				content.WriteString(strings.Repeat("\n", remainingLines))
			}

			helpStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(m.config.VerseNumColor)).Render(helpText)
			content.WriteString(m.centerText(helpStyled))
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

			helpStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(m.config.VerseNumColor)).Render(helpText)
			content.WriteString(m.centerText(helpStyled))
		}
	}

	return content.String()
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

	availableHeight := max(5, m.height-6)
	currentHeight := 0
	visibleCount := 0

	for i := m.scrollOffset; i < len(m.verses) && currentHeight < availableHeight; i++ {
		verseHeight := m.calculateVerseHeight(m.verses[i])
		if currentHeight+verseHeight <= availableHeight {
			currentHeight += verseHeight
			visibleCount++
		} else {
			break
		}
	}

	return max(1, visibleCount)
}

func (m model) calculateTextHeight(text string, paddingWidth int) int {
	textWidth := max(20, m.width-paddingWidth)
	return max(2, len(wrapVerseText(text, textWidth))+1)
}

const (
	verseTextPadding  = 6
	searchTextPadding = 23
)

func (m model) calculateVerseHeight(verse Verse) int {
	return m.calculateTextHeight(verse.Text, verseTextPadding)
}

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

func (m model) renderVerse(content *strings.Builder, verse Verse, isSelected bool, verseNumStr string, paddingWidth int) int {
	if isSelected {
		cursorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.config.HighlightColor)).
			Bold(true)
		content.WriteString(cursorStyle.Render(">"))
	} else {
		content.WriteString(" ")
	}
	content.WriteByte(' ')
	content.WriteString(verseNumStr)
	content.WriteByte(' ')

	textWidth := max(20, m.width-paddingWidth)
	verseLines := wrapVerseText(verse.Text, textWidth)

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

func (m *model) calculateVisibleSearchResults(availableHeight int) (linesUsed, visibleCount int) {
	for i := m.scrollOffset; i < len(m.searchResults) && linesUsed < availableHeight; i++ {
		resultHeight := m.calculateSearchResultHeight(m.searchResults[i])
		if linesUsed+resultHeight <= availableHeight {
			linesUsed += resultHeight
			visibleCount++
		} else {
			break
		}
	}
	return
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

func (m model) renderVerseZen(content *strings.Builder, verse Verse, isSelected bool, verseNumStr string, paddingWidth int) {
	textWidth := 10000
	verseLines := wrapVerseText(verse.Text, textWidth)

	if len(verseLines) == 0 {
		return
	}

	var style lipgloss.Style
	if isSelected {
		style = m.textStyle
	} else {
		style = m.dimStyle
	}

	line := style.Render(verseLines[0])
	visualWidth := lipgloss.Width(line)

	if visualWidth < m.width {
		leftPadding := (m.width - visualWidth) / 2
		content.WriteString(strings.Repeat(" ", leftPadding))
	}

	content.WriteString(line)
	content.WriteString("\n")
}
