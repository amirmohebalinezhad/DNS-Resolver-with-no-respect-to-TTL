package cache

import (
	"time"

	"dns-swr/internal/dnsmsg"

	"github.com/miekg/dns"
)

type Entry struct {
	Key          string
	WireResponse []byte

	RCode       int
	OriginalTTL uint32

	FetchedAt  time.Time
	ExpiresAt  time.Time
	StaleUntil time.Time

	IsNegative bool
}

func (e *Entry) Clone() *Entry {
	if e == nil {
		return nil
	}
	clone := *e
	if e.WireResponse != nil {
		clone.WireResponse = append([]byte(nil), e.WireResponse...)
	}
	return &clone
}

type Persistence interface {
	Load(now time.Time) ([]*Entry, error)
	Save(entry *Entry) error
	Delete(key string) error
	Flush() error
	Close() error
}

func EntryFromResponse(key string, msg *dns.Msg, now time.Time, maxStale time.Duration, negativeCacheTTL uint32) (*Entry, bool) {
	ttl, isNegative, cacheable := dnsmsg.CacheTTL(msg, negativeCacheTTL)
	if !cacheable {
		return nil, false
	}

	wire, err := msg.Pack()
	if err != nil {
		return nil, false
	}

	expiresAt := now.Add(time.Duration(ttl) * time.Second)
	return &Entry{
		Key:          key,
		WireResponse: wire,
		RCode:        msg.Rcode,
		OriginalTTL:  ttl,
		FetchedAt:    now,
		ExpiresAt:    expiresAt,
		StaleUntil:   expiresAt.Add(maxStale),
		IsNegative:   isNegative,
	}, true
}
