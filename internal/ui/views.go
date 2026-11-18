package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"primedos/internal/core"
)

func itoa(v int) string { return strconv.Itoa(v) }

func (m *Model) viewBanner() string {
	var art string
	if m.w < 80 {
		art = renderBannerNarrow(m.frame)
	} else {
		art = renderBanner(m.frame)
	}
	hint := styleDim.Render("нажмите любую клавишу")
	return "\n\n" + art + "\n\n\n        " + hint + "\n"
}

func (m *Model) viewMenu() string {
	w := min(max(m.w-2, 60), 84)
	lines := []string{
		styleTitle.Render("ВЫБЕРИ УРОВЕНЬ АТАКИ"),
		"",
		"  " + styleAccent.Render("[1]") + " Лёгкая атака     " + styleDim.Render("— L7, минимум шума"),
		"  " + styleAccent.Render("[2]") + " Средняя атака    " + styleDim.Render("— L7 + протокольные"),
		"  " + styleAccent.Render("[3]") + " Сложная атака    " + styleDim.Render("— все векторы, максимум давления"),
		"",
		styleDim.Render("  1: get-flood, post-flood, slowloris"),
		styleDim.Render("  2: + tls-flood, http2-reset, continuation-flood, rudy, slow-read, range-flood, api-flood"),
		styleDim.Render("  3: + syn-flood, udp-flood, ws-flood; включаются multi-vector и adaptive"),
		"",
		styleDim.Render("q / Esc — выход"),
	}
	return "\n" + panel(lines, w) + "\n"
}

func (m *Model) viewTarget() string {
	w := min(max(m.w-2, 60), 84)
	lines := []string{
		styleTitle.Render("ВВЕДИ ЦЕЛЬ"),
		"",
		"  " + styleAccent.Render("цель:") + " " + m.input + styleAccent.Render("▌"),
	}
	if m.resolving {
		lines = append(lines, "", "  "+styleDim.Render("резолвим DNS и проверяем CDN…"))
	} else if m.errMsg != "" {
		lines = append(lines, "", "  "+styleErr.Render(m.errMsg))
	}
	lines = append(lines, "", styleDim.Render("Enter — продолжить · Esc — назад"))
	return "\n" + panel(lines, w) + "\n"
}

func (m *Model) viewMode() string {
	w := min(max(m.w-2, 60), 84)
	lines := []string{
		styleTitle.Render("РЕЖИМ АТАКИ"),
		"",
		"  цель: " + styleAccent.Render(m.tgt.Host) + " → " + m.tgt.IP() + "   CDN: " + cdnName(m.tgt.CDN),
		"",
		"  " + styleAccent.Render("[1]") + " До падения          " + styleDim.Render("— идёт, пока цель не ляжет, затем отчёт"),
		"  " + styleAccent.Render("[2]") + " Бесконечный         " + styleDim.Render("— не давать вернуться в онлайн"),
		"  " + styleAccent.Render("[3]") + " Фиксированное время " + styleDim.Render("— задать длительность"),
		"",
		styleDim.Render("Esc — назад"),
	}
	if m.errMsg != "" {
		lines = append(lines, "", "  "+styleErr.Render(m.errMsg))
	}
	return "\n" + panel(lines, w) + "\n"
}

func (m *Model) viewDuration() string {
	w := min(max(m.w-2, 60), 84)
	lines := []string{
		styleTitle.Render("ДЛИТЕЛЬНОСТЬ"),
		"",
		"  " + styleAccent.Render("время:") + " " + m.durInput + styleAccent.Render("▌"),
	}
	if m.errMsg != "" {
		lines = append(lines, "", "  "+styleErr.Render(m.errMsg))
	}
	lines = append(lines, "", styleDim.Render("примеры: 30s, 5m, 1h · Enter — старт · Esc — назад"))
	return "\n" + panel(lines, w) + "\n"
}

