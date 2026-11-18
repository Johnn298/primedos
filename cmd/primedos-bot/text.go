package main

import (
	"fmt"
	"html"
	"strings"

	"primedos/internal/remote"
)

func esc(s string) string { return html.EscapeString(s) }

func levelChosenText(level int) string {
	name := map[int]string{1: "лёгкий", 2: "средний", 3: "сложный"}[level]
	return fmt.Sprintf("уровень %d, %s.\n\nпришли домен или ссылку, например <code>example.com</code>", level, name)
}

func levelsKeyboard() *inlineKeyboard {
	return &inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{
			{Text: "1 · лёгкий", CallbackData: "lvl:1"},
			{Text: "2 · средний", CallbackData: "lvl:2"},
			{Text: "3 · сложный", CallbackData: "lvl:3"},
		},
	}}
}

func modesKeyboard() *inlineKeyboard {
	return &inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: "до падения", CallbackData: "mode:1"}},
		{{Text: "бесконечный", CallbackData: "mode:2"}},
		{{Text: "по времени", CallbackData: "mode:3"}},
	}}
}

func stopKeyboard() *inlineKeyboard {
	return &inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: "стоп", CallbackData: "stop"}},
	}}
}

func restartKeyboard() *inlineKeyboard {
	return &inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: "ещё сессия", CallbackData: "again"}},
	}}
}

func modeName(m int) string {
	switch m {
	case 2:
		return "бесконечный"
	case 3:
		return "по времени"
	default:
		return "до падения"
	}
}

func stateWord(s string) string {
	switch s {
	case "ONLINE":
		return "🟢 онлайн"
	case "DEGRADED":
		return "🟡 просела"
	case "OFFLINE":
		return "🔴 лежит"
	default:
		return "⚪ " + strings.ToLower(s)
	}
}

func statusText(s remote.Status) string {
	var b strings.Builder

	b.WriteString("<b>цель</b> ")
	if s.Target == "" {
		b.WriteString("не задана")
	} else {
		b.WriteString("<code>" + esc(s.Target) + "</code>")
		if s.IP != "" {
			b.WriteString(" → <code>" + esc(s.IP) + "</code>")
		}
		if s.CDN != "" {
			b.WriteString(" · " + esc(s.CDN))
		}
	}
	b.WriteString("\n")

	if s.Elapsed == 0 && !s.Running {
		b.WriteString("\n<i>сессия не запущена. /start чтобы выбрать уровень.</i>")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("<b>режим</b> dry-run · %s · уровень %d\n", modeName(s.Mode), s.Level))
	b.WriteString(fmt.Sprintf("<b>векторов</b> %d · <b>воркеров</b> %d\n", len(s.Vectors), s.Workers))
	b.WriteString(fmt.Sprintf("<b>скорость</b> ↑ %s · %s pps · %s rps\n", humanRate(s.Bps), humanCount(s.Pps), humanCount(s.Rps)))
	b.WriteString(fmt.Sprintf("<b>статус</b> %s %s · %.1f s\n", stateWord(s.Health), codeText(s.Code), s.HealthFor))
	b.WriteString(fmt.Sprintf("<b>прошло</b> %s · %s · %d ошибок\n", clock(s.Elapsed), humanBytes(s.TotalWire), s.Errors))
	b.WriteString(fmt.Sprintf("<b>rtt</b> %.1f/%.1f/%.1f ms · потери %.2f%%\n", s.P50, s.P95, s.P99, s.Loss))

	if len(s.Logs) > 0 {
		b.WriteString("\n<pre>")
		start := 0
		if len(s.Logs) > 4 {
			start = len(s.Logs) - 4
		}
		for _, line := range s.Logs[start:] {
			b.WriteString(esc(line))
			b.WriteString("\n")
		}
		b.WriteString("</pre>")
	}

	if s.TimeToDown > 0 {
		b.WriteString(fmt.Sprintf("\n<b>легла через</b> %.1f s\n", s.TimeToDown))
	}
	if s.ReportJSON != "" {
		b.WriteString("\n<b>отчёт</b> <code>" + esc(s.ReportJSON) + "</code>\n<code>" + esc(s.ReportCSV) + "</code>")
	}
	if s.Error != "" {
		b.WriteString("\n<b>ошибка</b> " + esc(s.Error))
	}
	if !s.Running {
		b.WriteString("\n\n<i>сессия завершена.</i>")
	}
	return b.String()
}

func codeText(code int) string {
	if code == 0 {
		return ""
	}
	return fmt.Sprintf("%d", code)
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

func clock(sec float64) string {
	total := int(sec)
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total%3600)/60, total%60)
}
