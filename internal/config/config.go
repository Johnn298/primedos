package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Target           string   `yaml:"target"`
	Level            int      `yaml:"level"`
	Mode             int      `yaml:"mode"`
	Duration         string   `yaml:"duration"`
	Concurrency      int      `yaml:"concurrency"`
	RateLimitMbps    float64  `yaml:"rate_limit_mbps"`
	ProxiesFile      string   `yaml:"proxies_file"`
	Paths            []string `yaml:"paths"`
	UserAgents       []string `yaml:"user_agents"`
	TimeoutMS        int      `yaml:"timeout_ms"`
	HealthIntervalMS int      `yaml:"health_interval_ms"`
	HWTimestamps     bool     `yaml:"hw_timestamps"`
	DryRun           bool     `yaml:"dry_run"`
	Verbose          bool     `yaml:"verbose"`
}

func Default() *Config {
	return &Config{
		Level:            1,
		Mode:             1,
		Duration:         "1m",
		Concurrency:      256,
		RateLimitMbps:    0,
		Paths:            []string{"/", "/search?q=test", "/api/v1/items", "/login", "/cart"},
		UserAgents:       defaultUserAgents(),
		TimeoutMS:        5000,
		HealthIntervalMS: 500,
		HWTimestamps:     true,
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение конфига: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("разбор конфига: %w", err)
	}
	if len(cfg.Paths) == 0 {
		cfg.Paths = Default().Paths
	}
	if len(cfg.UserAgents) == 0 {
		cfg.UserAgents = Default().UserAgents
	}
	return cfg, nil
}

func (c *Config) Timeout() time.Duration {
	if c.TimeoutMS <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.TimeoutMS) * time.Millisecond
}

func (c *Config) HealthInterval() time.Duration {
	if c.HealthIntervalMS <= 0 {
		return 500 * time.Millisecond
	}
	return time.Duration(c.HealthIntervalMS) * time.Millisecond
}

func (c *Config) DurationValue() time.Duration {
	d, err := time.ParseDuration(c.Duration)
	if err != nil || d <= 0 {
		return time.Minute
	}
	return d
}

func (c *Config) Validate() error {
	if c.Level < 1 || c.Level > 3 {
		return fmt.Errorf("уровень должен быть 1..3, получено %d", c.Level)
	}
	if c.Mode < 1 || c.Mode > 3 {
		return fmt.Errorf("режим должен быть 1..3, получено %d", c.Mode)
	}
	if c.Concurrency < 1 {
		return fmt.Errorf("число воркеров должно быть > 0")
	}
	if c.Mode == 3 {
		if _, err := time.ParseDuration(c.Duration); err != nil {
			return fmt.Errorf("длительность %q не разобрана: %w", c.Duration, err)
		}
	}
	return nil
}

func defaultUserAgents() []string {
	return []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
	}
}
