package main

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Posters are drawn with quadrant block characters: each terminal cell shows
// 2×2 pixels in two colours (see renderPoster).
const (
	posterCols = 24
	posterRows = 18 // 48×36 pixels in 24×18 cells
)

var posterClient = &http.Client{Timeout: 10 * time.Second}

// postersEnabled is false when output isn't a colour terminal.
func postersEnabled() bool {
	return isTTY && lipgloss.DefaultRenderer().ColorProfile() != termenv.Ascii
}

func fetchPoster(url string) string {
	if url == "" {
		return posterPlaceholder("no cover")
	}
	resp, err := posterClient.Get(url)
	if err != nil {
		return posterPlaceholder("no cover")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return posterPlaceholder("no cover")
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return posterPlaceholder("no cover")
	}
	return renderPoster(img)
}

// posterFill is the style used to paint blank poster cells (the TUI sets its pane colour).
var posterFill = lipgloss.NewStyle()

func posterPlaceholder(text string) string {
	blank := posterFill.Render(strings.Repeat(" ", posterCols))
	rows := make([]string, posterRows)
	for i := range rows {
		rows[i] = blank
	}
	pad := (posterCols - lipgloss.Width(text)) / 2
	rows[posterRows/2] = posterFill.Render(strings.Repeat(" ", pad)) + posterFill.Foreground(cGrey).Render(text) +
		posterFill.Render(strings.Repeat(" ", posterCols-pad-lipgloss.Width(text)))
	return strings.Join(rows, "\n")
}

// quadrants maps a 2×2 pixel mask (1=top-left 2=top-right 4=bottom-left
// 8=bottom-right set in the foreground) to its block character.
var quadrants = [...]string{" ", "▘", "▝", "▀", "▖", "▌", "▞", "▛", "▗", "▚", "▐", "▜", "▄", "▙", "▟", "█"}

// renderPoster draws img with quadrant blocks: each cell shows 2×2 pixels,
// split into the two-colour (foreground/background) partition that fits best,
// which is twice the resolution of half-blocks in the same space.
func renderPoster(img image.Image) string {
	px := boxResize(img, posterCols*2, posterRows*2)
	hex := func(c [3]float64) lipgloss.Color {
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(c[0]+.5), uint8(c[1]+.5), uint8(c[2]+.5)))
	}
	var b strings.Builder
	for y := 0; y < posterRows; y++ {
		for x := 0; x < posterCols; x++ {
			var p [4][3]float64
			for i := range p {
				c := px.RGBAAt(2*x+i%2, 2*y+i/2)
				p[i] = [3]float64{float64(c.R), float64(c.G), float64(c.B)}
			}
			bestMask, bestErr := 3, -1.0
			var bestFg, bestBg [3]float64
			for mask := 1; mask < 8; mask++ { // masks 8-15 are these with fg/bg swapped
				var sum [2][3]float64
				var n [2]float64
				for i := range p {
					g := mask >> i & 1
					n[g]++
					for k := range 3 {
						sum[g][k] += p[i][k]
					}
				}
				var mean [2][3]float64
				for g := range mean {
					for k := range 3 {
						if n[g] > 0 {
							mean[g][k] = sum[g][k] / n[g]
						}
					}
				}
				var e float64
				for i := range p {
					g := mask >> i & 1
					for k := range 3 {
						d := p[i][k] - mean[g][k]
						e += d * d
					}
				}
				if bestErr < 0 || e < bestErr {
					bestMask, bestErr, bestFg, bestBg = mask, e, mean[1], mean[0]
				}
			}
			b.WriteString(lipgloss.NewStyle().Foreground(hex(bestFg)).Background(hex(bestBg)).Render(quadrants[bestMask]))
		}
		if y < posterRows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// boxResize shrinks img to w×h by averaging each destination pixel's source box.
func boxResize(src image.Image, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, n = r+uint64(cr>>8), g+uint64(cg>>8), bl+uint64(cb>>8), n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	return out
}

func withPoster(art, card string, fill lipgloss.Style) string {
	return hstack(fill, art, fill.Render("  "), card)
}
