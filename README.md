# bible-go

A terminal-based Bible reader built with Go and Bubble Tea.

## Features

- Navigate through books and chapters
- Search verses
- Switch between multiple Bible translations with 't' key
- Persistent state saving

## Usage

The app automatically loads Bible translations from the `bible-data/` directory within the project. Each translation is stored as a JSON file (e.g., `ESV_bible.json`, `KJV_bible.json`).

**Performance Note**: The app uses lazy loading - only the current translation is loaded at startup for fast startup times. Other translations are loaded on-demand when you switch to them.

Currently supported translations: AKJV, AMP, ASV, BRG, CSB, EHV, ESV, ESVUK, GNV, GW, ISV, JUB, KJ21, KJV, LEB, MEV, NASB, NASB1995, NET, NIV, NIVUK, NKJV, NLT, NLV, NOG, NRSV, NRSVUE, WEB, YLT.

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
go build
```
