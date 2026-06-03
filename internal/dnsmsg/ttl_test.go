package dnsmsg

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestCacheTTLPositiveUsesMinimumAnswerTTL(t *testing.T) {
	msg := new(dns.Msg)
	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	msg.SetReply(req)
	msg.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.IPv4(192, 0, 2, 1),
		},
		&dns.A{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   net.IPv4(192, 0, 2, 2),
		},
	}

	ttl, negative, cacheable := CacheTTL(msg, 60)
	if !cacheable {
		t.Fatal("expected response to be cacheable")
	}
	if negative {
		t.Fatal("expected positive response")
	}
	if ttl != 120 {
		t.Fatalf("ttl = %d, want 120", ttl)
	}
}

func TestCacheTTLNegativeForNXDOMAIN(t *testing.T) {
	msg := new(dns.Msg)
	req := new(dns.Msg)
	req.SetQuestion("missing.example.", dns.TypeA)
	msg.SetReply(req)
	msg.Rcode = dns.RcodeNameError

	ttl, negative, cacheable := CacheTTL(msg, 300)
	if !cacheable {
		t.Fatal("expected NXDOMAIN to be cacheable")
	}
	if !negative {
		t.Fatal("expected negative response")
	}
	if ttl != 300 {
		t.Fatalf("ttl = %d, want 300", ttl)
	}
}

func TestCloneWithIDAndTTL(t *testing.T) {
	msg := new(dns.Msg)
	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	msg.SetReply(req)
	msg.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   net.IPv4(192, 0, 2, 1),
		},
	}

	wire, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}

	cloned, err := CloneWithIDAndTTL(wire, 4242, 30)
	if err != nil {
		t.Fatal(err)
	}
	if cloned.Id != 4242 {
		t.Fatalf("id = %d, want 4242", cloned.Id)
	}
	if got := cloned.Answer[0].Header().Ttl; got != 30 {
		t.Fatalf("ttl = %d, want 30", got)
	}
}
