# bible-go

A terminal-based Bible reader built with Go and Bubble Tea.

## Features

- Navigate through books and chapters
- Advanced search with multiple methods (see Search Features below)
- Switch between multiple Bible translations with 't' key
- Zen mode for distraction-free reading
- Persistent state saving

## Usage

The app automatically loads Bible translations from the `~/.config/bible-go/translations/` directory. Each translation is stored as a JSON file (e.g., `ESV_bible.json`, `KJV_bible.json`).

Configuration can be customized by editing `~/.config/bible-go/config.json`. The default configuration is:
```json
{
  "highlightColor": "#cba6f7",
  "verseNumColor": "#89b4fa",
  "textColor": "#cdd6f4",
  "dimColor": "#313244"
}
```
- `highlightColor`: Hex color for the selected verse cursor (">") and book/chapter headers
- `verseNumColor`: Hex color for verse numbers and search result references
- `textColor`: Hex color for verse text content
- `dimColor`: Hex color for dimmed verses in zen mode

**Note**: Bible translation files are not included in this repository due to copyright restrictions. You can obtain them from [jadenzaleski/bible-translations](https://github.com/jadenzaleski/bible-translations) and place them in `~/.config/bible-go/translations/`.

**Performance Note**: The app uses lazy loading - only the current translation is loaded at startup for fast startup times. Other translations are loaded on-demand when you switch to them.

Supported translations are dynamically loaded from the available JSON files in `bible-data/`.

The JSON structure should be:
```json
{
  "BookName": {
    "1": {
      "1": "Verse text...",
      "2": "Verse text..."
    }
  }
}
```

### Controls

**Navigation:**
- `j/k` or `↑/↓`: Navigate verses
- `h/l` or `←/→`: Previous/Next chapter
- `b/w` or `PgUp/PgDn`: Previous/Next book
- `t/T`: Next/Previous translation
- `g/G`: Go to first/last verse
- `Ctrl+d/u`: Half page down/up

**Features:**
- `/`: Search (see Search Features below)
- `z`: Toggle zen mode (distraction-free reading with centered text)
- `q` or `Esc`: Quit (Esc exits search mode if active)

### Search Features

Press `/` to enter search mode. The search supports multiple methods:

1. **Bible Reference Search:**
   - `Genesis 1` - Shows all verses in Genesis chapter 1
   - `John 3:16` - Shows John chapter 3, verse 16
   - `gen 1` - Partial book names work (shows Genesis 1)

2. **Full-Text Search:**
   - `faith hope love` - Finds verses containing all these words
   - Results are ranked by relevance (exact phrase matches ranked higher)

3. **Book-Scoped Search:**
   - `Romans grace` - Search for "grace" only in the book of Romans
   - Format: `<book name> <search term>`

**Search Navigation:**
- Type your query and press `Enter` to execute the search
- Use `j/k` or arrow keys to navigate search results
- Press `Enter` on a result to jump to that verse in context
- Press `/` again for a new search or `Esc` to exit search mode

### Zen Mode

Press `z` to toggle zen mode, which provides a distraction-free reading experience:
- Centers the current verse on screen
- Shows 2 verses above and below for context
- Dims surrounding verses to focus attention
- Perfect for meditation and contemplative reading

## Building

```bash
go build -ldflags="-s -w"
```

The `-ldflags="-s -w"` flags strip debug symbols for a smaller binary size.

## Running

```bash
./bible-go
```

Ensure the `~/.config/bible-go/translations/` directory exists with Bible translation JSON files.
