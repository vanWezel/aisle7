package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var sortModes = []struct{ key, label string }{
	{"popular", "popularity"},
	{"imdb", "IMDb score"},
	{"rt", "Rotten Tomatoes"},
}

// rankMovies orders movies for the imdb/rt sorts, keeping only titles with
// enough IMDb votes to be meaningful. "popular" keeps JustWatch's order.
func rankMovies(movies []Movie, sortBy string) []Movie {
	if sortBy != "imdb" && sortBy != "rt" {
		return movies
	}
	var kept []Movie
	for _, m := range movies {
		if m.IMDbVotes >= minVotes {
			kept = append(kept, m)
		}
	}
	score := func(m Movie) float64 {
		if sortBy == "rt" {
			if m.RT == nil {
				return -1
			}
			return float64(*m.RT)
		}
		if m.IMDbScore == nil {
			return -1
		}
		return *m.IMDbScore
	}
	sort.SliceStable(kept, func(i, j int) bool { return score(kept[i]) > score(kept[j]) })
	return kept
}

var ratingSteps = []float64{0, 6, 7, 8}

type posterMsg struct{ url, art string }

type metaMsg struct {
	genres []Genre
	shorts []string // JustWatch codes for the user's services
	err    error
}

type resultMsg struct {
	seq    int
	movies []Movie
	total  int
	err    error
}

type tuiModel struct {
	cfg        Config
	watched    Watched
	hidden     Hidden
	watchlist  Watchlist
	onlyList   bool
	wantGenre  string // genre name from the command line, resolved once genres load
	showHidden bool
	w, h       int

	query, year textinput.Model
	focus       int // 0 list, 1 query, 2 year
	genres      []Genre
	genreIdx    int // 0 = any genre, otherwise genres[genreIdx-1]
	shorts      []string
	stockIdx    int // see stockLabels
	hideSeen    bool
	sortIdx     int
	ratingIdx   int // index into ratingSteps; 0 = any

	set      settingsState
	posters  map[string]string // cover art by URL
	pending  map[string]bool
	raw      []Movie // as fetched
	movies   []Movie // after local filtering and sorting
	total    int
	cursor   int
	offset   int
	loading  bool
	seq      int
	msg      string
	msgIsErr bool
}

func runTUI(o startOpts) {
	q := textinput.New()
	q.Prompt, q.Placeholder, q.Width, q.CharLimit = "", "any title", 18, 60
	styleInput(&q)
	y := textinput.New()
	y.Prompt, y.Placeholder, y.Width, y.CharLimit = "", "80s, 2010", 9, 9
	styleInput(&y)
	posterFill = white()
	m := tuiModel{cfg: LoadConfig(), watched: LoadWatched(), hidden: LoadHidden(), watchlist: LoadWatchlist(), query: q, year: y, loading: true,
		posters: map[string]string{}, pending: map[string]bool{}}
	m.query.SetValue(o.title)
	m.year.SetValue(o.year)
	m.wantGenre = o.genre
	m.ratingIdx = slices.Index(ratingSteps, o.rating)
	m.stockIdx, m.sortIdx, m.hideSeen, m.onlyList = o.stock, o.sort, o.unseen, o.watchlist
	if !configExists() {
		m.prepareSettings(true)
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fail("%v", err)
	}
}

func (m tuiModel) Init() tea.Cmd {
	country, cfg := m.cfg.Country, m.cfg
	meta := func() tea.Msg {
		genres, err := Genres()
		if err != nil {
			return metaMsg{err: err}
		}
		var shorts []string
		if len(cfg.Services) > 0 {
			if all, err := Services(country); err == nil {
				for _, s := range all {
					if cfg.Has(s.ID) {
						shorts = append(shorts, s.Short)
					}
				}
			}
		}
		return metaMsg{genres: genres, shorts: shorts}
	}
	if m.set.open {
		return tea.Batch(meta, loadServices(countries[m.set.country].Code))
	}
	return meta
}