func cdnName(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func (m *Model) viewBattle() string {
	st := m.stats
	w := min(max(m.w-2, 60), 84)
	label := func(s string) string { return styleDim.Render(pad(s, 10)) }

	state := st.Health.State.String()
	since := 0.0
	if !st.Health.Since.IsZero() {
		since = time.Since(st.Health.Since).Seconds()
	}
	modeName := "до падения"
	switch st.Mode {
	case 2:
		modeName = "бесконечный"
	case 3:
		modeName = "фиксированное время"
	}

	lines := []string{
		label("TARGET") + st.Target + " → " + st.IP + "    CDN: " + cdnName(st.CDN),
		label("MODE") + modeName + " · уровень " + itoa(st.Level) + " · " + itoa(len(st.Vectors)) + " векторов · " + itoa(st.Workers) + " воркеров",
		"",
		label("ПРОГРЕСС") + m.progressLine(st),
		label("СКОРОСТЬ") + styleAccent.Render("↑ "+humanRate(st.Bps)) + "   " +
			humanCount(st.Pps) + " pps   " + humanCount(st.Rps) + " rps",
		label("СТАТУС") + stateStyle(state).Render("● "+state) + "  " + codeText(st.Health.Code) +
			fmt.Sprintf("  %.1f s подряд", since),
		label("UPTIME") + clock(st.Elapsed) + "   ·   отправлено " + humanBytes(st.TotalWire) +
			"   ·   " + strconv.FormatUint(st.Errors, 10) + " ошибок сокета",
		"",
	}
	if len(st.Logs) == 0 {
		lines = append(lines, label("ЛОГ")+"—")
	} else {
		for i, l := range st.Logs {
			prefix := "ЛОГ"
			if i > 0 {
				prefix = ""
			}
			lines = append(lines, styleDim.Render(pad(prefix, 10))+l)
		}
	}
	lines = append(lines, "", styleDim.Render("q — остановить и сформировать отчёт · Ctrl+C — выход"))
	return "\n" + panel(lines, w) + "\n"
}

func (m *Model) progressLine(st core.Stats) string {
	barW := 30
	if m.w < 80 {
		barW = 16
	}
	switch st.Mode {
	case 2:
		pct := float64((m.frame * 6) % 100)
		return progressBar(pct, barW) + "   " + styleAccent.Render("∞") + styleDim.Render("   без ETA")
	case 3:
		total := m.cfg.DurationValue()
		pct := 100.0
		if total > 0 {
			pct = float64(st.Elapsed) / float64(total) * 100
		}
		if pct > 100 {
			pct = 100
		}
		eta := total - st.Elapsed
		return progressBar(pct, barW) + fmt.Sprintf("  %3.0f%%   ", pct) + styleDim.Render("ETA "+clock(eta))
	default:
		pct := st.Elapsed.Seconds() / 180 * 100
		if st.Health.State == core.HealthOffline {
			pct = 100
		} else if pct > 94 {
			pct = 94
		}
		return progressBar(pct, barW) + fmt.Sprintf("  %3.0f%%   ", pct) + styleDim.Render("до падения")
	}
}

func (m *Model) viewReport() string {
	st := m.stats
	w := min(max(m.w-2, 60), 84)
	row := func(k, v string) string { return styleDim.Render(pad(k, 20)) + v }

	lines := []string{
		styleTitle.Render("ОТЧЁТ О СЕССИИ"),
		"",
		row("цель", st.Target+" → "+st.IP),
		row("cdn", cdnName(st.CDN)),
		row("уровень", itoa(st.Level)),
		row("векторы", strings.Join(st.Vectors, ", ")),
		row("воркеров", itoa(st.Workers)),
		"",
		row("длительность", clock(st.Elapsed)),
		row("время до падения", dotted(st.TimeToDown)),
		row("финальный статус", stateStyle(st.Health.State.String()).Render(st.Health.State.String()+" "+codeText(st.Health.Code))),
		"",
		row("объём", humanBytes(st.TotalWire)+" (на проводе)"),
		row("полезная нагрузка", humanBytes(st.TotalPayload)),
		row("запросов", strconv.FormatUint(st.TotalReq, 10)),
		row("средняя скорость", humanRate(m.avg)),
		row("пиковая скорость", humanRate(m.peak)),
		row("pps / rps", humanCount(st.Pps)+" / "+humanCount(st.Rps)),
		row("rtt p50/p95/p99", fmt.Sprintf("%.1f / %.1f / %.1f ms", ms(st.P50), ms(st.P95), ms(st.P99))),
		row("потери", fmt.Sprintf("%.2f%%", st.Loss)),
		row("ошибок", strconv.FormatUint(st.Errors, 10)),
		"",
	}
	if len(m.reportPaths) == 2 {
		lines = append(lines, styleDim.Render("сохранено:"), "  "+m.reportPaths[0], "  "+m.reportPaths[1], "")
	}
	if m.errMsg != "" {
		lines = append(lines, styleErr.Render(m.errMsg), "")
	}
	lines = append(lines, styleDim.Render("любая клавиша — выход"))
	return "\n" + panel(lines, w) + "\n"
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func dotted(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}

func codeText(code int) string {
	if code == 0 {
		return ""
	}
	switch code {
	case 400:
		return "400 Bad Request"
	case 401:
		return "401 Unauthorized"
	case 403:
		return "403 Forbidden"
	case 404:
		return "404 Not Found"
	case 429:
		return "429 Too Many Requests"
	case 500:
		return "500 Internal Server Error"
	case 502:
		return "502 Bad Gateway"
	case 503:
		return "503 Service Unavailable"
	case 504:
		return "504 Gateway Timeout"
	}
	return strconv.Itoa(code)
}
