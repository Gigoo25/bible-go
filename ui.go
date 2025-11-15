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

const (
	configDir = "bible-go"
	stateFile = "state.json"
)

func getConfigPath() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(homeDir, ".config")
	}

	configPath := filepath.Join(configDir, "bible-go")
	err := os.MkdirAll(configPath, 0o755)
	if err != nil {
		return "", err
	}

	return filepath.Join(configPath, stateFile), nil
}

func saveState(state AppState) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0o644)
}

func loadState() (AppState, error) {
	var state AppState

	configPath, err := getConfigPath()
	if err != nil {
		return getDefaultAppState(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return getDefaultAppState(), nil
	}

	err = json.Unmarshal(data, &state)
	return state, err
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

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			PaddingLeft(1).
			PaddingRight(1).
			Background(lipgloss.Color("236"))

	bookStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("220"))

	verseStyle = lipgloss.NewStyle().
			MarginBottom(2)

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("220")).
			Bold(true)

	verseNumStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("242")).
			Bold(true)

	searchStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			MarginTop(1).
			PaddingLeft(1)
)

func initialModel() tea.Model {
	multiBibleData, err := NewMultiBibleData()
	if err != nil {
		panic(err)
	}

	savedState, err := loadState()
	if err != nil {
		savedState = getDefaultAppState()
		savedState.CurrentBook = "Genesis"
	}

	if savedState.CurrentTranslation == "" || !contains(multiBibleData.translationNames, savedState.CurrentTranslation) {
		savedState.CurrentTranslation = multiBibleData.translationNames[0]
	}

	bibleData := multiBibleData.GetCurrentBibleData(savedState.CurrentTranslation)
	books := bibleData.GetBooks()
	if len(books) == 0 {
		panic("no books found in bible data")
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
	bibleData := m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
	books := bibleData.GetBooks()
	for i, book := range books {
		if book == m.currentBook {
			newIndex := i + direction
			if newIndex >= 0 && newIndex < len(books) {
				m.currentBook = books[newIndex]
				m.currentChapter = 1
				m.updateVersesAndReset(bibleData)
			}
			break
		}
	}
}

func (m *model) goToPreviousChapter() {
	if m.mode != navigationMode {
		return
	}
	bibleData := m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
	if m.currentChapter > 1 {
		m.currentChapter--
		m.updateVersesAndReset(bibleData)
	} else {
		m.goToPreviousBookLastChapter(bibleData)
	}
}

func (m *model) goToNextChapter() {
	if m.mode != navigationMode {
		return
	}
	bibleData := m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
	m.currentChapter++
	m.verses = bibleData.GetVerses(m.currentBook, m.currentChapter)
	if len(m.verses) == 0 {
		m.goToNextBookFirstChapter(bibleData)
	} else {
		m.updateVersesAndReset(bibleData)
	}
}

func (m *model) updateVersesAndReset(bibleData *BibleData) {
	m.verses = bibleData.GetVerses(m.currentBook, m.currentChapter)
	m.selected = 0
	m.scrollOffset = 0
}

func (m *model) goToPreviousBookLastChapter(bibleData *BibleData) {
	books := bibleData.GetBooks()
	for i, book := range books {
		if book == m.currentBook && i > 0 {
			prevBook := books[i-1]
			m.currentBook = prevBook
			m.currentChapter = m.findLastChapter(bibleData, prevBook)
			m.updateVersesAndReset(bibleData)
			break
		}
	}
}

func (m *model) goToNextBookFirstChapter(bibleData *BibleData) {
	books := bibleData.GetBooks()
	for i, book := range books {
		if book == m.currentBook && i < len(books)-1 {
			nextBook := books[i+1]
			m.currentBook = nextBook
			m.currentChapter = 1
			m.updateVersesAndReset(bibleData)
			return
		}
	}
	m.currentChapter--
	m.updateVersesAndReset(bibleData)
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
		m.adjustScrollOffset(listLen, m.getVisibleVerses())
	}
}

func (m *model) moveDown(listLen int) {
	if m.selected < listLen-1 {
		m.selected++
		m.adjustScrollOffset(listLen, m.getVisibleVerses())
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
					bibleData := m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
					m.searchResults = bibleData.Search(m.searchQuery)
					m.selected = 0
					m.scrollOffset = 0
				} else if len(m.searchResults) > 0 && m.selected < len(m.searchResults) {
					result := m.searchResults[m.selected]
					m.currentBook = result.Book
					m.currentChapter = result.Chapter
					bibleData := m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
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
					if m.mode == navigationMode {
						m.selected = len(m.verses) - 1
						visibleVerses := m.getVisibleVerses()
						if m.selected >= visibleVerses {
							m.scrollOffset = m.selected - visibleVerses + 1
						}
					} else if m.mode == searchMode && len(m.searchResults) > 0 {
						m.selected = len(m.searchResults) - 1
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
					if m.mode == navigationMode {
						m.moveUp(len(m.verses))
					} else if m.mode == searchMode && len(m.searchResults) > 0 {
						m.moveUp(len(m.searchResults))
					}
				case 'j':
					if m.mode == navigationMode {
						m.moveDown(len(m.verses))
					} else if m.mode == searchMode && len(m.searchResults) > 0 {
						m.moveDown(len(m.searchResults))
					}
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
						bibleData := m.multiBibleData.GetCurrentBibleData(m.currentTranslation)
						books := bibleData.GetBooks()
						if !contains(books, m.currentBook) {
							m.currentBook = books[0]
							m.currentChapter = 1
						}
						m.updateVersesAndReset(bibleData)
					}
				case 'q':
					m.saveCurrentState()
					return m, tea.Quit
				}
			}

		case tea.KeyUp:
			if m.mode == navigationMode {
				m.moveUp(len(m.verses))
			} else if m.mode == searchMode && len(m.searchResults) > 0 {
				m.moveUp(len(m.searchResults))
			}

		case tea.KeyDown:
			if m.mode == navigationMode {
				m.moveDown(len(m.verses))
			} else if m.mode == searchMode && len(m.searchResults) > 0 {
				m.moveDown(len(m.searchResults))
			}

		case tea.KeyLeft:
			m.goToPreviousChapter()

		case tea.KeyRight:
			m.goToNextChapter()

		case tea.KeyPgUp:
			m.goToPreviousBook()

		case tea.KeyPgDown:
			m.goToNextBook()

		case tea.KeyCtrlD:
			if m.mode == navigationMode {
				m.pageDown(len(m.verses))
			} else if m.mode == searchMode && len(m.searchResults) > 0 {
				m.pageDown(len(m.searchResults))
			}

		case tea.KeyCtrlU:
			if m.mode == navigationMode {
				m.pageUp(len(m.verses))
			} else if m.mode == searchMode && len(m.searchResults) > 0 {
				m.pageUp(len(m.searchResults))
			}
		}
	}

	return m, nil
}

