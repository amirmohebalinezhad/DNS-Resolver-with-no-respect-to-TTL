package dnsmsg

import (
	"math"
	"time"

	"github.com/miekg/dns"
)

func CacheTTL(msg *dns.Msg, negativeTTL uint32) (ttl uint32, isNegative bool, cacheable bool) {
	if msg == nil {
		return 0, false, false
	}
	if msg.Rcode == dns.RcodeNameError {
		return negativeTTL, true, true
	}
	if msg.Rcode != dns.RcodeSuccess {
		return 0, false, false
	}
	if len(msg.Answer) == 0 {
		return negativeTTL, true, true
	}

	minTTL := ^uint32(0)
	seen := false
	for _, rr := range msg.Answer {
		if rr == nil || rr.Header().Rrtype == dns.TypeOPT {
			continue
		}
		seen = true
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
		}
	}
	if !seen {
		return negativeTTL, true, true
	}
	return minTTL, false, true
}

func RemainingTTL(expiresAt, now time.Time, maxClientTTL uint32) uint32 {
	if !expiresAt.After(now) {
		return 0
	}
	seconds := uint32(math.Ceil(expiresAt.Sub(now).Seconds()))
	if seconds == 0 {
		seconds = 1
	}
	if maxClientTTL > 0 && seconds > maxClientTTL {
		return maxClientTTL
	}
	return seconds
}

func AdjustTTLs(msg *dns.Msg, ttl uint32) {
	if msg == nil {
		return
	}
	for _, rr := range msg.Answer {
		setTTL(rr, ttl)
	}
	for _, rr := range msg.Ns {
		setTTL(rr, ttl)
	}
	for _, rr := range msg.Extra {
		setTTL(rr, ttl)
	}
}

func setTTL(rr dns.RR, ttl uint32) {
	if rr == nil {
		return
	}
	if rr.Header().Rrtype == dns.TypeOPT {
		return
	}
	rr.Header().Ttl = ttl
}
