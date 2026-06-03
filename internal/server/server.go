package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dns-swr/internal/cache"
	"dns-swr/internal/config"
	"dns-swr/internal/metrics"
	"dns-swr/internal/resolver"

	"github.com/miekg/dns"
)

type Server struct {
	cfg         *config.Config
	logger      *slog.Logger
	cache       *cache.MemoryStore
	metrics     *metrics.Metrics
	upstream    *resolver.UpstreamResolver
	refresher   *resolver.Refresher
	handler     *Handler
	udpServer   *dns.Server
	tcpServer   *dns.Server
	adminServer *http.Server
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	stopping    atomic.Bool
}

func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}

	var persistence cache.Persistence
	switch strings.ToLower(cfg.Cache.Persistence) {
	case "memory":
	case "bbolt":
		p, err := cache.OpenBbolt(cfg.Cache.Path)
		if err != nil {
			return nil, err
		}
		persistence = p
	default:
		return nil, errors.New("unsupported cache persistence")
	}

	store := cache.NewMemoryStore(persistence)
	loaded, err := store.LoadFromPersistence(time.Now())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if loaded > 0 {
		logger.Info("cache_loaded", "entries", loaded, "persistence", cfg.Cache.Persistence)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := metrics.New()
	upstream := resolver.NewUpstreamResolver(cfg.Upstreams, cfg.Timeouts.UpstreamTimeout.Duration, cfg.Policy.RetryOnSERVFAIL, logger)
	refresher := resolver.NewRefresher(ctx, upstream, store, cfg, m, logger)
	handler := NewHandler(ctx, cfg, store, upstream, refresher, m, logger)

	return &Server{
		cfg:       cfg,
		logger:    logger,
		cache:     store,
		metrics:   m,
		upstream:  upstream,
		refresher: refresher,
		handler:   handler,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

func (s *Server) Start() error {
	if s.cfg.Listen.UDP != "" {
		s.udpServer = &dns.Server{
			Addr:    s.cfg.Listen.UDP,
			Net:     "udp",
			Handler: s.handler,
		}
		s.startDNSServer(s.udpServer, "udp", s.cfg.Listen.UDP)
	}

	if s.cfg.Listen.TCP != "" {
		s.tcpServer = &dns.Server{
			Addr:    s.cfg.Listen.TCP,
			Net:     "tcp",
			Handler: s.handler,
		}
		s.startDNSServer(s.tcpServer, "tcp", s.cfg.Listen.TCP)
	}

	if s.cfg.Admin.Enabled {
		s.startAdminServer()
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.stopping.Store(true)
	s.cancel()

	var errs []error
	if s.udpServer != nil {
		if err := s.udpServer.Shutdown(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.tcpServer != nil {
		if err := s.tcpServer.Shutdown(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.adminServer != nil {
		if err := s.adminServer.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if s.refresher != nil {
		if err := s.refresher.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		errs = append(errs, ctx.Err())
	}

	if err := s.cache.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Server) startDNSServer(server *dns.Server, network, address string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Info("dns_server_started", "network", network, "address", address)
		if err := server.ListenAndServe(); err != nil && !s.stopping.Load() {
			s.logger.Error("dns_server_failed", "network", network, "address", address, "error", err)
		}
	}()
}

func (s *Server) startAdminServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/cache/flush", s.handleCacheFlush)

	s.adminServer = &http.Server{
		Addr:              s.cfg.Admin.Address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Info("admin_server_started", "address", s.cfg.Admin.Address)
		if err := s.adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && !s.stopping.Load() {
			s.logger.Error("admin_server_failed", "address", s.cfg.Admin.Address, "error", err)
		}
	}()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.metrics.Snapshot(s.cache.Len()))
}

func (s *Server) handleCacheFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.cache.Flush(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "flushed"})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
