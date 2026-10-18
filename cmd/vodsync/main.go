// Command vodsync builds the spoiler-free site data from the Riot schedule and YouTube uploads.
//
// Logs are spoiler-free by default. Use -v to print upload titles that did not match.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
	_ "time/tzdata" // CI images can lack tzdata

	"vods-non-spoiler/internal/riot"
	"vods-non-spoiler/internal/vods"
	"vods-non-spoiler/internal/youtube"
)

type config struct {
	league      string
	channel     string
	titleFilter string
	out         string
	state       string
	aliases     string
	tz          string
	verbose     bool
}

func main() {
	var c config
	flag.StringVar(&c.league, "league", "worlds", "Riot league slug, e.g. worlds or demacia_cup")
	flag.StringVar(&c.channel, "channel", "UCdOWyp25T0HDtjpnV2LpIyw", "YouTube channel ID (EpicSkillshot)")
	flag.StringVar(&c.titleFilter, "title-filter", "", "Only use uploads with this text in the title")
	flag.StringVar(&c.out, "out", "docs/data", "Output directory: <tournament>.json and index.json")
	flag.StringVar(&c.state, "state", ".state/videos.json", "Cache of seen uploads (contains spoilers)")
	flag.StringVar(&c.aliases, "aliases", "aliases.json", "Optional JSON map: title team code -> Riot team code")
	flag.StringVar(&c.tz, "tz", "Europe/Stockholm", "Time zone for day grouping")
	flag.BoolVar(&c.verbose, "v", false, "Print unmatched upload titles (can spoil)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := run(ctx, c); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, c config) error {
	loc, err := time.LoadLocation(c.tz)
	if err != nil {
		return err
	}
	aliases := map[string]string{}
	if err := readJSON(c.aliases, &aliases); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("aliases: %w", err)
	}

	rc := riot.New()
	leagueID, err := rc.LeagueID(ctx, c.league)
	if err != nil {
		return err
	}
	t, err := rc.LatestTournament(ctx, leagueID)
	if err != nil {
		return err
	}
	from := t.StartDate.Add(-24 * time.Hour)
	to := t.EndDate.Add(48 * time.Hour)
	matches, err := rc.Schedule(ctx, leagueID, from, to)
	if err != nil {
		return err
	}
	log.Printf("tournament %s: %d series", t.Slug, len(matches))

	videos, err := loadVideos(c.state)
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	yc := youtube.New()
	var fresh []youtube.Video
	if key := os.Getenv("YOUTUBE_API_KEY"); key != "" {
		fresh, err = yc.Uploads(ctx, key, c.channel, from)
		log.Printf("youtube api: %d uploads since %s", len(fresh), from.Format(time.DateOnly))
	} else {
		fresh, err = yc.RSS(ctx, c.channel)
		log.Printf("youtube rss: %d uploads (set YOUTUBE_API_KEY for full history)", len(fresh))
	}
	if err != nil {
		return err
	}
	for _, v := range fresh {
		videos[v.ID] = v
	}
	if err := writeJSON(c.state, videos); err != nil {
		return err
	}

	all := slices.SortedFunc(maps.Values(videos), func(a, b youtube.Video) int { return a.Published.Compare(b.Published) })
	games, unmatched := vods.Join(matches, all, aliases, c.titleFilter)
	site := vods.Build(t.Slug, matches, games, loc, time.Now())

	ready := 0
	for _, s := range site.Stages {
		for _, d := range s.Days {
			for _, m := range d.Matches {
				if m.Ready {
					ready++
				}
			}
		}
	}
	log.Printf("series ready: %d/%d, unmatched game uploads: %d", ready, len(matches), len(unmatched))
	if c.verbose {
		for _, v := range unmatched {
			log.Printf("unmatched: %s %q", v.Published.Format(time.RFC3339), v.Title)
		}
	}
	if err := writeJSON(filepath.Join(c.out, t.Slug+".json"), site); err != nil {
		return err
	}

	indexPath := filepath.Join(c.out, "index.json")
	var index []vods.IndexEntry
	if err := readJSON(indexPath, &index); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("index: %w", err)
	}
	index = vods.Upsert(index, vods.IndexEntry{
		Slug:      t.Slug,
		Name:      site.Name,
		StartDate: t.StartDate.Format(time.DateOnly),
		EndDate:   t.EndDate.Format(time.DateOnly),
	})
	return writeJSON(indexPath, index)
}

func loadVideos(path string) (map[string]youtube.Video, error) {
	videos := map[string]youtube.Video{}
	if err := readJSON(path, &videos); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return videos, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
