# bible-go

A terminal-based Bible reader built with Go and Bubble Tea.

## Features

- Navigate through books and chapters
- Search verses
- Switch between multiple Bible translations with 't' key
- Persistent state saving

## Usage

The app automatically loads Bible translations from the `~/.config/bible-go/translations/` directory. Each translation is stored as a JSON file (e.g., `ESV_bible.json`, `KJV_bible.json`).

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

- `j/k` or `↑/↓`: Navigate verses
- `h/l` or `←/→`: Previous/Next chapter
- `b/w`: Previous/Next book
- `t/T`: Next/Previous translation
- `g/G`: Go to first/last verse
- `Ctrl+d/u`: Page down/up
- `/`: Search
- `q`: Quit

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