// search fetches with the current query, year, genre and "mine" settings.
func (m *tuiModel) search() tea.Cmd {
	filter := map[string]any{}
	if q := strings.TrimSpace(m.query.Value()); q != "" {
		filter["searchQuery"] = q
	}
	if y := strings.TrimSpace(m.year.Value()); y != "" {
		lo, hi, ok := parseYears(y)
		if !ok {
			m.setMsg("invalid year (use 2010, 2010-2015 or 80s)", true)
			return nil
		}
		filter["releaseYear"] = map[string]int{"min": lo, "max": hi}
	}
	if m.genreIdx > 0 {
		filter["genres"] = []string{m.genres[m.genreIdx-1].Short}
	}
	if r := ratingSteps[m.ratingIdx]; r > 0 {
		filter["imdbScore"] = map[string]float64{"min": r}
	}
	applyStock(filter, m.stockIdx, m.shorts)
	m.seq++
	m.loading, m.msg = true, ""
	seq, country, pool := m.seq, m.cfg.Country, stockPool(m.stockIdx)
	if m.onlyList {
		list := m.watchlist
		return func() tea.Msg {
			movies, err := FindWatchlist(list, country)
			return resultMsg{seq, movies, len(movies), err}
		}
	}
	return func() tea.Msg {
		movies, total, err := Find(filter, country, pool)
		return resultMsg{seq, movies, total, err}
	}
}

func (m *tuiModel) setMsg(s string, isErr bool) { m.msg, m.msgIsErr = s, isErr }

// refresh applies the local filters (hide watched, sort) to the fetched list.
func (m *tuiModel) refresh() {
	var list []Movie
	for _, mv := range m.raw {
		if (m.onlyList && !m.localMatch(mv)) || (m.hideSeen && m.watched.Has(mv)) || (!m.showHidden && m.hidden.Has(mv)) || !matchStock(mv, m.stockIdx) {
			continue
		}
		list = append(list, mv)
	}
	m.movies = rankMovies(list, sortModes[m.sortIdx].key)
	m.cursor = max(0, min(m.cursor, len(m.movies)-1))
	m.scroll()
}

// localMatch applies the title, year, genre and rating filters to a watchlist
// entry; the server can't, because the list is looked up title by title.
func (m tuiModel) localMatch(mv Movie) bool {
	genre := ""
	if m.genreIdx > 0 {
		genre = m.genres[m.genreIdx-1].Name
	}
	return watchlistMatch(mv, m.query.Value(), m.year.Value(), genre, ratingSteps[m.ratingIdx])
}

// headerGaps is the number of blank chrome rows between the title bar, the two
// filter rows and the panes.
const headerGaps = 3

// bodyHeight is the height of the list and detail panes.
func (m tuiModel) bodyHeight() int { return m.h - 4 - headerGaps }

func (m tuiModel) listRows() int { return max(3, m.bodyHeight()-2) }

func (m *tuiModel) scroll() {
	rows := m.listRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, m.offset)
}

func (m *tuiModel) setFocus(f int) tea.Cmd {
	m.focus = f
	m.query.Blur()
	m.year.Blur()
	switch f {
	case 1:
		return m.query.Focus()
	case 2:
		return m.year.Focus()
	}
	return nil
}

