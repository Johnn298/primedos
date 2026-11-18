package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	colMagenta = lipgloss.Color("#c026d3")
	colCyan    = lipgloss.Color("#22d3ee")
	colGreen   = lipgloss.Color("#22c55e")
	colYellow  = lipgloss.Color("#eab308")
	colRed     = lipgloss.Color("#ef4444")
	colDim     = lipgloss.Color("#6b7280")
	colText    = lipgloss.Color("#e5e7eb")
	colAccent  = lipgloss.Color("#f472b6")

	styleTitle  = lipgloss.NewStyle().Foreground(colCyan).Bold(true)
	styleDim    = lipgloss.NewStyle().Foreground(colDim)
	styleText   = lipgloss.NewStyle().Foreground(colText)
	styleAccent = lipgloss.NewStyle().Foreground(colAccent)
	styleOK     = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	styleWarn   = lipgloss.NewStyle().Foreground(colYellow).Bold(true)
	styleBad    = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	styleErr    = lipgloss.NewStyle().Foreground(colRed)
)

var bannerColors = []string{"#f472b6", "#e879f9", "#c084fc", "#a78bfa", "#818cf8", "#22d3ee"}

func panel(lines []string, width int) string {
	if width < 40 {
		width = 40
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colMagenta).
		Padding(0, 1).
		Width(width - 4).
		Render(strings.Join(lines, "\n"))
}

func humanRate(bps float64) string {
	switch {
	case bps >= 1e12:
		return fmt.Sprintf("%.2f Tbit/s", bps/1e12)
	case bps >= 1e9:
		return fmt.Sprintf("%.2f Gbit/s", bps/1e9)
	case bps >= 1e6:
		return fmt.Sprintf("%.1f Mbit/s", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.0f kbit/s", bps/1e3)
	}
	return fmt.Sprintf("%.0f bit/s", bps)
}

func humanCount(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fG", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.0fk", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}

func humanBytes(v uint64) string {
	f := float64(v)
	switch {
	case f >= 1e12:
		return fmt.Sprintf("%.2f TB", f/1e12)
	case f >= 1e9:
		return fmt.Sprintf("%.2f GB", f/1e9)
	case f >= 1e6:
		return fmt.Sprintf("%.1f MB", f/1e6)
	case f >= 1e3:
		return fmt.Sprintf("%.1f KB", f/1e3)
	}
	return fmt.Sprintf("%d B", v)
}

func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total%3600)/60, total%60)
}

func progressBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(float64(width) * pct / 100)
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			idx := i * (len(bannerColors) - 1) / max(1, width-1)
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(bannerColors[idx])).Render("█"))
		} else {
			b.WriteString(styleDim.Render("░"))
		}
	}
	return b.String()
}

func pad(s string, width int) string {
	n := lipgloss.Width(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "ONLINE":
		return styleOK
	case "DEGRADED":
		return styleWarn
	default:
		return styleBad
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
