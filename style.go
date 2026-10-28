package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// Palette: 80s video-rental-store blue and yellow, with VHS-label accents.
var (
	cBlue   = lipgloss.Color("#1F3FA6")
	cSky    = lipgloss.Color("#5B7FE8")
	cYellow = lipgloss.Color("#FFD100")
	cRed    = lipgloss.Color("#D7263D")
	cGreen  = lipgloss.Color("#2E9E5B")
	cOrange = lipgloss.Color("#E67E22")
	cGrey   = lipgloss.Color("#8A8A99")
	cBlack  = lipgloss.Color("#111111")
	cWhite  = lipgloss.Color("#FFFFFF")
	dim     = lipgloss.NewStyle().Foreground(cGrey)
	bold    = lipgloss.NewStyle().Bold(true)
	okStyle = lipgloss.NewStyle().Foreground(cGreen).Bold(true)
	isTTY   = term.IsTerminal(int(os.Stdout.Fd()))
)

func badge(text string, fg, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(true).Padding(0, 1).Render(text)
}

// link wraps text in an OSC 8 hyperlink (clickable in iTerm2, kitty, WezTerm,
// VS Code, etc.; other terminals just show the text).
func link(url, text string) string {
	if !isTTY || url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// bestStream picks where to watch: one of your services first, else any.
func bestStream(m Movie, cfg Config) (Service, bool) {
	for _, s := range m.Streaming {
		if cfg.Has(s.ID) && s.URL != "" {
			return s, true
		}
	}
	for _, s := range m.Streaming {
		if s.URL != "" {
			return s, true
		}
	}
	return Service{}, false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// hstack places blocks side by side, top aligned, padding gaps with fill so
// background colours stay unbroken (lipgloss.JoinHorizontal pads unstyled).
func hstack(fill lipgloss.Style, blocks ...string) string {
	split := make([][]string, len(blocks))
	widths := make([]int, len(blocks))
	h := 0
	for i, b := range blocks {
		split[i] = strings.Split(b, "\n")
		for _, l := range split[i] {
			widths[i] = max(widths[i], lipgloss.Width(l))
		}
		h = max(h, len(split[i]))
	}
	rows := make([]string, h)
	for r := range rows {
		var sb strings.Builder
		for i := range blocks {
			l := ""
			if r < len(split[i]) {
				l = split[i][r]
			}
			sb.WriteString(l)
			if gap := widths[i] - lipgloss.Width(l); gap > 0 {
				sb.WriteString(fill.Render(strings.Repeat(" ", gap)))
			}
		}
		rows[r] = sb.String()
	}
	return strings.Join(rows, "\n")
}

// renderCardW draws one movie as a rental-shelf card w columns wide. With win
// set it is drawn on a white background for the Windows-style interface.
func renderCardW(m Movie, cfg Config, seen bool, w, overviewMax int, win, hidden, listed bool) string {
	base := lipgloss.NewStyle()
	on := func(s lipgloss.Style) lipgloss.Style { return s }
	dimS, boldS, okS := dim, bold, okStyle
	if win {
		base = base.Background(wPane).Foreground(wText)
		on = func(s lipgloss.Style) lipgloss.Style { return s.Background(wPane) }
		dimS, boldS, okS = on(dim), on(bold).Foreground(wText), on(okStyle)
	}
	lineText := base.Padding(0, 1).Width(w - 2)

	title := m.Title
	if m.Year > 0 {
		title += fmt.Sprintf(" (%d)", m.Year)
	}
	bar := lipgloss.NewStyle().Background(cBlue).Foreground(cYellow).Bold(true).
		Padding(0, 1).Width(w - 2).Render(title)

	imdb := badge("IMDb", cBlack, cYellow) + base.Render(" ") + dimS.Render("n/a")
	if m.IMDbScore != nil {
		imdb = badge("IMDb", cBlack, cYellow) + base.Render(" ") + boldS.Render(fmt.Sprintf("%.1f", *m.IMDbScore))
	}
	rt := badge("RT", cWhite, cRed) + base.Render(" ") + dimS.Render("n/a")
	if m.RT != nil {
		rt = badge("RT", cWhite, cRed) + base.Render(" ") + boldS.Render(fmt.Sprintf("%d%%", *m.RT))
	}
	scores := imdb + base.Render("   ") + rt
	if seen {
		scores += base.Render("   ") + badge("✓ SEEN IT", cWhite, cSky)
	}
	if listed {
		scores += base.Render("   ") + badge("+ WATCHLIST", cBlack, cYellow)
	}
	if hidden {
		scores += base.Render("   ") + badge("✗ HIDDEN", cWhite, cRed)
	}

	var mine, other []string
	for _, s := range m.Streaming {
		if cfg.Has(s.ID) {
			mine = append(mine, link(s.URL, okS.Render("✓ "+s.Name)))
		} else {
			other = append(other, link(s.URL, dimS.Render(s.Name)))
		}
	}
	var label string
	var names []string
	switch {
	case len(mine) > 0:
		label, names = badge("IN STOCK", cBlack, cGreen), append(mine, other...)
	case len(other) > 0 && len(cfg.Services) == 0:
		label, names = badge("STREAMING", cBlack, cGreen), other
	case len(other) > 0:
		label, names = badge("OTHER SHELVES", cBlack, cOrange), other
	case m.RentBuy:
		label = badge("RENTAL ONLY", cWhite, cBlue)
	default:
		label = badge("OUT OF STOCK", cWhite, cRed)
	}
	stock := label
	if len(names) > 0 {
		nw := w - 4 - lipgloss.Width(label) - 1
		stock = hstack(base, label, base.Render(" "),
			base.Width(nw).Render(strings.Join(names, dimS.Render(" · "))))
	}

	rows := []string{bar, lineText.Render(scores)}
	if len(m.Genres) > 0 {
		rows = append(rows, lineText.Render(boldS.Render(truncate(strings.Join(m.Genres, " · "), w-4))))
	}
	if m.Overview != "" {
		for _, l := range strings.Split(ansi.Wordwrap(truncate(m.Overview, overviewMax), w-4, ""), "\n") {
			rows = append(rows, lineText.Render(dimS.Render(l)))
		}
	}
	rows = append(rows, lineText.Render(stock))
	if m.IMDbID != "" {
		rows = append(rows, lineText.Render(link("https://www.imdb.com/title/"+m.IMDbID+"/", dimS.Render("imdb.com/title/"+m.IMDbID))))
	}
	frame := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cSky)
	if win {
		frame = frame.BorderBackground(wPane)
	}
	return frame.Render(strings.Join(rows, "\n"))
}