// findGenre matches a name or code exactly (ignoring case), then by prefix or
// substring, so "sci" or "horror" work.
func findGenre(genres []Genre, q string) int {
	q = strings.ToLower(strings.TrimSpace(q))
	for _, test := range []func(g Genre) bool{
		func(g Genre) bool { return strings.ToLower(g.Name) == q || g.Short == q },
		func(g Genre) bool { return strings.HasPrefix(strings.ToLower(g.Name), q) },
		func(g Genre) bool { return strings.Contains(strings.ToLower(g.Name), q) },
	} {
		for i, g := range genres {
			if test(g) {
				return i
			}
		}
	}
	return -1
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.scroll()
		return m, nil

	case metaMsg:
		if msg.err != nil {
			m.setMsg(msg.err.Error(), true)
			m.loading = false
			return m, nil
		}
		m.genres, m.shorts = msg.genres, msg.shorts
		var genreErr string
		if g := m.wantGenre; g != "" {
			m.wantGenre = ""
			if i := findGenre(m.genres, g); i >= 0 {
				m.genreIdx = i + 1
			} else {
				names := make([]string, len(m.genres))
				for i, x := range m.genres {
					names[i] = x.Name
				}
				genreErr = "unknown genre " + g + " (try: " + strings.Join(names, ", ") + ")"
			}
		}
		cmd := m.search()
		if genreErr != "" {
			m.setMsg(genreErr, true)
		}
		return m, cmd

	case servicesMsg:
		if !m.set.open || msg.country != countries[m.set.country].Code {
			return m, nil // stale: the country changed again
		}
		m.set.loading = false
		if msg.err != nil {
			m.set.err = msg.err.Error()
			return m, nil
		}
		m.set.services = msg.list
		return m, nil

	case posterMsg:
		m.posters[msg.url] = msg.art
		delete(m.pending, msg.url)
		return m, nil

	case resultMsg:
		if msg.seq != m.seq {
			return m, nil // a newer search is in flight
		}
		m.loading = false
		if msg.err != nil {
			m.setMsg(msg.err.Error(), true)
			return m, nil
		}
		m.raw, m.total = msg.movies, msg.total
		m.cursor, m.offset = 0, 0
		m.refresh()
		return m, m.loadPosters()

	case tea.KeyMsg:
		if m.set.open {
			return m.updateSettings(msg)
		}
		if m.focus != 0 {
			return m.updateInput(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m tuiModel) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		cmd := m.setFocus(0)
		return m, tea.Batch(cmd, m.search())
	case "esc":
		return m, m.setFocus(0)
	case "tab":
		return m, m.setFocus(3 - m.focus) // swap between the two inputs
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	if m.focus == 1 {
		m.query, cmd = m.query.Update(msg)
	} else {
		m.year, cmd = m.year.Update(msg)
	}
	return m, cmd
}

func (m tuiModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.movies)-1, m.cursor+1)
	case "pgup":
		m.cursor = max(0, m.cursor-m.listRows())
	case "pgdown":
		m.cursor = max(0, min(len(m.movies)-1, m.cursor+m.listRows()))
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = max(0, len(m.movies)-1)
	case "/", "t":
		return m, m.setFocus(1)
	case "y":
		return m, m.setFocus(2)
	case "g", "G":
		if len(m.genres) > 0 {
			step := 1
			if msg.String() == "G" {
				step = len(m.genres)
			}
			m.genreIdx = (m.genreIdx + step) % (len(m.genres) + 1)
			return m, m.search()
		}
	case "r":
		m.ratingIdx = (m.ratingIdx + 1) % len(ratingSteps)
		return m, m.search()
	case "s":
		m.sortIdx = (m.sortIdx + 1) % len(sortModes)
		m.cursor, m.offset = 0, 0
		m.refresh()
	case "u":
		m.hideSeen = !m.hideSeen
		m.refresh()
	case "m":
		if len(m.cfg.Services) == 0 || len(m.shorts) == 0 {
			m.setMsg("no services saved yet; press S to choose the services you subscribe to", true)
			break
		}
		if m.stockIdx == StockMine {
			m.stockIdx = StockAvail
		} else {
			m.stockIdx = StockMine
		}
		return m, m.search()
	case "a":
		next := (m.stockIdx + 1) % len(stockLabels)
		if next == StockMine && len(m.shorts) == 0 {
			next++ // no saved services to be "in stock" on
		}
		m.stockIdx = next
		return m, m.search()
	case "S":
		m.prepareSettings(false)
		return m, loadServices(countries[m.set.country].Code)
	case "c":
		m.query.SetValue("")
		m.year.SetValue("")
		m.genreIdx, m.ratingIdx, m.stockIdx, m.hideSeen, m.showHidden, m.onlyList = 0, 0, StockAvail, true, false, false
		return m, m.search()
	case "w":
		m.toggleWatched()
	case "h":
		m.toggleHidden()
	case "b":
		m.toggleWatchlist()
	case "B":
		m.onlyList = !m.onlyList
		return m, m.search()
	case "H":
		m.showHidden = !m.showHidden
		m.refresh()
	case "enter", "p":
		if len(m.movies) == 0 {
			break
		}
		if s, ok := bestStream(m.movies[m.cursor], m.cfg); ok {
			openURL(s.URL)
			m.setMsg("Opening "+s.Name+"…", false)
		} else {
			m.setMsg("Not streaming here; try l for the JustWatch page (rent/buy options)", true)
		}
	case "l":
		if len(m.movies) > 0 && m.movies[m.cursor].JWURL != "" {
			openURL(m.movies[m.cursor].JWURL)
			m.setMsg("Opening JustWatch…", false)
		}
	case "o":
		if len(m.movies) > 0 && m.movies[m.cursor].IMDbID != "" {
			openURL("https://www.imdb.com/title/" + m.movies[m.cursor].IMDbID + "/")
		}
	}
	m.scroll()
	return m, m.loadPosters()
}

