package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"primedos/internal/config"
	"primedos/internal/proxies"
	"primedos/internal/target"
)

type Local struct {
	BytesWire    atomic.Uint64
	BytesPayload atomic.Uint64
	Packets      atomic.Uint64
	Requests     atomic.Uint64
}

type Counters struct {
	BytesWire    atomic.Uint64
	BytesPayload atomic.Uint64
	Packets      atomic.Uint64
	Requests     atomic.Uint64
	Conns        atomic.Uint64
	Errors       atomic.Uint64
	Retrans      atomic.Uint64
	Segments     atomic.Uint64

	mu     sync.Mutex
	locals []*Local

	rttMu sync.Mutex
	rtts  [2048]time.Duration
	rttN  int
	rttI  int
}

func NewCounters() *Counters { return &Counters{} }

func (c *Counters) NewLocal() *Local {
	l := &Local{}
	c.mu.Lock()
	c.locals = append(c.locals, l)
	c.mu.Unlock()
	return l
}

func (c *Counters) Add(l *Local, wire, payload, packets, requests uint64) {
	c.BytesWire.Add(wire)
	c.BytesPayload.Add(payload)
	c.Packets.Add(packets)
	c.Requests.Add(requests)
	if l != nil {
		l.BytesWire.Add(wire)
		l.BytesPayload.Add(payload)
		l.Packets.Add(packets)
		l.Requests.Add(requests)
	}
}

func (c *Counters) SumLocal() (wire, payload, packets, requests uint64) {
	c.mu.Lock()
	locals := make([]*Local, len(c.locals))
	copy(locals, c.locals)
	c.mu.Unlock()
	for _, l := range locals {
		wire += l.BytesWire.Load()
		payload += l.BytesPayload.Load()
		packets += l.Packets.Load()
		requests += l.Requests.Load()
	}
	return
}

func (c *Counters) RecordRTT(d time.Duration) {
	if d <= 0 {
		return
	}
	c.rttMu.Lock()
	c.rtts[c.rttI] = d
	c.rttI = (c.rttI + 1) % len(c.rtts)
	if c.rttN < len(c.rtts) {
		c.rttN++
	}
	c.rttMu.Unlock()
}

func (c *Counters) RTTPercentiles() (p50, p95, p99 time.Duration) {
	c.rttMu.Lock()
	n := c.rttN
	buf := make([]time.Duration, n)
	copy(buf, c.rtts[:n])
	c.rttMu.Unlock()
	if n == 0 {
		return 0, 0, 0
	}
	sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })
	pick := func(q float64) time.Duration {
		i := int(q * float64(n-1))
		if i < 0 {
			i = 0
		}
		if i >= n {
			i = n - 1
		}
		return buf[i]
	}
	return pick(0.50), pick(0.95), pick(0.99)
}

type Limiter struct {
	rate   float64
	burst  float64
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func NewLimiter(bytesPerSec float64) *Limiter {
	if bytesPerSec <= 0 {
		return &Limiter{}
	}
	l := &Limiter{rate: bytesPerSec, burst: bytesPerSec * 0.05, last: time.Now()}
	if l.burst < 64*1024 {
		l.burst = 64 * 1024
	}
	l.tokens = l.burst
	return l
}

func (l *Limiter) Wait(ctx context.Context, n int) {
	if l == nil || l.rate <= 0 {
		return
	}
	need := float64(n)
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens += now.Sub(l.last).Seconds() * l.rate
		l.last = now
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		if l.tokens >= need {
			l.tokens -= need
			l.mu.Unlock()
			return
		}
		wait := (need - l.tokens) / l.rate
		l.mu.Unlock()
		if wait > 0.05 {
			wait = 0.05
		}
		if wait < 0.0002 {
			wait = 0.0002
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(wait * float64(time.Second))):
		}
	}
}

type Session struct {
	Cfg  *config.Config
	Tgt  *target.Target
	C    *Counters
	Pool *proxies.Pool
	Lim  *Limiter
	Dry  bool
	Logf func(string, ...any)
}

func (s *Session) BaseURL() string { return s.Tgt.BaseURL() }

func (s *Session) RandomPath(r *rand.Rand) string {
	paths := s.Cfg.Paths
	if len(paths) == 0 {
		return "/"
	}
	p := paths[r.Intn(len(paths))]
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	return p
}

