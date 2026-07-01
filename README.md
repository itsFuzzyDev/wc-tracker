# World Cup TUI

A terminal UI for browsing World Cup data. Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## What it looks like

- Browse matches, groups, standings, and stadiums
- Filter games by team, group, or stadium
- Live data refresh (every 30s)
- Dark/light theme auto-detection

## Build

```bash
go build .
```

## Run

```bash
./tui
```

## Controls

| Key | Action |
|-----|--------|
| `Tab` / arrow keys | Switch tabs |
| `↑` / `↓` | Navigate list |
| `/` | Filter games |
| `r` | Refresh data |
| `q` / `Ctrl+c` | Quit |

## Files

| File | What it does |
|------|-------------|
| `main.go` | UI state, key handling, layout |
| `data.go` | Data structures, standings logic |
| `fetch.go` | HTTP fetch for live data |
| `styles.go` | Colors, borders, theme detection |
| `data.json` | Local game data cache |
