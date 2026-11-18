package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"primedos/internal/config"
	"primedos/internal/core"
	"primedos/internal/engine"
	"primedos/internal/report"
	"primedos/internal/target"
)

const (
	maxBody     = 8 << 10
	watchPeriod = 300 * time.Millisecond
)

var (
	ErrDryRunOnly = errors.New("удалённый запуск разрешён только в режиме --dry-run")
	ErrBusy       = errors.New("сессия уже идёт")
	ErrNotRunning = errors.New("сессия не запущена")
)

type startRequest struct {
	Target   string `json:"target"`
	Level    int    `json:"level"`
	Mode     int    `json:"mode"`
	Duration string `json:"duration"`
	Workers  int    `json:"workers"`
}

type Status struct {
	DryRun       bool     `json:"dry_run"`
	Running      bool     `json:"running"`
	Target       string   `json:"target"`
	IP           string   `json:"ip"`
	CDN          string   `json:"cdn"`
	Level        int      `json:"level"`
	Mode         int      `json:"mode"`
	Workers      int      `json:"workers"`
	Vectors      []string `json:"vectors"`
	Elapsed      float64  `json:"elapsed_sec"`
	Bps          float64  `json:"bps"`
	Pps          float64  `json:"pps"`
	Rps          float64  `json:"rps"`
	Health       string   `json:"health"`
	Code         int      `json:"code"`
	HealthFor    float64  `json:"health_for_sec"`
	TotalWire    uint64   `json:"total_wire_bytes"`
	TotalPayload uint64   `json:"total_payload_bytes"`
	Requests     uint64   `json:"total_requests"`
	Errors       uint64   `json:"errors"`
	P50          float64  `json:"rtt_p50_ms"`
	P95          float64  `json:"rtt_p95_ms"`
	P99          float64  `json:"rtt_p99_ms"`
	Loss         float64  `json:"loss_pct"`
	TimeToDown   float64  `json:"time_to_down_sec"`
	Logs         []string `json:"logs"`
	ReportJSON   string   `json:"report_json"`
	ReportCSV    string   `json:"report_csv"`
	Error        string   `json:"error"`
}

type Session struct {
	dry  bool
	base *config.Config

	mu       sync.Mutex
	eng      *engine.Engine
	tgt      *target.Target
	cfg      *config.Config
	started  time.Time
	finished time.Time
	final    core.Stats
	peak     float64
	avg      float64
	jsonPath string
	csvPath  string
	lastErr  string
}

func NewSession(base *config.Config) *Session {
	return &Session{dry: base.DryRun, base: base}
}

