package main

import (
	"slices"
	"strings"
	"sync"
)

// Watchlist is the list of titles the user wants to watch. Entries share the
// watched list's shape and key scheme.
type Watchlist map[string]WatchedEntry

func LoadWatchlist() Watchlist { return Watchlist(loadEntries("watchlist.json")) }

func (w Watchlist) Save() error { return saveEntries("watchlist.json", w) }

func (w Watchlist) Has(m Movie) bool {
	_, ok := w[movieKey(m.Title, m.Year, m.IMDbID)]
	return ok
}

// watchlistMatch applies the title, year, genre and rating filters to a
// watchlist entry; the server can't, because the list is looked up title by
// title. Empty filters match everything.
func watchlistMatch(mv Movie, title, year, genre string, minRating float64) bool {
	if q := strings.ToLower(strings.TrimSpace(title)); q != "" && !strings.Contains(strings.ToLower(mv.Title), q) {
		return false
	}
	if y := strings.TrimSpace(year); y != "" {
		if lo, hi, ok := parseYears(y); ok && (mv.Year < lo || mv.Year > hi) {
			return false
		}
	}
	if genre != "" && !slices.Contains(mv.Genres, genre) {
		return false
	}
	if minRating > 0 && (mv.IMDbScore == nil || *mv.IMDbScore < minRating) {
		return false
	}
	return true
}

// FindWatchlist looks every entry up on JustWatch (in parallel) so the list
// shows current availability and scores whatever the other filters are.
func FindWatchlist(w Watchlist, country string) ([]Movie, error) {
	entries := make([]WatchedEntry, 0, len(w))
	for _, e := range w {
		entries = append(entries, e)
	}
	found := make([]*Movie, len(entries))
	errs := make([]error, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, e := range entries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			filter := map[string]any{"searchQuery": e.Title}
			if e.Year > 0 {
				filter["releaseYear"] = map[string]int{"min": e.Year, "max": e.Year}
			}
			res, _, err := Find(filter, country, 10)
			if err != nil {
				errs[i] = err
				return
			}
			for j, mv := range res {
				if (e.IMDbID != "" && mv.IMDbID == e.IMDbID) ||
					(strings.EqualFold(mv.Title, e.Title) && mv.Year == e.Year) {
					found[i] = &res[j]
					break
				}
			}
			if found[i] == nil {
				// Not found upstream: keep it listed with what we stored.
				found[i] = &Movie{Title: e.Title, Year: e.Year, IMDbID: e.IMDbID}
			}
		}()
	}
	wg.Wait()
	var out []Movie
	for i, f := range found {
		if f != nil {
			out = append(out, *f)
		} else if errs[i] != nil {
			return nil, errs[i]
		}
	}
	return out, nil
}
