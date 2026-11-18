package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"primedos/internal/remote"
)

type botConfig struct {
	Token        string  `yaml:"token"`
	AllowedUsers []int64 `yaml:"allowed_users"`
	Socket       string  `yaml:"socket"`
}

type chatState struct {
	step   string
	level  int
	mode   int
	target string
	liveID int64
	stop   chan struct{}
}

type bot struct {
	api    *api
	cfg    botConfig
	client *http.Client
	mu     sync.Mutex
	states map[int64]*chatState
}

func main() {
	path := flag.String("config", "bot.yaml", "путь к конфигу бота")
	flag.Parse()

	cfg, err := loadConfig(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "конфиг:", err)
		os.Exit(2)
	}
	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "в конфиге нет token")
		os.Exit(2)
	}

	b := &bot{
		api:    newAPI(cfg.Token),
		cfg:    cfg,
		client: unixClient(cfg.Socket),
		states: map[int64]*chatState{},
	}

	me, err := b.api.getMe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "telegram:", err)
		os.Exit(1)
	}
	log.Printf("бот @%s запущен, сокет %s", me.Username, cfg.Socket)
	if len(cfg.AllowedUsers) == 0 {
		log.Printf("allowed_users пуст: бот ответит только своим id")
	}
	if _, err := b.status(); err != nil {
		log.Printf("сокет недоступен: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	b.run(ctx)
}

func loadConfig(path string) (botConfig, error) {
	cfg := botConfig{Socket: defaultSocket()}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Socket == "" {
		cfg.Socket = defaultSocket()
	}
	return cfg, nil
}

func defaultSocket() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "primedos.sock")
}

func unixClient(path string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "unix", path)
			},
		},
	}
}

func (b *bot) run(ctx context.Context) {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		ups, err := b.api.getUpdates(offset)
		if err != nil {
			log.Printf("getUpdates: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			b.handle(u)
		}
	}
}

func (b *bot) handle(u tgUpdate) {
	if u.Callback != nil {
		if !b.allowed(u.Callback.From.ID) {
			b.denyCallback(u.Callback)
			return
		}
		b.handleCallback(u.Callback)
		return
	}
	if u.Message != nil {
		if !b.allowed(u.Message.From.ID) {
			b.denyMessage(u.Message)
			return
		}
		b.handleMessage(u.Message)
	}
}

func (b *bot) allowed(id int64) bool {
	for _, u := range b.cfg.AllowedUsers {
		if u == id {
			return true
		}
	}
	return false
}

func (b *bot) denyMessage(m *tgMessage) {
	if m.From != nil {
		log.Printf("отказ в доступе: id=%d username=%s", m.From.ID, m.From.Username)
	}
	text := fmt.Sprintf("доступа нет.\n\nтвой id: <code>%d</code>\nвпиши его в allowed_users в bot.yaml и перезапусти бота.", m.From.ID)
	b.api.sendMessage(m.Chat.ID, text, nil)
}

func (b *bot) denyCallback(cb *tgCallback) {
	log.Printf("отказ в доступе: id=%d username=%s", cb.From.ID, cb.From.Username)
	b.api.answerCallback(cb.ID, "доступа нет")
	if cb.Message != nil {
		text := fmt.Sprintf("доступа нет.\n\nтвой id: <code>%d</code>", cb.From.ID)
		b.api.sendMessage(cb.Message.Chat.ID, text, nil)
	}
}

func (b *bot) state(chatID int64) *chatState {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.states[chatID]
	if st == nil {
		st = &chatState{level: 1, mode: 1}
		b.states[chatID] = st
	}
	return st
}

func (b *bot) handleMessage(m *tgMessage) {
	chatID := m.Chat.ID
	text := strings.TrimSpace(m.Text)
	st := b.state(chatID)

	switch {
	case text == "/start" || text == "/menu":
		st.step = ""
		b.sendLevels(chatID)
	case text == "/status":
		b.sendStatus(chatID)
	case text == "/stop":
		b.stopSession(chatID, "команда /stop")
	case text == "/reports":
		b.sendReports(chatID)
	case text == "/help":
		b.sendHelp(chatID)
	case st.step == "target":
		b.takeTarget(chatID, st, text)
	case st.step == "duration":
		b.takeDuration(chatID, st, text)
	default:
		b.sendHelp(chatID)
	}
}

