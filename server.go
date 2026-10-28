package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed all:web/dist
var webFS embed.FS

// markMu serialises the read-modify-write of the watched/hidden/watchlist files.
var markMu sync.Mutex

var markFiles = map[string]string{"watched": "watched.json", "hidden": "hidden.json", "watchlist": "watchlist.json"}

type apiStream struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Mine bool   `json:"mine"`
}

type apiMovie struct {
	Title     string      `json:"title"`
	Year      int         `json:"year"`
	Overview  string      `json:"overview"`
	IMDbID    string      `json:"imdbId"`
	Genres    []string    `json:"genres"`
	JWURL     string      `json:"jwUrl"`
	Poster    string      `json:"poster"`
	IMDbScore *float64    `json:"imdb"`
	Votes     float64     `json:"votes"`
	RT        *int        `json:"rt"`
	Streaming []apiStream `json:"streaming"`
	RentBuy   bool        `json:"rentBuy"`
	Mine      bool        `json:"mine"`
	Watched   bool        `json:"watched"`
	Hidden    bool        `json:"hidden"`
	Listed    bool        `json:"listed"`
}

func jsonErr(c *gin.Context, code int, err error) { c.JSON(code, gin.H{"error": err.Error()}) }

// jsonOnly rejects writes that aren't application/json. Browsers can't send
// that cross-site without a CORS preflight (which we never allow), so another
// website can't change your lists just because this server is on your network.
func jsonOnly(c *gin.Context) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		if c.GetHeader("Content-Type") != "application/json" {
			c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": "expected application/json"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	}
}

// serviceShorts returns the JustWatch codes of the user's services. Configs
// saved by older versions lack them, so those are looked up again.
func serviceShorts(cfg Config) []string {
	var out []string
	for _, s := range cfg.Services {
		if s.Short != "" {
			out = append(out, s.Short)
		}
	}
	if len(out) == len(cfg.Services) {
		return out
	}
	out = nil
	if all, err := Services(cfg.Country); err == nil {
		for _, s := range all {
			if cfg.Has(s.ID) {
				out = append(out, s.Short)
			}
		}
	}
	return out
}

func handleMeta(c *gin.Context) {
	genres, err := Genres()
	if err != nil {
		jsonErr(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"config": LoadConfig(), "configured": configExists(), "genres": genres, "countries": countries})
}

