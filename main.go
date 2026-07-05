package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type tab int

const (
	gamesTab tab = iota
	dataTab
	settingsTab
)

const refreshInterval = 30 * time.Second
const sidebarW = 20

type tickMsg time.Time
type dataFetchedMsg struct{ data *WCData }
type fetchErrMsg struct{ err error }
type themeDetectedMsg struct{ theme string }

type model struct {
	data        *WCData
	styles      Styles
	width       int
	height      int
	tab         tab

	gamesFilter FilterState
	gamesIndex  int
	gamesScroll int
	detailMatch *Match

	activeSection string
	teamIndex     int
	teamScroll    int
	stadiumIndex  int
	stadiumScroll int

	timezoneOffset float64
	sidebarOpen bool
	filterFocus int
	theme       string
	autoDetectTheme bool
	standingsGroupIndex int
	standingsScroll     int

	searchActive bool
	searchQuery  string
	searchCursor int
	currentDate  string

	fetching   bool
	lastFetch  time.Time
	fetchErr   string
	err        string
}

func initialModel() model {
	data, err := loadData("data.json")
	if err != nil {
		data, err = loadData("data.json")
	}
	settings := LoadSettings()
	settingTheme := settings.Theme
	autoDetectTheme := settingTheme == ""
	if settingTheme == "" {
		settingTheme = "dark"
	}
	m := model{
		tab:         gamesTab,
		timezoneOffset: settings.TimezoneOffset,
		sidebarOpen: settings.SidebarOpen,
		theme:       settingTheme,
		filterFocus: 3,
		activeSection: "teams",
		fetching:   true,
		autoDetectTheme: autoDetectTheme,
	}
	if err != nil {
		m.err = err.Error()
	} else {
		m.data = data
		m.gamesFilter = FilterState{GroupFilter: "All", StageFilter: "All", StadiumFilter: "All"}
	}
	m.styles = NewStyles(m.theme, 80)
	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.SetWindowTitle("WC 2026"),
		tickCmd(),
		fetchCmd(),
	}
	if m.autoDetectTheme {
		cmds = append(cmds, detectThemeCmd())
	}
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func detectThemeCmd() tea.Cmd {
	return func() tea.Msg {
		if termenv.HasDarkBackground() {
			return themeDetectedMsg{"dark"}
		}
		return themeDetectedMsg{"light"}
	}
}

func fetchCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := fetchAndSave("data.json")
		if err != nil {
			return fetchErrMsg{err}
		}
		return dataFetchedMsg{data}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.theme, m.width)
		return m, nil

	case tickMsg:
		m.currentDate = time.Time(msg).Format("Mon, Jan 2")
		// Auto-fetch if any game is live
		if m.data != nil {
			for _, match := range m.data.Matches {
				if match.IsLive() {
					return m, tea.Batch(tickCmd(), fetchCmd())
				}
			}
		}
		return m, tickCmd()

	case dataFetchedMsg:
		m.data = msg.data
		m.fetching = false
		m.lastFetch = time.Now()
		m.fetchErr = ""
		m.err = ""
		return m, nil

		case fetchErrMsg:
			m.fetching = false
			m.fetchErr = msg.err.Error()
			return m, nil

		case tea.MouseMsg:
			return m.updateMouse(msg)

		case themeDetectedMsg:
			m.theme = msg.theme
			m.autoDetectTheme = false
			m.styles = NewStyles(m.theme, m.width)
			SaveSettings(Settings{TimezoneOffset: m.timezoneOffset, SidebarOpen: m.sidebarOpen, Theme: m.theme})
			return m, nil

		case tea.KeyMsg:
			// Search toggle: ctrl+space or alt+space
			if msg.String() == "ctrl+ " || msg.String() == "ctrl+space" || msg.String() == "alt+ " || msg.String() == "alt+space" {
				m.searchActive = !m.searchActive
				if !m.searchActive {
					m.searchQuery = ""
					m.searchCursor = 0
				}
				return m, nil
			}
		if m.searchActive && m.detailMatch == nil {
			switch msg.Type {
			case tea.KeyEscape:
				m.searchActive = false
				m.searchQuery = ""
				m.searchCursor = 0
				return m, nil
			case tea.KeyLeft:
				if m.searchCursor > 0 {
					m.searchCursor--
				}
				return m, nil
			case tea.KeyRight:
				if m.searchCursor < len(m.searchQuery) {
					m.searchCursor++
				}
				return m, nil
			case tea.KeyBackspace:
				if m.searchCursor > 0 {
					m.searchQuery = m.searchQuery[:m.searchCursor-1] + m.searchQuery[m.searchCursor:]
					m.searchCursor--
					m = m.clampSearchIndices()
				}
				return m, nil
			case tea.KeyDelete:
				if m.searchCursor < len(m.searchQuery) {
					m.searchQuery = m.searchQuery[:m.searchCursor] + m.searchQuery[m.searchCursor+1:]
					m = m.clampSearchIndices()
				}
				return m, nil
			case tea.KeySpace:
				m.searchQuery = m.searchQuery[:m.searchCursor] + " " + m.searchQuery[m.searchCursor:]
				m.searchCursor++
				return m, nil
			case tea.KeyRunes:
				m.searchQuery = m.searchQuery[:m.searchCursor] + string(msg.Runes) + m.searchQuery[m.searchCursor:]
				m.searchCursor += len(msg.Runes)
				m = m.clampSearchIndices()
				return m, nil
			}
			// Up, Down, Enter fall through to normal list navigation
		}

		// Auto-activate search on typing (when not in settings, not in detail view)
		if !m.searchActive && m.tab != settingsTab && m.detailMatch == nil {
			if m.shouldAutoSearch(msg) {
				m.searchActive = true
				m.searchQuery = msg.String()
				m.searchCursor = len(m.searchQuery)
				m.gamesIndex = 0
				m.teamIndex = 0
				m.stadiumIndex = 0
				return m, nil
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "1":
			m.switchTab(gamesTab)
			return m, nil
		case "2":
			m.switchTab(dataTab)
			return m, nil
		case "3":
			m.switchTab(settingsTab)
			return m, nil
		case "tab":
			if m.tab == gamesTab && m.detailMatch == nil {
				m.filterFocus = (m.filterFocus + 1) % 4
				return m, nil
			} else if m.tab == dataTab {
				if m.activeSection == "teams" {
					m.activeSection = "stadiums"
				} else {
					m.activeSection = "teams"
				}
				return m, nil
			}
		case "b":
			m.sidebarOpen = !m.sidebarOpen
			SaveSettings(Settings{TimezoneOffset: m.timezoneOffset, SidebarOpen: m.sidebarOpen, Theme: m.theme})
			return m, nil
		case "t":
			if m.theme == "dark" {
				m.theme = "light"
			} else {
				m.theme = "dark"
			}
			m.styles = NewStyles(m.theme, m.width)
			SaveSettings(Settings{TimezoneOffset: m.timezoneOffset, SidebarOpen: m.sidebarOpen, Theme: m.theme})
			return m, nil
		case "o":
			return m, m.openItem()
		case "r":
			if !m.fetching {
				m.fetching = true
				return m, fetchCmd()
			}
			return m, nil
		case "home":
			if m.tab == gamesTab {
				if m.detailMatch != nil {
					m.detailMatch = nil
				} else {
					m.gamesIndex = 0
					m.gamesScroll = 0
				}
			} else if m.tab == dataTab {
				m.teamIndex = 0
				m.teamScroll = 0
				m.stadiumIndex = 0
				m.stadiumScroll = 0
			}
			return m, nil
		case "end":
			if m.tab == gamesTab && m.detailMatch == nil {
				filtered := m.filteredGames()
				m.gamesIndex = len(filtered) - 1
				if m.gamesIndex < 0 {
					m.gamesIndex = 0
				}
				m.clampScroll()
			} else if m.tab == dataTab {
				if m.activeSection == "teams" {
					m.teamIndex = len(m.filteredTeamKeys()) - 1
					if m.teamIndex < 0 { m.teamIndex = 0 }
					contentH := m.height - 5
					if contentH < 4 { contentH = 4 }
					visible := contentH - 3
					if m.teamIndex >= m.teamScroll+visible { m.teamScroll = m.teamIndex - visible + 1 }
					if m.teamScroll < 0 { m.teamScroll = 0 }
				} else {
					m.stadiumIndex = len(m.filteredStadiumKeys()) - 1
					if m.stadiumIndex < 0 { m.stadiumIndex = 0 }
					contentH := m.height - 5
					if contentH < 4 { contentH = 4 }
					visible := contentH - 3
					if m.stadiumIndex >= m.stadiumScroll+visible { m.stadiumScroll = m.stadiumIndex - visible + 1 }
					if m.stadiumScroll < 0 { m.stadiumScroll = 0 }
				}
			}
			return m, nil
		}

		if m.detailMatch != nil {
			switch msg.String() {
			case "esc", "backspace", "left":
				m.detailMatch = nil
			}
			return m, nil
		}

		switch m.tab {
		case gamesTab:
			return m.updateGames(msg)
		case dataTab:
			return m.updateData(msg)
		case settingsTab:
			return m.updateSettings(msg)
		}
	}
	return m, nil
}

