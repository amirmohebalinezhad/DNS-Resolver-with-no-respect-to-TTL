package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Value == "" {
			d.Duration = 0
			return nil
		}
		if value.Tag == "!!int" {
			seconds, err := strconv.ParseInt(value.Value, 10, 64)
			if err != nil {
				return err
			}
			d.Duration = time.Duration(seconds) * time.Second
			return nil
		}
		parsed, err := time.ParseDuration(value.Value)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	default:
		return fmt.Errorf("duration must be a string such as 1200ms or 720h")
	}
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.String(), nil
}

func (d Duration) String() string {
	return d.Duration.String()
}

type ListenConfig struct {
	UDP string `yaml:"udp"`
	TCP string `yaml:"tcp"`
}

type AdminConfig struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
}

type TimeoutConfig struct {
	UpstreamTimeout Duration `yaml:"upstreamTimeout"`
}

type CacheConfig struct {
	MaxStale              Duration `yaml:"maxStale"`
	StaleClientTTL        uint32   `yaml:"staleClientTTL"`
	MaxFreshClientTTL     uint32   `yaml:"maxFreshClientTTL"`
	NegativeCacheTTL      uint32   `yaml:"negativeCacheTTL"`
	ServeStaleForNegative bool     `yaml:"serveStaleForNegative"`
	RefreshConcurrency    int      `yaml:"refreshConcurrency"`
	Persistence           string   `yaml:"persistence"`
	Path                  string   `yaml:"path"`
}

type PolicyConfig struct {
	BlockTXT        bool `yaml:"blockTXT"`
	BlockAAAA       bool `yaml:"blockAAAA"`
	RetryOnSERVFAIL bool `yaml:"retryOnSERVFAIL"`
}

type Config struct {
	Listen    ListenConfig  `yaml:"listen"`
	Admin     AdminConfig   `yaml:"admin"`
	Upstreams []string      `yaml:"upstreams"`
	Timeouts  TimeoutConfig `yaml:"timeouts"`
	Cache     CacheConfig   `yaml:"cache"`
	Policy    PolicyConfig  `yaml:"policy"`
}

