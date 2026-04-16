// internal/metrics/metrics.go
package metrics

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	queryDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "query_duration_seconds",
			Help:    "Query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	activeConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_connections",
			Help: "Active connections",
		},
	)

	wsMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_messages_total",
			Help: "WebSocket messages total",
		},
		[]string{"direction"},
	)

	memoryUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "memory_usage_bytes",
			Help: "Memory usage in bytes",
		},
	)

	securityViolations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "security_violations_total",
			Help: "Total security violations blocked",
		},
		[]string{"type"},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(activeConnections)
	prometheus.MustRegister(wsMessagesTotal)
	prometheus.MustRegister(memoryUsage)
	prometheus.MustRegister(securityViolations)
}

type Metrics struct {
	mu                 sync.RWMutex
	ActiveConnections  int64
	TotalConnections   int64
	ActiveQueries      int64
	SlowQueries        int64
	TotalQueries       int64
	queryTimes         []time.Duration
	VMPoolHits         int64
	VMPoolMisses       int64
	WsMessagesSent     int64
	WsMessagesReceived int64
	WsErrors           int64
	WsDropped          int64
	QueueRejected      int64
	ExecutorActive     int64
	ExecutorQueue      int64
	JsonOps            int64
	JsonErrors         int64
	StartTime          time.Time
	HttpRequests       int64
	HttpErrors         int64
	HttpDuration       time.Duration
	SecurityBlocked    int64
}

var GlobalMetrics = &Metrics{
	queryTimes: make([]time.Duration, 0, 1000),
	StartTime:  time.Now(),
}

func (m *Metrics) IncActiveConnections() {
	atomic.AddInt64(&m.ActiveConnections, 1)
	atomic.AddInt64(&m.TotalConnections, 1)
	activeConnections.Inc()
}

func (m *Metrics) DecActiveConnections() {
	atomic.AddInt64(&m.ActiveConnections, -1)
	activeConnections.Dec()
}

func (m *Metrics) AddQueryTime(d time.Duration) {
	atomic.AddInt64(&m.TotalQueries, 1)
	queryDuration.Observe(d.Seconds())
	if d > 200*time.Millisecond {
		atomic.AddInt64(&m.SlowQueries, 1)
	}

	m.mu.Lock()
	m.queryTimes = append(m.queryTimes, d)
	if len(m.queryTimes) > 1000 {
		m.queryTimes = m.queryTimes[1:]
	}
	m.mu.Unlock()
}

func (m *Metrics) AddHTTPRequest(d time.Duration, status int) {
	atomic.AddInt64(&m.HttpRequests, 1)
	httpRequestsTotal.WithLabelValues("", "", fmt.Sprint(status)).Inc()
	if status >= 400 {
		atomic.AddInt64(&m.HttpErrors, 1)
	}
	m.mu.Lock()
	m.HttpDuration += d
	m.mu.Unlock()
}

func (m *Metrics) IncVMPoolHit() {
	atomic.AddInt64(&m.VMPoolHits, 1)
}

func (m *Metrics) IncVMPoolMiss() {
	atomic.AddInt64(&m.VMPoolMisses, 1)
}

func (m *Metrics) IncWSSent() {
	atomic.AddInt64(&m.WsMessagesSent, 1)
}

func (m *Metrics) IncWSReceived() {
	atomic.AddInt64(&m.WsMessagesReceived, 1)
}

func (m *Metrics) IncWSError() {
	atomic.AddInt64(&m.WsErrors, 1)
}

func (m *Metrics) IncWSDropped() {
	atomic.AddInt64(&m.WsDropped, 1)
}

func (m *Metrics) IncQueueRejected() {
	atomic.AddInt64(&m.QueueRejected, 1)
}

func (m *Metrics) IncExecutorActive() {
	atomic.AddInt64(&m.ExecutorActive, 1)
}

func (m *Metrics) DecExecutorActive() {
	atomic.AddInt64(&m.ExecutorActive, -1)
}

func (m *Metrics) SetExecutorQueue(val int64) {
	atomic.StoreInt64(&m.ExecutorQueue, val)
}

func (m *Metrics) IncJsonOps() {
	atomic.AddInt64(&m.JsonOps, 1)
}

func (m *Metrics) IncJsonErrors() {
	atomic.AddInt64(&m.JsonErrors, 1)
}

func IncSecurityBlocked(violationType string) {
	atomic.AddInt64(&GlobalMetrics.SecurityBlocked, 1)
	securityViolations.WithLabelValues(violationType).Inc()
}

