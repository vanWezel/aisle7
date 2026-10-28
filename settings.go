package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// countries JustWatch covers, alphabetical by name for the dropdown.
var countries = []struct{ Code, Name string }{
	{"AR", "Argentina"}, {"AU", "Australia"}, {"AT", "Austria"}, {"BE", "Belgium"}, {"BR", "Brazil"},
	{"CA", "Canada"}, {"CL", "Chile"}, {"CO", "Colombia"}, {"CZ", "Czechia"}, {"DK", "Denmark"},
	{"EC", "Ecuador"}, {"FI", "Finland"}, {"FR", "France"}, {"DE", "Germany"}, {"GR", "Greece"},
	{"HK", "Hong Kong"}, {"HU", "Hungary"}, {"IN", "India"}, {"ID", "Indonesia"}, {"IE", "Ireland"},
	{"IT", "Italy"}, {"JP", "Japan"}, {"MY", "Malaysia"}, {"MX", "Mexico"}, {"NL", "Netherlands"},
	{"NZ", "New Zealand"}, {"NO", "Norway"}, {"PE", "Peru"}, {"PH", "Philippines"}, {"PL", "Poland"},
	{"PT", "Portugal"}, {"RO", "Romania"}, {"RU", "Russia"}, {"SG", "Singapore"}, {"ZA", "South Africa"},
	{"KR", "South Korea"}, {"ES", "Spain"}, {"SE", "Sweden"}, {"CH", "Switzerland"}, {"TW", "Taiwan"},
	{"TH", "Thailand"}, {"TR", "Türkiye"}, {"GB", "United Kingdom"}, {"US", "United States"}, {"VE", "Venezuela"},
}

type servicesMsg struct {
	country string
	list    []Service
	err     error
}

type settingsState struct {
	open      bool
	firstRun  bool
	country   int // index into countries
	services  []Service
	selected  map[int]bool // by service ID
	filter    textinput.Model
	filtering bool
	cursor    int
	offset    int
	loading   bool
	err       string
}

func configExists() bool {
	p, err := configPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// prepareSettings sets up the dialog from the saved config.
func (m *tuiModel) prepareSettings(first bool) {
	idx := 0
	for i, c := range countries {
		if c.Code == "NL" && idx == 0 {
			idx = i
		}
		if c.Code == m.cfg.Country {
			idx = i
			break
		}
	}
	f := textinput.New()
	f.Prompt, f.Placeholder, f.Width, f.CharLimit = "", "type to filter", 20, 40
	styleInput(&f)
	sel := map[int]bool{}
	for _, s := range m.cfg.Services {
		sel[s.ID] = true
	}
	m.set = settingsState{open: true, firstRun: first, country: idx, selected: sel, filter: f, loading: true}
}

func loadServices(code string) tea.Cmd {
	return func() tea.Msg {
		list, err := Services(code)
		return servicesMsg{code, list, err}
	}
}

func (m tuiModel) visibleServices() []Service {
	q := strings.ToLower(strings.TrimSpace(m.set.filter.Value()))
	if q == "" {
		return m.set.services
	}
	var out []Service
	for _, s := range m.set.services {
		if strings.Contains(strings.ToLower(s.Name), q) {
			out = append(out, s)
		}
	}
	return out
}

func (m tuiModel) settingsListRows() int { return max(3, m.h-12) }

func (m *tuiModel) scrollSettings() {
	rows := m.settingsListRows()
	s := &m.set
	s.cursor = max(0, min(s.cursor, len(m.visibleServices())-1))
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+rows {
		s.offset = s.cursor - rows + 1
	}
	s.offset = max(0, s.offset)
}

func (m tuiModel) changeCountry(d int) (tea.Model, tea.Cmd) {
	s := &m.set
	s.country = (s.country + d + len(countries)) % len(countries)
	s.selected, s.services = map[int]bool{}, nil // service ids differ per country
	s.loading, s.err = true, ""
	s.cursor, s.offset = 0, 0
	return m, loadServices(countries[s.country].Code)
}

func (m tuiModel) applySettings() (tea.Model, tea.Cmd) {
	s := &m.set
	var chosen []Service
	var shorts []string
	for _, sv := range s.services {
		if s.selected[sv.ID] {
			chosen = append(chosen, sv)
			shorts = append(shorts, sv.Short)
		}
	}
	m.cfg.Country, m.cfg.Services = countries[s.country].Code, chosen
	if err := SaveConfig(m.cfg); err != nil {
		m.setMsg("saving settings: "+err.Error(), true)
	} else {
		m.setMsg(fmt.Sprintf("Saved: %s, %d service(s)", m.cfg.Country, len(chosen)), false)
	}
	m.shorts = shorts
	if m.stockIdx == StockMine && len(shorts) == 0 {
		m.stockIdx = StockAvail
	}
	s.open = false
	return m, m.search()
}

func (m tuiModel) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.set
	if s.filtering {
		switch msg.String() {
		case "enter", "esc":
			s.filtering = false
			s.filter.Blur()
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		s.filter, cmd = s.filter.Update(msg)
		s.cursor, s.offset = 0, 0
		return m, cmd
	}
	vis := m.visibleServices()
	rows := m.settingsListRows()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		if s.firstRun {
			_ = SaveConfig(m.cfg) // remember that setup was seen
		}
		s.open = false
		return m, nil
	case "enter":
		return m.applySettings()
	case "left", "h":
		return m.changeCountry(-1)
	case "right", "l":
		return m.changeCountry(1)
	case "up", "k":
		s.cursor--
	case "down", "j":
		s.cursor++
	case "pgup":
		s.cursor -= rows
	case "pgdown":
		s.cursor += rows
	case "home":
		s.cursor = 0
	case "end":
		s.cursor = len(vis) - 1
	case " ", "x":
		if s.cursor >= 0 && s.cursor < len(vis) {
			id := vis[s.cursor].ID
			s.selected[id] = !s.selected[id]
		}
	case "/":
		s.filtering = true
		return m, s.filter.Focus()
	}
	m.scrollSettings()
	return m, nil
}

