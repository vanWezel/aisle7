package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

// announcedFile remembers which watchlist titles Home Assistant was told about.
const announcedFile = "notified.json"

// mineStream is the first of your services that streams m.
func mineStream(m Movie, cfg Config) (Service, bool) {
	for _, s := range m.Streaming {
		if cfg.Has(s.ID) {
			return s, true
		}
	}
	return Service{}, false
}

// newlyAvailable picks the watchlist titles that stream on one of your services
// and haven't been announced yet. Watched and hidden titles are skipped.
func newlyAvailable(movies []Movie, cfg Config, watched Watched, hidden Hidden, announced map[string]WatchedEntry) []Movie {
	var out []Movie
	for _, m := range movies {
		if _, done := announced[movieKey(m.Title, m.Year, m.IMDbID)]; done || watched.Has(m) || hidden.Has(m) {
			continue
		}
		if _, ok := mineStream(m, cfg); ok {
			out = append(out, m)
		}
	}
	return out
}

type alertTitle struct {
	Title   string `json:"title"`
	Year    int    `json:"year"`
	Service string `json:"service"`
	URL     string `json:"url"`
}

// alertPayload is what the Home Assistant webhook receives: a ready-made
// title and message, plus the details for anyone who wants to build their own.
func alertPayload(fresh []Movie, cfg Config) map[string]any {
	titles := make([]alertTitle, 0, len(fresh))
	var lines []string
	for _, m := range fresh {
		s, _ := mineStream(m, cfg)
		titles = append(titles, alertTitle{m.Title, m.Year, s.Name, s.URL})
		line := m.Title
		if m.Year > 0 {
			line += fmt.Sprintf(" (%d)", m.Year)
		}
		lines = append(lines, line+" on "+s.Name)
	}
	message := strings.Join(lines[:min(5, len(lines))], ", ")
	if len(lines) > 5 {
		message += fmt.Sprintf(" and %d more", len(lines)-5)
	}
	return map[string]any{"title": "Now streaming from your watchlist", "message": message, "count": len(fresh), "titles": titles}
}

func postWebhook(webhook string, payload any) error {
	body, _ := json.Marshal(payload)
	resp, err := client.Post(webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL holds the webhook's secret id, keep it out of the logs
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// checkWatchlist tells the webhook about watchlist titles that just became
// available. Titles are only marked as announced once the webhook accepted them,
// so a failed delivery is retried at the next check.
// shortcut: each title is announced once; one that leaves a service and comes back isn't announced again.
func checkWatchlist(webhook string) error {
	cfg := LoadConfig()
	list := LoadWatchlist()
	if len(list) == 0 {
		return nil
	}
	movies, err := FindWatchlist(list, cfg.Country)
	if err != nil {
		return err
	}
	markMu.Lock()
	defer markMu.Unlock()
	announced := loadEntries(announcedFile)
	fresh := newlyAvailable(movies, cfg, LoadWatched(), LoadHidden(), announced)
	if len(fresh) == 0 {
		return nil
	}
	if err := postWebhook(webhook, alertPayload(fresh, cfg)); err != nil {
		return err
	}
	for _, m := range fresh {
		announced[movieKey(m.Title, m.Year, m.IMDbID)] = WatchedEntry{m.Title, m.Year, m.IMDbID, time.Now()}
	}
	return saveEntries(announcedFile, announced)
}

func watchWatchlist(webhook string, every time.Duration) {
	for {
		if err := checkWatchlist(webhook); err != nil {
			log.Printf("watchlist check: %v", err)
		}
		time.Sleep(every)
	}
}
