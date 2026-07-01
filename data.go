package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// iso2ToFlag converts a two-letter ISO country code to a Unicode flag emoji.
func iso2ToFlag(iso2 string) string {
	if len(iso2) != 2 {
		return "🏳"
	}
	code := strings.ToUpper(iso2)
	var runes []rune
	for _, r := range code {
		runes = append(runes, r+127397)
	}
	return string(runes)
}
type IDs struct {
	ID  string `json:"id"`
	ID_ string `json:"_id"`
}

// Stage holds tournament stage info.
type Stage struct {
	Type     string      `json:"type"`
	Group    string      `json:"group"`
	Matchday interface{} `json:"matchday"`
}

// Datetime holds match timing.
type Datetime struct {
	Local string `json:"local"`
	ISO   string `json:"iso"`
}

// Status holds match status.
type Status struct {
	Finished bool   `json:"finished"`
	Elapsed  string `json:"elapsed"`
}

// TeamRef is a team reference with embedded data.
type TeamRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FIFACode string `json:"fifa_code,omitempty"`
	ISO2     string `json:"iso2,omitempty"`
	Flag     string `json:"flag,omitempty"`
	Group    string `json:"group,omitempty"`
}

// Score holds the score.
type Score struct {
	Home int `json:"home"`
	Away int `json:"away"`
}

// Event is a goal/card/etc event.
type Event struct {
	Player   string `json:"player"`
	Minute   string `json:"minute"`
	Penalty  bool   `json:"penalty"`
	OwnGoal  bool   `json:"own_goal"`
	Raw      string `json:"raw,omitempty"`
}

// VenueRef is a stadium reference with embedded data.
type VenueRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FIFAName string `json:"fifa_name,omitempty"`
	City     string `json:"city,omitempty"`
	Country  string `json:"country,omitempty"`
	Capacity int    `json:"capacity,omitempty"`
	Region   string `json:"region,omitempty"`
}

// Match is a single match.
type Match struct {
	IDs      IDs      `json:"ids"`
	Stage    Stage    `json:"stage"`
	Datetime Datetime `json:"datetime"`
	Status   Status   `json:"status"`
	Teams    struct {
		Home TeamRef `json:"home"`
		Away TeamRef `json:"away"`
	} `json:"teams"`
	Score   Score    `json:"score"`
	Events  struct {
		Home []Event `json:"home"`
		Away []Event `json:"away"`
	} `json:"events"`
	Venue VenueRef `json:"venue"`
}

// Meta is the top-level metadata.
type Meta struct {
	Source      string `json:"source"`
	FetchedAt   string `json:"fetched_at"`
	MatchCount  int    `json:"match_count"`
}

// WCData is the full dataset.
type WCData struct {
	Meta     Meta                `json:"meta"`
	Teams    map[string]TeamRef  `json:"teams"`
	Stadiums map[string]VenueRef `json:"stadiums"`
	Matches  []Match             `json:"matches"`
}

