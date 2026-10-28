package main

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

type WatchedEntry struct {
	Title  string    `json:"title"`
	Year   int       `json:"year"`
	IMDbID string    `json:"imdbId,omitempty"`
	At     time.Time `json:"at"`
}

// Watched maps a movie key (IMDb id, else "title|year") to when it was seen.
type Watched map[string]WatchedEntry

func movieKey(title string, year int, imdbID string) string {
	if imdbID != "" {
		return imdbID
	}
	return strings.ToLower(title) + "|" + itoa(year)
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func loadEntries(name string) map[string]WatchedEntry {
	e := map[string]WatchedEntry{}
	if p, err := dataPath(name); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &e)
		}
	}
	return e
}

func saveEntries(name string, e map[string]WatchedEntry) error {
	p, err := dataPath(name)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(e, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

func LoadWatched() Watched { return Watched(loadEntries("watched.json")) }

func (w Watched) Save() error { return saveEntries("watched.json", w) }

func (w Watched) Has(m Movie) bool {
	_, ok := w[movieKey(m.Title, m.Year, m.IMDbID)]
	return ok
}
