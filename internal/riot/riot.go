// Package riot reads the unofficial lolesports.com API.
//
// The API has no documentation and can change without notice. The key below is the public
// key that lolesports.com uses in the browser.
package riot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL = "https://esports-api.lolesports.com/persisted/gw"
	apiKey         = "0TvQnueqKa5mxJntVWt0w4LpLfEkrV1Ta8rQBb9Z"
)

// Client is a small lolesports API client
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

// New returns a client with sane defaults
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, BaseURL: defaultBaseURL}
}

// Tournament is one edition of a league, e.g. worlds_2026
type Tournament struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	StartDate time.Time `json:"-"`
	EndDate   time.Time `json:"-"`
}

// Team is one side of a match. Result holds spoilers and must not be published.
type Team struct {
	Name   string  `json:"name"`
	Code   string  `json:"code"`
	Result *Result `json:"result"`
}

// Result is the series result of one team
type Result struct {
	Outcome  string `json:"outcome"`
	GameWins int    `json:"gameWins"`
}

// Match is one series
type Match struct {
	ID        string
	StartTime time.Time
	State     string
	Block     string
	BestOf    int
	Teams     []Team
}

// GamesPlayed returns the number of games played in a series
func (m Match) GamesPlayed() int {
	n := 0
	for _, t := range m.Teams {
		if t.Result != nil {
			n += t.Result.GameWins
		}
	}
	return n
}

func (c *Client) get(ctx context.Context, endpoint string, q url.Values, out any) error {
	q.Set("hl", "en-US")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/"+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", apiKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// LeagueID resolves a league slug (e.g. "worlds", "demacia_cup") to its ID
func (c *Client) LeagueID(ctx context.Context, slug string) (string, error) {
	var r struct {
		Data struct {
			Leagues []struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
			} `json:"leagues"`
		} `json:"data"`
	}
	if err := c.get(ctx, "getLeagues", url.Values{}, &r); err != nil {
		return "", err
	}
	for _, l := range r.Data.Leagues {
		if l.Slug == slug {
			return l.ID, nil
		}
	}
	return "", fmt.Errorf("league %q not found", slug)
}

// LatestTournament returns the tournament of a league with the latest start date
func (c *Client) LatestTournament(ctx context.Context, leagueID string) (Tournament, error) {
	var r struct {
		Data struct {
			Leagues []struct {
				Tournaments []struct {
					ID        string `json:"id"`
					Slug      string `json:"slug"`
					StartDate string `json:"startDate"`
					EndDate   string `json:"endDate"`
				} `json:"tournaments"`
			} `json:"leagues"`
		} `json:"data"`
	}
	if err := c.get(ctx, "getTournamentsForLeague", url.Values{"leagueId": {leagueID}}, &r); err != nil {
		return Tournament{}, err
	}
	var best Tournament
	for _, l := range r.Data.Leagues {
		for _, t := range l.Tournaments {
			start, err := time.Parse(time.DateOnly, t.StartDate)
			if err != nil {
				return Tournament{}, fmt.Errorf("tournament %s: %w", t.Slug, err)
			}
			end, err := time.Parse(time.DateOnly, t.EndDate)
			if err != nil {
				return Tournament{}, fmt.Errorf("tournament %s: %w", t.Slug, err)
			}
			if best.ID == "" || start.After(best.StartDate) {
				best = Tournament{ID: t.ID, Slug: t.Slug, StartDate: start, EndDate: end}
			}
		}
	}
	if best.ID == "" {
		return Tournament{}, errors.New("no tournaments found")
	}
	return best, nil
}

type schedulePage struct {
	Data struct {
		Schedule struct {
			Pages struct {
				Older *string `json:"older"`
				Newer *string `json:"newer"`
			} `json:"pages"`
			Events []struct {
				StartTime time.Time `json:"startTime"`
				State     string    `json:"state"`
				Type      string    `json:"type"`
				BlockName string    `json:"blockName"`
				Match     struct {
					ID       string `json:"id"`
					Teams    []Team `json:"teams"`
					Strategy struct {
						Type  string `json:"type"`
						Count int    `json:"count"`
					} `json:"strategy"`
				} `json:"match"`
			} `json:"events"`
		} `json:"schedule"`
	} `json:"data"`
}

// Schedule returns all matches of a league between from and to (inclusive)
func (c *Client) Schedule(ctx context.Context, leagueID string, from, to time.Time) ([]Match, error) {
	seen := map[string]bool{}
	var out []Match

	// The first page is around "now", then we walk in both directions
	fetch := func(token string) (schedulePage, error) {
		q := url.Values{"leagueId": {leagueID}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var p schedulePage
		err := c.get(ctx, "getSchedule", q, &p)
		return p, err
	}
	collect := func(p schedulePage) (oldest, newest time.Time) {
		for _, e := range p.Data.Schedule.Events {
			if oldest.IsZero() || e.StartTime.Before(oldest) {
				oldest = e.StartTime
			}
			if e.StartTime.After(newest) {
				newest = e.StartTime
			}
			if e.Type != "match" || seen[e.Match.ID] || e.StartTime.Before(from) || e.StartTime.After(to) {
				continue
			}
			seen[e.Match.ID] = true
			out = append(out, Match{
				ID:        e.Match.ID,
				StartTime: e.StartTime,
				State:     e.State,
				Block:     e.BlockName,
				BestOf:    e.Match.Strategy.Count,
				Teams:     e.Match.Teams,
			})
		}
		return oldest, newest
	}

	first, err := fetch("")
	if err != nil {
		return nil, err
	}
	oldest, newest := collect(first)

	for tok := first.Data.Schedule.Pages.Older; tok != nil && !oldest.Before(from); {
		p, err := fetch(*tok)
		if err != nil {
			return nil, err
		}
		oldest, _ = collect(p)
		tok = p.Data.Schedule.Pages.Older
	}
	for tok := first.Data.Schedule.Pages.Newer; tok != nil && !newest.After(to); {
		p, err := fetch(*tok)
		if err != nil {
			return nil, err
		}
		_, newest = collect(p)
		tok = p.Data.Schedule.Pages.Newer
	}
	return out, nil
}
