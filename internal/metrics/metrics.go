package metrics

import (
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"primedos/internal/core"
)

type Sample struct {
	At  time.Time
	Bps float64
	Pps float64
	Rps float64
}

type Sampler struct {
	mu sync.Mutex

	prevWire uint64
	prevPkts uint64
	prevReqs uint64
	prevAt   time.Time

	last     Sample
	ewmaBps  float64
	ewmaPps  float64
	ewmaRps  float64
	peakBps  float64
	sumBps   float64
	nSamples int
}

func New() *Sampler { return &Sampler{} }

func (s *Sampler) Observe(c *core.Counters) Sample {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	wire := c.BytesWire.Load()
	pkts := c.Packets.Load()
	reqs := c.Requests.Load()

	if s.prevAt.IsZero() {
		s.prevAt, s.prevWire, s.prevPkts, s.prevReqs = now, wire, pkts, reqs
		return Sample{At: now}
	}
	dt := now.Sub(s.prevAt).Seconds()
	if dt < 0.02 {
		return s.last
	}
	bps := float64(wire-s.prevWire) * 8 / dt
	pps := float64(pkts-s.prevPkts) / dt
	rps := float64(reqs-s.prevReqs) / dt

	const a = 0.2
	if s.nSamples == 0 {
		s.ewmaBps, s.ewmaPps, s.ewmaRps = bps, pps, rps
	} else {
		s.ewmaBps = a*bps + (1-a)*s.ewmaBps
		s.ewmaPps = a*pps + (1-a)*s.ewmaPps
		s.ewmaRps = a*rps + (1-a)*s.ewmaRps
	}
	s.nSamples++
	if bps > s.peakBps {
		s.peakBps = bps
	}
	s.sumBps += bps

	s.prevAt, s.prevWire, s.prevPkts, s.prevReqs = now, wire, pkts, reqs
	s.last = Sample{At: now, Bps: s.ewmaBps, Pps: s.ewmaPps, Rps: s.ewmaRps}
	return s.last
}

func (s *Sampler) Peak() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakBps
}

func (s *Sampler) Average() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nSamples == 0 {
		return 0
	}
	return s.sumBps / float64(s.nSamples)
}

func DefaultIface() string {
	out, err := exec.Command("ip", "route", "get", "1.1.1.1").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func ProbeHWTimestamps(iface string) (bool, string) {
	if iface == "" {
		return false, "интерфейс маршрута по умолчанию не определён"
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return false, "socket: " + err.Error()
	}
	defer unix.Close(fd)
	cfg, err := unix.IoctlGetHwTstamp(fd, iface)
	if err != nil {
		return false, "SIOCGHWTSTAMP " + iface + ": " + err.Error()
	}
	if cfg.Tx_type == unix.HWTSTAMP_TX_OFF && cfg.Rx_filter == unix.HWTSTAMP_FILTER_NONE {
		return false, iface + ": аппаратные метки не сконфигурированы"
	}
	return true, iface + ": аппаратные метки доступны"
}

func OffloadWarning(iface string) string {
	if iface == "" {
		return ""
	}
	out, err := exec.Command("ethtool", "-k", iface).Output()
	if err != nil {
		return ""
	}
	text := strings.ToLower(string(out))
	var on []string
	for _, f := range []string{"tso", "gro", "lro"} {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), f+":") && strings.Contains(line, "on") {
				on = append(on, strings.ToUpper(f))
			}
		}
	}
	if len(on) == 0 {
		return ""
	}
	return "offload включён (" + strings.Join(on, ", ") + "): размеры пакетов завышены"
}