func handleServices(c *gin.Context) {
	list, err := Services(c.Query("country"))
	if err != nil {
		jsonErr(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func handleSaveConfig(c *gin.Context) {
	var cfg Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		jsonErr(c, http.StatusBadRequest, err)
		return
	}
	if !slices.ContainsFunc(countries, func(x struct{ Code, Name string }) bool { return x.Code == cfg.Country }) {
		jsonErr(c, http.StatusBadRequest, fmt.Errorf("unknown country %q", cfg.Country))
		return
	}
	if err := SaveConfig(cfg); err != nil {
		jsonErr(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, cfg)
}

func handleMark(c *gin.Context) {
	var req struct {
		List   string `json:"list"`
		Title  string `json:"title"`
		Year   int    `json:"year"`
		IMDbID string `json:"imdbId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, err)
		return
	}
	file, ok := markFiles[req.List]
	if !ok || strings.TrimSpace(req.Title) == "" {
		jsonErr(c, http.StatusBadRequest, errors.New("need a known list and a title"))
		return
	}
	markMu.Lock()
	defer markMu.Unlock()
	entries := loadEntries(file)
	key := movieKey(req.Title, req.Year, req.IMDbID)
	_, had := entries[key]
	if had {
		delete(entries, key)
	} else {
		entries[key] = WatchedEntry{req.Title, req.Year, req.IMDbID, time.Now()}
	}
	if err := saveEntries(file, entries); err != nil {
		jsonErr(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"on": !had})
}

// handleSearch mirrors the TUI's search: the server-side filters go to
// JustWatch, availability is trimmed here. Hiding watched/hidden titles and
// sorting are left to the client so toggling them needs no new search.
func handleSearch(c *gin.Context) {
	cfg := LoadConfig()
	title := strings.TrimSpace(c.Query("title"))
	year := strings.TrimSpace(c.Query("year"))
	filter := map[string]any{}
	if title != "" {
		filter["searchQuery"] = title
	}
	if year != "" {
		lo, hi, ok := parseYears(year)
		if !ok {
			jsonErr(c, http.StatusBadRequest, errors.New("invalid year (use 2010, 2010-2015 or 80s)"))
			return
		}
		filter["releaseYear"] = map[string]int{"min": lo, "max": hi}
	}
	genre := c.Query("genre")
	if genre != "" {
		filter["genres"] = []string{genre}
	}
	rating, _ := strconv.ParseFloat(c.Query("rating"), 64)
	if rating > 0 {
		filter["imdbScore"] = map[string]float64{"min": rating}
	}
	stock := indexOf(stockNames, c.Query("stock"))
	if stock < 0 {
		stock = StockAvail
	}
	shorts := serviceShorts(cfg)
	if stock == StockMine && len(shorts) == 0 {
		stock = StockAvail // nothing to be "in stock" on
	}
	applyStock(filter, stock, shorts)

	onlyList := c.Query("list") == "1"
	var movies []Movie
	var total int
	var err error
	genreName := ""
	if onlyList {
		movies, err = FindWatchlist(LoadWatchlist(), cfg.Country)
		total = len(movies)
		if genre != "" && err == nil {
			var gs []Genre
			if gs, err = Genres(); err == nil {
				for _, g := range gs {
					if g.Short == genre {
						genreName = g.Name
					}
				}
			}
		}
	} else {
		movies, total, err = Find(filter, cfg.Country, stockPool(stock))
	}
	if err != nil {
		jsonErr(c, http.StatusBadGateway, err)
		return
	}

	watched, hidden, listed := LoadWatched(), LoadHidden(), LoadWatchlist()
	out := make([]apiMovie, 0, len(movies))
	for _, m := range movies {
		if !matchStock(m, stock) || (onlyList && !watchlistMatch(m, title, year, genreName, rating)) {
			continue
		}
		am := apiMovie{Title: m.Title, Year: m.Year, Overview: m.Overview, IMDbID: m.IMDbID, Genres: m.Genres, JWURL: m.JWURL,
			Poster: m.Poster, IMDbScore: m.IMDbScore, Votes: m.IMDbVotes, RT: m.RT, RentBuy: m.RentBuy,
			Streaming: []apiStream{}, Watched: watched.Has(m), Hidden: hidden.Has(m), Listed: listed.Has(m)}
		for _, s := range m.Streaming {
			mine := cfg.Has(s.ID)
			am.Mine = am.Mine || mine
			am.Streaming = append(am.Streaming, apiStream{s.Name, s.URL, mine})
		}
		out = append(out, am)
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "fetched": len(movies), "movies": out})
}

// lanURLs lists the addresses other devices on the network can use.
func lanURLs(port string) []string {
	var urls []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			urls = append(urls, "http://"+ipn.IP.String()+":"+port)
		}
	}
	return urls
}

func runServer(args []string) {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := f.String("addr", ":8080", "listen address; use 127.0.0.1:8080 to stay local")
	webhook := f.String("webhook", "", "Home Assistant webhook URL, called when a watchlist title starts streaming on your services")
	every := f.Duration("every", time.Hour, "how often to check the watchlist (with -webhook)")
	if err := f.Parse(args); err != nil {
		fail("%v", err)
	}
	dist, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		fail("%v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), jsonOnly)
	api := r.Group("/api")
	api.GET("/meta", handleMeta)
	api.GET("/services", handleServices)
	api.PUT("/config", handleSaveConfig)
	api.GET("/search", handleSearch)
	api.POST("/mark", handleMark)
	// The React app: anything that isn't an API route is a static file.
	files := http.FileServer(http.FS(dist))
	r.NoRoute(func(c *gin.Context) { files.ServeHTTP(c.Writer, c.Request) })

	if *webhook != "" {
		go watchWatchlist(*webhook, *every)
	}

	_, port, _ := net.SplitHostPort(*addr)
	fmt.Println("aisle7 web is running (anyone on your network can use it; there is no login)")
	fmt.Println("  this computer: http://localhost:" + port)
	for _, u := range lanURLs(port) {
		fmt.Println("  network:       " + u)
	}
	if *webhook != "" {
		fmt.Println("  watchlist alerts: on, every " + every.String())
	}
	fail("%v", r.Run(*addr))
}