// loadPosters fetches covers for the selected movie and its neighbours.
func (m *tuiModel) loadPosters() tea.Cmd {
	if !postersEnabled() {
		return nil
	}
	var cmds []tea.Cmd
	for i := m.cursor - 1; i <= m.cursor+2; i++ {
		if i < 0 || i >= len(m.movies) {
			continue
		}
		u := m.movies[i].Poster
		if _, ok := m.posters[u]; ok || m.pending[u] {
			continue
		}
		m.pending[u] = true
		cmds = append(cmds, func() tea.Msg { return posterMsg{u, fetchPoster(u)} })
	}
	return tea.Batch(cmds...)
}

func (m *tuiModel) toggleWatched() {
	if len(m.movies) == 0 {
		return
	}
	mv := m.movies[m.cursor]
	key := movieKey(mv.Title, mv.Year, mv.IMDbID)
	if m.watched.Has(mv) {
		delete(m.watched, key)
		m.setMsg("Removed from watched: "+mv.Title, false)
	} else {
		m.watched[key] = WatchedEntry{mv.Title, mv.Year, mv.IMDbID, time.Now()}
		m.setMsg("✓ Marked as watched: "+mv.Title, false)
	}
	if err := m.watched.Save(); err != nil {
		m.setMsg("saving watched list: "+err.Error(), true)
	}
	if m.hideSeen {
		m.refresh()
	}
}

func (m *tuiModel) toggleWatchlist() {
	if len(m.movies) == 0 {
		return
	}
	mv := m.movies[m.cursor]
	key := movieKey(mv.Title, mv.Year, mv.IMDbID)
	if m.watchlist.Has(mv) {
		delete(m.watchlist, key)
		m.setMsg("Removed from watchlist: "+mv.Title, false)
	} else {
		m.watchlist[key] = WatchedEntry{mv.Title, mv.Year, mv.IMDbID, time.Now()}
		m.setMsg("+ Added to watchlist: "+mv.Title+"  (B shows only your watchlist)", false)
	}
	if err := m.watchlist.Save(); err != nil {
		m.setMsg("saving watchlist: "+err.Error(), true)
	}
	if m.onlyList {
		m.raw = slices.DeleteFunc(slices.Clone(m.raw), func(x Movie) bool { return !m.watchlist.Has(x) })
		m.refresh()
	}
}

func (m *tuiModel) toggleHidden() {
	if len(m.movies) == 0 {
		return
	}
	mv := m.movies[m.cursor]
	key := movieKey(mv.Title, mv.Year, mv.IMDbID)
	if m.hidden.Has(mv) {
		delete(m.hidden, key)
		m.setMsg("Unhidden: "+mv.Title, false)
	} else {
		m.hidden[key] = WatchedEntry{mv.Title, mv.Year, mv.IMDbID, time.Now()}
		m.setMsg("✗ Hidden: "+mv.Title+"  (H shows hidden titles)", false)
	}
	if err := m.hidden.Save(); err != nil {
		m.setMsg("saving hidden list: "+err.Error(), true)
	}
	m.refresh()
}

func openURL(url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}

// ---- rendering: an old-school Windows 3.x window in Blockbuster colours ----

var (
	wGray  = lipgloss.Color("#161616") // window chrome
	wPane  = lipgloss.Color("#000000") // list and detail panes, inputs
	wText  = lipgloss.Color("#E6E6E6")
	wDark  = lipgloss.Color("#000000") // bevel shadow (sunken top/left)
	wShade = lipgloss.Color("#000000") // bevel shadow (raised bottom/right)
	wLight = lipgloss.Color("#7A7A7A") // bevel highlight, scrollbar thumb
	wTrack = lipgloss.Color("#4A4A4A") // scrollbar track, placeholders
	wNavy  = lipgloss.Color("#000080")
)

