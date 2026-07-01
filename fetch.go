package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://worldcup26.ir"

// ── Raw API structs ──────────────────────────────────────────────────────────

type rawGame struct {
	ID             string `json:"id"`
	ID_            string `json:"_id"`
	Type           string `json:"type"`
	Group          string `json:"group"`
	Matchday       interface{} `json:"matchday"`
	LocalDate      string `json:"local_date"`
	Finished       string `json:"finished"`
	TimeElapsed    string `json:"time_elapsed"`
	HomeTeamID     string `json:"home_team_id"`
	AwayTeamID     string `json:"away_team_id"`
	HomeTeamNameEN string `json:"home_team_name_en"`
	AwayTeamNameEN string `json:"away_team_name_en"`
	HomeScore      interface{} `json:"home_score"`
	AwayScore      interface{} `json:"away_score"`
	HomeScorers    string `json:"home_scorers"`
	AwayScorers    string `json:"away_scorers"`
	StadiumID      string `json:"stadium_id"`
}

type rawTeam struct {
	ID       string `json:"id"`
	ID_      string `json:"_id"`
	NameEN   string `json:"name_en"`
	FIFACode string `json:"fifa_code"`
	ISO2     string `json:"iso2"`
	Flag     string `json:"flag"`
	Groups   string `json:"groups"`
}

type rawStadium struct {
	ID        string `json:"id"`
	ID_       string `json:"_id"`
	NameEN    string `json:"name_en"`
	FIFAName  string `json:"fifa_name"`
	CityEN    string `json:"city_en"`
	CountryEN string `json:"country_en"`
	Capacity  interface{} `json:"capacity"`
	Region    string `json:"region"`
}

type rawGamesResp struct {
	Games []rawGame `json:"games"`
}

type rawTeamsResp struct {
	Teams []rawTeam `json:"teams"`
}

type rawStadiumsResp struct {
	Stadiums []rawStadium `json:"stadiums"`
}

// ── Fetch ──────────────────────────────────────────────────────────────────────

func fetchJSON(url string, v interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func fetchData() (*WCData, error) {
	var gamesResp rawGamesResp
	var teamsResp rawTeamsResp
	var stadiumsResp rawStadiumsResp

	if err := fetchJSON(apiBase+"/get/games", &gamesResp); err != nil {
		return nil, fmt.Errorf("games: %w", err)
	}
	if err := fetchJSON(apiBase+"/get/teams", &teamsResp); err != nil {
		return nil, fmt.Errorf("teams: %w", err)
	}
	if err := fetchJSON(apiBase+"/get/stadiums", &stadiumsResp); err != nil {
		return nil, fmt.Errorf("stadiums: %w", err)
	}

	teams := make(map[string]TeamRef, len(teamsResp.Teams))
	for _, t := range teamsResp.Teams {
		teams[t.ID] = TeamRef{
			ID:       t.ID,
			Name:     t.NameEN,
			FIFACode: t.FIFACode,
			ISO2:     t.ISO2,
			Flag:     t.Flag,
			Group:    t.Groups,
		}
	}

	stadiums := make(map[string]VenueRef, len(stadiumsResp.Stadiums))
	for _, s := range stadiumsResp.Stadiums {
		var cap int
		switch v := s.Capacity.(type) {
		case float64:
			cap = int(v)
		case string:
			cap, _ = strconv.Atoi(v)
		}
		stadiums[s.ID] = VenueRef{
			ID:       s.ID,
			Name:     s.NameEN,
			FIFAName: s.FIFAName,
			City:     s.CityEN,
			Country:  s.CountryEN,
			Capacity: cap,
			Region:   s.Region,
		}
	}

	var matches []Match
	for _, g := range gamesResp.Games {
		home := teams[g.HomeTeamID]
		if home.Name == "" {
			home = TeamRef{ID: g.HomeTeamID, Name: g.HomeTeamNameEN}
		}
		away := teams[g.AwayTeamID]
		if away.Name == "" {
			away = TeamRef{ID: g.AwayTeamID, Name: g.AwayTeamNameEN}
		}
		venue := stadiums[g.StadiumID]

		matches = append(matches, Match{
			IDs: IDs{ID: g.ID, ID_: g.ID_},
			Stage: Stage{
				Type:    g.Type,
				Group:   g.Group,
				Matchday: parseMatchday(g.Matchday),
			},
			Datetime: Datetime{
				Local: g.LocalDate,
				ISO:   parseDate(g.LocalDate),
			},
			Status: Status{
				Finished: strings.ToUpper(g.Finished) == "TRUE",
				Elapsed:  g.TimeElapsed,
			},
			Teams: struct {
				Home TeamRef `json:"home"`
				Away TeamRef `json:"away"`
			}{Home: home, Away: away},
			Score: Score{
				Home: parseScore(g.HomeScore),
				Away: parseScore(g.AwayScore),
			},
			Events: struct {
				Home []Event `json:"home"`
				Away []Event `json:"away"`
			}{
				Home: parseEvents(g.HomeScorers),
				Away: parseEvents(g.AwayScorers),
			},
			Venue: venue,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Datetime.Local < matches[j].Datetime.Local
	})

	return &WCData{
		Meta: Meta{
			Source:     apiBase,
			FetchedAt:  time.Now().UTC().Format(time.RFC3339),
			MatchCount: len(matches),
		},
		Teams:    teams,
		Stadiums: stadiums,
		Matches:  matches,
	}, nil
}

func fetchAndSave(path string) (*WCData, error) {
	data, err := fetchData()
	if err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		return nil, err
	}
	return data, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

var quoteChars = "\"\u201c\u201d\u2018\u2019"
var scorerRe = regexp.MustCompile(`[` + regexp.QuoteMeta(quoteChars) + `](.*?)[` + regexp.QuoteMeta(quoteChars) + `]`)
var eventRe = regexp.MustCompile(`(.+?)\s+(\d+(?:'?\+\d+)?')(?:\s*\((p|P|OG|og)\))?\s*$`)

func parseScorers(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "NULL" {
		return nil
	}
	if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
		return nil
	}
	inner := raw[1 : len(raw)-1]
	if inner == "" {
		return nil
	}
	items := scorerRe.FindAllStringSubmatch(inner, -1)
	var out []string
	for _, m := range items {
		it := strings.TrimSpace(m[1])
		it = strings.ReplaceAll(it, `\'`, "'")
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}

func parseEvents(raw string) []Event {
	var out []Event
	for _, s := range parseScorers(raw) {
		s = strings.TrimSpace(s)
		for _, c := range quoteChars {
			s = strings.Trim(s, string(c))
		}
		m := eventRe.FindStringSubmatch(s)
		if m == nil {
			out = append(out, Event{Raw: s})
			continue
		}
		e := Event{Player: strings.TrimSpace(m[1]), Minute: m[2]}
		if m[3] != "" {
			switch strings.ToLower(m[3]) {
			case "p":
				e.Penalty = true
			case "og":
				e.OwnGoal = true
			}
		}
		out = append(out, e)
	}
	return out
}

func parseDate(s string) string {
	t, err := time.Parse("01/02/2006 15:04", s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

func parseScore(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func parseMatchday(v interface{}) interface{} {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
		return n
	}
	return v
}
