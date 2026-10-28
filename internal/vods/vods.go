// Package vods joins Riot matches with YouTube uploads and builds the public, spoiler-free
// site data.
//
// Rule: nothing in the output may tell the result of a series. No scores, no records, no
// outcome, and no hint of the number of games played. The series format (Bo1, Bo3, Bo5) is
// shown, because the schedule tells it before the series is played.
package vods

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"vods-non-spoiler/internal/riot"
	"vods-non-spoiler/internal/youtube"
)

// MaxUploadDelay is how long after the series start we accept an upload for it
const MaxUploadDelay = 48 * time.Hour

// MaxUploadEarly is how long before the series start we accept an upload for it. Riot's start
// time is not always the real start, e.g. Worlds 2026 FUR vs C9 had uploads 80 min before it.
const MaxUploadEarly = 6 * time.Hour

// Examples:
//
//	"WE vs BFX - Game 1 | Final Demacia Cup Global Invitational 2026 | ..."
//	"MKOI vs DK - Game 1 (Bo3) | Day 3 LoL Worlds 2026 Swiss Stage | ..." (series format after game number)
//	"RED vs NAVI | Day 1 Demacia Cup Global Invitational 2026 | ... full" (Bo1, no game number)
var titleRe = regexp.MustCompile(`(?i)^\s*([^\s|]+)\s+vs\.?\s+([^\s|]+)\s+(?:-\s+Game\s+(\d+)\s*(?:\(Bo\d+\)\s*)?)?\|`)

// Game is a parsed upload title. Number is 0 when the title has no game number.
type Game struct {
	TeamA, TeamB string
	Number       int
}

// ParseTitle parses a single game title. It returns false for other uploads, e.g. "All Games".
func ParseTitle(title string, aliases map[string]string) (Game, bool) {
	m := titleRe.FindStringSubmatch(title)
	if m == nil {
		return Game{}, false
	}
	n := 0
	if m[3] != "" {
		var err error
		if n, err = strconv.Atoi(m[3]); err != nil || n < 1 {
			return Game{}, false
		}
	}
	return Game{TeamA: alias(m[1], aliases), TeamB: alias(m[2], aliases), Number: n}, true
}

func alias(code string, aliases map[string]string) string {
	code = strings.ToUpper(code)
	if a, ok := aliases[code]; ok {
		return strings.ToUpper(a)
	}
	return code
}

func sameTeams(m riot.Match, g Game) bool {
	if len(m.Teams) != 2 {
		return false
	}
	a, b := strings.ToUpper(m.Teams[0].Code), strings.ToUpper(m.Teams[1].Code)
	return (a == g.TeamA && b == g.TeamB) || (a == g.TeamB && b == g.TeamA)
}

// Join maps match ID -> game number -> video ID. It also returns the uploads it could not
// map, for debugging (they can contain spoilers, so do not log them by default).
func Join(matches []riot.Match, videos []youtube.Video, aliases map[string]string, titleFilter string) (map[string]map[int]string, []youtube.Video) {
	out := map[string]map[int]string{}
	published := map[string]time.Time{}
	var unmatched []youtube.Video

	for _, v := range videos {
		if titleFilter != "" && !strings.Contains(strings.ToLower(v.Title), strings.ToLower(titleFilter)) {
			continue
		}
		g, ok := ParseTitle(v.Title, aliases)
		if !ok {
			continue
		}
		// Pick the latest series of these teams that started before the upload (with tolerance)
		var best *riot.Match
		for i := range matches {
			m := &matches[i]
			if !sameTeams(*m, g) || m.StartTime.After(v.Published.Add(MaxUploadEarly)) || v.Published.Sub(m.StartTime) > MaxUploadDelay {
				continue
			}
			if best == nil || m.StartTime.After(best.StartTime) {
				best = m
			}
		}
		if best == nil || g.Number > best.BestOf {
			unmatched = append(unmatched, v)
			continue
		}
		// No game number is only clear for a Bo1
		if g.Number == 0 {
			if best.BestOf != 1 {
				unmatched = append(unmatched, v)
				continue
			}
			g.Number = 1
		}
		key := best.ID + "/" + strconv.Itoa(g.Number)
		// On re-upload, keep the newest video
		if prev, ok := published[key]; ok && prev.After(v.Published) {
			continue
		}
		published[key] = v.Published
		if out[best.ID] == nil {
			out[best.ID] = map[int]string{}
		}
		out[best.ID][g.Number] = v.ID
	}
	return out, unmatched
}