func gray() lipgloss.Style  { return lipgloss.NewStyle().Background(wGray).Foreground(wText) }
func white() lipgloss.Style { return lipgloss.NewStyle().Background(wPane).Foreground(wText) }

func spaces(st lipgloss.Style, n int) string {
	if n <= 0 {
		return ""
	}
	return st.Render(strings.Repeat(" ", n))
}

func padTo(s string, w int, st lipgloss.Style) string { return s + spaces(st, w-lipgloss.Width(s)) }

func styleInput(in *textinput.Model) {
	wh := white()
	in.TextStyle, in.PromptStyle = wh, wh
	in.PlaceholderStyle = wh.Foreground(wTrack)
	in.Cursor.Style = lipgloss.NewStyle().Reverse(true)
	in.Cursor.TextStyle = wh
}

// hot renders label with the character at idx underlined, like a menu hotkey.
func hot(label string, idx int, st lipgloss.Style) string {
	r := []rune(label)
	if idx < 0 || idx >= len(r) {
		return st.Render(label)
	}
	return st.Render(string(r[:idx])) + st.Underline(true).Render(string(r[idx])) + st.Render(string(r[idx+1:]))
}

func bevel(content string, tl, br lipgloss.Color, inner lipgloss.Style) string {
	return inner.Border(lipgloss.NormalBorder()).
		BorderTopForeground(tl).BorderLeftForeground(tl).
		BorderBottomForeground(br).BorderRightForeground(br).
		BorderBackground(wGray).Render(content)
}

func sunken(content string) string {
	return bevel(content, wDark, wLight, lipgloss.NewStyle())
}

func raised(content string) string {
	return bevel(content, wLight, wShade, gray().Padding(0, 1))
}

func ratingLabel(r float64) string {
	if r == 0 {
		return "any"
	}
	return fmt.Sprintf("≥%.0f", r)
}

func (m tuiModel) onMine(mv Movie) bool {
	for _, s := range mv.Streaming {
		if m.cfg.Has(s.ID) {
			return true
		}
	}
	return false
}

func (m tuiModel) View() string {
	if m.w == 0 {
		return ""
	}
	if m.set.open {
		return m.settingsView()
	}
	bodyH := m.bodyHeight()
	gap := spaces(gray(), m.w)
	lw := max(34, m.w*4/10)
	if postersEnabled() {
		lw = max(34, m.w*32/100) // leave room for the cover
	}
	rows := []string{m.titleBar(), gap}
	rows = append(rows, strings.Split(m.toolbar(), "\n")...)
	rows = append(rows, gap)
	rows = append(rows, strings.Split(hstack(gray(), m.listPane(lw, bodyH), m.detailPane(m.w-lw, bodyH)), "\n")...)
	rows = append(rows, m.statusBar())
	for i, r := range rows {
		rows[i] = lipgloss.NewStyle().MaxWidth(m.w).Render(r)
	}
	return strings.Join(rows, "\n")
}

// statusText is the country and match count shown in the title bar.
func (m tuiModel) statusText() string {
	status := fmt.Sprintf("%s │ %d matches", m.cfg.Country, m.total)
	if needsLocalStock(m.stockIdx) {
		status = fmt.Sprintf("%s │ %d shown of top %d", m.cfg.Country, len(m.movies), len(m.raw))
	}
	if m.loading {
		status = m.cfg.Country + " │ searching…"
	}
	return status
}

func (m tuiModel) titleBar() string {
	return m.chromeBar(" ★ MOVIE RENTAL ★  —  Video Guide ", m.statusText())
}

// chromeBar draws the navy window title bar with its little buttons.
func (m tuiModel) chromeBar(text, status string) string {
	nav := lipgloss.NewStyle().Background(wNavy).Foreground(cYellow).Bold(true)
	btn := func(s string) string { return gray().Bold(true).Render(" " + s + " ") }
	left, right := btn("▬"), btn("▼")+btn("▲")
	title := nav.Render(text)
	info := ""
	if status != "" {
		info = nav.Foreground(cWhite).Bold(false).Render(status + " ")
	}
	return left + title + spaces(nav, m.w-lipgloss.Width(left)-lipgloss.Width(title)-lipgloss.Width(info)-lipgloss.Width(right)) + info + right
}

