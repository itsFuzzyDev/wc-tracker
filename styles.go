package main

import (
	"github.com/charmbracelet/lipgloss"
)

type Styles struct {
	App       lipgloss.Style
	Header    lipgloss.Style
	Tab       lipgloss.Style
	TabActive lipgloss.Style
	Sidebar   lipgloss.Style
	Main      lipgloss.Style
	MainFill  lipgloss.Style
	ListItem  lipgloss.Style
	Selected  lipgloss.Style
	Detail    lipgloss.Style
	Badge     lipgloss.Style
	Score     lipgloss.Style
	Dim       lipgloss.Style
	Accent    lipgloss.Style
	Help      lipgloss.Style
	SearchBar lipgloss.Style
}

func NewStyles(theme string, width int) Styles {
	var bg, panel, text, dim, accent lipgloss.Color
	var accentDim, border, selectedBg lipgloss.Color

	if theme == "light" {
		bg = lipgloss.Color("#f5f5f7")
		panel = lipgloss.Color("#ffffff")
		text = lipgloss.Color("#1a1a1a")
		dim = lipgloss.Color("#6b6b6b")
		accent = lipgloss.Color("#16a34a")
		accentDim = lipgloss.Color("#dcfce7")
		border = lipgloss.Color("#e0e0e0")
		selectedBg = lipgloss.Color("#dcfce7")
	} else {
		bg = lipgloss.Color("#0f1115")
		panel = lipgloss.Color("#1a1d23")
		text = lipgloss.Color("#e8e8e8")
		dim = lipgloss.Color("#8a8f98")
		accent = lipgloss.Color("#16a34a")
		accentDim = lipgloss.Color("#14532d")
		border = lipgloss.Color("#2a2d35")
		selectedBg = lipgloss.Color("#1a2e1f")
	}

	return Styles{
		App: lipgloss.NewStyle().
			Background(bg).
			Foreground(text).
			Padding(1, 2),

		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(text).
			MarginBottom(1),

		Tab: lipgloss.NewStyle().
			Padding(0, 2).
			MarginRight(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Foreground(dim),

		TabActive: lipgloss.NewStyle().
			Padding(0, 2).
			MarginRight(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Foreground(accent).
			Bold(true),

		Sidebar: lipgloss.NewStyle().
			Width(18).
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Background(panel),

		Main: lipgloss.NewStyle().
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Background(panel),

		MainFill: lipgloss.NewStyle().
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Background(panel),

		ListItem: lipgloss.NewStyle().
			Padding(0, 1).
			MarginBottom(0),

		Selected: lipgloss.NewStyle().
			Padding(0, 1).
			Background(selectedBg).
			Foreground(accent).
			Bold(true),

		Detail: lipgloss.NewStyle().
			Width(40).
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Background(panel),

		Badge: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(accent).
			Background(accentDim).
			Bold(true),

		Score: lipgloss.NewStyle().
			Bold(true).
			Foreground(text).
			Padding(0, 1),

		Dim: lipgloss.NewStyle().
			Foreground(dim),

		Accent: lipgloss.NewStyle().
			Foreground(accent),

		Help: lipgloss.NewStyle().
			Foreground(dim).
			PaddingTop(1),

		SearchBar: lipgloss.NewStyle().
			Foreground(accent).
			Background(panel).
			Padding(0, 1).
			Bold(true),
	}
}