func (m *model) switchTab(t tab) {
	if m.tab == t {
		return
	}
	m.tab = t
	m.detailMatch = nil
	m.filterFocus = 3
	m.gamesIndex = 0
	m.gamesScroll = 0
	m.teamIndex = 0
	m.teamScroll = 0
	m.stadiumIndex = 0
	m.stadiumScroll = 0
	m.standingsGroupIndex = 0
	m.standingsScroll = 0
	m.activeSection = "teams"
	m.searchActive = false
	m.searchQuery = ""
	m.searchCursor = 0
}

func (m *model) clampScroll() {
	filtered := m.filteredGames()
	if m.gamesIndex < 0 {
		m.gamesIndex = 0
	}
	if m.gamesIndex >= len(filtered) {
		m.gamesIndex = len(filtered) - 1
	}
	if m.gamesIndex < 0 {
		m.gamesIndex = 0
	}
	visible := m.height - 8
	if visible < 1 {
		visible = 1
	}
	m.gamesScroll = smartScroll(m.gamesIndex, len(filtered), visible)
}


func (m model) openItem() tea.Cmd {
	var urlStr string
	switch m.tab {
	case gamesTab:
		var match *Match
		if m.detailMatch != nil {
			match = m.detailMatch
		} else {
			filtered := m.filteredGames()
			if m.gamesIndex >= 0 && m.gamesIndex < len(filtered) {
				match = &filtered[m.gamesIndex]
			}
		}
		if match != nil {
			q := url.QueryEscape(fmt.Sprintf("%s vs %s World Cup 2026", match.Teams.Home.Name, match.Teams.Away.Name))
			urlStr = "https://www.google.com/search?q=" + q
		}
	case dataTab:
		if m.activeSection == "teams" {
			keys := m.teamKeys()
			if m.teamIndex >= 0 && m.teamIndex < len(keys) {
				t := m.data.Teams[keys[m.teamIndex]]
				q := url.QueryEscape(fmt.Sprintf("%s World Cup 2026 football", t.Name))
				urlStr = "https://www.google.com/search?q=" + q
			}
		} else {
			keys := m.stadiumKeys()
			if m.stadiumIndex >= 0 && m.stadiumIndex < len(keys) {
				s := m.data.Stadiums[keys[m.stadiumIndex]]
				q := url.QueryEscape(fmt.Sprintf("%s stadium World Cup 2026", s.Name))
				urlStr = "https://www.google.com/search?q=" + q
			}
		}
	}
	if urlStr == "" {
		return nil
	}
	return tea.Cmd(func() tea.Msg {
		exec.Command("open", urlStr).Start()
		return nil
	})
}

