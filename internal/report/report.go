package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"primedos/internal/core"
)

type Report struct {
	Target       string   `json:"target"`
	IP           string   `json:"ip"`
	CDN          string   `json:"cdn"`
	Level        int      `json:"level"`
	Mode         int      `json:"mode"`
	Workers      int      `json:"workers"`
	Vectors      []string `json:"vectors"`
	DurationSec  float64  `json:"duration_sec"`
	TimeToDown   float64  `json:"time_to_down_sec"`
	TotalWire    uint64   `json:"total_wire_bytes"`
	TotalPayload uint64   `json:"total_payload_bytes"`
	TotalReq     uint64   `json:"total_requests"`
	BpsAvg       float64  `json:"bps_avg"`
	BpsPeak      float64  `json:"bps_peak"`
	BpsP95       float64  `json:"bps_p95"`
	PpsAvg       float64  `json:"pps_avg"`
	RpsAvg       float64  `json:"rps_avg"`
	RTTp50Ms     float64  `json:"rtt_p50_ms"`
	RTTp95Ms     float64  `json:"rtt_p95_ms"`
	RTTp99Ms     float64  `json:"rtt_p99_ms"`
	LossPct      float64  `json:"loss_pct"`
	Errors       uint64   `json:"errors"`
	Proxies      int      `json:"proxies_alive"`
	FinalState   string   `json:"final_state"`
	FinalCode    int      `json:"final_code"`
	GeneratedAt  string   `json:"generated_at"`
}

func Build(st core.Stats, peak float64) Report {
	return Report{
		Target:       st.Target,
		IP:           st.IP,
		CDN:          st.CDN,
		Level:        st.Level,
		Mode:         st.Mode,
		Workers:      st.Workers,
		Vectors:      st.Vectors,
		DurationSec:  st.Elapsed.Seconds(),
		TimeToDown:   st.TimeToDown.Seconds(),
		TotalWire:    st.TotalWire,
		TotalPayload: st.TotalPayload,
		TotalReq:     st.TotalReq,
		BpsAvg:       st.Bps,
		BpsPeak:      peak,
		BpsP95:       st.Bps,
		PpsAvg:       st.Pps,
		RpsAvg:       st.Rps,
		RTTp50Ms:     float64(st.P50.Microseconds()) / 1000,
		RTTp95Ms:     float64(st.P95.Microseconds()) / 1000,
		RTTp99Ms:     float64(st.P99.Microseconds()) / 1000,
		LossPct:      st.Loss,
		Errors:       st.Errors,
		Proxies:      st.Proxies,
		FinalState:   st.Health.State.String(),
		FinalCode:    st.Health.Code,
		GeneratedAt:  time.Now().Format(time.RFC3339),
	}
}

func Write(dir string, r Report) (string, string, error) {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		}
		return '_'
	}, r.Target)
	if safe == "" {
		safe = "target"
	}
	ts := time.Now().Format("20060102_150405")
	jsonPath := filepath.Join(dir, fmt.Sprintf("report_%s_%s.json", safe, ts))
	csvPath := filepath.Join(dir, fmt.Sprintf("report_%s_%s.csv", safe, ts))

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o644); err != nil {
		return "", "", err
	}

	var sb strings.Builder
	sb.WriteString("key,value\n")
	rows := [][2]string{
		{"target", r.Target},
		{"ip", r.IP},
		{"cdn", r.CDN},
		{"level", fmt.Sprint(r.Level)},
		{"mode", fmt.Sprint(r.Mode)},
		{"workers", fmt.Sprint(r.Workers)},
		{"vectors", strings.Join(r.Vectors, "|")},
		{"duration_sec", fmt.Sprintf("%.2f", r.DurationSec)},
		{"time_to_down_sec", fmt.Sprintf("%.2f", r.TimeToDown)},
		{"total_wire_bytes", fmt.Sprint(r.TotalWire)},
		{"total_payload_bytes", fmt.Sprint(r.TotalPayload)},
		{"total_requests", fmt.Sprint(r.TotalReq)},
		{"bps_avg", fmt.Sprintf("%.0f", r.BpsAvg)},
		{"bps_peak", fmt.Sprintf("%.0f", r.BpsPeak)},
		{"pps_avg", fmt.Sprintf("%.0f", r.PpsAvg)},
		{"rps_avg", fmt.Sprintf("%.0f", r.RpsAvg)},
		{"rtt_p50_ms", fmt.Sprintf("%.3f", r.RTTp50Ms)},
		{"rtt_p95_ms", fmt.Sprintf("%.3f", r.RTTp95Ms)},
		{"rtt_p99_ms", fmt.Sprintf("%.3f", r.RTTp99Ms)},
		{"loss_pct", fmt.Sprintf("%.3f", r.LossPct)},
		{"errors", fmt.Sprint(r.Errors)},
		{"proxies_alive", fmt.Sprint(r.Proxies)},
		{"final_state", r.FinalState},
		{"final_code", fmt.Sprint(r.FinalCode)},
		{"generated_at", r.GeneratedAt},
	}
	for _, row := range rows {
		sb.WriteString(row[0])
		sb.WriteString(",")
		sb.WriteString(strings.ReplaceAll(row[1], ",", ";"))
		sb.WriteString("\n")
	}
	if err := os.WriteFile(csvPath, []byte(sb.String()), 0o644); err != nil {
		return "", "", err
	}
	return jsonPath, csvPath, nil
}
