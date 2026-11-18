package proxies

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type entry struct {
	u    *url.URL
	dead time.Time
	lat  time.Duration
}

type Pool struct {
	mu   sync.Mutex
	list []*entry
	idx  int
}

func Load(path string) (*Pool, error) {
	p := &Pool{}
	if strings.TrimSpace(path) == "" {
		return p, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("открытие %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "http://" + line
		}
		u, err := url.Parse(line)
		if err != nil || u.Host == "" {
			continue
		}
		p.list = append(p.list, &entry{u: u})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Pool) Enabled() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.list) > 0
}

func (p *Pool) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.list)
}

func (p *Pool) Alive() int {
	if p == nil {
		return 0
	}
	now := time.Now()
	n := 0
	p.mu.Lock()
	for _, e := range p.list {
		if e.dead.Before(now) {
			n++
		}
	}
	p.mu.Unlock()
	return n
}

func (p *Pool) Next() *url.URL {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.list) == 0 {
		return nil
	}
	now := time.Now()
	for i := 0; i < len(p.list); i++ {
		e := p.list[(p.idx+i)%len(p.list)]
		if e.dead.Before(now) {
			p.idx = (p.idx + i + 1) % len(p.list)
			return e.u
		}
	}
	return nil
}

func (p *Pool) MarkDead(u *url.URL, d time.Duration) {
	if p == nil || u == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.list {
		if e.u.Host == u.Host && e.u.Scheme == u.Scheme {
			e.dead = time.Now().Add(d)
			return
		}
	}
}

func (p *Pool) Check(ctx context.Context, timeout time.Duration) {
	if p == nil || !p.Enabled() {
		return
	}
	p.mu.Lock()
	list := make([]*entry, len(p.list))
	copy(list, p.list)
	p.mu.Unlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	for _, e := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(e *entry) {
			defer wg.Done()
			defer func() { <-sem }()
			d := net.Dialer{Timeout: timeout}
			t0 := time.Now()
			c, err := d.DialContext(ctx, "tcp", e.u.Host)
			if err != nil {
				p.MarkDead(e.u, 10*time.Minute)
				return
			}
			c.Close()
			p.mu.Lock()
			e.lat = time.Since(t0)
			e.dead = time.Time{}
			p.mu.Unlock()
		}(e)
	}
	wg.Wait()
}
