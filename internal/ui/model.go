package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"primedos/internal/config"
	"primedos/internal/core"
	"primedos/internal/engine"
	"primedos/internal/report"
	"primedos/internal/target"
)

type screen int

const (
	scrBanner screen = iota
	scrMenu
	scrTarget
	scrMode
	scrDuration
	scrBattle
	scrReport
)

type tickMsg time.Time

type resolveMsg struct {
	t   *target.Target
	err error
}

type Model struct {
	cfg *config.Config
	scr screen

	w, h  int
	frame int

	level     int
	input     string
	durInput  string
	errMsg    string
	resolving bool
	tgt       *target.Target

	eng         *engine.Engine
	stats       core.Stats
	finalized   bool
	reportPaths []string
	peak        float64
	avg         float64
}

func New(cfg *config.Config) *Model {
	m := &Model{cfg: cfg, w: 100, h: 40, level: cfg.Level, input: cfg.Target}
	if m.level < 1 || m.level > 3 {
		m.level = 1
	}
	return m
}

func (m *Model) Init() tea.Cmd { return tickCmd() }

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.frame++
		if m.scr == scrBanner && m.frame >= 14 {
			m.scr = scrMenu
		}
		if m.scr == scrBattle && m.eng != nil {
			m.stats = m.eng.Snapshot()
			if m.stats.Done {
				m.finalize()
			}
		}
		return m, tickCmd()

	case resolveMsg:
		m.resolving = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.tgt = msg.t
		m.errMsg = ""
		m.scr = scrMode
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.shutdown()
		return m, tea.Quit
	}

	switch m.scr {
	case scrBanner:
		m.scr = scrMenu

	case scrMenu:
		switch msg.String() {
		case "1", "2", "3":
			m.level = int(msg.String()[0] - '0')
			m.scr = scrTarget
		case "q", "esc":
			return m, tea.Quit
		}

	case scrTarget:
		switch msg.String() {
		case "esc":
			m.scr = scrMenu
			m.errMsg = ""
		case "enter":
			raw := strings.TrimSpace(m.input)
			if raw == "" {
				m.errMsg = "введите домен или URL"
				return m, nil
			}
			if m.resolving {
				return m, nil
			}
			m.resolving = true
			m.errMsg = ""
			return m, resolveCmd(raw, m.cfg.Timeout(), !m.cfg.DryRun)
		case "backspace":
			if len(m.input) > 0 {
				r := []rune(m.input)
				m.input = string(r[:len(r)-1])
			}
		case "ctrl+u":
			m.input = ""
		default:
			if msg.Type == tea.KeyRunes {
				m.input += string(msg.Runes)
			} else if msg.String() == " " {
				m.input += " "
			}
		}

	case scrMode:
		switch msg.String() {
		case "1":
			m.cfg.Mode = 1
			return m, m.start()
		case "2":
			m.cfg.Mode = 2
			return m, m.start()
		case "3":
			m.cfg.Mode = 3
			m.scr = scrDuration
		case "esc":
			m.scr = scrTarget
		}

	case scrDuration:
		switch msg.String() {
		case "esc":
			m.scr = scrMode
		case "enter":
			d := strings.TrimSpace(m.durInput)
			if _, err := time.ParseDuration(d); err != nil {
				m.errMsg = "формат: 30s, 5m, 1h"
				return m, nil
			}
			m.cfg.Duration = d
			m.errMsg = ""
			return m, m.start()
		case "backspace":
			if len(m.durInput) > 0 {
				r := []rune(m.durInput)
				m.durInput = string(r[:len(r)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				m.durInput += string(msg.Runes)
			}
		}

	case scrBattle:
		switch msg.String() {
		case "q":
			m.finalize()
		}

	case scrReport:
		return m, tea.Quit
	}
	return m, nil
}

func resolveCmd(raw string, timeout time.Duration, probeCDN bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout*2)
		defer cancel()
		t, err := target.Resolve(ctx, raw, timeout, probeCDN)
		return resolveMsg{t: t, err: err}
	}
}

func (m *Model) start() tea.Cmd {
	m.cfg.Level = m.level
	eng, err := engine.New(m.cfg, m.tgt)
	if err != nil {
		m.errMsg = err.Error()
		return nil
	}
	if err := eng.Start(); err != nil {
		m.errMsg = err.Error()
		return nil
	}
	m.eng = eng
	m.errMsg = ""
	m.finalized = false
	m.scr = scrBattle
	return nil
}

func (m *Model) finalize() {
	if m.finalized || m.eng == nil {
		return
	}
	m.finalized = true
	m.eng.Stop()
	m.stats = m.eng.Snapshot()
	m.peak = m.eng.Peak()
	m.avg = m.eng.Average()
	r := report.Build(m.stats, m.peak)
	r.BpsAvg = m.avg
	jsonPath, csvPath, err := report.Write(".", r)
	if err != nil {
		m.errMsg = "отчёт: " + err.Error()
	} else {
		m.reportPaths = []string{jsonPath, csvPath}
	}
	m.scr = scrReport
}

func (m *Model) shutdown() {
	if m.eng != nil && !m.finalized {
		m.finalize()
	}
}

func (m *Model) View() string {
	switch m.scr {
	case scrBanner:
		return m.viewBanner()
	case scrMenu:
		return m.viewMenu()
	case scrTarget:
		return m.viewTarget()
	case scrMode:
		return m.viewMode()
	case scrDuration:
		return m.viewDuration()
	case scrBattle:
		return m.viewBattle()
	case scrReport:
		return m.viewReport()
	}
	return ""
}
