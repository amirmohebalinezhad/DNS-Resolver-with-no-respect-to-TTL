package server

import (
	"context"
	"log/slog"
	"net"
	"time"

	"dns-swr/internal/cache"
	"dns-swr/internal/config"
	"dns-swr/internal/dnsmsg"
	"dns-swr/internal/metrics"
	"dns-swr/internal/resolver"

	"github.com/miekg/dns"
)

type Handler struct {
	ctx       context.Context
	config    *config.Config
	cache     *cache.MemoryStore
	resolver  resolver.Resolver
	refresher *resolver.Refresher
	metrics   *metrics.Metrics
	logger    *slog.Logger
}

func NewHandler(ctx context.Context, cfg *config.Config, store *cache.MemoryStore, upstream resolver.Resolver, refresher *resolver.Refresher, metrics *metrics.Metrics, logger *slog.Logger) *Handler {
	return &Handler{
		ctx:       ctx,
		config:    cfg,
		cache:     store,
		resolver:  upstream,
		refresher: refresher,
		metrics:   metrics,
		logger:    logger,
	}
}

func (h *Handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	start := time.Now()
	qname, qtype := dnsmsg.QuestionNameAndType(r)
	defer h.recoverPanic(w, r, qname, qtype)

	if h.metrics != nil {
		h.metrics.IncTotalQueries()
	}

	if r == nil || len(r.Question) == 0 {
		h.writeFailure(w, r, dns.RcodeFormatError)
		h.logInfo("query_formerr", "remote", remoteAddr(w), "duration_ms", time.Since(start).Milliseconds())
		return
	}

	if blockedType, blockedName, blocked := dnsmsg.BlockedQuestion(r, h.config.Policy.BlockTXT, h.config.Policy.BlockAAAA); blocked {
		h.recordBlockedQuery(blockedType)
		event := "blocked_query_refused"
		if blockedType == dns.TypeTXT {
			event = "txt_query_refused"
		}
		if blockedType == dns.TypeAAAA {
			event = "aaaa_query_refused"
		}
		h.logInfo(
			event,
			"remote", remoteAddr(w),
			"qname", qname,
			"qtype", blockedName,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		refuseBlockedQuery(w, r)
		return
	}

	key, _, ok := dnsmsg.QuestionKey(r)
	if !ok {
		h.writeFailure(w, r, dns.RcodeFormatError)
		return
	}

	if !dnsmsg.HasTXTQuestion(r) {
		if h.tryCache(w, r, key, qname, qtype, start) {
			return
		}
	}

	if h.metrics != nil {
		h.metrics.IncCacheMisses()
	}

	resp, upstream, upstreamDuration, err := h.resolver.Resolve(h.ctx, r)
	if err != nil {
		if h.metrics != nil {
			h.metrics.IncUpstreamFailures()
		}
		h.logInfo(
			"upstream_resolution_failed",
			"remote", remoteAddr(w),
			"qname", qname,
			"qtype", qtype,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err.Error(),
		)
		h.writeFailure(w, r, dns.RcodeServerFailure)
		return
	}

	resp.Id = r.Id
	if resp.Rcode == dns.RcodeServerFailure {
		if h.metrics != nil {
			h.metrics.IncUpstreamFailures()
		}
	} else if h.metrics != nil {
		h.metrics.IncUpstreamSuccesses()
	}

	if !dnsmsg.HasTXTQuestion(r) {
		if entry, ok := cache.EntryFromResponse(key, resp, time.Now(), h.config.Cache.MaxStale.Duration, h.config.Cache.NegativeCacheTTL); ok {
			if err := h.cache.Set(entry); err != nil {
				h.logInfo("cache_store_failed", "key", key, "error", err.Error())
			} else {
				h.logInfo(
					"cache_store",
					"key", key,
					"rcode", dns.RcodeToString[resp.Rcode],
					"original_ttl", entry.OriginalTTL,
					"expires_at", entry.ExpiresAt.Format(time.RFC3339),
					"stale_until", entry.StaleUntil.Format(time.RFC3339),
					"is_negative", entry.IsNegative,
				)
			}
		}
	}

	if err := w.WriteMsg(resp); err != nil {
		h.logInfo("client_write_failed", "qname", qname, "qtype", qtype, "error", err.Error())
		return
	}

	h.logInfo(
		"query_forwarded",
		"remote", remoteAddr(w),
		"qname", qname,
		"qtype", qtype,
		"upstream", upstream,
		"upstream_duration_ms", upstreamDuration.Milliseconds(),
		"duration_ms", time.Since(start).Milliseconds(),
		"rcode", dns.RcodeToString[resp.Rcode],
	)
}

func (h *Handler) recordBlockedQuery(qtype uint16) {
	if h.metrics == nil {
		return
	}
	switch qtype {
	case dns.TypeTXT:
		h.metrics.IncTXTRefused()
	case dns.TypeAAAA:
		h.metrics.IncAAAARefused()
	}
}

func (h *Handler) tryCache(w dns.ResponseWriter, r *dns.Msg, key, qname, qtype string, start time.Time) bool {
	entry, ok := h.cache.Get(key)
	if !ok {
		return false
	}

	now := time.Now()
	if entry.ExpiresAt.After(now) {
		ttl := dnsmsg.RemainingTTL(entry.ExpiresAt, now, h.config.Cache.MaxFreshClientTTL)
		if err := h.writeCached(w, r, entry, ttl); err != nil {
			_ = h.cache.Delete(key)
			h.logInfo("cache_entry_malformed", "key", key, "error", err.Error())
			return false
		}
		if h.metrics != nil {
			h.metrics.IncCacheFreshHits()
		}
		h.logInfo(
			"cache_hit_fresh",
			"remote", remoteAddr(w),
			"qname", qname,
			"qtype", qtype,
			"key", key,
			"client_ttl", ttl,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return true
	}

	if entry.StaleUntil.After(now) && (!entry.IsNegative || h.config.Cache.ServeStaleForNegative) {
		ttl := h.config.Cache.StaleClientTTL
		if err := h.writeCached(w, r, entry, ttl); err != nil {
			_ = h.cache.Delete(key)
			h.logInfo("cache_entry_malformed", "key", key, "error", err.Error())
			return false
		}
		if h.metrics != nil {
			h.metrics.IncCacheStaleHits()
		}
		h.logInfo(
			"cache_hit_stale",
			"remote", remoteAddr(w),
			"qname", qname,
			"qtype", qtype,
			"key", key,
			"client_ttl", ttl,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		if h.refresher != nil {
			h.refresher.RefreshAsync(key, entry)
		}
		return true
	}

	if !entry.StaleUntil.After(now) {
		_ = h.cache.Delete(key)
	}
	return false
}

func (h *Handler) writeCached(w dns.ResponseWriter, r *dns.Msg, entry *cache.Entry, ttl uint32) error {
	msg, err := dnsmsg.CloneWithIDAndTTL(entry.WireResponse, r.Id, ttl)
	if err != nil {
		return err
	}
	return w.WriteMsg(msg)
}

func (h *Handler) writeFailure(w dns.ResponseWriter, r *dns.Msg, rcode int) {
	msg := new(dns.Msg)
	if r != nil {
		msg.SetReply(r)
	} else {
		msg.Response = true
	}
	msg.Rcode = rcode
	msg.Authoritative = false
	msg.RecursionAvailable = true
	_ = w.WriteMsg(msg)
}

func (h *Handler) recoverPanic(w dns.ResponseWriter, r *dns.Msg, qname, qtype string) {
	if recovered := recover(); recovered != nil {
		h.logInfo("dns_handler_panic", "qname", qname, "qtype", qtype, "panic", recovered)
		h.writeFailure(w, r, dns.RcodeServerFailure)
	}
}

func refuseBlockedQuery(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Rcode = dns.RcodeRefused
	msg.Authoritative = false
	msg.RecursionAvailable = true
	_ = w.WriteMsg(msg)
}

func remoteAddr(w dns.ResponseWriter) string {
	if w == nil {
		return ""
	}
	addr := w.RemoteAddr()
	if addr == nil {
		return ""
	}
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.String()
	}
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		return udpAddr.String()
	}
	return addr.String()
}

func (h *Handler) logInfo(event string, attrs ...any) {
	if h.logger != nil {
		h.logger.Info(event, attrs...)
	}
}
