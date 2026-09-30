package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const testBibleJSON = `{
  "Genesis": {
    "1": {"1": "In the beginning God created the heaven and the earth.", "2": "And the earth was without form."},
    "2": {"1": "Thus the heavens were finished."}
  },
  "Song Of Solomon": {
    "1": {"1": "The song of songs, which is Solomon's. So loved he."}
  },
  "John": {
    "3": {
      "16": "For God so loved the world, that he gave his only begotten Son.",
      "17": "For God sent not his Son into the world to condemn the world.",
      "18": "He that believeth on him is not condemned."
    },
    "15": {"12": "This is my commandment, That ye love one another, as I have loved you."}
  },
  "Empty": {}
}`

func testBible(t *testing.T) *BibleData {
	t.Helper()
	bd, err := NewBibleData([]byte(testBibleJSON))
	if err != nil {
		t.Fatal(err)
	}
	return bd
}

func refs(vs []Verse) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = fmt.Sprintf("%s %d:%d", v.Book, v.Chapter, v.Verse)
	}
	return out
}

func TestIndexesAndOrder(t *testing.T) {
	bd := testBible(t)
	if got := strings.Join(bd.GetBooks(), ","); got != "Genesis,Song Of Solomon,John" {
		t.Errorf("books = %s (empty book should be dropped)", got)
	}
	if got := bd.Chapters("John"); len(got) != 2 || got[0] != 3 || got[1] != 15 {
		t.Errorf("John chapters = %v", got)
	}
	if got := refs(bd.GetVerses("John", 3)); strings.Join(got, ",") != "John 3:16,John 3:17,John 3:18" {
		t.Errorf("John 3 = %v", got)
	}
	if len(bd.GetVerses("John", 99)) != 0 {
		t.Error("missing chapter returned verses")
	}
}

func TestSearchSubstringNotJustWholeWords(t *testing.T) {
	bd := testBible(t)
	// "love" must also match "loved".
	if got := len(bd.Search("love")); got != 3 {
		t.Errorf("Search(love) = %d results, want 3: %v", got, refs(bd.Search("love")))
	}
}

func TestSearchPhraseBeatsBookPrefix(t *testing.T) {
	bd := testBible(t)
	got := refs(bd.Search("so loved"))
	if len(got) == 0 || got[0] != "John 3:16" {
		t.Errorf("Search(so loved) = %v, want John 3:16 first", got)
	}
}

func TestSearchInBook(t *testing.T) {
	bd := testBible(t)
	got := refs(bd.Search("genesis earth"))
	if strings.Join(got, ",") != "Genesis 1:2,Genesis 1:1" { // ranked by match position
		t.Errorf("Search(genesis earth) = %v", got)
	}
}

func TestSearchReferences(t *testing.T) {
	bd := testBible(t)
	cases := map[string]string{
		"John 3:16":    "John 3:16",
		"jo 3:17":      "John 3:17",
		"John 3:16-17": "John 3:16,John 3:17",
		"John 3":       "John 3:16,John 3:17,John 3:18",
		"gen 2":        "Genesis 2:1",
	}
	for q, want := range cases {
		if got := strings.Join(refs(bd.searchByReference(q)), ","); got != want {
			t.Errorf("searchByReference(%q) = %s, want %s", q, got, want)
		}
	}
	for _, q := range []string{"John", "John:3", "3:16", "John 3:x", "John 3:17-16"} {
		if got := bd.searchByReference(q); len(got) != 0 {
			t.Errorf("searchByReference(%q) = %v, want none", q, refs(got))
		}
	}
}

func TestNearestChapter(t *testing.T) {
	chs := []int{1, 2, 5, 9}
	for in, want := range map[int]int{0: 1, 2: 2, 3: 2, 4: 5, 7: 5, 8: 9, 50: 9} {
		if got := nearestChapter(chs, in); got != want {
			t.Errorf("nearestChapter(%d) = %d, want %d", in, got, want)
		}
	}
	if nearestChapter(nil, 4) != 1 {
		t.Error("empty chapters should give 1")
	}
}