func loadData(path string) (*WCData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var data WCData
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// FilteredMatches returns matches filtered by criteria.
type FilterState struct {
	GroupFilter   string
	StageFilter   string
	StadiumFilter string
}

func (f FilterState) matches(m Match) bool {
	if f.GroupFilter != "" && f.GroupFilter != "All" && m.Stage.Group != f.GroupFilter {
		return false
	}
	if f.StageFilter != "" && f.StageFilter != "All" {
		stage := m.Stage.Type
		if stage == "group" && f.StageFilter != "Group" {
			return false
		}
		if stage == "r32" && f.StageFilter != "R32" {
			return false
		}
		if stage == "r16" && f.StageFilter != "R16" {
			return false
		}
		if stage == "qf" && f.StageFilter != "QF" {
			return false
		}
		if stage == "sf" && f.StageFilter != "SF" {
			return false
		}
		if stage == "final" && f.StageFilter != "Final" {
			return false
		}
		if stage == "third" && f.StageFilter != "3rd" {
			return false
		}
	}
	if f.StadiumFilter != "" && f.StadiumFilter != "All" && m.Venue.Name != f.StadiumFilter {
		return false
	}
	return true
}

// Standing holds a team's record in group play.
type Standing struct {
	Team   TeamRef
	Played int
	Won    int
	Drawn  int
	Lost   int
	GF     int
	GA     int
	GD     int
	Points int
}

// ComputeStandings calculates group tables from finished/ongoing group matches.
func ComputeStandings(data *WCData) map[string][]Standing {
	groups := make(map[string]map[string]*Standing)

	for _, match := range data.Matches {
		if match.Stage.Type != "group" || match.IsUpcoming() {
			continue
		}
		group := match.Stage.Group
		if group == "" {
			continue
		}
		if groups[group] == nil {
			groups[group] = make(map[string]*Standing)
		}
		homeID := match.Teams.Home.ID
		awayID := match.Teams.Away.ID

		if groups[group][homeID] == nil {
			groups[group][homeID] = &Standing{Team: data.Teams[homeID]}
		}
		if groups[group][awayID] == nil {
			groups[group][awayID] = &Standing{Team: data.Teams[awayID]}
		}

		home := groups[group][homeID]
		away := groups[group][awayID]
		home.Played++
		away.Played++
		home.GF += match.Score.Home
		home.GA += match.Score.Away
		away.GF += match.Score.Away
		away.GA += match.Score.Home

		if match.Score.Home > match.Score.Away {
			home.Won++; home.Points += 3
			away.Lost++
		} else if match.Score.Home < match.Score.Away {
			away.Won++; away.Points += 3
			home.Lost++
		} else {
			home.Drawn++; home.Points++
			away.Drawn++; away.Points++
		}
	}

	result := make(map[string][]Standing)
	for group, teamMap := range groups {
		var standings []Standing
		for _, s := range teamMap {
			s.GD = s.GF - s.GA
			standings = append(standings, *s)
		}
		sort.Slice(standings, func(i, j int) bool {
			if standings[i].Points != standings[j].Points {
				return standings[i].Points > standings[j].Points
			}
			if standings[i].GD != standings[j].GD {
				return standings[i].GD > standings[j].GD
			}
			if standings[i].GF != standings[j].GF {
				return standings[i].GF > standings[j].GF
			}
			return standings[i].Team.Name < standings[j].Team.Name
		})
		result[group] = standings
	}
	return result
}

// AllGroups returns sorted unique groups from matches.
func (d *WCData) AllGroups() []string {
	seen := map[string]bool{"All": true}
	for _, m := range d.Matches {
		if m.Stage.Group != "" {
			seen[m.Stage.Group] = true
		}
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	// Ensure All first
	for i, v := range out {
		if v == "All" {
			out[0], out[i] = out[i], out[0]
			break
		}
	}
	return out
}

// AllStages returns stage filter options.
func AllStages() []string {
	return []string{"All", "Group", "R32", "R16", "QF", "SF", "3rd", "Final"}
}

// AllStadiums returns stadium names.
func (d *WCData) AllStadiums() []string {
	seen := map[string]bool{"All": true}
	for _, s := range d.Stadiums {
		seen[s.Name] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	for i, v := range out {
		if v == "All" {
			out[0], out[i] = out[i], out[0]
			break
		}
	}
	return out
}

// MatchdayString returns a string representation.
func (s Stage) MatchdayString() string {
	if s.Type == "group" {
		if md, ok := s.Matchday.(float64); ok {
			return fmt.Sprintf("MD%d", int(md))
		}
		if md, ok := s.Matchday.(string); ok {
			return "MD" + md
		}
	}
	return strings.ToUpper(s.Type)
}

// IsLive returns true if match is currently in progress.
func (m *Match) IsLive() bool {
	return !m.Status.Finished && m.Status.Elapsed != "" && m.Status.Elapsed != "finished" && m.Status.Elapsed != "HT"
}

// IsUpcoming returns true if match hasn't started.
func (m *Match) IsUpcoming() bool {
	return !m.Status.Finished && m.Status.Elapsed == ""
}

// FormatDate returns a human-readable date with timezone offset.
func (m *Match) FormatDate(tzOffset float64) string {
	if m.Datetime.ISO != "" {
		d, err := time.Parse(time.RFC3339, m.Datetime.ISO)
		if err == nil {
			// ISO is local time masquerading as UTC; convert to real UTC first.
			d = d.Add(time.Duration(-m.venueTZOffset() * float64(time.Hour)))
			d = d.Add(time.Duration(tzOffset * float64(time.Hour)))
			return d.Format("Jan 2")
		}
	}
	if m.Datetime.Local == "" {
		return ""
	}
	parts := strings.Split(m.Datetime.Local, " ")
	if len(parts) >= 1 {
		d, _ := time.Parse("01/02/2006", parts[0])
		if !d.IsZero() {
			return d.Format("Jan 2")
		}
	}
	return parts[0]
}

// FormatTime returns the time portion with timezone offset.
func (m *Match) FormatTime(tzOffset float64) string {
	if m.Datetime.ISO != "" {
		d, err := time.Parse(time.RFC3339, m.Datetime.ISO)
		if err == nil {
			// ISO is local time masquerading as UTC; convert to real UTC first.
			d = d.Add(time.Duration(-m.venueTZOffset() * float64(time.Hour)))
			d = d.Add(time.Duration(tzOffset * float64(time.Hour)))
			return d.Format("15:04")
		}
	}
	parts := strings.Split(m.Datetime.Local, " ")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// ElapsedDisplay returns a formatted elapsed indicator.
func (m *Match) ElapsedDisplay() string {
	if m.Status.Finished {
		return "FT"
	}
	if m.Status.Elapsed == "HT" {
		return "HT"
	}
	if m.Status.Elapsed == "" || m.Status.Elapsed == "notstarted" {
		return ""
	}
	// Try to parse as minute number from API
	if min, err := strconv.Atoi(m.Status.Elapsed); err == nil {
		return fmt.Sprintf("LIVE %d'", min)
	}
	// Live but no minute number — calculate from ISO datetime
	if m.IsLive() && m.Datetime.ISO != "" {
		start, err := time.Parse(time.RFC3339, m.Datetime.ISO)
		if err == nil {
			// local_date was parsed as UTC but it's actually in the stadium's
			// local timezone. Shift start forward by that offset so the elapsed
			// calculation is correct.
			start = start.Add(time.Duration(-m.venueTZOffset() * float64(time.Hour)))
			elapsed := int(time.Since(start).Minutes())
			if elapsed < 0 {
				elapsed = 0
			}
			if elapsed > 180 {
				// Stale data — match should be done
				return "LIVE"
			}
			// Subtract halftime breaks to show play minute, not wall-clock.
			// Standard: 15 min at HT. Extra time (knockout): +5 min at 90'.
			if elapsed > 60 && elapsed <= 105 {
				elapsed -= 15
			} else if elapsed > 105 {
				elapsed -= 20
			}
			if elapsed < 0 {
				elapsed = 0
			}
			return fmt.Sprintf("LIVE %d'", elapsed)
		}
	}
	return m.Status.Elapsed
}

// venueTZOffset returns the UTC offset (hours) for the stadium's timezone.
func (m *Match) venueTZOffset() float64 {
	switch m.Venue.Region {
	case "Eastern":
		return -4 // EDT
	case "Central":
		if m.Venue.Country == "Mexico" {
			return -6 // CST (no DST)
		}
		return -5 // CDT
	case "Western":
		return -7 // PDT
	default:
		return 0
	}
}

// AllEvents returns chronologically sorted events (approximate by minute).
func (m *Match) AllEvents() []Event {
	var out []Event
	for _, e := range m.Events.Home {
		e.Raw = "home"
		out = append(out, e)
	}
	for _, e := range m.Events.Away {
		e.Raw = "away"
		out = append(out, e)
	}
	// Sort by minute (rough)
	sort.Slice(out, func(i, j int) bool {
		mi := parseMinute(out[i].Minute)
		mj := parseMinute(out[j].Minute)
		return mi < mj
	})
	return out
}

func parseMinute(m string) int {
	m = strings.TrimSuffix(m, "'")
	m = strings.Replace(m, "+", "", -1)
	if n, err := strconv.Atoi(m); err == nil {
		return n
	}
	return 999
}

// ── Settings persistence ─────────────────────────────────────────────────────

type Settings struct {
	TimezoneOffset float64 `json:"timezone_offset"`
	SidebarOpen    bool    `json:"sidebar_open"`
	Theme          string  `json:"theme"`
}

func settingsPath() string {
	if dir := os.Getenv("HOME"); dir != "" {
		return filepath.Join(dir, ".config", "tui", "settings.json")
	}
	return "settings.json"
}

func LoadSettings() Settings {
	var s Settings
	path := settingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	json.Unmarshal(data, &s)
	return s
}

func SaveSettings(s Settings) error {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