func (m model) updateGames(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredGames()

	switch msg.String() {
	case "up":
		if m.filterFocus == 3 {
			if m.gamesIndex > 0 {
				m.gamesIndex--
			}
		} else {
			m.cycleFilter(-1)
		}
	case "down":
		if m.filterFocus == 3 {
			if m.gamesIndex < len(filtered)-1 {
				m.gamesIndex++
			}
		} else {
			m.cycleFilter(1)
		}
	case "enter", "right":
		if m.filterFocus == 3 && len(filtered) > 0 {
			m.detailMatch = &filtered[m.gamesIndex]
		}
	case "g":
		m.filterFocus = 0
	case "f":
		m.filterFocus = 3
	case "pgup":
		m.gamesIndex -= m.height - 5
		if m.gamesIndex < 0 {
			m.gamesIndex = 0
		}
	case "pgdown":
		m.gamesIndex += m.height - 5
		if m.gamesIndex >= len(filtered) {
			m.gamesIndex = len(filtered) - 1
		}
	}
	m.clampScroll()
	return m, nil
}

func (m *model) cycleFilter(dir int) {
	var opts []string
	var cur *string
	switch m.filterFocus {
	case 0:
		opts = m.data.AllGroups()
		cur = &m.gamesFilter.GroupFilter
	case 1:
		opts = AllStages()
		cur = &m.gamesFilter.StageFilter
	case 2:
		opts = m.data.AllStadiums()
		cur = &m.gamesFilter.StadiumFilter
	default:
		return
	}
	for i, v := range opts {
		if v == *cur {
			idx := i + dir
			if idx < 0 {
				idx = len(opts) - 1
			} else if idx >= len(opts) {
				idx = 0
			}
			*cur = opts[idx]
			m.gamesIndex = 0
			m.gamesScroll = 0
			return
		}
	}
}

func (m model) teamKeys() []string {
	if m.data == nil {
		return nil
	}
	var out []string
	for k := range m.data.Teams {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m.data.Teams[out[i]].Group != m.data.Teams[out[j]].Group {
			return m.data.Teams[out[i]].Group < m.data.Teams[out[j]].Group
		}
		return m.data.Teams[out[i]].Name < m.data.Teams[out[j]].Name
	})
	return out
}

