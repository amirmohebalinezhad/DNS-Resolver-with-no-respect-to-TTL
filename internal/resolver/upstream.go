package resolver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/miekg/dns"
)

type Resolver interface {
	Resolve(ctx context.Context, req *dns.Msg) (*dns.Msg, string, time.Duration, error)
}

type UpstreamResolver struct {
	upstreams       []string
	timeout         time.Duration
	retryOnSERVFAIL bool
	logger          *slog.Logger
}

func NewUpstreamResolver(upstreams []string, timeout time.Duration, retryOnSERVFAIL bool, logger *slog.Logger) *UpstreamResolver {
	return &UpstreamResolver{
		upstreams:       append([]string(nil), upstreams...),
		timeout:         timeout,
		retryOnSERVFAIL: retryOnSERVFAIL,
		logger:          logger,
	}
}

func (r *UpstreamResolver) Resolve(ctx context.Context, req *dns.Msg) (*dns.Msg, string, time.Duration, error) {
	if req == nil || len(req.Question) == 0 {
		return nil, "", 0, errors.New("request has no questions")
	}
	if len(r.upstreams) == 0 {
		return nil, "", 0, errors.New("no upstreams configured")
	}

	var lastErr error
	var lastSERVFAIL *dns.Msg
	var lastSERVFAILUpstream string
	var lastSERVFAILDuration time.Duration

	for _, upstream := range r.upstreams {
		start := time.Now()
		resp, err := r.exchange(ctx, upstream, "udp", req)
		if err != nil {
			lastErr = err
			r.logDebug("upstream_try_failed", upstream, "udp", time.Since(start), err, -1)
			continue
		}

		if resp.Truncated {
			resp, err = r.exchange(ctx, upstream, "tcp", req)
			if err != nil {
				lastErr = err
				r.logDebug("upstream_try_failed", upstream, "tcp", time.Since(start), err, -1)
				continue
			}
		}

		duration := time.Since(start)
		if resp == nil {
			lastErr = fmt.Errorf("malformed response from %s", upstream)
			r.logDebug("upstream_try_failed", upstream, "udp", duration, lastErr, -1)
			continue
		}
		if resp.Id != req.Id {
			lastErr = fmt.Errorf("malformed response from %s: mismatched dns id", upstream)
			r.logDebug("upstream_try_failed", upstream, "udp", duration, lastErr, resp.Rcode)
			continue
		}
		if resp.Rcode == dns.RcodeServerFailure && r.retryOnSERVFAIL {
			lastSERVFAIL = resp
			lastSERVFAILUpstream = upstream
			lastSERVFAILDuration = duration
			lastErr = fmt.Errorf("servfail from %s", upstream)
			r.logDebug("upstream_servfail_retry", upstream, "udp", duration, lastErr, resp.Rcode)
			continue
		}

		return resp, upstream, duration, nil
	}

	if lastSERVFAIL != nil {
		return lastSERVFAIL, lastSERVFAILUpstream, lastSERVFAILDuration, nil
	}
	if lastErr == nil {
		lastErr = errors.New("all upstreams failed")
	}
	return nil, "", 0, lastErr
}

func (r *UpstreamResolver) exchange(ctx context.Context, upstream, network string, req *dns.Msg) (*dns.Msg, error) {
	exchangeCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	client := &dns.Client{
		Net:     network,
		Timeout: r.timeout,
	}
	resp, _, err := client.ExchangeContext(exchangeCtx, req.Copy(), upstream)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (r *UpstreamResolver) logDebug(event, upstream, network string, duration time.Duration, err error, rcode int) {
	if r.logger == nil {
		return
	}
	attrs := []any{
		"upstream", upstream,
		"network", network,
		"duration_ms", duration.Milliseconds(),
	}
	if rcode >= 0 {
		attrs = append(attrs, "rcode", dns.RcodeToString[rcode])
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	r.logger.Debug(event, attrs...)
}
