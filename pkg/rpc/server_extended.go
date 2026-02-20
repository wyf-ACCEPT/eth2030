package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Server extension errors.
var (
	ErrServerClosed   = errors.New("rpc server: closed")
	ErrServerStarted  = errors.New("rpc server: already started")
	ErrAuthFailed     = errors.New("rpc server: authentication failed")
	ErrRateLimited    = errors.New("rpc server: rate limited")
	ErrRequestTooLarge = errors.New("rpc server: request body too large")
)

// ServerConfig holds configuration for the extended RPC server.
type ServerConfig struct {
	MaxRequestSize   int64
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	CORSAllowOrigins []string
	AuthSecret       string
	RateLimitPerSec  int
	MaxBatchSize     int
	ShutdownTimeout  time.Duration
}

// DefaultServerConfig returns sensible server defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		MaxRequestSize:   5 * 1024 * 1024, // 5 MiB
		ReadTimeout:      30 * time.Second,
		WriteTimeout:     30 * time.Second,
		IdleTimeout:      120 * time.Second,
		CORSAllowOrigins: []string{"*"},
		RateLimitPerSec:  100,
		MaxBatchSize:     100,
		ShutdownTimeout:  10 * time.Second,
	}
}

// RateLimiter is a simple token-bucket rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	tokens     int
	maxTokens  int
	refillRate int
	lastRefill time.Time
}

// NewRateLimiter creates a rate limiter that allows rps requests per second.
func NewRateLimiter(rps int) *RateLimiter {
	if rps <= 0 {
		rps = 100
	}
	return &RateLimiter{
		tokens:     rps,
		maxTokens:  rps,
		refillRate: rps,
		lastRefill: time.Now(),
	}
}

// Allow returns true if the request is allowed under the rate limit.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	if elapsed >= time.Second {
		refill := int(elapsed.Seconds()) * rl.refillRate
		rl.tokens += refill
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}
		rl.lastRefill = now
	}

	if rl.tokens <= 0 {
		return false
	}
	rl.tokens--
	return true
}

// MiddlewareFunc is an HTTP middleware function.
type MiddlewareFunc func(http.Handler) http.Handler

// ExtServer is a full-featured JSON-RPC server with middleware, CORS,
// auth, rate limiting, batch handling, and graceful shutdown.
type ExtServer struct {
	config      ServerConfig
	api         *EthAPI
	batch       *BatchHandler
	rateLimiter *RateLimiter
	httpServer  *http.Server
	listener    net.Listener
	mu          sync.Mutex
	started     atomic.Bool
	middlewares []MiddlewareFunc
	requestCount atomic.Int64
}

// NewExtServer creates a new extended JSON-RPC server.
func NewExtServer(backend Backend, config ServerConfig) *ExtServer {
	api := NewEthAPI(backend)
	return &ExtServer{
		config:      config,
		api:         api,
		batch:       NewBatchHandler(api),
		rateLimiter: NewRateLimiter(config.RateLimitPerSec),
	}
}

// Use adds a middleware to the server's middleware chain.
func (s *ExtServer) Use(mw MiddlewareFunc) {
	s.middlewares = append(s.middlewares, mw)
}

// buildHandler constructs the full HTTP handler with middleware.
func (s *ExtServer) buildHandler() http.Handler {
	var handler http.Handler = http.HandlerFunc(s.handleRPC)

	// Apply middlewares in reverse order so first added is outermost.
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		handler = s.middlewares[i](handler)
	}
	return handler
}

// Start starts the HTTP server on the given address.
func (s *ExtServer) Start(addr string) error {
	if s.started.Load() {
		return ErrServerStarted
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	handler := s.buildHandler()
	mux := http.NewServeMux()
	mux.Handle("/", handler)

	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	s.mu.Lock()
	s.httpServer = srv
	s.listener = ln
	s.mu.Unlock()
	s.started.Store(true)

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Addr returns the listener address. Useful when started on port 0.
func (s *ExtServer) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Stop gracefully shuts down the server.
func (s *ExtServer) Stop() error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()

	if srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(ctx)
}

// RequestCount returns the total number of requests served.
func (s *ExtServer) RequestCount() int64 {
	return s.requestCount.Load()
}

// Handler returns the HTTP handler for testing without starting a listener.
func (s *ExtServer) Handler() http.Handler {
	return s.buildHandler()
}

// handleRPC is the main request handler that routes single and batch requests.
func (s *ExtServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	s.requestCount.Add(1)

	// Method check.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// CORS headers.
	s.setCORSHeaders(w, r)

	// Handle CORS preflight.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Auth check.
	if s.config.AuthSecret != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != s.config.AuthSecret {
			writeExtError(w, nil, ErrCodeInvalidRequest, "authentication failed")
			return
		}
	}

	// Rate limit check.
	if !s.rateLimiter.Allow() {
		writeExtError(w, nil, ErrCodeInternal, "rate limited")
		return
	}

	// Read body with size limit.
	body, err := io.ReadAll(io.LimitReader(r.Body, s.config.MaxRequestSize))
	if err != nil {
		writeExtError(w, nil, ErrCodeParse, "failed to read request body")
		return
	}

	// Check if batch request.
	if IsBatchRequest(body) {
		s.handleBatch(w, body)
		return
	}

	// Single request.
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeExtError(w, nil, ErrCodeParse, "invalid JSON")
		return
	}

	resp := s.api.HandleRequest(&req)
	writeExtJSON(w, resp)
}

// handleBatch processes a batch JSON-RPC request.
func (s *ExtServer) handleBatch(w http.ResponseWriter, body []byte) {
	responses, err := s.batch.HandleBatch(body)
	if err != nil {
		writeExtError(w, nil, ErrCodeInvalidRequest, err.Error())
		return
	}
	writeExtJSON(w, responses)
}

// setCORSHeaders sets CORS headers based on config.
func (s *ExtServer) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	for _, allowed := range s.config.CORSAllowOrigins {
		if allowed == "*" || allowed == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "3600")
			return
		}
	}
}

// ExtCORSMiddleware creates a middleware that handles CORS preflight requests.
func ExtCORSMiddleware(allowedOrigins []string) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				for _, allowed := range allowedOrigins {
					if allowed == "*" || allowed == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
						w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
						break
					}
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ExtAuthMiddleware creates a middleware that validates a Bearer token.
func ExtAuthMiddleware(secret string) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != secret {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"error":   map[string]interface{}{"code": ErrCodeInvalidRequest, "message": "unauthorized"},
					"id":      nil,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ExtRateLimitMiddleware creates a middleware that enforces rate limiting.
func ExtRateLimitMiddleware(rl *RateLimiter) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0",
					"error":   map[string]interface{}{"code": ErrCodeInternal, "message": "rate limited"},
					"id":      nil,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeExtJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeExtError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := &Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: message},
		ID:      id,
	}
	writeExtJSON(w, resp)
}
