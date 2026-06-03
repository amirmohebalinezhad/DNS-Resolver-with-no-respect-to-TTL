# dns-swr

`dns-swr` is a Go forwarding DNS resolver with ordered upstreams, an in-memory cache, stale-while-revalidate behavior, TXT/AAAA query refusal, UDP/TCP listeners, structured logs, YAML config, and a small admin API.

It is not a recursive resolver and it is not authoritative DNS. It forwards queries to configured upstream DNS servers and caches the resulting wire responses.

## Features

- Ordered upstream resolution
- UDP and TCP DNS listeners
- Generic record type forwarding
- UDP upstream retry over TCP when responses are truncated
- Fresh cache hits with capped client TTLs
- Stale cache hits with a short client TTL
- Background refresh with singleflight deduplication
- Optional long stale retention
- Optional bbolt persistence
- Negative caching for NXDOMAIN and NODATA
- TXT and AAAA query refusal before cache or upstream access
- JSON `slog` structured logs
- Admin `/health`, `/stats`, and `/cache/flush` endpoints

## Run

```powershell
go mod tidy
go test ./...
go run ./cmd/dns-swr -config configs/config.example.yaml
```

Binding to port `53` may require administrator privileges. For local testing, change the listeners to `127.0.0.1:5353`.

```yaml
listen:
  udp: "127.0.0.1:5353"
  tcp: "127.0.0.1:5353"
```

Then query it:

```bash
dig @127.0.0.1 -p 5353 example.com A
dig @127.0.0.1 -p 5353 gmail.com MX
dig @127.0.0.1 -p 5353 example.com TXT
dig @127.0.0.1 -p 5353 google.com AAAA
```

TXT queries return `REFUSED` when `policy.blockTXT` is enabled. AAAA queries return `REFUSED` when `policy.blockAAAA` is enabled.

## Upstreams From Env

Configured upstreams can be overridden at runtime:

```bash
DNS_SWR_UPSTREAMS="1.1.1.1,8.8.8.8:53,9.9.9.9" dns-swr -config configs/config.example.yaml
```

Bare providers automatically use port `53`. `DNS_SWR_REMOTE_DNS_PROVIDERS` is also accepted as an alias.

## Cache Behavior

Fresh answers are returned immediately from cache. The client-facing TTL is:

```text
min(remaining TTL, cache.maxFreshClientTTL)
```

Expired answers inside `cache.maxStale` are served stale immediately. The client-facing TTL is:

```text
cache.staleClientTTL
```

Stale hits trigger an asynchronous refresh. A refresh only replaces the cache when upstream resolution succeeds with a cacheable response.

Negative answers are cached for `cache.negativeCacheTTL`. Stale negative answers are not served unless `cache.serveStaleForNegative` is set to `true`.

TXT queries are never cached. Blocked TXT and AAAA queries are refused before cache and upstream logic.

## Admin API

```bash
curl http://127.0.0.1:8053/health
curl http://127.0.0.1:8053/stats
curl -X POST http://127.0.0.1:8053/cache/flush
```

## Persistence

Memory cache is the default:

```yaml
cache:
  persistence: "memory"
```

To enable bbolt persistence:

```yaml
cache:
  persistence: "bbolt"
  path: "./dns-swr.bbolt"
```

On startup, usable entries are loaded into memory. Entries older than `staleUntil` are ignored and removed.

## Deployment

Build a container:

```bash
docker build -t dns-swr .
docker run --rm -p 5353:53/udp -p 5353:53/tcp dns-swr
```

The example systemd unit is in `configs/dns-swr.service`. Adjust paths, user, and capabilities for your host.