// Site is the public data for one tournament
type Site struct {
	Tournament string    `json:"tournament"`
	Name       string    `json:"name"`
	Generated  time.Time `json:"generated"`
	Stages     []Stage   `json:"stages"`
}

// IndexEntry is one tournament in data/index.json
type IndexEntry struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

// DisplayName makes a name from a slug, e.g. "demacia_cup_2026" -> "Demacia Cup 2026"
func DisplayName(slug string) string {
	words := strings.Fields(strings.ReplaceAll(slug, "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// Upsert adds or replaces an entry by slug, newest tournament first
func Upsert(index []IndexEntry, e IndexEntry) []IndexEntry {
	index = slices.DeleteFunc(index, func(x IndexEntry) bool { return x.Slug == e.Slug })
	index = append(index, e)
	slices.SortStableFunc(index, func(a, b IndexEntry) int { return strings.Compare(b.StartDate, a.StartDate) })
	return index
}

// Stage is e.g. "Swiss" or "Quarterfinals"
type Stage struct {
	Name string `json:"name"`
	Days []Day  `json:"days"`
}

// Day groups matches on one calendar day
type Day struct {
	Date    string `json:"date"`
	Matches []Card `json:"matches"`
}

// Card is one series. Games always has length Slots when Ready. An empty string is a dummy slot.
type Card struct {
	ID    string    `json:"id"`
	Start time.Time `json:"start"`
	Teams [2]string `json:"teams"`
	Slots int       `json:"slots"`
	Ready bool      `json:"ready"`
	Games []string  `json:"games,omitempty"`
}

// Build creates the public site data. A series is ready only when Riot marks it completed
// and all games played have an upload. Until then, it shows no games at all.
func Build(tournament string, matches []riot.Match, games map[string]map[int]string, loc *time.Location, now time.Time) Site {
	sorted := slices.Clone(matches)
	slices.SortStableFunc(sorted, func(a, b riot.Match) int { return a.StartTime.Compare(b.StartTime) })

	site := Site{Tournament: tournament, Name: DisplayName(tournament), Generated: now.UTC(), Stages: []Stage{}}
	stageIdx := map[string]int{}
	for _, m := range sorted {
		si, ok := stageIdx[m.Block]
		if !ok {
			si = len(site.Stages)
			stageIdx[m.Block] = si
			site.Stages = append(site.Stages, Stage{Name: m.Block})
		}
		st := &site.Stages[si]

		date := m.StartTime.In(loc).Format(time.DateOnly)
		if len(st.Days) == 0 || st.Days[len(st.Days)-1].Date != date {
			st.Days = append(st.Days, Day{Date: date})
		}
		day := &st.Days[len(st.Days)-1]

		card := Card{ID: m.ID, Start: m.StartTime.UTC(), Slots: m.BestOf}
		for i := 0; i < 2 && i < len(m.Teams); i++ {
			card.Teams[i] = cmp.Or(m.Teams[i].Code, m.Teams[i].Name)
		}

		played := m.GamesPlayed()
		got := games[m.ID]
		ready := m.State == "completed" && played > 0
		for n := 1; ready && n <= played; n++ {
			if got[n] == "" {
				ready = false
			}
		}
		if ready {
			card.Ready = true
			card.Games = make([]string, card.Slots)
			for n := 1; n <= played; n++ {
				card.Games[n-1] = got[n]
			}
		}
		day.Matches = append(day.Matches, card)
	}
	return site
}
