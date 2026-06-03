package resolver

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"dns-swr/internal/cache"
	"dns-swr/internal/config"
	"dns-swr/internal/dnsmsg"
	"dns-swr/internal/metrics"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

type Refresher struct {
	ctx              context.Context
	cancel           context.CancelFunc
	upstream         Resolver
	store            *cache.MemoryStore
	maxStale         time.Duration
	negativeCacheTTL uint32
	sem              chan struct{}
	group            singleflight.Group
	wg               sync.WaitGroup
	metrics          *metrics.Metrics
	logger           *slog.Logger
}

func NewRefresher(parent context.Context, upstream Resolver, store *cache.MemoryStore, cfg *config.Config, metrics *metrics.Metrics, logger *slog.Logger) *Refresher {
	ctx, cancel := context.WithCancel(parent)
	concurrency := cfg.Cache.RefreshConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	return &Refresher{
		ctx:              ctx,
		cancel:           cancel,
		upstream:         upstream,
		store:            store,
		maxStale:         cfg.Cache.MaxStale.Duration,
		negativeCacheTTL: cfg.Cache.NegativeCacheTTL,
		sem:              make(chan struct{}, concurrency),
		metrics:          metrics,
		logger:           logger,
	}
}

func (r *Refresher) RefreshAsync(key string, entry *cache.Entry) {
	if r == nil || entry == nil {
		return
	}
	snapshot := entry.Clone()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		_, _, _ = r.group.Do(key, func() (interface{}, error) {
			return nil, r.refresh(key, snapshot)
		})
	}()
}

func (r *Refresher) refresh(key string, entry *cache.Entry) error {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-r.ctx.Done():
		return r.ctx.Err()
	}

	r.logInfo("refresh_started", "key", key)

	req, err := requestFromEntry(entry)
	if err != nil {
		r.recordRefreshFailure("refresh_failed", key, "", 0, err)
		return err
	}

	resp, upstream, duration, err := r.upstream.Resolve(r.ctx, req)
	if err != nil {
		r.recordRefreshFailure("refresh_failed", key, upstream, duration, err)
		return err
	}

	newEntry, ok := cache.EntryFromResponse(key, resp, time.Now(), r.maxStale, r.negativeCacheTTL)
	if !ok {
		err := errors.New("refresh response is not cacheable")
		r.recordRefreshFailure("refresh_failed", key, upstream, duration, err)
		return err
	}
	if err := r.store.Set(newEntry); err != nil {
		r.recordRefreshFailure("refresh_failed", key, upstream, duration, err)
		return err
	}

	if r.metrics != nil {
		r.metrics.IncRefreshSuccesses()
	}
	r.logInfo(
		"refresh_success",
		"key", key,
		"upstream", upstream,
		"duration_ms", duration.Milliseconds(),
		"rcode", dns.RcodeToString[resp.Rcode],
		"expires_at", newEntry.ExpiresAt.Format(time.RFC3339),
	)
	return nil
}

func (r *Refresher) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.cancel()
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func requestFromEntry(entry *cache.Entry) (*dns.Msg, error) {
	msg := new(dns.Msg)
	if err := msg.Unpack(entry.WireResponse); err != nil {
		return nil, err
	}
	if len(msg.Question) == 0 {
		return nil, errors.New("cached response has no question")
	}
	req := new(dns.Msg)
	req.Id = dns.Id()
	req.RecursionDesired = true
	req.Question = []dns.Question{msg.Question[0]}
	if dnsmsg.HasTXTQuestion(req) {
		return nil, errors.New("txt records are not refreshed")
	}
	return req, nil
}

func (r *Refresher) recordRefreshFailure(event, key, upstream string, duration time.Duration, err error) {
	if r.metrics != nil {
		r.metrics.IncRefreshFailures()
	}
	r.logInfo(
		event,
		"key", key,
		"upstream", upstream,
		"duration_ms", duration.Milliseconds(),
		"error", err.Error(),
	)
}

func (r *Refresher) logInfo(event string, attrs ...any) {
	if r.logger != nil {
		r.logger.Info(event, attrs...)
	}
}
