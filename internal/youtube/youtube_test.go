package youtube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const secret = "SECRET-KEY-123"

func TestErrorsDoNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"errors":[{"reason":"quotaExceeded"}]}}`, http.StatusForbidden)
	}))
	defer srv.Close()

	c := New()
	c.APIBaseURL = srv.URL
	_, err := c.Uploads(context.Background(), secret, "UCabc", time.Time{})
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "quotaExceeded") {
		t.Fatalf("status error = %v", err)
	}

	// Connection error: net/http puts the full URL in *url.Error
	srv.Close()
	_, err = c.Uploads(context.Background(), secret, "UCabc", time.Time{})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("connection error = %v", err)
	}
}

func TestUploadsStopsAtOldVideos(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("playlistId") != "UUabc" {
			t.Errorf("playlistId = %q", r.URL.Query().Get("playlistId"))
		}
		_, _ = w.Write([]byte(`{"nextPageToken":"p2","items":[
			{"snippet":{"title":"new"},"contentDetails":{"videoId":"v1","videoPublishedAt":"2026-10-10T00:00:00Z"}},
			{"snippet":{"title":"old"},"contentDetails":{"videoId":"v0","videoPublishedAt":"2026-01-01T00:00:00Z"}}]}`))
	}))
	defer srv.Close()

	c := New()
	c.APIBaseURL = srv.URL
	got, err := c.Uploads(context.Background(), "k", "UCabc", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got) != 1 || got[0].ID != "v1" {
		t.Fatalf("calls=%d got=%v", calls, got)
	}
}
