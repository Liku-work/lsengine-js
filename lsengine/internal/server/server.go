// internal/server/server.go
package server

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"lsengine/internal/config"
)

type HttpServer struct {
	Config      *config.AppConfig
	ProjectRoot string
	StaticCache sync.Map
	startTime   time.Time
}

func NewHttpServer(cfg *config.AppConfig, projectRoot string) *HttpServer {
	return &HttpServer{
		Config:      cfg,
		ProjectRoot: projectRoot,
		startTime:   time.Now(),
	}
}

func (s *HttpServer) Start() {
	mux := http.NewServeMux()
	handler := s.applyMiddlewares(mux)
	s.registerRoutes(mux)

	server := &http.Server{
		Addr:           s.Config.Port,
		Handler:        handler,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    time.Duration(s.Config.KeepAlive) * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	log.Printf("🚀 Servidor iniciado en http://localhost%s", s.Config.Port)
	log.Printf("📊 Métricas en http://localhost%s/metrics", s.Config.Port)
	log.Printf("❤️  Health check en http://localhost%s/health", s.Config.Port)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func (s *HttpServer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.staticHandler)
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/metrics", s.metricsHandler)
	mux.HandleFunc("/upload", s.uploadHandler)
	mux.HandleFunc("/stream", s.streamHandler)
	mux.HandleFunc("/api/info", s.infoHandler)
}

func (s *HttpServer) applyMiddlewares(next http.Handler) http.Handler {
	handler := next
	if s.Config.Compression {
		handler = s.compressionMiddleware(handler)
	}
	handler = s.securityMiddleware(handler)
	if s.Config.CORS != nil && s.Config.CORS.Enabled {
		handler = s.corsMiddleware(handler)
	}
	handler = s.loggingMiddleware(handler)
	handler = s.recoveryMiddleware(handler)
	return handler
}

func (s *HttpServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("[%s] %s - %d - %v", r.Method, r.URL.Path, rw.statusCode, time.Since(start))
	})
}

func (s *HttpServer) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Panic: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *HttpServer) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Config.Security != nil {
			if s.Config.Security.CSP != "" {
				w.Header().Set("Content-Security-Policy", s.Config.Security.CSP)
			}
			if s.Config.Security.HSTS {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			if s.Config.Security.XFrameOptions != "" {
				w.Header().Set("X-Frame-Options", s.Config.Security.XFrameOptions)
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HttpServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, allowed := range s.Config.CORS.AllowedOrigins {
			if allowed == "*" || allowed == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(s.Config.CORS.AllowedMethods, ", "))
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(s.Config.CORS.AllowedHeaders, ", "))

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HttpServer) compressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			gzw := &gzipResponseWriter{ResponseWriter: w, Writer: gz}
			next.ServeHTTP(gzw, r)
		} else {
			next.ServeHTTP(w, r)
		}
	})
}

func (s *HttpServer) staticHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if strings.Contains(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	if path == "/" {
		indexPath := filepath.Join(s.ProjectRoot, s.Config.EntryPoint)
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
		s.serveUI(w, r)
		return
	}

	fullPath := filepath.Join(s.ProjectRoot, path)
	rel, err := filepath.Rel(s.ProjectRoot, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, r)
		return
	}

	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") ||
		strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") ||
		strings.HasSuffix(path, ".ico") {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}

	http.ServeFile(w, r, fullPath)
}

func (s *HttpServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"time":    time.Now().Unix(),
		"uptime":  int64(time.Since(s.startTime).Seconds()),
		"version": s.Config.Version,
		"app":     s.Config.Name,
	})
}

func (s *HttpServer) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("format") == "prometheus" {
		promhttp.Handler().ServeHTTP(w, r)
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"goroutines":     runtime.NumGoroutine(),
		"memory_alloc":   mem.Alloc,
		"memory_sys":     mem.Sys,
		"num_gc":         mem.NumGC,
		"uptime_seconds": int64(time.Since(s.startTime).Seconds()),
		"go_version":     runtime.Version(),
		"num_cpu":        runtime.NumCPU(),
	})
}

func (s *HttpServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.Config.MaxUploadSize)
	if err := r.ParseMultipartForm(s.Config.MaxUploadSize); err != nil {
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error reading file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := header.Filename
	uploadDir := filepath.Join(s.ProjectRoot, "uploads")
	os.MkdirAll(uploadDir, 0755)

	savePath := filepath.Join(uploadDir, filename)
	dst, err := os.Create(savePath)
	if err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	size, _ := io.Copy(dst, file)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"filename": filename,
		"size":     size,
		"path":     "/uploads/" + filename,
	})
}

func (s *HttpServer) streamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "data: {\"type\":\"keepalive\",\"timestamp\":%d}\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}

func (s *HttpServer) infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":        s.Config.Name,
		"version":     s.Config.Version,
		"author":      s.Config.Author,
		"license":     s.Config.License,
		"environment": s.Config.Environment,
		"port":        s.Config.Port,
	})
}

func (s *HttpServer) serveUI(w http.ResponseWriter, r *http.Request) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>%s - LS Engine</title>
<style>
body { font-family: Arial; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); color: #fff; padding: 40px; }
.container { max-width: 800px; margin: 0 auto; }
h1 { text-align: center; }
.stats { display: grid; grid-template-columns: repeat(2, 1fr); gap: 20px; margin: 30px 0; }
.card { background: rgba(255,255,255,0.1); padding: 20px; border-radius: 10px; text-align: center; }
.value { font-size: 32px; font-weight: bold; }
.label { font-size: 12px; margin-top: 10px; }
</style>
</head>
<body>
<div class="container">
<h1>🚀 %s</h1>
<div class="stats">
<div class="card"><div class="value" id="goroutines">0</div><div class="label">Goroutines</div></div>
<div class="card"><div class="value" id="memory">0 MB</div><div class="label">Memoria</div></div>
</div>
</div>
<script>
async function load() {
    const res = await fetch('/metrics');
    const data = await res.json();
    document.getElementById('goroutines').textContent = data.goroutines || 0;
    document.getElementById('memory').textContent = Math.round((data.memory_alloc || 0) / 1024 / 1024) + ' MB';
}
setInterval(load, 2000);
load();
</script>
</body>
</html>`, s.Config.Name, s.Config.Name)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (s *HttpServer) Shutdown(timeoutSeconds int) {
	log.Println("🛑 Apagando servidor...")
	time.Sleep(time.Duration(timeoutSeconds) * time.Second)
	log.Println("✅ Apagado completo")
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}