func (m *Metrics) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var avgQueryTime time.Duration
	if len(m.queryTimes) > 0 {
		var sum time.Duration
		for _, t := range m.queryTimes {
			sum += t
		}
		avgQueryTime = sum / time.Duration(len(m.queryTimes))
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	memoryUsage.Set(float64(mem.Alloc))

	avgHTTPDuration := int64(0)
	if m.HttpRequests > 0 {
		avgHTTPDuration = m.HttpDuration.Milliseconds() / m.HttpRequests
	}

	return map[string]interface{}{
		"active_connections":   atomic.LoadInt64(&m.ActiveConnections),
		"total_connections":    atomic.LoadInt64(&m.TotalConnections),
		"active_queries":       atomic.LoadInt64(&m.ActiveQueries),
		"slow_queries":         atomic.LoadInt64(&m.SlowQueries),
		"total_queries":        atomic.LoadInt64(&m.TotalQueries),
		"avg_query_time_ms":    avgQueryTime.Milliseconds(),
		"vm_pool_hits":         atomic.LoadInt64(&m.VMPoolHits),
		"vm_pool_misses":       atomic.LoadInt64(&m.VMPoolMisses),
		"ws_messages_sent":     atomic.LoadInt64(&m.WsMessagesSent),
		"ws_messages_received": atomic.LoadInt64(&m.WsMessagesReceived),
		"ws_errors":            atomic.LoadInt64(&m.WsErrors),
		"ws_dropped":           atomic.LoadInt64(&m.WsDropped),
		"queue_rejected":       atomic.LoadInt64(&m.QueueRejected),
		"executor_active":      atomic.LoadInt64(&m.ExecutorActive),
		"executor_queue":       atomic.LoadInt64(&m.ExecutorQueue),
		"json_ops":             atomic.LoadInt64(&m.JsonOps),
		"json_errors":          atomic.LoadInt64(&m.JsonErrors),
		"http_requests":        atomic.LoadInt64(&m.HttpRequests),
		"http_errors":          atomic.LoadInt64(&m.HttpErrors),
		"http_avg_duration_ms": avgHTTPDuration,
		"uptime_seconds":       int64(time.Since(m.StartTime).Seconds()),
		"goroutines":           runtime.NumGoroutine(),
		"memory_alloc":         mem.Alloc,
		"memory_total_alloc":   mem.TotalAlloc,
		"memory_sys":           mem.Sys,
		"memory_gc_cpufraction": mem.GCCPUFraction,
		"num_gc":               mem.NumGC,
		"go_version":           runtime.Version(),
		"go_os":                runtime.GOOS,
		"go_arch":              runtime.GOARCH,
		"go_maxprocs":          runtime.GOMAXPROCS(0),
		"security_blocked":     atomic.LoadInt64(&m.SecurityBlocked),
	}
}

func (m *Metrics) GetQueryStats() (int64, int64, time.Duration) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var avgQueryTime time.Duration
	if len(m.queryTimes) > 0 {
		var sum time.Duration
		for _, t := range m.queryTimes {
			sum += t
		}
		avgQueryTime = sum / time.Duration(len(m.queryTimes))
	}
	
	return atomic.LoadInt64(&m.TotalQueries), atomic.LoadInt64(&m.SlowQueries), avgQueryTime
}

func (m *Metrics) GetConnectionStats() (int64, int64) {
	return atomic.LoadInt64(&m.ActiveConnections), atomic.LoadInt64(&m.TotalConnections)
}

func (m *Metrics) GetWSStats() (int64, int64, int64, int64) {
	return atomic.LoadInt64(&m.WsMessagesSent),
		atomic.LoadInt64(&m.WsMessagesReceived),
		atomic.LoadInt64(&m.WsErrors),
		atomic.LoadInt64(&m.WsDropped)
}

func (m *Metrics) GetSecurityStats() int64 {
	return atomic.LoadInt64(&m.SecurityBlocked)
}

func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	atomic.StoreInt64(&m.ActiveConnections, 0)
	atomic.StoreInt64(&m.TotalConnections, 0)
	atomic.StoreInt64(&m.ActiveQueries, 0)
	atomic.StoreInt64(&m.SlowQueries, 0)
	atomic.StoreInt64(&m.TotalQueries, 0)
	m.queryTimes = make([]time.Duration, 0, 1000)
	atomic.StoreInt64(&m.VMPoolHits, 0)
	atomic.StoreInt64(&m.VMPoolMisses, 0)
	atomic.StoreInt64(&m.WsMessagesSent, 0)
	atomic.StoreInt64(&m.WsMessagesReceived, 0)
	atomic.StoreInt64(&m.WsErrors, 0)
	atomic.StoreInt64(&m.WsDropped, 0)
	atomic.StoreInt64(&m.QueueRejected, 0)
	atomic.StoreInt64(&m.ExecutorActive, 0)
	atomic.StoreInt64(&m.ExecutorQueue, 0)
	atomic.StoreInt64(&m.JsonOps, 0)
	atomic.StoreInt64(&m.JsonErrors, 0)
	atomic.StoreInt64(&m.HttpRequests, 0)
	atomic.StoreInt64(&m.HttpErrors, 0)
	m.HttpDuration = 0
	atomic.StoreInt64(&m.SecurityBlocked, 0)
	m.StartTime = time.Now()
}

func (m *Metrics) IncrementActiveQueries() {
	atomic.AddInt64(&m.ActiveQueries, 1)
}

func (m *Metrics) DecrementActiveQueries() {
	atomic.AddInt64(&m.ActiveQueries, -1)
}