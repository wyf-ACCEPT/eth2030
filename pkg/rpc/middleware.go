// middleware.go provides an HTTP middleware stack for the JSON-RPC server.
// It includes CORS, authentication, logging, and gzip compression middleware
// that can be composed into a chain wrapping any http.Handler.
package rpc

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPMiddleware is a function that wraps an http.Handler.
type HTTPMiddleware func(http.Handler) http.Handler

// MiddlewareChain composes multiple middleware into a single handler chain.
// Middleware are applied in order: the first middleware in the slice is the
// outermost (executes first). Returns the inner handler if no middleware.
func MiddlewareChain(handler http.Handler, middlewares ...HTTPMiddleware) http.Handler {
	// Apply in reverse so first middleware is outermost.
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// --- CORS Middleware ---

// CORSConfig holds the configuration for CORS middleware.
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int // seconds
}

// DefaultCORSConfig returns a permissive CORS config suitable for
// development. Production deployments should restrict origins.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         3600,
	}
}

// CORSMiddleware returns middleware that sets CORS headers on responses.
// Preflight OPTIONS requests are handled automatically.
func CORSMiddleware(config CORSConfig) HTTPMiddleware {
	methods := strings.Join(config.AllowedMethods, ", ")
	headers := strings.Join(config.AllowedHeaders, ", ")
	maxAge := ""
	if config.MaxAge > 0 {
		maxAge = formatCORSMaxAge(config.MaxAge)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if the request origin is allowed.
			origin := r.Header.Get("Origin")
			if origin != "" {
				if corsOriginAllowed(origin, config.AllowedOrigins) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else if len(config.AllowedOrigins) > 0 && config.AllowedOrigins[0] == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			} else if len(config.AllowedOrigins) > 0 && config.AllowedOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}

			w.Header().Set("Access-Control-Allow-Methods", methods)
			w.Header().Set("Access-Control-Allow-Headers", headers)
			if maxAge != "" {
				w.Header().Set("Access-Control-Max-Age", maxAge)
			}

			// Handle preflight.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// corsOriginAllowed checks whether the origin matches any allowed origin.
func corsOriginAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}

// formatCORSMaxAge converts seconds to a string for the header value.
func formatCORSMaxAge(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	// Manual integer-to-string to avoid fmt import dependency here.
	result := make([]byte, 0, 10)
	if seconds == 0 {
		return "0"
	}
	for seconds > 0 {
		result = append(result, byte('0'+seconds%10))
		seconds /= 10
	}
	// Reverse.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

// --- Auth Middleware ---

// AuthConfig holds configuration for authentication middleware.
type AuthConfig struct {
	// JWTSecret is the shared secret for JWT token validation.
	// If empty, JWT auth is disabled.
	JWTSecret string

	// APIKeys is a set of valid API keys. If empty, API key auth
	// is disabled.
	APIKeys map[string]bool

	// AllowUnauthenticated controls whether requests without any
	// auth credentials are allowed through.
	AllowUnauthenticated bool
}

// AuthMiddleware returns middleware that validates authentication tokens.
// It checks for Bearer tokens and API keys in the Authorization header.
func AuthMiddleware(config AuthConfig) HTTPMiddleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")

			// No auth header present.
			if authHeader == "" {
				if config.AllowUnauthenticated {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "unauthorized: missing credentials", http.StatusUnauthorized)
				return
			}

			// Check Bearer token (JWT).
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := authHeader[7:]
				if config.JWTSecret != "" && token == config.JWTSecret {
					next.ServeHTTP(w, r)
					return
				}
				// For a full implementation, decode and verify the JWT.
				// For now, treat the token as a simple secret comparison.
				if config.JWTSecret != "" {
					http.Error(w, "unauthorized: invalid token", http.StatusUnauthorized)
					return
				}
			}

			// Check API key.
			if strings.HasPrefix(authHeader, "ApiKey ") {
				key := authHeader[7:]
				if config.APIKeys != nil && config.APIKeys[key] {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "unauthorized: invalid API key", http.StatusUnauthorized)
				return
			}

			if config.AllowUnauthenticated {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "unauthorized: unrecognized auth scheme", http.StatusUnauthorized)
		})
	}
}

// --- Logging Middleware ---

// LogEntry captures a single request/response log record.
type LogEntry struct {
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
	RemoteAddr string
	Timestamp  time.Time
}

// LogStore is a simple in-memory log store for testing. Thread-safe.
type LogStore struct {
	mu      sync.Mutex
	entries []LogEntry
}

// NewLogStore creates a new empty log store.
func NewLogStore() *LogStore {
	return &LogStore{}
}

// Add appends a log entry.
func (ls *LogStore) Add(entry LogEntry) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.entries = append(ls.entries, entry)
}

// Entries returns a copy of all log entries.
func (ls *LogStore) Entries() []LogEntry {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	cp := make([]LogEntry, len(ls.entries))
	copy(cp, ls.entries)
	return cp
}

// Len returns the number of stored entries.
func (ls *LogStore) Len() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.entries)
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.statusCode = code
	sr.ResponseWriter.WriteHeader(code)
}

// LoggingMiddleware returns middleware that logs request/response metadata
// to the provided LogStore.
func LoggingMiddleware(store *LogStore) HTTPMiddleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rec := &statusRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rec, r)

			entry := LogEntry{
				Method:     r.Method,
				Path:       r.URL.Path,
				StatusCode: rec.statusCode,
				Duration:   time.Since(start),
				RemoteAddr: r.RemoteAddr,
				Timestamp:  start,
			}
			store.Add(entry)
		})
	}
}

// --- Compression Middleware ---

// gzipResponseWriter wraps http.ResponseWriter with gzip compression.
type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (grw *gzipResponseWriter) Write(b []byte) (int, error) {
	return grw.writer.Write(b)
}

// CompressionMiddleware returns middleware that gzip-compresses responses
// when the client advertises Accept-Encoding: gzip support.
func CompressionMiddleware() HTTPMiddleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only compress if client supports gzip.
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Del("Content-Length")

			gz := gzip.NewWriter(w)
			defer gz.Close()

			grw := &gzipResponseWriter{
				ResponseWriter: w,
				writer:         gz,
			}

			next.ServeHTTP(grw, r)
		})
	}
}

// --- Rate Limiting Middleware (bonus) ---

// RateLimitConfig configures the rate limiter.
type RateLimitConfig struct {
	// RequestsPerSecond is the max requests per second per IP.
	RequestsPerSecond int
}

// rateLimiterState tracks request timestamps per client IP.
type rateLimiterState struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

// RateLimitMiddleware returns middleware that limits requests per IP.
func RateLimitMiddleware(config RateLimitConfig) HTTPMiddleware {
	state := &rateLimiterState{
		requests: make(map[string][]time.Time),
	}

	rps := config.RequestsPerSecond
	if rps <= 0 {
		rps = 100 // default
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r)
			now := time.Now()
			windowStart := now.Add(-time.Second)

			state.mu.Lock()

			// Clean old entries.
			times := state.requests[ip]
			cleaned := times[:0]
			for _, t := range times {
				if t.After(windowStart) {
					cleaned = append(cleaned, t)
				}
			}

			if len(cleaned) >= rps {
				state.mu.Unlock()
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			state.requests[ip] = append(cleaned, now)
			state.mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP extracts the client IP from a request, checking
// X-Forwarded-For and X-Real-IP headers first.
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr, strip port.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}

// Ensure gzipResponseWriter implements io.Writer.
var _ io.Writer = (*gzipResponseWriter)(nil)