func (b *bot) handleCallback(cb *tgCallback) {
	chatID := cb.Message.Chat.ID
	st := b.state(chatID)
	data := cb.Data

	switch {
	case strings.HasPrefix(data, "lvl:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(data, "lvl:"))
		if n < 1 || n > 3 {
			b.api.answerCallback(cb.ID, "нет такого уровня")
			return
		}
		st.level = n
		st.step = "target"
		b.api.answerCallback(cb.ID, "")
		b.edit(chatID, cb.Message.MessageID, levelChosenText(n), nil)
	case strings.HasPrefix(data, "mode:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(data, "mode:"))
		if n < 1 || n > 3 {
			b.api.answerCallback(cb.ID, "нет такого режима")
			return
		}
		st.mode = n
		b.api.answerCallback(cb.ID, "")
		if n == 3 {
			st.step = "duration"
			b.edit(chatID, cb.Message.MessageID, "режим: по времени.\n\nпришли длительность: <code>30s</code>, <code>5m</code>, <code>1h</code>", nil)
			return
		}
		st.step = ""
		b.edit(chatID, cb.Message.MessageID, "запускаю.", nil)
		b.launch(chatID, st, "")
	case data == "stop":
		b.api.answerCallback(cb.ID, "останавливаю")
		b.stopSession(chatID, "кнопка стоп")
	case data == "again":
		b.api.answerCallback(cb.ID, "")
		b.sendLevels(chatID)
	default:
		b.api.answerCallback(cb.ID, "не понял")
	}
}

func (b *bot) takeTarget(chatID int64, st *chatState, text string) {
	if text == "" || strings.HasPrefix(text, "/") {
		b.api.sendMessage(chatID, "нужен домен или ссылка, например <code>example.com</code>", nil)
		return
	}
	st.target = text
	st.step = ""
	msg := fmt.Sprintf("цель: <code>%s</code>\nуровень %d.\n\nтеперь режим:", esc(st.target), st.level)
	b.api.sendMessage(chatID, msg, modesKeyboard())
}

func (b *bot) takeDuration(chatID int64, st *chatState, text string) {
	if _, err := time.ParseDuration(text); err != nil {
		b.api.sendMessage(chatID, "не разобрал. формат: <code>30s</code>, <code>5m</code>, <code>1h</code>", nil)
		return
	}
	st.step = ""
	b.api.sendMessage(chatID, "запускаю.", nil)
	b.launch(chatID, st, text)
}

func (b *bot) sendLevels(chatID int64) {
	text := "выбери уровень.\n\n" +
		"<b>1</b> лёгкий: GET, POST, slowloris\n" +
		"<b>2</b> средний: плюс TLS, HTTP/2, RUDY, slow read, range, апи\n" +
		"<b>3</b> сложный: плюс raw SYN, UDP, WebSocket"
	b.api.sendMessage(chatID, text, levelsKeyboard())
}

func (b *bot) sendHelp(chatID int64) {
	text := "что умею:\n\n" +
		"/start - выбрать уровень и запустить dry-run сессию\n" +
		"/status - текущая телеметрия\n" +
		"/stop - остановить сессию\n" +
		"/reports - пути к последним отчётам\n\n" +
		"бот управляет только режимом <code>--dry-run</code>: интерфейс и телеметрия без трафика. " +
		"боевые сессии запускаются с самой машины."
	b.api.sendMessage(chatID, text, nil)
}

func (b *bot) sendStatus(chatID int64) {
	s, err := b.status()
	if err != nil {
		b.api.sendMessage(chatID, "сокет недоступен: "+esc(err.Error()), nil)
		return
	}
	b.api.sendMessage(chatID, statusText(s), nil)
}

func (b *bot) sendReports(chatID int64) {
	s, err := b.status()
	if err != nil {
		b.api.sendMessage(chatID, "сокет недоступен: "+esc(err.Error()), nil)
		return
	}
	if s.ReportJSON == "" {
		b.api.sendMessage(chatID, "отчётов пока нет", nil)
		return
	}
	text := "последний отчёт:\n\n<code>" + esc(s.ReportJSON) + "</code>\n<code>" + esc(s.ReportCSV) + "</code>"
	b.api.sendMessage(chatID, text, nil)
}

func (b *bot) launch(chatID int64, st *chatState, duration string) {
	if st.target == "" {
		b.api.sendMessage(chatID, "сначала /start и выбери цель", nil)
		return
	}
	if duration == "" {
		duration = "1m"
	}
	err := b.start(st.target, st.level, st.mode, duration)
	if err != nil {
		b.api.sendMessage(chatID, "не запустилось: "+esc(err.Error()), nil)
		return
	}
	if st.stop != nil {
		close(st.stop)
		st.stop = nil
	}
	b.live(chatID, st)
}

func (b *bot) stopSession(chatID int64, reason string) {
	if err := b.stop(); err != nil && !errors.Is(err, remote.ErrNotRunning) {
		b.api.sendMessage(chatID, "не остановилось: "+esc(err.Error()), nil)
		return
	}
	b.api.sendMessage(chatID, "стоп: "+reason, nil)
}

func (b *bot) edit(chatID, msgID int64, text string, kb *inlineKeyboard) {
	if err := b.api.editMessage(chatID, msgID, text, kb); err != nil {
		log.Printf("edit: %v", err)
	}
}

func (b *bot) live(chatID int64, st *chatState) {
	msgID, err := b.api.sendMessage(chatID, "сессия запускается.", nil)
	if err != nil {
		log.Printf("send live: %v", err)
		return
	}
	stop := make(chan struct{})
	st.stop = stop
	st.liveID = msgID

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		last := ""
		for {
			select {
			case <-stop:
				return
			case <-t.C:
			}
			s, err := b.status()
			if err != nil {
				b.edit(chatID, msgID, "сокет недоступен: "+esc(err.Error()), nil)
				return
			}
			text := statusText(s)
			if !s.Running && s.Elapsed > 0 {
				last = text
				b.edit(chatID, msgID, text, restartKeyboard())
				return
			}
			if text == last {
				continue
			}
			last = text
			b.edit(chatID, msgID, text, stopKeyboard())
		}
	}()
}

func (b *bot) status() (remote.Status, error) {
	var s remote.Status
	if err := b.request(http.MethodGet, "/status", nil, &s); err != nil {
		return s, err
	}
	return s, nil
}

func (b *bot) start(target string, level, mode int, duration string) error {
	in := map[string]any{
		"target":   target,
		"level":    level,
		"mode":     mode,
		"duration": duration,
		"workers":  256,
	}
	return b.request(http.MethodPost, "/start", in, nil)
}

func (b *bot) stop() error {
	return b.request(http.MethodPost, "/stop", map[string]any{}, nil)
}

func (b *bot) request(method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://unix"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("сокет %s: %w", b.cfg.Socket, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return errors.New(resp.Status)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
