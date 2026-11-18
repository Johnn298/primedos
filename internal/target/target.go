package target

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Target struct {
	Raw    string
	Scheme string
	Host   string
	Port   int
	Path   string
	IPs    []string
	CDN    string
}

func (t *Target) Addr() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

func (t *Target) BaseURL() string {
	return t.Scheme + "://" + t.HostHeader()
}

func (t *Target) HostHeader() string {
	if (t.Scheme == "https" && t.Port == 443) || (t.Scheme == "http" && t.Port == 80) {
		return t.Host
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

func (t *Target) IP() string {
	for _, s := range t.IPs {
		if strings.Count(s, ".") == 3 {
			return s
		}
	}
	if len(t.IPs) > 0 {
		return t.IPs[0]
	}
	return ""
}

func Resolve(ctx context.Context, raw string, timeout time.Duration, probeCDN bool) (*Target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("пустая цель")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("не разобран URL %q: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("поддерживаются только http/https, получено %q", scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("в %q нет имени хоста", raw)
	}
	port := u.Port()
	portNum := 0
	if port != "" {
		portNum, err = strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			return nil, fmt.Errorf("некорректный порт %q", port)
		}
	} else if scheme == "https" {
		portNum = 443
	} else {
		portNum = 80
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}

	t := &Target{Raw: raw, Scheme: scheme, Host: host, Port: portNum, Path: path}

	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(rctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS %s: %w", host, err)
	}
	seen := map[string]bool{}
	for _, a := range addrs {
		s := a.IP.String()
		if !seen[s] {
			seen[s] = true
			t.IPs = append(t.IPs, s)
		}
	}
	if len(t.IPs) == 0 {
		return nil, fmt.Errorf("DNS не вернул адресов для %s", host)
	}
	t.CDN = detectCDN(t, probeCDN)
	return t, nil
}

func detectCDN(t *Target, probe bool) string {
	if name := cdnByIP(t.IPs); name != "" {
		return name
	}
	if !probe {
		return ""
	}
	client := &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DialContext:     (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequest(http.MethodHead, t.BaseURL()+t.Path, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0 Safari/537.36")
	req.Host = t.HostHeader()
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	h := resp.Header
	switch {
	case h.Get("Cf-Ray") != "" || strings.Contains(strings.ToLower(h.Get("Server")), "cloudflare"):
		return "Cloudflare"
	case h.Get("X-Akamai-Request-Id") != "" || strings.Contains(strings.ToLower(h.Get("Server")), "akamai") || h.Get("X-Akamai-Transformed") != "":
		return "Akamai"
	case h.Get("X-Served-By") != "" || h.Get("X-Fastly-Request-Id") != "":
		return "Fastly"
	case h.Get("X-Sucuri-Id") != "":
		return "Sucuri"
	case h.Get("X-Iinfo") != "" || h.Get("X-Cdn") == "Incapsula":
		return "Imperva"
	}
	return ""
}

var cdnNets = []struct {
	name string
	cidr string
}{
	{"Cloudflare", "104.16.0.0/13"},
	{"Cloudflare", "172.64.0.0/13"},
	{"Cloudflare", "173.245.48.0/20"},
	{"Cloudflare", "162.158.0.0/15"},
	{"Cloudflare", "141.101.64.0/18"},
	{"Cloudflare", "108.162.192.0/18"},
	{"Cloudflare", "190.93.240.0/20"},
	{"Cloudflare", "188.114.96.0/20"},
	{"Cloudflare", "197.234.240.0/22"},
	{"Cloudflare", "198.41.128.0/17"},
	{"Akamai", "23.32.0.0/11"},
	{"Akamai", "104.64.0.0/10"},
	{"Fastly", "151.101.0.0/16"},
	{"Fastly", "199.232.0.0/16"},
}

func cdnByIP(ips []string) string {
	for _, s := range ips {
		ip := net.ParseIP(s)
		if ip == nil {
			continue
		}
		for _, n := range cdnNets {
			_, cidr, err := net.ParseCIDR(n.cidr)
			if err == nil && cidr.Contains(ip) {
				return n.name
			}
		}
	}
	return ""
}
