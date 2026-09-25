package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigDir points bible-go at a temp config dir holding the given files.
func withConfigDir(t *testing.T, files map[string]string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	dir := filepath.Join(root, "bible-go")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestColorsFileOverridesThemeAndConfig(t *testing.T) {
	withConfigDir(t, map[string]string{
		"config.json": `{"theme":"gruvbox","textColor":"#111111"}`,
		"colors.json": `{"highlightColor":"#e5c799","textColor":"#dcd8cd"}`,
	})
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.HighlightColor != "#e5c799" || got.TextColor != "#dcd8cd" {
		t.Errorf("overridden = (%q, %q), want (#e5c799, #dcd8cd)", got.HighlightColor, got.TextColor)
	}
	// Fields colors.json leaves out keep the theme's values.
	if want := themes["gruvbox"].VerseNumColor; got.VerseNumColor != want {
		t.Errorf("verseNumColor = %q, want theme's %q", got.VerseNumColor, want)
	}
}

func TestColorsFileIgnoresMalformedValues(t *testing.T) {
	withConfigDir(t, map[string]string{
		"config.json": `{"theme":"nord"}`,
		"colors.json": `{"dimColor":"grey","textColor":"#12345"}`,
	})
	got, _ := loadConfig()
	if got.DimColor != themes["nord"].DimColor || got.TextColor != themes["nord"].TextColor {
		t.Errorf("malformed values applied: %+v", got)
	}
}

func TestColorsFileNotWrittenOnFirstRun(t *testing.T) {
	withConfigDir(t, map[string]string{
		"colors.json": `{"highlightColor":"#e5c799"}`,
	})
	got, _ := loadConfig()
	if got.HighlightColor != "#e5c799" {
		t.Errorf("first run ignored colors.json: %q", got.HighlightColor)
	}
	saved, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "bible-go", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 || strings.Contains(string(saved), "#e5c799") {
		t.Errorf("first-run config.json leaked colors.json: %s", saved)
	}
}

func TestNoColorsFileKeepsBehaviour(t *testing.T) {
	withConfigDir(t, map[string]string{"config.json": `{"theme":"dracula"}`})
	got, _ := loadConfig()
	if got.HighlightColor != themes["dracula"].HighlightColor {
		t.Errorf("highlight = %q, want dracula's", got.HighlightColor)
	}
}
