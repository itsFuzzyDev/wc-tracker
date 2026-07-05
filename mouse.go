package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Screen layout constants shared by View and mouse hit-testing.
// Tabs render 3 rows (rounded border), followed by one blank line.
const contentTop = 4

// One row per wheel event: terminals already send several events per
// physical notch, so anything higher feels too fast.
const wheelStep = 1

func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.data == nil {
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.mouseWheel(-wheelStep, msg.X)
		case tea.MouseButtonWheelDown:
			m.mouseWheel(wheelStep, msg.X)
		case tea.MouseButtonLeft:
			return m.mouseClick(msg.X, msg.Y)
		}
	case tea.MouseActionMotion:
		if msg.Button == tea.MouseButtonNone {
			m.mouseHover(msg.X, msg.Y)
		}
	}
	return m, nil
}

func (m *model) mouseWheel(delta, x int) {
	switch m.tab {
	case gamesTab:
		if m.detailMatch != nil {
			return
		}
		filtered := m.filteredGames()
		m.gamesIndex += delta
		if m.gamesIndex >= len(filtered) {
			m.gamesIndex = len(filtered) - 1
		}
		if m.gamesIndex < 0 {
			m.gamesIndex = 0
		}
		m.clampScroll()
	case dataTab:
		if m.activeSection == "standings" {
			standings := ComputeStandings(m.data)
			lines := m.standingsVisibleLines(standings, m.filteredGroupKeys(standings))
			m.standingsScroll += delta
			if m.standingsScroll >= lines {
				m.standingsScroll = lines - 1
			}
			if m.standingsScroll < 0 {
				m.standingsScroll = 0
			}
			return
		}
		visible := m.dataVisible()
		if x < m.dataPanelW()+2 {
			keys := m.filteredTeamKeys()
			m.teamIndex += delta
			if m.teamIndex >= len(keys) {
				m.teamIndex = len(keys) - 1
			}
			if m.teamIndex < 0 {
				m.teamIndex = 0
			}
			m.teamScroll = smartScroll(m.teamIndex, len(keys), visible)
		} else {
			keys := m.filteredStadiumKeys()
			m.stadiumIndex += delta
			if m.stadiumIndex >= len(keys) {
				m.stadiumIndex = len(keys) - 1
			}
			if m.stadiumIndex < 0 {
				m.stadiumIndex = 0
			}
			m.stadiumScroll = smartScroll(m.stadiumIndex, len(keys), visible)
		}
	}
}

func (m *model) mouseHover(x, y int) {
	switch m.tab {
	case gamesTab:
		if m.detailMatch != nil {
			return
		}
		if idx := m.gamesIndexAt(x, y); idx >= 0 {
			m.gamesIndex = idx
			m.filterFocus = 3
		}
	case dataTab:
		if m.activeSection == "standings" {
			return
		}
		section, idx := m.dataIndexAt(x, y)
		if idx < 0 {
			return
		}
		m.activeSection = section
		if section == "teams" {
			m.teamIndex = idx
		} else {
			m.stadiumIndex = idx
		}
	}
}

func (m model) mouseClick(x, y int) (tea.Model, tea.Cmd) {
	if y < contentTop-1 {
		if t, ok := m.tabAt(x); ok {
			m.switchTab(t)
		}
		return m, nil
	}

	switch m.tab {
	case gamesTab:
		if m.detailMatch != nil {
			return m, nil
		}
		if m.sidebarOpen && x < sidebarW {
			m.sidebarClick(y - (contentTop + 2))
			return m, nil
		}
		if idx := m.gamesIndexAt(x, y); idx >= 0 {
			if idx == m.gamesIndex {
				filtered := m.filteredGames()
				m.detailMatch = &filtered[idx]
			} else {
				m.gamesIndex = idx
				m.filterFocus = 3
				m.clampScroll()
			}
		}
	case dataTab:
		if m.activeSection == "standings" {
			return m, nil
		}
		section, idx := m.dataIndexAt(x, y)
		if idx < 0 {
			return m, nil
		}
		if section == "teams" {
			if m.activeSection == "teams" && idx == m.teamIndex {
				return m, m.openItem()
			}
			m.activeSection = "teams"
			m.teamIndex = idx
		} else {
			if m.activeSection == "stadiums" && idx == m.stadiumIndex {
				return m, m.openItem()
			}
			m.activeSection = "stadiums"
			m.stadiumIndex = idx
		}
	}
	return m, nil
}

