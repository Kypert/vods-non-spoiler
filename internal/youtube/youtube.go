// Package youtube lists uploads of a channel, via RSS (no key, last ~15 uploads) or the
// Data API (key needed, full history).
package youtube

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Video is one upload
type Video struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Published time.Time `json:"published"`
}

// Client fetches channel uploads
type Client struct {
	HTTP       *http.Client
	RSSBaseURL string
	APIBaseURL string
}

// New returns a client with sane defaults
func New() *Client {
	return &Client{
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		RSSBaseURL: "https://www.youtube.com/feeds/videos.xml",
		APIBaseURL: "https://www.googleapis.com/youtube/v3",
	}
}

// The URL can hold the API key, so errors only tell the host and path
func (c *Client) do(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errors.New("youtube: invalid request URL")
	}
	safe := req.URL.Host + req.URL.Path
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("GET %s: %w", safe, err)
	}
	if resp.StatusCode != http.StatusOK {
		// Google error bodies tell the reason, e.g. quotaExceeded, and do not echo the key
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: unexpected status %s: %s", safe, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

// RSS returns the latest uploads from the channel feed
func (c *Client) RSS(ctx context.Context, channelID string) ([]Video, error) {
	resp, err := c.do(ctx, c.RSSBaseURL+"?"+url.Values{"channel_id": {channelID}}.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var feed struct {
		Entries []struct {
			VideoID   string    `xml:"videoId"`
			Title     string    `xml:"title"`
			Published time.Time `xml:"published"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode feed: %w", err)
	}
	out := make([]Video, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		out = append(out, Video{ID: e.VideoID, Title: e.Title, Published: e.Published})
	}
	return out, nil
}

// Uploads returns uploads published after since, via the Data API uploads playlist.
// The cost is 1 quota unit per 50 videos.
func (c *Client) Uploads(ctx context.Context, apiKey, channelID string, since time.Time) ([]Video, error) {
	if !strings.HasPrefix(channelID, "UC") {
		return nil, fmt.Errorf("unexpected channel ID %q", channelID)
	}
	playlist := "UU" + strings.TrimPrefix(channelID, "UC")

	var out []Video
	token := ""
	for {
		q := url.Values{
			"part":       {"snippet,contentDetails"},
			"playlistId": {playlist},
			"maxResults": {"50"},
			"key":        {apiKey},
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		resp, err := c.do(ctx, c.APIBaseURL+"/playlistItems?"+q.Encode())
		if err != nil {
			return nil, err
		}
		var page struct {
			NextPageToken string `json:"nextPageToken"`
			Items         []struct {
				Snippet struct {
					Title string `json:"title"`
				} `json:"snippet"`
				ContentDetails struct {
					VideoID          string    `json:"videoId"`
					VideoPublishedAt time.Time `json:"videoPublishedAt"`
				} `json:"contentDetails"`
			} `json:"items"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode playlistItems: %w", err)
		}

		reachedOld := false
		for _, it := range page.Items {
			if it.ContentDetails.VideoPublishedAt.Before(since) {
				reachedOld = true
				continue
			}
			out = append(out, Video{
				ID:        it.ContentDetails.VideoID,
				Title:     it.Snippet.Title,
				Published: it.ContentDetails.VideoPublishedAt,
			})
		}
		// The uploads playlist is newest first, so stop at the first page with old videos
		if reachedOld || page.NextPageToken == "" {
			return out, nil
		}
		token = page.NextPageToken
	}
}
