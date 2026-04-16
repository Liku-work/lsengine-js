// internal/middleware/middleware.go
package middleware

import (
	"compress/gzip"
	"context"
	//"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	//"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"lsengine/internal/config"
	"lsengine/internal/metrics"
)

type Middleware func(http.Handler) http.Handler

type MiddlewareChain struct {
	middlewares []Middleware
}

func NewMiddlewareChain() *MiddlewareChain {
	return &MiddlewareChain{
		middlewares: make([]Middleware, 0),
	}
}

func (mc *MiddlewareChain) Use(m Middleware) {
	mc.middlewares = append(mc.middlewares, m)
}

func (mc *MiddlewareChain) Then(h http.Handler) http.Handler {
	for i := len(mc.middlewares) - 1; i >= 0; i-- {
		h = mc.middlewares[i](h)
	}
	return h
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, status: 200}

		defer func() {
			duration := time.Since(start)
			if duration > 100*time.Millisecond {
				log.Printf("[SLOW] %s %s - %v", r.Method, r.URL.Path, duration)
			}
		}()

		next.ServeHTTP(wrapped, r)
	})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{w, http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		metrics.GlobalMetrics.AddHTTPRequest(duration, rw.status)

		log.Printf("[HTTP] %s %s - %d - %v", r.Method, r.URL.Path, rw.status, duration)
	})
}

type rateLimiterShard struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
}

func RateLimitMiddleware(rateLimit int) Middleware {
	const shardCount = 256
	shards := make([]*rateLimiterShard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &rateLimiterShard{
			limiters: make(map[string]*rate.Limiter),
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}

			shard := shards[fnvHash(ip)%shardCount]

			shard.mu.RLock()
			limiter, exists := shard.limiters[ip]
			shard.mu.RUnlock()

			if !exists {
				shard.mu.Lock()
				if limiter, exists = shard.limiters[ip]; !exists {
					limiter = rate.NewLimiter(rate.Limit(rateLimit), rateLimit*2)
					shard.limiters[ip] = limiter
				}
				shard.mu.Unlock()
			}

			if !limiter.Allow() {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func fnvHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func SecurityMiddleware(sec *config.SecurityConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sec != nil {
				if sec.CSP != "" {
					w.Header().Set("Content-Security-Policy", sec.CSP)
				}
				if sec.HSTS {
					w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
				}
				if sec.XFrameOptions != "" {
					w.Header().Set("X-Frame-Options", sec.XFrameOptions)
				}
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.Header().Set("X-XSS-Protection", "1; mode=block")
				w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
				w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func CORSMiddleware(cors *config.CORSConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cors != nil && cors.Enabled {
				origin := r.Header.Get("Origin")
				for _, allowed := range cors.AllowedOrigins {
					if allowed == "*" || allowed == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cors.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cors.AllowedHeaders, ", "))
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func CompressionMiddleware(enabled bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Upgrade") == "websocket" {
				next.ServeHTTP(w, r)
				return
			}
			if enabled && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				gz := gzip.NewWriter(w)
				defer gz.Close()
				gzw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
				next.ServeHTTP(gzw, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

func ValidationMiddleware(maxUploadSize int64, sec *config.SecurityConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxUploadSize {
				http.Error(w, "Request entity too large", http.StatusRequestEntityTooLarge)
				return
			}
			if sec != nil && sec.InputSanitize {
				r.URL.RawQuery = strings.ReplaceAll(r.URL.RawQuery, "<", "&lt;")
				r.URL.RawQuery = strings.ReplaceAll(r.URL.RawQuery, ">", "&gt;")
			}
			next.ServeHTTP(w, r)
		})
	}
}

type Router struct {
	routes     map[string]map[string]http.HandlerFunc
	params     map[string]map[string][]string
	middleware *MiddlewareChain
}

func NewRouter() *Router {
	return &Router{
		routes:     make(map[string]map[string]http.HandlerFunc),
		params:     make(map[string]map[string][]string),
		middleware: NewMiddlewareChain(),
	}
}

func (r *Router) Use(m Middleware) {
	r.middleware.Use(m)
}

func (r *Router) Handle(method, pattern string, handler http.HandlerFunc) {
	if r.routes[method] == nil {
		r.routes[method] = make(map[string]http.HandlerFunc)
		r.params[method] = make(map[string][]string)
	}
	paramNames := make([]string, 0)
	re := regexp.MustCompile(`:([^/]+)`)
	matches := re.FindAllStringSubmatch(pattern, -1)
	for _, match := range matches {
		if len(match) > 1 {
			paramNames = append(paramNames, match[1])
		}
	}
	if len(paramNames) > 0 {
		r.params[method][pattern] = paramNames
	}
	r.routes[method][pattern] = handler
}

func (r *Router) GET(pattern string, handler http.HandlerFunc) {
	r.Handle("GET", pattern, handler)
}

func (r *Router) POST(pattern string, handler http.HandlerFunc) {
	r.Handle("POST", pattern, handler)
}

func (r *Router) PUT(pattern string, handler http.HandlerFunc) {
	r.Handle("PUT", pattern, handler)
}

func (r *Router) DELETE(pattern string, handler http.HandlerFunc) {
	r.Handle("DELETE", pattern, handler)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	handlers, ok := r.routes[req.Method]
	if !ok {
		http.NotFound(w, req)
		return
	}
	path := req.URL.Path
	for pattern, handler := range handlers {
		if pattern == path {
			r.middleware.Then(http.HandlerFunc(handler)).ServeHTTP(w, req)
			return
		}
	}
	for pattern, handler := range handlers {
		if paramNames, hasParams := r.params[req.Method][pattern]; hasParams {
			regexPattern := regexp.MustCompile(`^` + strings.ReplaceAll(regexp.QuoteMeta(pattern), `:([^/]+)`, `([^/]+)`) + `$`)
			matches := regexPattern.FindStringSubmatch(path)
			if len(matches) > 1 {
				params := make(map[string]string)
				for i, name := range paramNames {
					if i+1 < len(matches) {
						params[name] = matches[i+1]
					}
				}
				ctx := context.WithValue(req.Context(), "params", params)
				r.middleware.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handler(w, r.WithContext(ctx))
				})).ServeHTTP(w, req)
				return
			}
		}
	}
	http.NotFound(w, req)
}