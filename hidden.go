package main

// Hidden is the list of titles the user never wants to see in results.
// Entries share the watched list's shape and key scheme.
type Hidden map[string]WatchedEntry

func LoadHidden() Hidden { return Hidden(loadEntries("hidden.json")) }

func (h Hidden) Save() error { return saveEntries("hidden.json", h) }

func (h Hidden) Has(m Movie) bool {
	_, ok := h[movieKey(m.Title, m.Year, m.IMDbID)]
	return ok
}