func (m model) stadiumKeys() []string {
	if m.data == nil {
		return nil
	}
	var out []string
	for k := range m.data.Stadiums {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func smartScroll(selected, total, visible int) int {
	if total <= visible {
		return 0
	}
	// Keep selected at 1/3 from top — like a real scrollable list
	offset := selected - visible/3
	if offset < 0 {
		return 0
	}
	max := total - visible
	if offset > max {
		return max
	}
	return offset
}

func (m model) updateData(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	teamKeys := m.filteredTeamKeys()
	stadiumKeys := m.filteredStadiumKeys()
	standings := ComputeStandings(m.data)
	groupKeys := m.filteredGroupKeys(standings)
	contentH := m.height - 6
	if contentH < 4 {
		contentH = 4
	}
	visible := contentH - 4

	standingsLines := m.standingsVisibleLines(standings, groupKeys)

	switch msg.String() {
	case "up":
		if m.activeSection == "teams" && m.teamIndex > 0 {
			m.teamIndex--
		} else if m.activeSection == "stadiums" && m.stadiumIndex > 0 {
			m.stadiumIndex--
		} else if m.activeSection == "standings" && m.standingsGroupIndex > 0 {
			m.standingsGroupIndex--
			m.standingsScroll = 0
		}
	case "down":
		if m.activeSection == "teams" && m.teamIndex < len(teamKeys)-1 {
			m.teamIndex++
		} else if m.activeSection == "stadiums" && m.stadiumIndex < len(stadiumKeys)-1 {
			m.stadiumIndex++
		} else if m.activeSection == "standings" && m.standingsGroupIndex < len(groupKeys)-1 {
			m.standingsGroupIndex++
			m.standingsScroll = 0
		}
	case "tab":
		if m.activeSection == "teams" {
			m.activeSection = "stadiums"
		} else if m.activeSection == "stadiums" {
			m.activeSection = "standings"
		} else {
			m.activeSection = "teams"
		}
	case "enter":
		return m, m.openItem()
	case "pgup":
		if m.activeSection == "teams" {
			m.teamIndex -= visible
			if m.teamIndex < 0 { m.teamIndex = 0 }
		} else if m.activeSection == "stadiums" {
			m.stadiumIndex -= visible
			if m.stadiumIndex < 0 { m.stadiumIndex = 0 }
		} else if m.activeSection == "standings" {
			m.standingsScroll -= visible
			if m.standingsScroll < 0 { m.standingsScroll = 0 }
		}
	case "pgdown":
		if m.activeSection == "teams" {
			m.teamIndex += visible
			if m.teamIndex >= len(teamKeys) { m.teamIndex = len(teamKeys) - 1 }
		} else if m.activeSection == "stadiums" {
			m.stadiumIndex += visible
			if m.stadiumIndex >= len(stadiumKeys) { m.stadiumIndex = len(stadiumKeys) - 1 }
		} else if m.activeSection == "standings" {
			m.standingsScroll += visible
			if m.standingsScroll >= standingsLines { m.standingsScroll = standingsLines - 1 }
			if m.standingsScroll < 0 { m.standingsScroll = 0 }
		}
	}

	// clamp scrolls
	if m.teamIndex < 0 { m.teamIndex = 0 }
	if m.teamIndex >= len(teamKeys) { m.teamIndex = len(teamKeys) - 1 }
	if m.teamIndex < 0 { m.teamIndex = 0 }
	m.teamScroll = smartScroll(m.teamIndex, len(teamKeys), visible)

	if m.stadiumIndex < 0 { m.stadiumIndex = 0 }
	if m.stadiumIndex >= len(stadiumKeys) { m.stadiumIndex = len(stadiumKeys) - 1 }
	if m.stadiumIndex < 0 { m.stadiumIndex = 0 }
	m.stadiumScroll = smartScroll(m.stadiumIndex, len(stadiumKeys), visible)

	if m.standingsGroupIndex < 0 { m.standingsGroupIndex = 0 }
	if m.standingsGroupIndex >= len(groupKeys) { m.standingsGroupIndex = len(groupKeys) - 1 }
	if m.standingsGroupIndex < 0 { m.standingsGroupIndex = 0 }
	if m.standingsScroll < 0 { m.standingsScroll = 0 }
	if m.standingsScroll >= standingsLines { m.standingsScroll = standingsLines - 1 }
	if m.standingsScroll < 0 { m.standingsScroll = 0 }

	return m, nil
}

func (m model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "[", "]":
		step := 0.5
		if msg.String() == "]" {
			m.timezoneOffset += step
		} else {
			m.timezoneOffset -= step
		}
		if m.timezoneOffset < -12 { m.timezoneOffset = -12 }
		if m.timezoneOffset > 14 { m.timezoneOffset = 14 }
			SaveSettings(Settings{TimezoneOffset: m.timezoneOffset, SidebarOpen: m.sidebarOpen, Theme: m.theme})
	}
	return m, nil
}

func (m model) filteredGames() []Match {
	if m.data == nil {
		return nil
	}
	var out []Match
	for _, match := range m.data.Matches {
		if !m.gamesFilter.matches(match) {
			continue
		}
		if m.searchActive && m.searchQuery != "" {
			if !matchSearch(m.searchQuery, match) {
				continue
			}
		}
		out = append(out, match)
	}
	return out
}

// matchSearch checks if a match matches the search query.
// Supports <team1>><team2> for matchup searches (e.g. "ger>france").
// Multiple space-separated terms are OR'd (any term matches any field).
func matchSearch(query string, match Match) bool {
	query = strings.ToLower(query)
	if sep := strings.Index(query, "<>"); sep != -1 {
		left := strings.TrimSpace(query[:sep])
		right := strings.TrimSpace(query[sep+2:])
		home := strings.ToLower(match.Teams.Home.Name)
		away := strings.ToLower(match.Teams.Away.Name)
		if left != "" && right != "" {
			if (strings.Contains(home, left) && strings.Contains(away, right)) ||
				(strings.Contains(away, left) && strings.Contains(home, right)) {
				return true
			}
			return false
		}
		if left != "" && (strings.Contains(home, left) || strings.Contains(away, left)) {
			return true
		}
		if right != "" && (strings.Contains(home, right) || strings.Contains(away, right)) {
			return true
		}
		return false
	}
	for _, term := range strings.Fields(query) {
		if term == "" {
			continue
		}
		for _, field := range []string{
			strings.ToLower(match.Teams.Home.Name),
			strings.ToLower(match.Teams.Away.Name),
			strings.ToLower(match.Venue.Name),
		} {
			if strings.Contains(field, term) {
				return true
			}
		}
	}
	return false
}

func (m model) filteredGroupKeys(standings map[string][]Standing) []string {
	var out []string
	for k := range standings {
		out = append(out, k)
	}
	sort.Strings(out)
	if !m.searchActive || m.searchQuery == "" {
		return out
	}
	terms := searchTerms(m.searchQuery)
	var filtered []string
	for _, g := range out {
		for _, s := range standings[g] {
			if anyTermMatches(terms, strings.ToLower(s.Team.Name), strings.ToLower(g)) {
				filtered = append(filtered, g)
				break
			}
		}
	}
	return filtered
}

func (m model) standingsVisibleLines(standings map[string][]Standing, groupKeys []string) int {
	lines := 0
	for _, g := range groupKeys {
		if len(standings[g]) == 0 {
			continue
		}
		lines += 2 // header + column header
		lines += len(standings[g]) + 1 // rows + blank separator
	}
	return lines
}

func (m model) filteredTeamKeys() []string {
	keys := m.teamKeys()
	if !m.searchActive || m.searchQuery == "" {
		return keys
	}
	terms := searchTerms(m.searchQuery)
	var out []string
	for _, k := range keys {
		if anyTermMatches(terms, strings.ToLower(m.data.Teams[k].Name)) {
			out = append(out, k)
		}
	}
	return out
}

func (m model) filteredStadiumKeys() []string {
	keys := m.stadiumKeys()
	if !m.searchActive || m.searchQuery == "" {
		return keys
	}
	terms := searchTerms(m.searchQuery)
	var out []string
	for _, k := range keys {
		if anyTermMatches(terms, strings.ToLower(m.data.Stadiums[k].Name)) {
			out = append(out, k)
		}
	}
	return out
}

func searchTerms(query string) []string {
	var terms []string
	for _, t := range strings.Fields(query) {
		if t != "" {
			terms = append(terms, strings.ToLower(t))
		}
	}
	return terms
}

func anyTermMatches(terms []string, fields ...string) bool {
	for _, term := range terms {
		for _, field := range fields {
			if strings.Contains(field, term) {
				return true
			}
		}
	}
	return false
}

func (m model) shouldAutoSearch(msg tea.KeyMsg) bool {
	s := msg.String()
	if len(s) != 1 {
		return false
	}
	c := s[0]
	switch c {
	case 'q', 'r', 'b', 'o', 't', '1', '2', '3':
		return false
	}
	if c < ' ' || c > '~' {
		return false
	}
	return true
}

func (m model) clampSearchIndices() model {
	if m.tab == gamesTab {
		filtered := m.filteredGames()
		if m.gamesIndex >= len(filtered) {
			m.gamesIndex = len(filtered) - 1
			if m.gamesIndex < 0 {
				m.gamesIndex = 0
			}
		}
	} else if m.tab == dataTab {
		if m.activeSection == "teams" {
			keys := m.filteredTeamKeys()
			if m.teamIndex >= len(keys) {
				m.teamIndex = len(keys) - 1
				if m.teamIndex < 0 {
					m.teamIndex = 0
				}
			}
		} else {
			keys := m.filteredStadiumKeys()
			if m.stadiumIndex >= len(keys) {
				m.stadiumIndex = len(keys) - 1
				if m.stadiumIndex < 0 {
					m.stadiumIndex = 0
				}
			}
		}
	}
	return m
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.err != "" {
		return m.styles.Dim.Render(fmt.Sprintf("Error: %s\n\nPress q to quit.", m.err))
	}
	if m.data == nil {
		return m.styles.Dim.Render("Loading...")
	}

	var lines []string

	// Tabs
	lines = append(lines, m.renderTabs())
	lines = append(lines, "")

	// Content
	contentLines := m.renderContent()
	lines = append(lines, contentLines...)

	// Help
	lines = append(lines, "")
	lines = append(lines, m.styles.Dim.Render("1-3 tabs • ↑↓/wheel navigate • enter/click detail • o open • r refresh • tab panel • esc back • ctrl+space search • teamA<>teamB • b sidebar • t theme • q quit"))

	// Pad to exact height
	rendered := strings.Join(lines, "\n")
	renderedLines := strings.Split(rendered, "\n")
	for len(renderedLines) < m.height {
		renderedLines = append(renderedLines, "")
	}
	if len(renderedLines) > m.height {
		renderedLines = renderedLines[:m.height]
	}

	return strings.Join(renderedLines, "\n")
}

func (m model) renderTabs() string {
	names := []string{"Games", "Data", "Settings"}
	var parts []string
	for i, n := range names {
		label := fmt.Sprintf(" %s ", n)
		if tab(i) == m.tab {
			parts = append(parts, m.styles.TabActive.Render(label))
		} else {
			parts = append(parts, m.styles.Tab.Render(label))
		}
	}
	tabs := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	if m.currentDate != "" {
		dateStr := m.styles.Dim.Render(m.currentDate)
		avail := m.width - 4 - lipgloss.Width(tabs) - lipgloss.Width(dateStr)
		if avail > 0 {
			return tabs + strings.Repeat(" ", avail) + dateStr
		}
	}
	return tabs
}

func (m model) renderContent() []string {
	switch m.tab {
	case gamesTab:
		return m.renderGames()
	case dataTab:
		return m.renderData()
	case settingsTab:
		return m.renderSettings()
	}
	return nil
}

func (m model) renderSearchBar() string {
	if !m.searchActive {
		return ""
	}
	cursor := m.searchCursor
	if cursor > len(m.searchQuery) {
		cursor = len(m.searchQuery)
	}
	if cursor < 0 {
		cursor = 0
	}

	var display string
	if cursor == len(m.searchQuery) {
		display = m.searchQuery + "|"
	} else {
		display = m.searchQuery[:cursor] + "|" + m.searchQuery[cursor:]
	}

	count := 0
		if m.tab == gamesTab {
			count = len(m.filteredGames())
		} else if m.tab == dataTab {
			if m.activeSection == "teams" {
				count = len(m.filteredTeamKeys())
			} else if m.activeSection == "stadiums" {
				count = len(m.filteredStadiumKeys())
			} else {
				count = len(m.filteredGroupKeys(ComputeStandings(m.data)))
			}
		}

	return m.styles.SearchBar.Render(fmt.Sprintf("\u003e %s (%d)", display, count))
}

func (m model) renderGames() []string {
	filtered := m.filteredGames()
	if m.detailMatch != nil {
		return m.renderDetail(m.detailMatch)
	}

	contentH := m.height - 6
	if contentH < 1 {
		contentH = 1
	}

	// Search bar
	var searchBar string
	if m.searchActive {
		searchBar = m.renderSearchBar()
		contentH--
	}

	// Sidebar
	var sidebarLines []string
	if m.sidebarOpen {
		sidebarLines = m.renderGamesSidebar()
	}
	for len(sidebarLines) < contentH {
		sidebarLines = append(sidebarLines, "")
	}
	if len(sidebarLines) > contentH {
		sidebarLines = sidebarLines[:contentH]
	}

	// Main list
	var mainLines []string
	if searchBar != "" {
		mainLines = append(mainLines, searchBar)
	}
	mainLines = append(mainLines, m.styles.Dim.Render(fmt.Sprintf("%d matches", len(filtered))))

	closureLines := 2 // header + bottom closure
	if searchBar != "" {
		closureLines = 3 // search bar + header + bottom closure
	}
	end := m.gamesScroll + contentH - closureLines
	if end > len(filtered) {
		end = len(filtered)
	}
	for i := m.gamesScroll; i < end && i >= 0; i++ {
		mainLines = append(mainLines, m.renderMatchLine(filtered[i], i == m.gamesIndex))
	}
	// Bottom closure
	mainLines = append(mainLines, m.styles.Dim.Render("─"))
	for len(mainLines) < contentH {
		mainLines = append(mainLines, "")
	}
	if len(mainLines) > contentH {
		mainLines = mainLines[:contentH]
	}

	if m.sidebarOpen {
		sidebarBlock := m.styles.Sidebar.Render(strings.Join(sidebarLines, "\n"))
		mainBlock := m.styles.Main.Render(strings.Join(mainLines, "\n"))
		combined := lipgloss.JoinHorizontal(lipgloss.Top, sidebarBlock, "  ", mainBlock)
		return strings.Split(combined, "\n")
	}
	mainBlock := m.styles.Main.Render(strings.Join(mainLines, "\n"))
	return strings.Split(mainBlock, "\n")
}

type sbKind int

const (
	sbHeader sbKind = iota
	sbItem
	sbSep
	sbBlank
)

// sbEntry is one sidebar line; the same list drives rendering and mouse
// hit-testing, so the two can't drift apart.
type sbEntry struct {
	kind  sbKind
	label string
	focus int
	value string
}

func (m model) sidebarEntries() []sbEntry {
	const maxVisible = 6 // 1 pinned "All" + 5 scrollable
	var out []sbEntry

	section := func(title string, focus int, opts []string, cur string) {
		if len(out) > 0 {
			out = append(out, sbEntry{kind: sbBlank})
		}
		out = append(out, sbEntry{kind: sbHeader, label: title})
		// Pinned "All"
		out = append(out, sbEntry{kind: sbItem, label: "All", focus: focus, value: "All"})
		// Scrollable rest (skip "All")
		var rest []string
		for _, o := range opts {
			if o != "All" {
				rest = append(rest, o)
			}
		}
		selIdx := 0
		for i, o := range rest {
			if o == cur {
				selIdx = i
				break
			}
		}
		scroll := smartScroll(selIdx, len(rest), maxVisible-1)
		end := scroll + maxVisible - 1
		if end > len(rest) {
			end = len(rest)
		}
		for i := scroll; i < end; i++ {
			label := rest[i]
			if len(label) > 14 {
				label = label[:14] + ".."
			}
			out = append(out, sbEntry{kind: sbItem, label: label, focus: focus, value: rest[i]})
		}
		out = append(out, sbEntry{kind: sbSep})
	}

	section("GROUP", 0, m.data.AllGroups(), m.gamesFilter.GroupFilter)
	section("STAGE", 1, AllStages(), m.gamesFilter.StageFilter)
	section("STADIUM", 2, m.data.AllStadiums(), m.gamesFilter.StadiumFilter)
	return out
}

func (m model) currentFilter(focus int) string {
	switch focus {
	case 0:
		return m.gamesFilter.GroupFilter
	case 1:
		return m.gamesFilter.StageFilter
	case 2:
		return m.gamesFilter.StadiumFilter
	}
	return ""
}

func (m model) renderGamesSidebar() []string {
	var lines []string
	for _, e := range m.sidebarEntries() {
		switch e.kind {
		case sbHeader:
			lines = append(lines, m.styles.Accent.Bold(true).Render(e.label))
		case sbSep:
			lines = append(lines, m.styles.Dim.Render("─"))
		case sbBlank:
			lines = append(lines, "")
		case sbItem:
			selected := m.currentFilter(e.focus) == e.value
			if e.focus == m.filterFocus && selected {
				lines = append(lines, m.styles.Badge.Render(" "+e.label+" "))
			} else if selected {
				lines = append(lines, m.styles.Selected.Render(" "+e.label+" "))
			} else {
				lines = append(lines, m.styles.Dim.Render(" "+e.label+" "))
			}
		}
	}
	return lines
}

func (m model) renderMatchLine(match Match, selected bool) string {
	status := match.ElapsedDisplay()
	statusStyle := m.styles.Dim
	if match.IsLive() || match.Status.Elapsed == "HT" {
		status = "● " + status
		statusStyle = m.styles.Accent
	} else if status == "" && !match.Status.Finished {
		status = match.FormatTime(m.timezoneOffset)
	}

	badge := m.styles.Badge.Render(" " + match.Stage.MatchdayString() + " ")

	scoreStr := fmt.Sprintf("%d-%d", match.Score.Home, match.Score.Away)
	scoreStyle := m.styles.Score
	if !match.Status.Finished && match.Score.Home == 0 && match.Score.Away == 0 && match.Status.Elapsed == "" {
		scoreStr = "vs"
		scoreStyle = m.styles.Dim
	}
	if !match.Status.Finished && !match.IsLive() {
		scoreStyle = m.styles.Dim
	}

	left := badge + " " + statusStyle.Render(status)
	center := fmt.Sprintf(" %-12s %s %-12s ", truncate(match.Teams.Home.Name, 12), scoreStyle.Render(scoreStr), truncate(match.Teams.Away.Name, 12))
	meta := m.styles.Dim.Render(fmt.Sprintf(" %s %s • %s ", match.FormatDate(m.timezoneOffset), match.FormatTime(m.timezoneOffset), truncate(match.Venue.Name, 20)))

	row := left + center + meta

	if selected {
		return m.styles.Selected.Render(row)
	}
	return m.styles.ListItem.Render(row)
}

func truncate(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	for lipgloss.Width(s) > max-1 {
		if len(s) <= 3 {
			break
		}
		s = s[:len(s)-1]
	}
	return s + "…"
}

func (m model) renderDetail(match *Match) []string {
	var lines []string

	// Search indicator when active
	if m.searchActive && m.searchQuery != "" {
		lines = append(lines, m.styles.Dim.Render(fmt.Sprintf("  🔍 %s", m.searchQuery)))
		lines = append(lines, "")
	}

	scoreStr := fmt.Sprintf("%d - %d", match.Score.Home, match.Score.Away)
	if !match.Status.Finished && match.Score.Home == 0 && match.Score.Away == 0 && match.Status.Elapsed == "" {
		scoreStr = "vs"
	}

	lines = append(lines, "")

	// Team header with flags, codes, and groups
	homeFlag := iso2ToFlag(match.Teams.Home.ISO2)
	awayFlag := iso2ToFlag(match.Teams.Away.ISO2)
	homeLabel := fmt.Sprintf("%s  %s  (%s)", homeFlag, match.Teams.Home.Name, match.Teams.Home.FIFACode)
	awayLabel := fmt.Sprintf("(%s)  %s  %s", match.Teams.Away.FIFACode, match.Teams.Away.Name, awayFlag)
	homeName := lipgloss.NewStyle().Bold(true).Width(24).Align(lipgloss.Right).Render(homeLabel)
	awayName := lipgloss.NewStyle().Bold(true).Width(24).Align(lipgloss.Left).Render(awayLabel)
	score := lipgloss.NewStyle().Bold(true).Padding(0, 2).Render(scoreStr)
	lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Center, homeName, score, awayName))

	// Group badges
	if match.Teams.Home.Group != "" || match.Teams.Away.Group != "" {
		homeGrp := m.styles.Badge.Render(" " + match.Teams.Home.Group + " ")
		awayGrp := m.styles.Badge.Render(" " + match.Teams.Away.Group + " ")
		grpLine := lipgloss.JoinHorizontal(lipgloss.Center,
			lipgloss.NewStyle().Width(24).Align(lipgloss.Right).Render(homeGrp),
			lipgloss.NewStyle().Width(6).Render(""),
			lipgloss.NewStyle().Width(24).Align(lipgloss.Left).Render(awayGrp),
		)
		lines = append(lines, grpLine)
	}
	lines = append(lines, "")

	// MATCH INFO section
	lines = append(lines, m.styles.Accent.Bold(true).Render("MATCH INFO"))
	infoLines := []string{
		fmt.Sprintf("  Date:     %s", match.FormatDate(m.timezoneOffset)),
		fmt.Sprintf("  Time:     %s", match.FormatTime(m.timezoneOffset)),
	}
	stageStr := stageDisplayName(match.Stage.Type)
	if match.Stage.Group != "" {
		stageStr += fmt.Sprintf(" — Group %s", match.Stage.Group)
	}
	if match.Stage.Matchday != nil {
		stageStr += fmt.Sprintf(" — Matchday %v", match.Stage.Matchday)
	}
	infoLines = append(infoLines, fmt.Sprintf("  Stage:    %s", stageStr))

	statusStr := match.ElapsedDisplay()
	if statusStr == "" {
		statusStr = "Upcoming"
	} else if match.Status.Finished {
		statusStr = "Finished"
	}
	infoLines = append(infoLines, fmt.Sprintf("  Status:   %s", statusStr))
	lines = append(lines, infoLines...)
	lines = append(lines, "")

	// Score/Events section
	if len(match.AllEvents()) > 0 {
		lines = append(lines, m.styles.Accent.Bold(true).Render("SCORING"))
		events := match.AllEvents()
		for _, e := range events {
			marker := "⚽"
			if e.OwnGoal {
				marker = "🥅 OG"
			} else if e.Penalty {
				marker = "⚽ (P)"
			}
			sideName := match.Teams.Home.Name
			if e.Raw == "away" {
				sideName = match.Teams.Away.Name
			}
			lines = append(lines, fmt.Sprintf("  %-6s %s  %s  %s", e.Minute, marker, sideName, e.Player))
		}
		lines = append(lines, "")
	}

	// Venue details
	lines = append(lines, m.styles.Accent.Bold(true).Render("VENUE"))
	venueLines := []string{fmt.Sprintf("  %s", match.Venue.Name)}
	if match.Venue.FIFAName != "" && match.Venue.FIFAName != match.Venue.Name {
		venueLines = append(venueLines, fmt.Sprintf("  FIFA Name: %s", match.Venue.FIFAName))
	}
	if match.Venue.City != "" {
		cityLine := fmt.Sprintf("  %s", match.Venue.City)
		if match.Venue.Country != "" {
			cityLine += fmt.Sprintf(", %s", match.Venue.Country)
		}
		venueLines = append(venueLines, cityLine)
	}
	if match.Venue.Region != "" {
		venueLines = append(venueLines, fmt.Sprintf("  Region: %s", match.Venue.Region))
	}
	if match.Venue.Capacity > 0 {
		venueLines = append(venueLines, fmt.Sprintf("  Capacity: %d", match.Venue.Capacity))
	}
	lines = append(lines, venueLines...)
	lines = append(lines, "")
	lines = append(lines, m.styles.Dim.Render("Press o to open match page • esc back"))

	return lines
}