// field draws "Label [ sunken box ]" with the label's first letter as hotkey.
func (m tuiModel) field(label string, focused bool, box string, w int, arrow bool) string {
	g := gray()
	ls := g
	if focused {
		ls = g.Bold(true).Foreground(cYellow)
	}
	out := hot(label, 0, ls) + g.Render(" ") + padTo(box, w, white())
	if arrow {
		out += gray().Bold(true).Render("▼")
	}
	return out
}

func (m tuiModel) checkbox(label string, on bool) string {
	mark := " "
	if on {
		mark = "x"
	}
	return gray().Render("["+mark+"] ") + hot(label, 0, gray())
}

func (m tuiModel) toolbar() string {
	g, wh := gray(), white()
	genre := "any"
	if m.genreIdx > 0 {
		genre = m.genres[m.genreIdx-1].Name
	}
	gap := spaces(g, 3)
	rowA := g.Render(" ") +
		m.field("Title", m.focus == 1, strings.TrimRight(m.query.View(), " "), 20, false) + gap +
		m.field("Year", m.focus == 2, strings.TrimRight(m.year.View(), " "), 10, false) + gap +
		m.field("Genre", false, wh.Render(" "+genre), 20, true) + gap +
		m.field("Rating", false, wh.Render(" "+ratingLabel(ratingSteps[m.ratingIdx])), 6, true)
	rowB := g.Render(" ") +
		m.field("Sort", false, wh.Render(" "+sortModes[m.sortIdx].label), 17, true) + gap +
		m.field("Availability", false, wh.Render(" "+stockLabels[m.stockIdx]), 20, true) + gap + m.checkbox("Unseen", m.hideSeen) + gap + m.checkbox("Hidden", m.showHidden) + gap + m.checkbox("Watchlist", m.onlyList)
	return padTo(rowA, m.w, g) + "\n" + spaces(g, m.w) + "\n" + padTo(rowB, m.w, g)
}

func (m tuiModel) statusBar() string {
	g := gray()
	text := g.Render(" ") + g.Render("↑↓ Move  Enter Play  L Lookup  O IMDb  A Stock  W Watched  B Watchlist  H Hide  S Settings  C Clear  Q Quit")
	switch {
	case m.msg != "" && m.msgIsErr:
		text = g.Render(" ") + g.Foreground(cRed).Bold(true).Render(m.msg)
	case m.msg != "":
		text = g.Render(" ") + g.Foreground(cGreen).Bold(true).Render(m.msg)
	case m.focus != 0:
		text = g.Render(" Enter search  Tab next field  Esc cancel")
	}
	return padTo(text, m.w, g)
}

func scrollbar(rows, n, offset int) []string {
	bar := make([]string, rows)
	track := lipgloss.NewStyle().Background(wGray).Foreground(wTrack)
	for i := range bar {
		bar[i] = track.Render("░")
	}
	if rows >= 3 {
		bar[0], bar[rows-1] = gray().Render("▲"), gray().Render("▼")
		if n > rows {
			span := rows - 2
			th := max(1, span*rows/n)
			pos := offset * (span - th) / (n - rows)
			for i := 0; i < th; i++ {
				bar[1+pos+i] = lipgloss.NewStyle().Background(wGray).Foreground(wLight).Render("█")
			}
		}
	}
	return bar
}

func (m tuiModel) listPane(w, h int) string {
	iw, rows := w-3, h-2 // border (2) + scrollbar (1)
	wh := white()
	var lines []string
	switch {
	case m.loading && len(m.movies) == 0:
		lines = append(lines, padTo(wh.Render(" Searching…"), iw, wh))
	case m.msg != "" && m.msgIsErr && len(m.movies) == 0:
		lines = append(lines, padTo(wh.Foreground(cRed).Render(" "+truncate(m.msg, iw-2)), iw, wh))
	case len(m.movies) == 0:
		lines = append(lines, padTo(wh.Render(" No results."), iw, wh))
	default:
		for i := m.offset; i < min(len(m.movies), m.offset+rows); i++ {
			lines = append(lines, m.row(m.movies[i], i == m.cursor, iw))
		}
	}
	for len(lines) < rows {
		lines = append(lines, spaces(wh, iw))
	}
	bar := scrollbar(rows, len(m.movies), m.offset)
	for i := range lines {
		lines[i] += bar[i]
	}
	return sunken(strings.Join(lines, "\n"))
}

