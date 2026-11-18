package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var bannerArt = []string{
	"██████╗ ██████╗ ██╗███╗   ███╗███████╗██████╗  ██████╗ ███████╗",
	"██╔══██╗██╔══██╗██║████╗ ████║██╔════╝██╔══██╗██╔═══██╗██╔════╝",
	"██████╔╝██████╔╝██║██╔████╔██║█████╗  ██║  ██║██║   ██║███████╗",
	"██╔═══╝ ██╔══██╗██║██║╚██╔╝██║██╔══╝  ██║  ██║██║   ██║╚════██║",
	"██║     ██║  ██║██║██║ ╚═╝ ██║███████╗██████╔╝╚██████╔╝███████║",
	"╚═╝     ╚═╝  ╚═╝╚═╝╚═╝     ╚═╝╚══════╝╚═════╝  ╚═════╝ ╚══════╝",
}

var sparkFrames = []string{
	"   .         ",
	"  *  .       ",
	" .  *   .    ",
	"  .  *    .  ",
	"    .  *  .  ",
	"     .  * .  ",
	"      .  * · ",
	"       ·  .  ",
}

var lightning = []string{
	"      ▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄",
	"         ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀",
}

func renderBanner(frame int) string {
	var rows []string
	for i, line := range bannerArt {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color(bannerColors[i%len(bannerColors)])).Render(line))
	}
	spark := sparkFrames[frame%len(sparkFrames)]
	rows = append(rows, "")
	rows = append(rows, styleAccent.Render("        ⚡ "+strings.TrimSpace(lightning[0]))+styleDim.Render(spark))
	rows = append(rows, styleDim.Render("        "+strings.TrimSpace(lightning[1])))
	rows = append(rows, "")
	rows = append(rows, "        "+styleTitle.Render("tg: @PrimeNikee")+styleDim.Render("   v1.0"))
	return strings.Join(rows, "\n")
}

func renderBannerNarrow(frame int) string {
	spark := sparkFrames[frame%len(sparkFrames)]
	lines := []string{
		styleTitle.Render("  ██████╗ ██████╗ ██╗███╗   ███╗███████╗"),
		lipgloss.NewStyle().Foreground(colMagenta).Render("  ██╔══██╗██╔══██╗██║████╗ ████║██╔════╝"),
		lipgloss.NewStyle().Foreground(colAccent).Render("  ██████╔╝██████╔╝██║██╔████╔██║█████╗  "),
		lipgloss.NewStyle().Foreground(colCyan).Render("  ██╔═══╝ ██╔══██╗██║██║╚██╔╝██║██╔══╝  "),
		styleDim.Render("  ██║     ██║  ██║██║██║ ╚═╝ ██║███████╗  " + spark),
		"",
		"  " + styleAccent.Render("tg: @PrimeNikee") + styleDim.Render("   v1.0"),
	}
	return strings.Join(lines, "\n")
}