func stageDisplayName(t string) string {
	switch t {
	case "group":
		return "Group Stage"
	case "r32":
		return "Round of 32"
	case "r16":
		return "Round of 16"
	case "qf":
		return "Quarter-Finals"
	case "sf":
		return "Semi-Finals"
	case "final":
		return "Final"
	case "third":
		return "3rd Place Playoff"
	default:
		return strings.ToUpper(t)
	}
}

func (m model) renderData() []string {
	contentH := m.height - 6
	if contentH < 4 {
		contentH = 4
	}

	var searchBar string
	if m.searchActive {
		searchBar = m.renderSearchBar()
		contentH--
	}

	if m.activeSection == "standings" {
		lines := m.renderStandingsPanel(contentH)
		if searchBar != "" {
			lines = append([]string{searchBar}, lines...)
		}
		return lines
	}

	visible := contentH - 4
	gap := 2
	panelW := (m.width - gap - 4) / 2
	if panelW < 24 {
		panelW = 24
	}
	rowW := panelW - 6

	// Left panel: Teams
	teamKeys := m.filteredTeamKeys()
	var leftLines []string
	leftLines = append(leftLines, m.styles.Accent.Bold(true).Render("  TEAMS"))
	leftLines = append(leftLines, m.styles.Dim.Render(fmt.Sprintf("  %d teams", len(teamKeys))))
	leftLines = append(leftLines, "")
	end := m.teamScroll + visible
	if end > len(teamKeys) { end = len(teamKeys) }
	for i := m.teamScroll; i < end && i >= 0; i++ {
		t := m.data.Teams[teamKeys[i]]
		code := m.styles.Badge.Render(" " + t.FIFACode + " ")
		name := truncate(t.Name, rowW-14)
		grp := m.styles.Dim.Render(fmt.Sprintf("Grp %s", t.Group))
		row := lipgloss.JoinHorizontal(lipgloss.Left, code, " "+name+" ", grp)
		if i == m.teamIndex && m.activeSection == "teams" {
			leftLines = append(leftLines, m.styles.Selected.Render(row))
		} else {
			leftLines = append(leftLines, m.styles.ListItem.Render(row))
		}
	}
	leftLines = append(leftLines, m.styles.Dim.Render("─"))
	for len(leftLines) < contentH { leftLines = append(leftLines, "") }
	if len(leftLines) > contentH { leftLines = leftLines[:contentH] }
	leftBlock := m.styles.Main.Width(panelW).Render(strings.Join(leftLines, "\n"))

	// Right panel: Stadiums
	stadiumKeys := m.filteredStadiumKeys()
	var rightLines []string
	rightLines = append(rightLines, m.styles.Accent.Bold(true).Render("  STADIUMS"))
	rightLines = append(rightLines, m.styles.Dim.Render(fmt.Sprintf("  %d venues", len(stadiumKeys))))
	rightLines = append(rightLines, "")
	end2 := m.stadiumScroll + visible
	if end2 > len(stadiumKeys) { end2 = len(stadiumKeys) }
	for i := m.stadiumScroll; i < end2 && i >= 0; i++ {
		s := m.data.Stadiums[stadiumKeys[i]]
		name := truncate(s.Name, rowW-20)
		city := m.styles.Dim.Render(truncate(s.City, 12))
		cap := m.styles.Dim.Render(fmt.Sprintf("%d", s.Capacity))
		row := lipgloss.JoinHorizontal(lipgloss.Left, " "+name+"  ", city, " ", cap)
		if i == m.stadiumIndex && m.activeSection == "stadiums" {
			rightLines = append(rightLines, m.styles.Selected.Render(row))
		} else {
			rightLines = append(rightLines, m.styles.ListItem.Render(row))
		}
	}
	rightLines = append(rightLines, m.styles.Dim.Render("─"))
	for len(rightLines) < contentH { rightLines = append(rightLines, "") }
	if len(rightLines) > contentH { rightLines = rightLines[:contentH] }
	rightBlock := m.styles.Main.Width(panelW).Render(strings.Join(rightLines, "\n"))

	combined := lipgloss.JoinHorizontal(lipgloss.Top, leftBlock, strings.Repeat(" ", gap), rightBlock)
	lines := strings.Split(combined, "\n")
	if searchBar != "" {
		lines = append([]string{searchBar}, lines...)
	}
	return lines
}