func DefaultConfig() Config {
	return Config{
		Listen: ListenConfig{
			UDP: ":53",
			TCP: ":53",
		},
		Admin: AdminConfig{
			Enabled: true,
			Address: "0.0.0.0:8053",
		},
		Upstreams: []string{
			"1.1.1.1:53",
			"8.8.8.8:53",
			"9.9.9.9:53",
			"208.67.222.222:53",
			"185.51.200.2:53",
		},
		Timeouts: TimeoutConfig{
			UpstreamTimeout: Duration{Duration: 1200 * time.Millisecond},
		},
		Cache: CacheConfig{
			MaxStale:              Duration{Duration: 720 * time.Hour},
			StaleClientTTL:        30,
			MaxFreshClientTTL:     3600,
			NegativeCacheTTL:      300,
			ServeStaleForNegative: false,
			RefreshConcurrency:    50,
			Persistence:           "memory",
			Path:                  "./dns-swr.bbolt",
		},
		Policy: PolicyConfig{
			BlockTXT:        true,
			BlockAAAA:       true,
			RetryOnSERVFAIL: true,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}

	cfg.applyEnvOverrides()
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	c.Cache.Persistence = strings.ToLower(strings.TrimSpace(c.Cache.Persistence))
	if c.Cache.Persistence == "" {
		c.Cache.Persistence = "memory"
	}
	if c.Cache.Path == "" {
		c.Cache.Path = "./dns-swr.bbolt"
	}
	if c.Cache.RefreshConcurrency == 0 {
		c.Cache.RefreshConcurrency = 50
	}
	if c.Timeouts.UpstreamTimeout.Duration == 0 {
		c.Timeouts.UpstreamTimeout = Duration{Duration: 1200 * time.Millisecond}
	}
	for i, upstream := range c.Upstreams {
		c.Upstreams[i] = normalizeProviderAddress(upstream)
	}
}

func (c *Config) applyEnvOverrides() {
	upstreams := strings.TrimSpace(os.Getenv("DNS_SWR_UPSTREAMS"))
	if upstreams == "" {
		upstreams = strings.TrimSpace(os.Getenv("DNS_SWR_REMOTE_DNS_PROVIDERS"))
	}
	if upstreams == "" {
		return
	}

	parts := strings.Split(upstreams, ",")
	parsed := make([]string, 0, len(parts))
	for _, part := range parts {
		provider := normalizeProviderAddress(part)
		if provider != "" {
			parsed = append(parsed, provider)
		}
	}
	if len(parsed) > 0 {
		c.Upstreams = parsed
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Listen.UDP) == "" && strings.TrimSpace(c.Listen.TCP) == "" {
		return fmt.Errorf("at least one DNS listener must be configured")
	}
	if c.Admin.Enabled && strings.TrimSpace(c.Admin.Address) == "" {
		return fmt.Errorf("admin.address is required when admin.enabled=true")
	}
	if c.Admin.Enabled {
		if err := validateAddress("admin.address", c.Admin.Address); err != nil {
			return err
		}
	}
	if len(c.Upstreams) == 0 {
		return fmt.Errorf("at least one upstream DNS server is required")
	}
	for i, upstream := range c.Upstreams {
		if strings.TrimSpace(upstream) == "" {
			return fmt.Errorf("upstreams[%d] is empty", i)
		}
		if err := validateAddress(fmt.Sprintf("upstreams[%d]", i), upstream); err != nil {
			return err
		}
	}
	if c.Timeouts.UpstreamTimeout.Duration <= 0 {
		return fmt.Errorf("timeouts.upstreamTimeout must be greater than zero")
	}
	if c.Cache.MaxStale.Duration < 0 {
		return fmt.Errorf("cache.maxStale must not be negative")
	}
	if c.Cache.StaleClientTTL == 0 {
		return fmt.Errorf("cache.staleClientTTL must be greater than zero")
	}
	if c.Cache.MaxFreshClientTTL == 0 {
		return fmt.Errorf("cache.maxFreshClientTTL must be greater than zero")
	}
	if c.Cache.NegativeCacheTTL == 0 {
		return fmt.Errorf("cache.negativeCacheTTL must be greater than zero")
	}
	if c.Cache.RefreshConcurrency <= 0 {
		return fmt.Errorf("cache.refreshConcurrency must be greater than zero")
	}
	switch c.Cache.Persistence {
	case "memory":
	case "bbolt":
		if strings.TrimSpace(c.Cache.Path) == "" {
			return fmt.Errorf("cache.path is required when cache.persistence=bbolt")
		}
	default:
		return fmt.Errorf("cache.persistence must be memory or bbolt")
	}
	return nil
}

func (c Config) Summary() string {
	admin := "disabled"
	if c.Admin.Enabled {
		admin = "enabled@" + c.Admin.Address
	}
	return fmt.Sprintf(
		"udp=%q tcp=%q admin=%s upstreams=%d cache=%s maxStale=%s staleClientTTL=%d",
		c.Listen.UDP,
		c.Listen.TCP,
		admin,
		len(c.Upstreams),
		c.Cache.Persistence,
		c.Cache.MaxStale,
		c.Cache.StaleClientTTL,
	)
}

func validateAddress(field, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s must be host:port: %w", field, err)
	}
	if port == "" {
		return fmt.Errorf("%s port is required", field)
	}
	if host == "" {
		return nil
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if strings.Contains(host, " ") {
		return fmt.Errorf("%s host is invalid", field)
	}
	return nil
}

func normalizeProviderAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		address = strings.TrimPrefix(strings.TrimSuffix(address, "]"), "[")
	}
	return net.JoinHostPort(address, "53")
}