func (m tuiModel) settingsView() string {
	s := m.set
	g, wh := gray(), white()
	W := m.w
	gap := spaces(g, W)

	country := fmt.Sprintf(" %s (%s)", countries[s.country].Name, countries[s.country].Code)
	countryRow := g.Render(" ") + hot("Country", 0, g) + g.Render(" ") + padTo(wh.Render(country), 30, wh) +
		g.Bold(true).Render("▼") + g.Render("   ") + g.Foreground(cGrey).Render("←/→ change")

	picked := 0
	for _, sv := range s.services {
		if s.selected[sv.ID] {
			picked++
		}
	}
	label := g.Render(fmt.Sprintf(" Streaming services you subscribe to (%d selected)", picked))
	ls := g
	if s.filtering {
		ls = g.Bold(true).Foreground(cYellow)
	}
	find := hot("Find", 0, ls) + g.Render(" ") + padTo(strings.TrimRight(s.filter.View(), " "), 22, wh) + g.Render(" ")
	labelRow := label + spaces(g, W-lipgloss.Width(label)-lipgloss.Width(find)) + find

	iw, rows := W-3, m.settingsListRows()
	vis := m.visibleServices()
	var lines []string
	switch {
	case s.loading:
		lines = append(lines, padTo(wh.Render(" Loading services…"), iw, wh))
	case s.err != "":
		lines = append(lines, padTo(wh.Foreground(cRed).Render(" "+truncate(s.err, iw-2)), iw, wh))
	case len(vis) == 0:
		lines = append(lines, padTo(wh.Render(" No services match."), iw, wh))
	default:
		for i := s.offset; i < min(len(vis), s.offset+rows); i++ {
			box := "[ ]"
			if s.selected[vis[i].ID] {
				box = "[x]"
			}
			text := " " + box + " " + truncate(vis[i].Name, iw-6)
			if i == s.cursor {
				lines = append(lines, lipgloss.NewStyle().Background(wNavy).Foreground(cYellow).Bold(true).Width(iw).Render(text))
			} else {
				lines = append(lines, padTo(wh.Render(text), iw, wh))
			}
		}
	}
	for len(lines) < rows {
		lines = append(lines, spaces(wh, iw))
	}
	bar := scrollbar(rows, len(vis), s.offset)
	for i := range lines {
		lines[i] += bar[i]
	}

	buttons := hstack(g, g.Render(" "), raised(hot("OK", 0, g)), g.Render("  "), raised(hot("Cancel", 0, g)))
	hint := "↑↓ Move  Space Toggle  / Find  ←→ Country  Enter OK  Esc Cancel"
	if s.firstRun {
		hint = "Welcome! Pick your country and the services you subscribe to, then press Enter."
	}

	out := []string{m.chromeBar(" ★ SETTINGS ★  —  Your Video Store ", ""), gap, padTo(countryRow, W, g), gap, padTo(labelRow, W, g)}
	out = append(out, strings.Split(sunken(strings.Join(lines, "\n")), "\n")...)
	out = append(out, gap)
	for _, l := range strings.Split(buttons, "\n") {
		out = append(out, padTo(l, W, g))
	}
	out = append(out, padTo(g.Render(" "+hint), W, g))
	for i, r := range out {
		out[i] = lipgloss.NewStyle().MaxWidth(W).Render(r)
	}
	return strings.Join(out, "\n")
}
