package config

import "testing"

func TestApplyEnvOverridesForUpstreams(t *testing.T) {
	t.Setenv("DNS_SWR_UPSTREAMS", "1.1.1.1, 8.8.8.8:53, dns.google")

	cfg := DefaultConfig()
	cfg.applyEnvOverrides()
	cfg.applyDefaults()

	want := []string{"1.1.1.1:53", "8.8.8.8:53", "dns.google:53"}
	if len(cfg.Upstreams) != len(want) {
		t.Fatalf("upstreams = %#v, want %#v", cfg.Upstreams, want)
	}
	for i := range want {
		if cfg.Upstreams[i] != want[i] {
			t.Fatalf("upstreams = %#v, want %#v", cfg.Upstreams, want)
		}
	}
}

func TestRemoteDNSProvidersAlias(t *testing.T) {
	t.Setenv("DNS_SWR_REMOTE_DNS_PROVIDERS", "9.9.9.9")

	cfg := DefaultConfig()
	cfg.applyEnvOverrides()
	cfg.applyDefaults()

	if got, want := cfg.Upstreams[0], "9.9.9.9:53"; got != want {
		t.Fatalf("first upstream = %q, want %q", got, want)
	}
}
