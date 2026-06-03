package metrics

import "sync/atomic"

type Metrics struct {
	totalQueries      atomic.Uint64
	cacheFreshHits    atomic.Uint64
	cacheStaleHits    atomic.Uint64
	cacheMisses       atomic.Uint64
	txtRefused        atomic.Uint64
	aaaaRefused       atomic.Uint64
	upstreamSuccesses atomic.Uint64
	upstreamFailures  atomic.Uint64
	refreshSuccesses  atomic.Uint64
	refreshFailures   atomic.Uint64
}

type Snapshot struct {
	TotalQueries      uint64 `json:"totalQueries"`
	CacheFreshHits    uint64 `json:"cacheFreshHits"`
	CacheStaleHits    uint64 `json:"cacheStaleHits"`
	CacheMisses       uint64 `json:"cacheMisses"`
	TXTRefused        uint64 `json:"txtRefused"`
	AAAARefused       uint64 `json:"aaaaRefused"`
	UpstreamSuccesses uint64 `json:"upstreamSuccesses"`
	UpstreamFailures  uint64 `json:"upstreamFailures"`
	RefreshSuccesses  uint64 `json:"refreshSuccesses"`
	RefreshFailures   uint64 `json:"refreshFailures"`
	CacheEntries      int    `json:"cacheEntries"`
}

func New() *Metrics {
	return &Metrics{}
}

func (m *Metrics) IncTotalQueries() {
	m.totalQueries.Add(1)
}

func (m *Metrics) IncCacheFreshHits() {
	m.cacheFreshHits.Add(1)
}

func (m *Metrics) IncCacheStaleHits() {
	m.cacheStaleHits.Add(1)
}

func (m *Metrics) IncCacheMisses() {
	m.cacheMisses.Add(1)
}

func (m *Metrics) IncTXTRefused() {
	m.txtRefused.Add(1)
}

func (m *Metrics) IncAAAARefused() {
	m.aaaaRefused.Add(1)
}

func (m *Metrics) IncUpstreamSuccesses() {
	m.upstreamSuccesses.Add(1)
}

func (m *Metrics) IncUpstreamFailures() {
	m.upstreamFailures.Add(1)
}

func (m *Metrics) IncRefreshSuccesses() {
	m.refreshSuccesses.Add(1)
}

func (m *Metrics) IncRefreshFailures() {
	m.refreshFailures.Add(1)
}

func (m *Metrics) Snapshot(cacheEntries int) Snapshot {
	return Snapshot{
		TotalQueries:      m.totalQueries.Load(),
		CacheFreshHits:    m.cacheFreshHits.Load(),
		CacheStaleHits:    m.cacheStaleHits.Load(),
		CacheMisses:       m.cacheMisses.Load(),
		TXTRefused:        m.txtRefused.Load(),
		AAAARefused:       m.aaaaRefused.Load(),
		UpstreamSuccesses: m.upstreamSuccesses.Load(),
		UpstreamFailures:  m.upstreamFailures.Load(),
		RefreshSuccesses:  m.refreshSuccesses.Load(),
		RefreshFailures:   m.refreshFailures.Load(),
		CacheEntries:      cacheEntries,
	}
}
