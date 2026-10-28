package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/term"
)

const usage = `aisle7 - an interactive video-rental guide: where to watch a movie, with IMDb and Rotten Tomatoes scores

Usage: aisle7 [options] [title words]
       aisle7 serve [-addr :8080]    the same guide as a web app for your network

Run it in a terminal (at least 110 columns wide for cover images). Options
prefill the search; everything can still be changed inside the app.
  -t TITLE   title search (or just give the words: aisle7 blade runner)
  -y YEARS   2010, 2010-2015 or 80s       -g GENRE   e.g. horror, "science-fiction"
  -r N       minimum IMDb rating: 6, 7 or 8
  -a MODE    availability: any, mine, streaming, rental, both (default), out
  -s SORT    popular, imdb or rt          -u=false  include watched      -w  watchlist only

Keys:
  ↑↓ / j k  move          PgUp PgDn Home End  jump
  / or t    title search  y  year (2010, 2010-2015, 80s)
  g G       next / previous genre
  r         minimum IMDb rating (any, 6, 7, 8)
  a         availability: any, in stock (your services), streaming, rental only,
            streaming + rental, out of stock          m  in stock on/off
  s         sort: popularity, IMDb score, Rotten Tomatoes
  u         hide movies you have watched (on by default)
  w         mark / unmark as watched
  b         add / remove from your watchlist     B  show only the watchlist
  h         hide / unhide a title        H  show hidden titles
  Enter / p open it on a streaming service    l  JustWatch page    o  IMDb page
  S         settings: your country and streaming services
  c         clear filters                q  quit

Data comes from JustWatch's unofficial API; the RT score is the critics' Tomatometer.`

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
	os.Exit(1)
}

// startOpts prefills the search from the command line.
type startOpts struct {
	title, year, genre string
	rating             float64
	stock, sort        int
	unseen, watchlist  bool
}

var stockNames = []string{"any", "mine", "streaming", "rental", "both", "out"}

func indexOf(names []string, s string) int {
	for i, n := range names {
		if strings.EqualFold(n, s) {
			return i
		}
	}
	return -1
}

func parseArgs(args []string) (startOpts, error) {
	var o startOpts
	var stock, sortBy string
	fs := flag.NewFlagSet("aisle7", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.title, "t", "", "")
	fs.StringVar(&o.year, "y", "", "")
	fs.StringVar(&o.genre, "g", "", "")
	fs.Float64Var(&o.rating, "r", 0, "")
	fs.StringVar(&stock, "a", "both", "")
	fs.StringVar(&sortBy, "s", "popular", "")
	fs.BoolVar(&o.unseen, "u", true, "")
	fs.BoolVar(&o.watchlist, "w", false, "")
	if err := fs.Parse(args); err != nil {
		return o, fmt.Errorf("%v (see: aisle7 --help)", err)
	}
	if rest := strings.TrimSpace(strings.Join(fs.Args(), " ")); rest != "" {
		if o.title != "" {
			return o, fmt.Errorf("give the title once, with -t or as plain words")
		}
		o.title = rest
	}
	if o.year != "" {
		if _, _, ok := parseYears(o.year); !ok {
			return o, fmt.Errorf("invalid year %q (use 2010, 2010-2015 or 80s)", o.year)
		}
	}
	if !slices.Contains(ratingSteps, o.rating) {
		return o, fmt.Errorf("invalid rating %v (use 6, 7 or 8)", o.rating)
	}
	if o.stock = indexOf(stockNames, stock); o.stock < 0 {
		return o, fmt.Errorf("invalid availability %q (use %s)", stock, strings.Join(stockNames, ", "))
	}
	for i, sm := range sortModes {
		if strings.EqualFold(sm.key, sortBy) {
			o.sort = i
			return o, nil
		}
	}
	return o, fmt.Errorf("invalid sort %q (use popular, imdb or rt)", sortBy)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "-help", "help":
			fmt.Println(usage)
			return
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		runServer(os.Args[2:])
		return
	}
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fail("%v", err)
	}
	if !isTTY || !term.IsTerminal(int(os.Stdin.Fd())) {
		fail("aisle7 needs an interactive terminal")
	}
	runTUI(opts)
}

const minVotes = 10000

var yearRe = regexp.MustCompile(`^(\d{4})(?:-(\d{4}))?$`)
var decadeRe = regexp.MustCompile(`^(\d{2}|\d{4})s$`)

// parseYears accepts 2010, 2010-2015, or a decade like 80s / 1980s.
// Two-digit decades from 30s up are 1900s, below that 2000s (00s-20s).
func parseYears(s string) (lo, hi int, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if m := decadeRe.FindStringSubmatch(s); m != nil {
		lo, _ = strconv.Atoi(m[1])
		if len(m[1]) == 2 {
			if lo >= 30 {
				lo += 1900
			} else {
				lo += 2000
			}
		}
		if lo%10 != 0 {
			return 0, 0, false
		}
		return lo, lo + 9, true
	}
	m := yearRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	lo, _ = strconv.Atoi(m[1])
	hi = lo
	if m[2] != "" {
		hi, _ = strconv.Atoi(m[2])
	}
	return lo, hi, hi >= lo
}
