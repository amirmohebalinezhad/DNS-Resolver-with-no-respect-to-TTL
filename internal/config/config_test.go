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

func TestApplyEnvOverridesForStaticRecords(t *testing.T) {
	t.Setenv("DNS_SWR_STATIC_RECORDS", "router.lan=192.168.88.1,api.local:10.0.0.5")
	t.Setenv("DNS_SWR_STATIC_TTL", "120")

	cfg := DefaultConfig()
	cfg.applyEnvOverrides()
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Static.Records["router.lan."], "192.168.88.1"; got != want {
		t.Fatalf("router.lan static record = %q, want %q", got, want)
	}
	if got, want := cfg.Static.Records["api.local."], "10.0.0.5"; got != want {
		t.Fatalf("api.local static record = %q, want %q", got, want)
	}
	if got, want := cfg.Static.TTL, uint32(120); got != want {
		t.Fatalf("static ttl = %d, want %d", got, want)
	}
}

func TestValidateStaticRecordsRejectsNonIPv4(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Static.Records = map[string]string{"api.local.": "2001:db8::1"}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid IPv6 static A record to fail validation")
	}
}