func TestWrapUsesDisplayWidth(t *testing.T) {
	// Curly quotes are 3 bytes but one cell wide.
	text := "“abcd” “abcd”"
	if got := wrapVerseText(text, 13); len(got) != 1 {
		t.Errorf("wrap = %q, want one line", got)
	}
	if got := wrapVerseText("aaa bbb ccc", 7); strings.Join(got, "|") != "aaa bbb|ccc" {
		t.Errorf("wrap = %q", got)
	}
}

func testModel(t *testing.T) model {
	t.Helper()
	withConfigDir(t, nil)
	bd := testBible(t)
	mbd := &MultiBibleData{
		translations:     map[string]*BibleData{"TST": bd},
		translationNames: []string{"TST"},
		filePaths:        map[string]string{},
		failed:           map[string]bool{},
		lastGood:         bd,
	}
	return model{
		multiBibleData:     mbd,
		currentTranslation: "TST",
		currentBook:        "John",
		currentChapter:     3,
		verses:             bd.GetVerses("John", 3),
		height:             24,
		width:              80,
		bookmarkSet:        map[Bookmark]bool{},
	}
}

func press(m model, keys ...string) model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(model)
		_ = m.View()
	}
	return m
}

func TestEmptyBookmarksNoPanic(t *testing.T) {
	m := press(testModel(t), "'", "G", "y", "d", "enter")
	if m.selected != 0 {
		t.Errorf("selected = %d in empty bookmarks", m.selected)
	}
}

func TestBackspaceMultibyte(t *testing.T) {
	m := press(testModel(t), "/", "é", "backspace")
	if m.searchQuery != "" {
		t.Errorf("query = %q, want empty", m.searchQuery)
	}
}

func TestLastChapterNextIsNoop(t *testing.T) {
	m := testModel(t)
	m.currentChapter = 15
	m.verses = m.getBibleData().GetVerses("John", 15)
	m = press(m, "l")
	if m.currentBook != "John" || m.currentChapter != 15 {
		t.Errorf("moved to %s %d", m.currentBook, m.currentChapter)
	}
}

func TestChapterNavigationSkipsGaps(t *testing.T) {
	m := press(testModel(t), "l")
	if m.currentChapter != 15 {
		t.Errorf("next chapter = %d, want 15", m.currentChapter)
	}
	m = press(m, "h", "h")
	if m.currentBook != "Song Of Solomon" || m.currentChapter != 1 {
		t.Errorf("prev = %s %d", m.currentBook, m.currentChapter)
	}
}

func TestSelectedAlwaysVisible(t *testing.T) {
	m := testModel(t)
	long := strings.Repeat("word ", 60)
	var vs []Verse
	for i := 1; i <= 30; i++ {
		text := "short"
		if i%3 == 0 {
			text = long
		}
		vs = append(vs, Verse{Book: "John", Chapter: 3, Verse: i, Text: text})
	}
	m.verses = vs
	m.height = 12
	m.width = 40
	for sel := range vs {
		m.selected = sel
		off := m.listOffset(vs, verseTextPadding)
		n := len(m.visibleLines(vs, off, verseTextPadding))
		if sel < off || sel >= off+n {
			t.Fatalf("selected %d not in window [%d,%d)", sel, off, off+n)
		}
	}
}

func TestSaveJSONAtomicFollowsSymlink(t *testing.T) {
	withConfigDir(t, nil)
	dir, _ := getConfigDir()
	real := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(real, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, stateFile)
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := saveState(AppState{CurrentTranslation: "X"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced")
	}
	if b, _ := os.ReadFile(real); !strings.Contains(string(b), `"X"`) {
		t.Errorf("target not written: %s", b)
	}
}

func TestBatchedRunesAllHandled(t *testing.T) {
	m := testModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/so")})
	if got := next.(model).searchQuery; got != "so" {
		t.Errorf("query = %q, want %q", got, "so")
	}
}
