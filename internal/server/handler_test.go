package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"dns-swr/internal/cache"
	"dns-swr/internal/config"
	"dns-swr/internal/dnsmsg"
	"dns-swr/internal/metrics"

	"github.com/miekg/dns"
)

type fakeResolver struct {
	calls atomic.Int32
	fn    func(req *dns.Msg) (*dns.Msg, error)
}

func (r *fakeResolver) Resolve(_ context.Context, req *dns.Msg) (*dns.Msg, string, time.Duration, error) {
	r.calls.Add(1)
	resp, err := r.fn(req)
	return resp, "127.0.0.1:53535", time.Millisecond, err
}

type testResponseWriter struct {
	msg *dns.Msg
}

func (w *testResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5353}
}

func (w *testResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53000}
}

func (w *testResponseWriter) WriteMsg(msg *dns.Msg) error {
	w.msg = msg.Copy()
	return nil
}

func (w *testResponseWriter) Write(bytes []byte) (int, error) {
	msg := new(dns.Msg)
	if err := msg.Unpack(bytes); err != nil {
		return 0, err
	}
	w.msg = msg
	return len(bytes), nil
}

func (w *testResponseWriter) Close() error {
	return nil
}

func (w *testResponseWriter) TsigStatus() error {
	return nil
}

func (w *testResponseWriter) TsigTimersOnly(bool) {}

func (w *testResponseWriter) Hijack() {}

func TestTXTRefusedBypassesResolverAndCache(t *testing.T) {
	cfg := testConfig()
	store := cache.NewMemoryStore(nil)
	fake := &fakeResolver{fn: answerA}
	handler := testHandler(cfg, store, fake)

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeTXT)
	w := &testResponseWriter{}

	handler.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("expected response")
	}
	if w.msg.Rcode != dns.RcodeRefused {
		t.Fatalf("rcode = %s, want REFUSED", dns.RcodeToString[w.msg.Rcode])
	}
	if got := fake.calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if got := store.Len(); got != 0 {
		t.Fatalf("cache entries = %d, want 0", got)
	}
}

func TestAAAARefusedBypassesResolverAndCache(t *testing.T) {
	cfg := testConfig()
	store := cache.NewMemoryStore(nil)
	fake := &fakeResolver{fn: answerA}
	handler := testHandler(cfg, store, fake)

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeAAAA)
	w := &testResponseWriter{}

	handler.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("expected response")
	}
	if w.msg.Rcode != dns.RcodeRefused {
		t.Fatalf("rcode = %s, want REFUSED", dns.RcodeToString[w.msg.Rcode])
	}
	if got := fake.calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if got := store.Len(); got != 0 {
		t.Fatalf("cache entries = %d, want 0", got)
	}
}

func TestFreshCacheHit(t *testing.T) {
	cfg := testConfig()
	cfg.Cache.MaxFreshClientTTL = 60
	store := cache.NewMemoryStore(nil)
	fake := &fakeResolver{fn: answerA}
	handler := testHandler(cfg, store, fake)

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	key := dnsmsg.KeyFromQuestion(req.Question[0])

	cached := new(dns.Msg)
	cached.SetReply(req)
	cached.Answer = []dns.RR{aRecord("example.com.", 300)}
	wire, err := cached.Pack()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := store.Set(&cache.Entry{
		Key:          key,
		WireResponse: wire,
		RCode:        dns.RcodeSuccess,
		OriginalTTL:  300,
		FetchedAt:    now.Add(-time.Minute),
		ExpiresAt:    now.Add(5 * time.Minute),
		StaleUntil:   now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	clientReq := new(dns.Msg)
	clientReq.SetQuestion("example.com.", dns.TypeA)
	clientReq.Id = 1234
	w := &testResponseWriter{}

	handler.ServeDNS(w, clientReq)

	if got := fake.calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if w.msg.Id != 1234 {
		t.Fatalf("response id = %d, want 1234", w.msg.Id)
	}
	if got := w.msg.Answer[0].Header().Ttl; got != 60 {
		t.Fatalf("ttl = %d, want capped ttl 60", got)
	}
}

func TestStaleCacheHitReturnsLowTTL(t *testing.T) {
	cfg := testConfig()
	cfg.Cache.StaleClientTTL = 30
	store := cache.NewMemoryStore(nil)
	fake := &fakeResolver{fn: answerA}
	handler := testHandler(cfg, store, fake)

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	key := dnsmsg.KeyFromQuestion(req.Question[0])

	cached := new(dns.Msg)
	cached.SetReply(req)
	cached.Answer = []dns.RR{aRecord("example.com.", 300)}
	wire, err := cached.Pack()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := store.Set(&cache.Entry{
		Key:          key,
		WireResponse: wire,
		RCode:        dns.RcodeSuccess,
		OriginalTTL:  300,
		FetchedAt:    now.Add(-10 * time.Minute),
		ExpiresAt:    now.Add(-time.Minute),
		StaleUntil:   now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	clientReq := new(dns.Msg)
	clientReq.SetQuestion("example.com.", dns.TypeA)
	w := &testResponseWriter{}

	handler.ServeDNS(w, clientReq)

	if got := fake.calls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if got := w.msg.Answer[0].Header().Ttl; got != 30 {
		t.Fatalf("ttl = %d, want stale ttl 30", got)
	}
}

func TestCacheMissForwardsAndStores(t *testing.T) {
	cfg := testConfig()
	store := cache.NewMemoryStore(nil)
	fake := &fakeResolver{fn: answerA}
	handler := testHandler(cfg, store, fake)

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	w := &testResponseWriter{}

	handler.ServeDNS(w, req)

	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("cache entries = %d, want 1", got)
	}
	if w.msg == nil || w.msg.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected successful response, got %#v", w.msg)
	}
}

func testConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Listen.UDP = "127.0.0.1:0"
	cfg.Listen.TCP = "127.0.0.1:0"
	cfg.Admin.Enabled = false
	cfg.Cache.Persistence = "memory"
	cfg.Cache.MaxStale.Duration = time.Hour
	cfg.Upstreams = []string{"127.0.0.1:53535"}
	return &cfg
}

func testHandler(cfg *config.Config, store *cache.MemoryStore, upstream *fakeResolver) *Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHandler(context.Background(), cfg, store, upstream, nil, metrics.New(), logger)
}

func answerA(req *dns.Msg) (*dns.Msg, error) {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.RecursionAvailable = true
	resp.Answer = []dns.RR{aRecord(req.Question[0].Name, 120)}
	return resp, nil
}

func aRecord(name string, ttl uint32) dns.RR {
	return &dns.A{
		Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		A:   net.IPv4(192, 0, 2, 1),
	}
}