func (m model) View() string {
	var content strings.Builder

	if m.mode == navigationMode {
		content.WriteString(bookStyle.Render(fmt.Sprintf("%s %s %d", m.currentTranslation, m.currentBook, m.currentChapter)))
		content.WriteString("\n\n")

		visibleVerses := m.getVisibleVerses()
		end := m.scrollOffset + visibleVerses
		if end > len(m.verses) {
			end = len(m.verses)
		}

		m.adjustScrollOffset(len(m.verses), visibleVerses)
		end = m.scrollOffset + visibleVerses
		if end > len(m.verses) {
			end = len(m.verses)
		}

		linesUsed := 0
		linesUsed += 3

		for i := m.scrollOffset; i < end; i++ {
			verse := m.verses[i]
			verseNumStr := verseNumStyle.Render(fmt.Sprintf("%3d", verse.Verse))
			paddingWidth := 6

			lineCount := m.renderVerse(&content, verse, i == m.selected, verseNumStr, paddingWidth)
			linesUsed += lineCount
		}

		remainingLines := m.height - linesUsed - 1
		if remainingLines > 0 {
			content.WriteString(strings.Repeat("\n", remainingLines))
		}
	} else {
		if len(m.searchResults) > 0 {
			content.WriteString(bookStyle.Render(fmt.Sprintf("Search: %s (%d results)", m.searchQuery, len(m.searchResults))))
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

			end := m.scrollOffset + visibleCount
			if end > len(m.searchResults) {
				end = len(m.searchResults)
			}

			actualLinesUsed := 0
			for i := m.scrollOffset; i < end; i++ {
				result := m.searchResults[i]
				reference := truncateText(fmt.Sprintf("%s %d:%d", result.Book, result.Chapter, result.Verse), 20)
				verseNumStr := verseNumStyle.Render(fmt.Sprintf("%-20s", reference))
				paddingWidth := 23

				lineCount := m.renderVerse(&content, result, i == m.selected, verseNumStr, paddingWidth)
				actualLinesUsed += lineCount
			}

			remainingLines := m.height - 4 - actualLinesUsed
			if remainingLines > 0 {
				content.WriteString(strings.Repeat("\n", remainingLines))
			}
		} else {
			content.WriteString(bookStyle.Render(fmt.Sprintf("Search: %s", m.searchQuery)))
			content.WriteString("\n\n")

			if m.searchQuery != "" {
				content.WriteString("Press Enter to search")
			} else {
				content.WriteString("Type to search...")
			}
			content.WriteString("\n")

			remainingLines := m.height - 5
			if remainingLines > 0 {
				content.WriteString(strings.Repeat("\n", remainingLines))
			}
		}
	}

	helpText := "j/k: Navigate • h/l: Chapter • b/w: Book • t/T: Translation • g/G: Top/Bottom • Ctrl+d/u: Half page • /: Search • q: Quit"
	if m.mode == searchMode {
		if len(m.searchResults) > 0 {
			helpText = "j/k: Navigate • g/G: Top/Bottom • Ctrl+d/u: Half page • Enter: Select • /: New search • Esc: Back"
		} else {
			helpText = "Type to search • Enter: Execute • Esc: Back • q: Quit"
		}
	}
	content.WriteString(helpStyle.Render(helpText))

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

func (m model) calculateVerseHeight(verse Verse) int {
	return m.calculateTextHeight(verse.Text, 2+3+1)
}

func (m model) calculateSearchResultHeight(result Verse) int {
	return m.calculateTextHeight(result.Text, 2+20+1)
}

func wrapVerseText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	lines := make([]string, 0, len(words)/4)
	var currentLine strings.Builder

	for i, word := range words {
		if currentLine.Len() > 0 {
			if currentLine.Len()+1+len(word) > maxWidth {
				lines = append(lines, currentLine.String())
				currentLine.Reset()
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
		content.WriteString(verseLines[0])
	}
	content.WriteByte('\n')
	linesUsed := 1

	padding := strings.Repeat(" ", paddingWidth)
	for _, line := range verseLines[1:] {
		content.WriteString(padding)
		content.WriteString(line)
		content.WriteByte('\n')
		linesUsed++
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
