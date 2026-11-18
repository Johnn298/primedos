package vectors

import (
	"context"
	"math/rand"

	"primedos/internal/core"
)

type Def struct {
	Name     string
	Tier     int
	NeedsRaw bool
	Run      func(ctx context.Context, w *core.Worker) error
}

var Registry = []Def{
	{Name: "get-flood", Tier: 1, Run: GetFlood},
	{Name: "post-flood", Tier: 1, Run: PostFlood},
	{Name: "slowloris", Tier: 1, Run: Slowloris},
	{Name: "tls-flood", Tier: 2, Run: TLSFlood},
	{Name: "http2-reset", Tier: 2, Run: H2Reset},
	{Name: "continuation-flood", Tier: 2, Run: ContinuationFlood},
	{Name: "rudy", Tier: 2, Run: Rudy},
	{Name: "slow-read", Tier: 2, Run: SlowRead},
	{Name: "range-flood", Tier: 2, Run: RangeFlood},
	{Name: "api-flood", Tier: 2, Run: APIFlood},
	{Name: "syn-flood", Tier: 3, NeedsRaw: true, Run: SynFlood},
	{Name: "udp-flood", Tier: 3, Run: UDPFlood},
	{Name: "ws-flood", Tier: 3, Run: WSFlood},
}

func Select(level int) []Def {
	var out []Def
	for _, d := range Registry {
		if d.Tier <= level {
			out = append(out, d)
		}
	}
	return out
}

func Names(defs []Def) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

const hexDigits = "0123456789abcdef"

func randHex(r *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = hexDigits[r.Intn(16)]
	}
	return string(b)
}

func randAlpha(r *rand.Rand, n int) string {
	const alpha = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = alpha[r.Intn(len(alpha))]
	}
	return string(b)
}

func randBytes(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	r.Read(b)
	return b
}
