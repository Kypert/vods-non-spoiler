package vods

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vods-non-spoiler/internal/riot"
	"vods-non-spoiler/internal/youtube"
)

func TestParseTitle(t *testing.T) {
	tests := []struct {
		title string
		want  Game
		ok    bool
	}{
		{"WE vs BFX - Game 1 | Final Demacia Cup Global Invitational 2026 | Team WE vs BNK FEARX G1 full", Game{"WE", "BFX", 1}, true},
		{"FUR vs C9 - Game 4 | Round 1 S16 LoL Worlds 2026 Play-Ins | FURIA vs Cloud 9 G4 Full", Game{"FUR", "C9", 4}, true},
		{"MVK vs FUR - All Games (Bo5) | Round 2 S16 LoL Worlds 2026 Play-Ins | mega-vod", Game{}, false},
		{"KC vs MVK + C9 vs FUR - All Games | Worlds 2026 Play-Ins Day 2 full day mega-vod", Game{}, false},
		{"fly vs t1 - game 2 | x", Game{"FLY", "T1", 2}, true},
		{"RED vs NAVI | Day 1 Demacia Cup Global Invitational 2026 | RED Canids vs Natus Vincere full", Game{"RED", "NAVI", 0}, true},
		{"Swiss Stage Round 2 Draw - 2026 Demacia Cup Global Invitational", Game{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseTitle(tt.title, nil)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseTitle(%q) = %+v, %v, want %+v, %v", tt.title, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseTitleAlias(t *testing.T) {
	got, ok := ParseTitle("FX vs WE - Game 1 | x", map[string]string{"FX": "bfx"})
	if !ok || got.TeamA != "BFX" {
		t.Fatalf("got %+v, %v", got, ok)
	}
}

var t0 = time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)

func match(id, block string, start time.Time, bestOf int, a, b string, winsA, winsB int) riot.Match {
	return riot.Match{
		ID: id, Block: block, StartTime: start, BestOf: bestOf, State: "completed",
		Teams: []riot.Team{
			{Code: a, Result: &riot.Result{GameWins: winsA}},
			{Code: b, Result: &riot.Result{GameWins: winsB}},
		},
	}
}

func video(id, title string, published time.Time) youtube.Video {
	return youtube.Video{ID: id, Title: title, Published: published}
}

func TestJoinPicksLatestSeriesAndTeamOrder(t *testing.T) {
	matches := []riot.Match{
		match("m1", "Swiss", t0, 1, "AAA", "BBB", 1, 0),
		match("m2", "Swiss", t0.Add(30*time.Hour), 3, "BBB", "AAA", 2, 0),
	}
	videos := []youtube.Video{
		video("v1", "AAA vs BBB - Game 1 | x", t0.Add(time.Hour)),
		video("v2", "AAA vs BBB - Game 1 | x", t0.Add(31*time.Hour)),
		video("v3", "BBB vs AAA - Game 2 | x", t0.Add(32*time.Hour)),
		video("old", "AAA vs BBB - Game 1 | x", t0.Add(-time.Hour)),
		video("late", "AAA vs BBB - Game 3 | x", t0.Add(30*time.Hour+MaxUploadDelay+time.Hour)),
	}
	got, unmatched := Join(matches, videos, nil, "")
	if got["m1"][1] != "v1" || got["m2"][1] != "v2" || got["m2"][2] != "v3" {
		t.Fatalf("join = %v", got)
	}
	if len(unmatched) != 2 {
		t.Fatalf("unmatched = %v", unmatched)
	}
}

func TestJoinNoGameNumber(t *testing.T) {
	matches := []riot.Match{
		match("bo1", "Swiss", t0, 1, "AAA", "BBB", 1, 0),
		match("bo3", "Swiss", t0.Add(72*time.Hour), 3, "AAA", "BBB", 2, 0),
	}
	videos := []youtube.Video{
		video("v1", "AAA vs BBB | Day 1 x | full", t0.Add(time.Hour)),
		video("v2", "AAA vs BBB | Day 4 x | full", t0.Add(73*time.Hour)),
	}
	got, unmatched := Join(matches, videos, nil, "")
	if got["bo1"][1] != "v1" || got["bo3"] != nil || len(unmatched) != 1 || unmatched[0].ID != "v2" {
		t.Fatalf("join = %v, unmatched = %v", got, unmatched)
	}
}

func TestJoinTitleFilter(t *testing.T) {
	matches := []riot.Match{match("m1", "Swiss", t0, 1, "AAA", "BBB", 1, 0)}
	videos := []youtube.Video{video("v1", "AAA vs BBB - Game 1 | Other Cup", t0.Add(time.Hour))}
	got, _ := Join(matches, videos, nil, "demacia cup")
	if len(got) != 0 {
		t.Fatalf("join = %v", got)
	}
}

func TestBuildHidesSeriesLength(t *testing.T) {
	matches := []riot.Match{
		match("bo1", "Swiss", t0, 1, "AAA", "BBB", 1, 0),
		match("bo3", "Swiss", t0.Add(time.Hour), 3, "CCC", "DDD", 2, 0),
		match("stomp", "Finals", t0.Add(48*time.Hour), 5, "AAA", "CCC", 3, 0),
		match("partial", "Finals", t0.Add(72*time.Hour), 5, "BBB", "DDD", 3, 2),
	}
	matches[3].State = "inProgress"
	games := map[string]map[int]string{
		"bo1":     {1: "a"},
		"bo3":     {1: "b", 2: "c"},
		"stomp":   {1: "d", 2: "e", 3: "f"},
		"partial": {1: "g"},
	}
	site := Build("t", matches, games, time.UTC, t0)

	cards := map[string]Card{}
	for _, s := range site.Stages {
		for _, d := range s.Days {
			for _, c := range d.Matches {
				cards[c.ID] = c
			}
		}
	}
	// Bo1 and Bo3 in the same stage must look the same
	if cards["bo1"].Slots != 3 || cards["bo3"].Slots != 3 {
		t.Errorf("swiss slots = %d, %d, want 3", cards["bo1"].Slots, cards["bo3"].Slots)
	}
	if c := cards["stomp"]; !c.Ready || len(c.Games) != 5 || c.Games[3] != "" || c.Games[4] != "" {
		t.Errorf("stomp = %+v", c)
	}
	if c := cards["partial"]; c.Ready || c.Games != nil {
		t.Errorf("unfinished series must show no games: %+v", c)
	}

	// The public JSON must not contain result fields
	b, err := json.Marshal(site)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"gameWins", "outcome", "record", "result", "state"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("site JSON contains %q: %s", bad, b)
		}
	}
}

func TestBuildNotReadyWhenVideoMissing(t *testing.T) {
	matches := []riot.Match{match("m", "Finals", t0, 5, "AAA", "BBB", 3, 1)}
	site := Build("t", matches, map[string]map[int]string{"m": {1: "a", 2: "b", 3: "c"}}, time.UTC, t0)
	if c := site.Stages[0].Days[0].Matches[0]; c.Ready {
		t.Errorf("want not ready with game 4 missing: %+v", c)
	}
}

func TestDisplayName(t *testing.T) {
	if got := DisplayName("demacia_cup_2026"); got != "Demacia Cup 2026" {
		t.Errorf("got %q", got)
	}
}

func TestUpsert(t *testing.T) {
	idx := []IndexEntry{{Slug: "a", StartDate: "2026-01-01"}, {Slug: "b", StartDate: "2026-06-01"}}
	idx = Upsert(idx, IndexEntry{Slug: "a", Name: "new", StartDate: "2026-10-01"})
	if len(idx) != 2 || idx[0].Slug != "a" || idx[0].Name != "new" || idx[1].Slug != "b" {
		t.Errorf("got %+v", idx)
	}
}