func (s *Session) Start(req startRequest) error {
	if !s.dry {
		return ErrDryRunOnly
	}
	if req.Level < 1 || req.Level > 3 {
		return fmt.Errorf("уровень должен быть 1..3, получено %d", req.Level)
	}
	if req.Mode < 1 || req.Mode > 3 {
		return fmt.Errorf("режим должен быть 1..3, получено %d", req.Mode)
	}
	if req.Target == "" {
		return errors.New("пустая цель")
	}
	if req.Duration == "" {
		req.Duration = "1m"
	}
	if _, err := time.ParseDuration(req.Duration); err != nil {
		return fmt.Errorf("длительность %q не разобрана", req.Duration)
	}

	s.mu.Lock()
	busy := s.eng != nil && s.finished.IsZero()
	s.mu.Unlock()
	if busy {
		return ErrBusy
	}

	tgt, err := target.Resolve(context.Background(), req.Target, 8*time.Second, false)
	if err != nil {
		return err
	}

	cfg := *s.base
	cfg.Target = req.Target
	cfg.Level = req.Level
	cfg.Mode = req.Mode
	cfg.Duration = req.Duration
	cfg.DryRun = true
	if req.Workers > 0 {
		cfg.Concurrency = req.Workers
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	eng, err := engine.New(&cfg, tgt)
	if err != nil {
		return err
	}
	if err := eng.Start(); err != nil {
		return err
	}

	s.mu.Lock()
	s.eng = eng
	s.tgt = tgt
	s.cfg = &cfg
	s.started = time.Now()
	s.finished = time.Time{}
	s.final = core.Stats{}
	s.peak = 0
	s.avg = 0
	s.jsonPath = ""
	s.csvPath = ""
	s.lastErr = ""
	s.mu.Unlock()

	go s.watch(eng)
	return nil
}

func (s *Session) watch(eng *engine.Engine) {
	for {
		time.Sleep(watchPeriod)
		if eng.Snapshot().Done {
			break
		}
	}
	eng.Stop()

	st := eng.Snapshot()
	peak := eng.Peak()
	avg := eng.Average()
	r := report.Build(st, peak)
	r.BpsAvg = avg
	jsonPath, csvPath, err := report.Write(".", r)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.final = st
	s.peak = peak
	s.avg = avg
	s.finished = time.Now()
	if err != nil {
		s.lastErr = "отчёт: " + err.Error()
	} else {
		s.jsonPath = jsonPath
		s.csvPath = csvPath
	}
}

func (s *Session) Stop() error {
	s.mu.Lock()
	eng := s.eng
	finished := s.finished
	s.mu.Unlock()
	if eng == nil {
		return ErrNotRunning
	}
	if !finished.IsZero() {
		return nil
	}
	eng.Stop()
	return nil
}

func (s *Session) Status() Status {
	st := Status{DryRun: s.dry}

	s.mu.Lock()
	eng := s.eng
	cfg := s.cfg
	tgt := s.tgt
	finished := s.finished
	final := s.final
	avg := s.avg
	jsonPath := s.jsonPath
	csvPath := s.csvPath
	lastErr := s.lastErr
	if cfg != nil {
		st.Level = cfg.Level
		st.Mode = cfg.Mode
	}
	if tgt != nil {
		st.Target = tgt.Host
		st.IP = tgt.IP()
		st.CDN = tgt.CDN
	}
	if jsonPath != "" {
		st.ReportJSON = jsonPath
		st.ReportCSV = csvPath
	}
	st.Error = lastErr
	s.mu.Unlock()

	if eng == nil {
		return st
	}

	snap := eng.Snapshot()
	if !finished.IsZero() {
		snap = final
	}
	st.Running = finished.IsZero()
	st.Workers = snap.Workers
	st.Vectors = snap.Vectors
	st.Elapsed = snap.Elapsed.Seconds()
	st.TotalWire = snap.TotalWire
	st.TotalPayload = snap.TotalPayload
	st.Requests = snap.TotalReq
	st.Errors = snap.Errors
	st.Loss = snap.Loss
	st.TimeToDown = snap.TimeToDown.Seconds()
	st.Health = snap.Health.State.String()
	st.Code = snap.Health.Code
	if !snap.Health.Since.IsZero() {
		st.HealthFor = time.Since(snap.Health.Since).Seconds()
	}
	st.P50 = float64(snap.P50.Microseconds()) / 1000
	st.P95 = float64(snap.P95.Microseconds()) / 1000
	st.P99 = float64(snap.P99.Microseconds()) / 1000
	st.Logs = snap.Logs
	if !finished.IsZero() {
		st.Bps = avg
		st.Pps = snap.Pps
		st.Rps = snap.Rps
	} else {
		st.Bps = snap.Bps
		st.Pps = snap.Pps
		st.Rps = snap.Rps
	}
	return st
}

type Server struct {
	path string
	sess *Session
	ln   net.Listener
}

func NewServer(path string, base *config.Config) *Server {
	return &Server{path: path, sess: NewSession(base)}
}

func (s *Server) Session() *Session { return s.sess }

func (s *Server) Serve(ctx context.Context) error {
	if _, err := os.Stat(s.path); err == nil {
		c, err := net.DialTimeout("unix", s.path, time.Second)
		if err == nil {
			c.Close()
			return fmt.Errorf("сокет %s уже занят", s.path)
		}
		os.Remove(s.path)
	}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("сокет %s: %w", s.path, err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		ln.Close()
		os.Remove(s.path)
		return err
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/stop", s.handleStop)

	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	err = srv.Serve(ln)
	os.Remove(s.path)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "только GET", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.sess.Status())
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return
	}
	var req startRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "тело запроса: " + err.Error()})
		return
	}
	if err := s.sess.Start(req); err != nil {
		code := http.StatusConflict
		if errors.Is(err, ErrDryRunOnly) {
			code = http.StatusForbidden
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, s.sess.Status())
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return
	}
	if err := s.sess.Stop(); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.sess.Status())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