func (s *Session) RandomUA(r *rand.Rand) string {
	uas := s.Cfg.UserAgents
	if len(uas) == 0 {
		return "Mozilla/5.0"
	}
	return uas[r.Intn(len(uas))]
}

func (s *Session) ApplyHeaders(req *http.Request, r *rand.Rand) {
	req.Header.Set("User-Agent", s.RandomUA(r))
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", []string{"en-US,en;q=0.9", "ru-RU,ru;q=0.9,en;q=0.8", "de-DE,de;q=0.9"}[r.Intn(3)])
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Cache-Control", []string{"no-cache", "no-store", "max-age=0"}[r.Intn(3)])
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-Dest", "document")
}

func (s *Session) NewHTTPClient() *http.Client {
	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   s.Cfg.Timeout(),
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         s.Tgt.Host,
			MinVersion:         tls.VersionTLS12,
			ClientSessionCache: nil,
		},
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   512,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   s.Cfg.Timeout(),
		ResponseHeaderTimeout: s.Cfg.Timeout(),
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
	}
	if s.Pool != nil && s.Pool.Enabled() {
		tr.Proxy = func(*http.Request) (*url.URL, error) {
			return s.Pool.Next(), nil
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   s.Cfg.Timeout(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *Session) DialRaw(ctx context.Context) (net.Conn, error) {
	d := &net.Dialer{Timeout: s.Cfg.Timeout()}
	c, err := d.DialContext(ctx, "tcp", s.Tgt.Addr())
	if err != nil {
		return nil, err
	}
	if s.Tgt.Scheme != "https" {
		return c, nil
	}
	tc := tls.Client(c, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         s.Tgt.Host,
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"http/1.1"},
	})
	if err := tc.HandshakeContext(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return tc, nil
}

type Worker struct {
	S   *Session
	L   *Local
	R   *rand.Rand
	ID  int
	Ctx context.Context
}

func (w *Worker) Account(wire, payload, packets, requests int) {
	if wire < 0 {
		wire = 0
	}
	if payload < 0 {
		payload = 0
	}
	if packets < 0 {
		packets = 0
	}
	w.S.C.Add(w.L, uint64(wire), uint64(payload), uint64(packets), uint64(requests))
	ctx := w.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	w.S.Lim.Wait(ctx, wire)
}

func (w *Worker) Error() { w.S.C.Errors.Add(1) }

func (w *Worker) Conn() { w.S.C.Conns.Add(1) }

func (w *Worker) RTT(d time.Duration) { w.S.C.RecordRTT(d) }

func WireOverhead(payload int) int {
	seg := (payload + 1459) / 1460
	if seg < 1 {
		seg = 1
	}
	return seg * (14 + 20 + 20 + 20 + 5)
}

type HealthState int

const (
	HealthOnline HealthState = iota
	HealthDegraded
	HealthOffline
)

func (h HealthState) String() string {
	switch h {
	case HealthOnline:
		return "ONLINE"
	case HealthDegraded:
		return "DEGRADED"
	case HealthOffline:
		return "OFFLINE"
	}
	return "UNKNOWN"
}

type HealthStatus struct {
	State     HealthState
	Code      int
	Since     time.Time
	DownSince time.Time
}

type Stats struct {
	Target       string
	CDN          string
	IP           string
	Level        int
	Mode         int
	Workers      int
	Vectors      []string
	Running      bool
	Done         bool
	Elapsed      time.Duration
	Bps          float64
	Pps          float64
	Rps          float64
	TotalWire    uint64
	TotalPayload uint64
	TotalReq     uint64
	Errors       uint64
	P50          time.Duration
	P95          time.Duration
	P99          time.Duration
	Loss         float64
	Health       HealthStatus
	TimeToDown   time.Duration
	Logs         []string
	Proxies      int
	RateMbps     float64
}

type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	cap   int
}

func NewLogBuffer(n int) *LogBuffer { return &LogBuffer{cap: n} }

func (b *LogBuffer) Add(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	b.mu.Lock()
	b.lines = append(b.lines, line)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
	b.mu.Unlock()
}

func (b *LogBuffer) Tail(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) <= n {
		out := make([]string, len(b.lines))
		copy(out, b.lines)
		return out
	}
	out := make([]string, n)
	copy(out, b.lines[len(b.lines)-n:])
	return out
}
