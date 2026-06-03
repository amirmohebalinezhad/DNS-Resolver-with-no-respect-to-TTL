package dnsmsg

import (
	"testing"

	"github.com/miekg/dns"
)

func TestKeyFromQuestion(t *testing.T) {
	key := KeyFromQuestion(dns.Question{
		Name:   "Example.COM",
		Qtype:  dns.TypeA,
		Qclass: dns.ClassINET,
	})

	want := "example.com.|A|IN"
	if key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
}

func TestQuestionTypeHelpers(t *testing.T) {
	msg := new(dns.Msg)
	msg.Question = []dns.Question{
		{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
		{Name: "example.com.", Qtype: dns.TypeTXT, Qclass: dns.ClassINET},
		{Name: "example.com.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET},
	}

	if !HasTXTQuestion(msg) {
		t.Fatal("expected TXT question to be detected")
	}
	if !HasAAAAQuestion(msg) {
		t.Fatal("expected AAAA question to be detected")
	}
}

func TestBlockedQuestion(t *testing.T) {
	msg := new(dns.Msg)
	msg.Question = []dns.Question{
		{Name: "example.com.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET},
	}

	qtype, name, blocked := BlockedQuestion(msg, true, true)
	if !blocked {
		t.Fatal("expected blocked question")
	}
	if qtype != dns.TypeAAAA || name != "AAAA" {
		t.Fatalf("blocked question = %d/%s, want AAAA", qtype, name)
	}
}