func (m tuiModel) row(mv Movie, selected bool, w int) string {
	mark, markColor := " ", cGreen
	if m.hidden.Has(mv) {
		mark, markColor = "✗", cRed
	} else if m.watched.Has(mv) {
		mark = "✓"
	} else if m.watchlist.Has(mv) {
		mark, markColor = "+", cSky
	}
	score := "   "
	if mv.IMDbScore != nil {
		score = fmt.Sprintf("%.1f", *mv.IMDbScore)
	}
	title := mv.Title
	if mv.Year > 0 {
		title += fmt.Sprintf(" (%d)", mv.Year)
	}
	title = truncate(title, max(8, w-10))
	padN := max(1, w-lipgloss.Width(title)-lipgloss.Width(score)-6)

	if selected {
		return lipgloss.NewStyle().Background(wNavy).Foreground(cYellow).Bold(true).Width(w).
			Render(" " + mark + " ● " + title + strings.Repeat(" ", padN) + score + " ")
	}
	dotColor := cRed
	switch {
	case m.onMine(mv):
		dotColor = cGreen
	case len(mv.Streaming) > 0:
		dotColor = cOrange
	case mv.RentBuy:
		dotColor = cSky
	}
	wh := white()
	return wh.Render(" ") + wh.Foreground(markColor).Bold(true).Render(mark) + wh.Render(" ") +
		wh.Foreground(dotColor).Render("●") + wh.Render(" "+title+strings.Repeat(" ", padN)) +
		wh.Foreground(cGrey).Render(score) + wh.Render(" ")
}

func (m tuiModel) detailPane(w, h int) string {
	iw, rows := w-2, h-2
	wh := white()
	var content []string
	if len(m.movies) > 0 {
		mv := m.movies[m.cursor]
		inner := iw - 2
		cw := inner
		usePoster := postersEnabled() && inner-posterCols-2 >= 44
		if usePoster {
			cw = inner - posterCols - 2
		}
		card := renderCardW(mv, m.cfg, m.watched.Has(mv), cw, 600, true, m.hidden.Has(mv), m.watchlist.Has(mv))
		if usePoster {
			art, ok := m.posters[mv.Poster]
			if !ok {
				art = posterPlaceholder("loading…")
			}
			card = withPoster(art, card, wh)
		}
		dot := func(c lipgloss.Color, text string) string {
			return wh.Foreground(c).Render("●") + wh.Foreground(cGrey).Render(" "+text+"  ")
		}
		legend := dot(cGreen, "in stock") + dot(cOrange, "other shelves") + dot(cSky, "rental") + dot(cRed, "out of stock")
		content = append(content, "", card, "", legend)
		if rows-len(strings.Split(card, "\n"))-3 >= 4 {
			watchedLabel, watchedHot := "Watched", 0
			if m.watched.Has(mv) {
				watchedLabel, watchedHot = "Unwatch", 2
			}
			hideLabel, hideHot := "Hide", 0
			if m.hidden.Has(mv) {
				hideLabel, hideHot = "Unhide", 2
			}
			btns := hstack(wh,
				raised(hot("Play", 0, gray())), wh.Render(" "),
				raised(hot("Lookup", 0, gray())), wh.Render(" "),
				raised(hot("Open IMDb", 0, gray())), wh.Render(" "),
				raised(hot(watchedLabel, watchedHot, gray())), wh.Render(" "),
				raised(hot(hideLabel, hideHot, gray())))
			content = append(content, "", btns)
		}
	}
	var lines []string
	for _, block := range content {
		for _, l := range strings.Split(block, "\n") {
			lines = append(lines, wh.Render(" ")+padTo(l, iw-1, wh))
		}
	}
	for len(lines) < rows {
		lines = append(lines, spaces(wh, iw))
	}
	return sunken(strings.Join(lines[:rows], "\n"))
}
