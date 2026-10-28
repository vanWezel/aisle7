package main

import (
	"strings"
	"testing"
)

func TestNewlyAvailable(t *testing.T) {
	cfg := Config{Services: []Service{{ID: 1}}}
	mine, other := []Service{{ID: 1}}, []Service{{ID: 2}}
	mk := func(title string, st []Service) Movie { return Movie{Title: title, Year: 2020, Streaming: st} }
	movies := []Movie{mk("new", mine), mk("elsewhere", other), mk("seen", mine), mk("hidden", mine), mk("announced", mine), mk("rental", nil)}
	key := func(m Movie) string { return movieKey(m.Title, m.Year, m.IMDbID) }

	got := newlyAvailable(movies, cfg,
		Watched{key(movies[2]): {}}, Hidden{key(movies[3]): {}}, map[string]WatchedEntry{key(movies[4]): {}})
	if len(got) != 1 || got[0].Title != "new" {
		t.Fatalf("got %v, want only \"new\"", got)
	}
}

func TestAlertPayloadCapsTheMessage(t *testing.T) {
	cfg := Config{Services: []Service{{ID: 1}}}
	var fresh []Movie
	for _, title := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		fresh = append(fresh, Movie{Title: title, Year: 1999, Streaming: []Service{{ID: 1, Name: "Netflix"}}})
	}
	p := alertPayload(fresh, cfg)
	if msg := p["message"].(string); !strings.HasSuffix(msg, "e (1999) on Netflix and 2 more") || p["count"] != 7 {
		t.Fatalf("message %q, count %v", msg, p["count"])
	}
}