// gamesIndexAt returns the filtered-games index under (x, y), or -1.
func (m model) gamesIndexAt(x, y int) int {
	mainLeft := 0
	if m.sidebarOpen {
		mainLeft = sidebarW + 2
	}
	if x < mainLeft {
		return -1
	}
	top, rows := m.gamesRowsWindow()
	if y < top || y >= top+rows {
		return -1
	}
	idx := m.gamesScroll + (y - top)
	if idx < 0 || idx >= len(m.filteredGames()) {
		return -1
	}
	return idx
}

// gamesRowsWindow returns the screen row of the first match line and how
// many match lines are visible; mirrors the math in renderGames.
func (m model) gamesRowsWindow() (top, rows int) {
	contentH := m.height - 6
	closure := 2
	top = contentTop + 3 // panel border + padding + count header
	if m.searchActive {
		contentH--
		closure = 3
		top++
	}
	if contentH < 1 {
		contentH = 1
	}
	rows = contentH - closure
	if rows < 0 {
		rows = 0
	}
	return top, rows
}

// dataIndexAt returns ("teams"|"stadiums", index) under (x, y), or ("", -1).
func (m model) dataIndexAt(x, y int) (string, int) {
	top := contentTop + 5 // panel border + padding + title + count + blank
	if m.searchActive {
		top++
	}
	if y < top {
		return "", -1
	}
	visible := m.dataVisible()
	panelW := m.dataPanelW()
	if x < panelW+2 {
		idx := m.teamScroll + (y - top)
		if idx < m.teamScroll+visible && idx < len(m.filteredTeamKeys()) {
			return "teams", idx
		}
	} else if x >= panelW+4 {
		idx := m.stadiumScroll + (y - top)
		if idx < m.stadiumScroll+visible && idx < len(m.filteredStadiumKeys()) {
			return "stadiums", idx
		}
	}
	return "", -1
}

// dataVisible mirrors the visible-row math in renderData.
func (m model) dataVisible() int {
	contentH := m.height - 6
	if m.searchActive {
		contentH--
	}
	if contentH < 4 {
		contentH = 4
	}
	return contentH - 4
}

// dataPanelW mirrors the panel-width math in renderData.
func (m model) dataPanelW() int {
	w := (m.width - 2 - 4) / 2
	if w < 24 {
		w = 24
	}
	return w
}

func (m model) tabAt(x int) (tab, bool) {
	names := []string{"Games", "Data", "Settings"}
	x0 := 0
	for i, n := range names {
		w := lipgloss.Width(" "+n+" ") + 6 // padding (4) + border (2)
		if x >= x0 && x < x0+w {
			return tab(i), true
		}
		x0 += w + 1 // margin right
	}
	return 0, false
}

// sidebarClick applies the filter entry at row y within the sidebar content
// (0 = first sidebar line).
func (m *model) sidebarClick(y int) {
	entries := m.sidebarEntries()
	if y < 0 || y >= len(entries) {
		return
	}
	e := entries[y]
	if e.kind != sbItem {
		return
	}
	m.filterFocus = e.focus
	switch e.focus {
	case 0:
		m.gamesFilter.GroupFilter = e.value
	case 1:
		m.gamesFilter.StageFilter = e.value
	case 2:
		m.gamesFilter.StadiumFilter = e.value
	}
	m.gamesIndex = 0
	m.gamesScroll = 0
}
