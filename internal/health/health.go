package health

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"primedos/internal/core"
	"primedos/internal/target"
)

type Prober struct {
	tgt      *target.Target
	client   *http.Client
	interval time.Duration
	timeout  time.Duration

	mu         sync.Mutex
	status     core.HealthStatus
	failStreak int
	baseline   time.Duration
	baseN      int
	baseUntil  time.Time
}

func New(tgt *target.Target, timeout, interval time.Duration) *Prober {
	tr := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives:   true,
		DisableCompression:  true,
		MaxIdleConnsPerHost: 4,
	}
	return &Prober{
		tgt:      tgt,
		client:   &http.Client{Transport: tr, Timeout: timeout},
		interval: interval,
		timeout:  timeout,
		status:   core.HealthStatus{State: core.HealthOnline, Since: time.Now()},
	}
}

func (p *Prober) Snapshot() core.HealthStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Prober) SetSimulated(state core.HealthState, code int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status.State != state {
		p.status.Since = time.Now()
		if state == core.HealthOffline {
			p.status.DownSince = time.Now()
		}
	}
	p.status.State = state
	if code != 0 {
		p.status.Code = code
	}
}

func (p *Prober) Run(ctx context.Context) {
	p.mu.Lock()
	p.baseUntil = time.Now().Add(10 * time.Second)
	p.mu.Unlock()

	for {
		cycleStart := time.Now()
		p.cycle(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := p.interval - time.Since(cycleStart)
		if wait < 50*time.Millisecond {
			wait = 50 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

type probeResult struct {
	code int
	err  error
	rtt  time.Duration
}

func (p *Prober) cycle(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(3)

	var head, get probeResult
	var tcpOK bool
	var tcpRTT time.Duration

	go func() {
		defer wg.Done()
		head = p.httpProbe(ctx, http.MethodHead, p.tgt.BaseURL()+"/")
	}()
	go func() {
		defer wg.Done()
		get = p.httpProbe(ctx, http.MethodGet, p.tgt.BaseURL()+p.tgt.Path)
	}()
	go func() {
		defer wg.Done()
		t0 := time.Now()
		d := net.Dialer{Timeout: p.timeout}
		c, err := d.DialContext(ctx, "tcp", p.tgt.Addr())
		tcpRTT = time.Since(t0)
		if err == nil {
			tcpOK = true
			c.Close()
		}
	}()
	wg.Wait()
	if ctx.Err() != nil {
		return
	}

	var good, overload, softBad, netFail int
	var code int
	for _, r := range []probeResult{head, get} {
		switch {
		case r.err != nil:
			netFail++
		case r.code >= 200 && r.code < 400:
			good++
			if code == 0 {
				code = r.code
			}
		case r.code == 400 || r.code == 401 || r.code == 403 || r.code == 404 || r.code == 405:
			softBad++
			if code == 0 {
				code = r.code
			}
		default:
			overload++
			if code == 0 {
				code = r.code
			}
		}
	}
	if !tcpOK {
		netFail++
	}

	avg := (head.rtt + get.rtt + tcpRTT) / 3

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	if now.Before(p.baseUntil) && good > 0 {
		if p.baseN == 0 {
			p.baseline = avg
		} else {
			p.baseline = (p.baseline*time.Duration(p.baseN) + avg) / time.Duration(p.baseN+1)
		}
		p.baseN++
	}

	candidate := core.HealthOnline
	switch {
	case good > 0:
		if p.baseline > 0 && avg > 3*p.baseline && avg > 300*time.Millisecond {
			candidate = core.HealthDegraded
		}
	case netFail+overload >= 2 && softBad == 0:
		candidate = core.HealthOffline
	default:
		candidate = core.HealthDegraded
	}

	next := candidate
	if next == core.HealthOffline {
		p.failStreak++
		if p.failStreak < 2 {
			next = core.HealthDegraded
		}
	} else {
		p.failStreak = 0
	}

	if next != p.status.State {
		p.status.Since = now
		if next == core.HealthOffline {
			p.status.DownSince = now
		}
	}
	p.status.State = next
	if code != 0 {
		p.status.Code = code
	}
}

func (p *Prober) httpProbe(ctx context.Context, method, url string) probeResult {
	t0 := time.Now()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return probeResult{err: err, rtt: time.Since(t0)}
	}
	req.Host = p.tgt.HostHeader()
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := p.client.Do(req)
	if err != nil {
		return probeResult{err: err, rtt: time.Since(t0)}
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()
	return probeResult{code: resp.StatusCode, rtt: time.Since(t0)}
}