func (m model) renderStandingsPanel(contentH int) []string {
	standings := ComputeStandings(m.data)
	groupKeys := m.filteredGroupKeys(standings)
	var allLines []string
	allLines = append(allLines, m.styles.Accent.Bold(true).Render("  STANDINGS"))
	allLines = append(allLines, m.styles.Dim.Render(fmt.Sprintf("  %d groups", len(groupKeys))))
	allLines = append(allLines, "")

	for _, g := range groupKeys {
		grp := standings[g]
		if len(grp) == 0 {
			continue
		}
		header := m.styles.Accent.Bold(true).Render(fmt.Sprintf("  GROUP %s", g))
		allLines = append(allLines, header)
		allLines = append(allLines, m.styles.Dim.Render(fmt.Sprintf("  %-16s P  W  D  L  GF  GA  GD  Pts", "Team")))
		for i, s := range grp {
			flag := iso2ToFlag(s.Team.ISO2)
			name := truncate(s.Team.Name, 14)
			row := fmt.Sprintf("  %s %-14s %2d %2d %2d %2d  %2d  %2d  %3d  %3d", flag, name, s.Played, s.Won, s.Drawn, s.Lost, s.GF, s.GA, s.GD, s.Points)
			if i == m.standingsGroupIndex && m.activeSection == "standings" {
				allLines = append(allLines, m.styles.Selected.Render(row))
			} else {
				allLines = append(allLines, m.styles.ListItem.Render(row))
			}
		}
		allLines = append(allLines, "")
	}
	allLines = append(allLines, m.styles.Dim.Render("─"))
	for len(allLines) < contentH {
		allLines = append(allLines, "")
	}
	if len(allLines) > contentH {
		allLines = allLines[m.standingsScroll:]
		if len(allLines) > contentH {
			allLines = allLines[:contentH]
		}
	}
	block := m.styles.Main.Render(strings.Join(allLines, "\n"))
	return strings.Split(block, "\n")
}
func (m model) renderSettings() []string {
	var lines []string

	lines = append(lines, m.styles.Header.Render("Settings"))
	lines = append(lines, m.styles.Dim.Render(strings.Repeat("─", 40)))
	lines = append(lines, "")

		lines = append(lines, fmt.Sprintf("  Sidebar:  %v  (toggle with b)", m.sidebarOpen))
		lines = append(lines, fmt.Sprintf("  Theme:    %s  (toggle with t)", m.theme))
	tzStr := fmt.Sprintf("UTC%+.1f", m.timezoneOffset)
	if m.timezoneOffset == 0 { tzStr = "UTC" }
	lines = append(lines, fmt.Sprintf("  Timezone: %s  (adjust with [ ] )", tzStr))
	_, sysOffset := time.Now().Zone()
	sysOffsetH := float64(sysOffset) / 3600
	sysTzStr := fmt.Sprintf("UTC%+.1f", sysOffsetH)
	if sysOffsetH == 0 { sysTzStr = "UTC" }
	lines = append(lines, m.styles.Dim.Render(fmt.Sprintf("  System TZ: %s (set to match your clock)", sysTzStr)))
	lines = append(lines, "")
	lines = append(lines, m.styles.Dim.Render("Search:       ctrl+space / alt+space to toggle"))
	lines = append(lines, m.styles.Dim.Render("              type to filter, ←→ move cursor, esc clear"))
	lines = append(lines, m.styles.Dim.Render("              teamA<>teamB for specific matchups"))
	lines = append(lines, "")
	lines = append(lines, m.styles.Dim.Render("Fetched:      "+m.data.Meta.FetchedAt))
	lines = append(lines, m.styles.Dim.Render(fmt.Sprintf("Matches:      %d", m.data.Meta.MatchCount)))
	if m.fetching {
		lines = append(lines, m.styles.Accent.Render("Fetching..."))
	}
	if m.fetchErr != "" {
		lines = append(lines, m.styles.Dim.Render("Last fetch error: "+m.fetchErr))
	}
	if !m.lastFetch.IsZero() {
		lines = append(lines, m.styles.Dim.Render("Last updated: "+m.lastFetch.Format("15:04:05")))
	}

	return lines
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
