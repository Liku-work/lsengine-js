// internal/config/config.go
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MAX_VM_POOL_SIZE       = 500
	MIN_VM_POOL_SIZE       = 10
	MAX_WS_CONNECTIONS     = 10000
	MAX_WEBSOCKET_MESSAGE  = 5 * 1024 * 1024
	WS_WRITE_WAIT          = 10 * time.Second
	WS_PONG_WAIT           = 60 * time.Second
	WS_PING_PERIOD         = 30 * time.Second
	DEFAULT_QUERY_TIMEOUT  = 30 * time.Second
	MAX_CONCURRENT_QUERIES = 500
	SLOW_QUERY_THRESHOLD   = 200 * time.Millisecond
	MAX_JOB_QUEUE_SIZE     = 5000
	MAX_EVENT_LOOP_QUEUE   = 10000
	METRICS_INTERVAL       = 10 * time.Second
	MAX_RESPONSE_SIZE      = 1024 * 1024
	EXECUTION_WORKERS      = 200
	JSON_PAGE_SIZE         = 1000
	JSON_SHARD_COUNT       = 16
	BATCH_INSERT_SIZE      = 500
)

const (
	MAX_IMPORT_FILE_SIZE  = 1 * 1024 * 1024
	MAX_REMOTE_DOWNLOAD   = 0
	MAX_IMPORTS_PER_FILE  = 50
	MAX_IMPORT_DEPTH      = 5
	MAX_SCRIPT_CODE_SIZE  = 512 * 1024
)

type AppConfig struct {
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Port          string             `json:"port"`
	Ports         []string           `json:"ports"`
	EntryPoint    string             `json:"entryPoint"`
	Author        string             `json:"author"`
	License       string             `json:"license"`
	Indie         bool               `json:"indie"`
	Main          string             `json:"main"`
	Server        string             `json:"server"`
	MaxVM         int                `json:"maxVM"`
	MaxWS         int                `json:"maxWS"`
	TLS           *TLSConfig         `json:"tls"`
	IPv6          bool               `json:"ipv6"`
	KeepAlive     int                `json:"keepAlive"`
	Compression   bool               `json:"compression"`
	Brotli        bool               `json:"brotli"`
	MaxUploadSize int64              `json:"maxUploadSize"`
	RateLimit     int                `json:"rateLimit"`
	CORS          *CORSConfig        `json:"cors"`
	Security      *SecurityConfig    `json:"security"`
	Cluster       *ClusterConfig     `json:"cluster"`
	Plugins       []PluginConfig     `json:"plugins"`
	Cache         *CacheConfig       `json:"cache"`
	Logging       *LoggingConfig     `json:"logging"`
	Metrics       *MetricsConfig     `json:"metrics"`
	Database      *DatabaseConfig    `json:"database"`
	GraphQL       *GraphQLConfig     `json:"graphql"`
	Process       *ProcessConfig     `json:"process"`
	Environment   string             `json:"environment"`
	HotReload     bool               `json:"hotReload"`
	Routes        []RouteConfig      `json:"routes"`
	Middleware    []MiddlewareConfig `json:"middleware"`
	ImportSecurity *ImportSecurityConfig `json:"importSecurity"`
}

type ImportSecurityConfig struct {
	AllowRemoteImports bool     `json:"allowRemoteImports"`
	AllowedSubdirs     []string `json:"allowedSubdirs"`
	MaxFileSize        int64    `json:"maxFileSize"`
	MaxImportsPerFile  int      `json:"maxImportsPerFile"`
}

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

type CORSConfig struct {
	Enabled        bool     `json:"enabled"`
	AllowedOrigins []string `json:"allowedOrigins"`
	AllowedMethods []string `json:"allowedMethods"`
	AllowedHeaders []string `json:"allowedHeaders"`
}

type SecurityConfig struct {
	CSP           string   `json:"csp"`
	HSTS          bool     `json:"hsts"`
	XFrameOptions string   `json:"xFrameOptions"`
	CSRF          bool     `json:"csrf"`
	InputSanitize bool     `json:"inputSanitize"`
	Permissions   []string `json:"permissions"`
}

type ClusterConfig struct {
	Enabled      bool     `json:"enabled"`
	Workers      int      `json:"workers"`
	LoadBalancer string   `json:"loadBalancer"`
	Nodes        []string `json:"nodes"`
	HealthCheck  string   `json:"healthCheck"`
}

type PluginConfig struct {
	Name    string                 `json:"name"`
	Enabled bool                   `json:"enabled"`
	Config  map[string]interface{} `json:"config"`
}

type CacheConfig struct {
	Enabled     bool          `json:"enabled"`
	Type        string        `json:"type"`
	Size        int           `json:"size"`
	TTL         time.Duration `json:"ttl"`
	Distributed bool          `json:"distributed"`
	RedisURL    string        `json:"redisUrl"`
}

type LoggingConfig struct {
	Level      string   `json:"level"`
	Format     string   `json:"format"`
	Outputs    []string `json:"outputs"`
	Structured bool     `json:"structured"`
}

type MetricsConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
}

type DatabaseConfig struct {
	Enabled     bool                `json:"enabled"`
	PoolSize    int                 `json:"poolSize"`
	MaxIdle     int                 `json:"maxIdle"`
	Timeout     int                 `json:"timeout"`
	Connections map[string]DBConfig `json:"connections"`
}

type DBConfig struct {
	Driver  string `json:"driver"`
	DSN     string `json:"dsn"`
	MaxOpen int    `json:"maxOpen"`
	MaxIdle int    `json:"maxIdle"`
}

type GraphQLConfig struct {
	Enabled    bool   `json:"enabled"`
	Endpoint   string `json:"endpoint"`
	Playground bool   `json:"playground"`
	Schema     string `json:"schema"`
}

type ProcessConfig struct {
	MemoryLimit    int64 `json:"memoryLimit"`
	CPUQuota       int   `json:"cpuQuota"`
	MaxProcesses   int   `json:"maxProcesses"`
	SandboxEnabled bool  `json:"sandboxEnabled"`
}

type RouteConfig struct {
	Path     string   `json:"path"`
	Target   string   `json:"target"`
	Rewrite  string   `json:"rewrite"`
	Redirect string   `json:"redirect"`
	Methods  []string `json:"methods"`
}

type MiddlewareConfig struct {
	Name   string                 `json:"name"`
	Path   string                 `json:"path"`
	Config map[string]interface{} `json:"config"`
}

var ProjectRoot string
var AppCfg *AppConfig
var LogFile *os.File

func LoadAppConfig() *AppConfig {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	ProjectRoot = cwd

	for _, dir := range []string{"logs", "modules", "data", "cache", "plugins", "uploads"} {
		if err := os.MkdirAll(filepath.Join(cwd, dir), 0755); err != nil {
			log.Fatal(err)
		}
	}

	logPath := filepath.Join(cwd, "logs", fmt.Sprintf("ls-%s.log", time.Now().Format("2006-01-02")))
	LogFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}

	log.SetOutput(io.MultiWriter(os.Stdout, LogFile))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	appPath := filepath.Join(cwd, "app.json")
	data, err := os.ReadFile(appPath)
	if err == nil {
		AppCfg = &AppConfig{
			MaxVM:         MAX_VM_POOL_SIZE,
			MaxWS:         MAX_WS_CONNECTIONS,
			MaxUploadSize: 10 * 1024 * 1024,
			RateLimit:     100,
			Environment:   "development",
		}
		err = json.Unmarshal(data, AppCfg)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		AppCfg = &AppConfig{
			Name:          filepath.Base(cwd),
			Version:       "1.0.0",
			Port:          ":1505",
			Ports:         []string{":1505", ":1506"},
			EntryPoint:    "index.html",
			Main:          "index.html",
			Server:        "server.js",
			Author:        "LS User",
			License:       "MIT",
			Indie:         true,
			MaxVM:         MAX_VM_POOL_SIZE,
			MaxWS:         MAX_WS_CONNECTIONS,
			MaxUploadSize: 10 * 1024 * 1024,
			RateLimit:     100,
			IPv6:          true,
			KeepAlive:     60,
			Compression:   true,
			Environment:   "development",
			Security: &SecurityConfig{
				CSP:           "default-src 'self'",
				HSTS:          true,
				XFrameOptions: "DENY",
				CSRF:          true,
				InputSanitize: true,
				Permissions:   []string{"fs_read"},
			},
			CORS: &CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
				AllowedHeaders: []string{"Content-Type", "Authorization"},
			},
			Process: &ProcessConfig{
				MemoryLimit:    512 * 1024 * 1024,
				CPUQuota:       100,
				MaxProcesses:   10,
				SandboxEnabled: true,
			},
			Cache: &CacheConfig{
				Enabled: true,
				Type:    "memory",
				Size:    1000,
				TTL:     5 * time.Minute,
			},
			Logging: &LoggingConfig{
				Level:      "info",
				Format:     "text",
				Outputs:    []string{"stdout", "file"},
				Structured: true,
			},
			ImportSecurity: &ImportSecurityConfig{
				AllowRemoteImports: false,
				AllowedSubdirs:     []string{"js", "public", "public/js", "modules", "scripts", "src", "lib"},
				MaxFileSize:        MAX_IMPORT_FILE_SIZE,
				MaxImportsPerFile:  MAX_IMPORTS_PER_FILE,
			},
		}

		configData, _ := json.MarshalIndent(AppCfg, "", "  ")
		os.WriteFile(appPath, configData, 0644)
	}

	if AppCfg.Port == "" {
		AppCfg.Port = ":1505"
	}
	if !strings.HasPrefix(AppCfg.Port, ":") {
		AppCfg.Port = ":" + AppCfg.Port
	}

	if AppCfg.Version == "" {
		AppCfg.Version = "1.0.0"
	}
	if AppCfg.EntryPoint == "" {
		if AppCfg.Main != "" {
			AppCfg.EntryPoint = AppCfg.Main
		} else {
			AppCfg.EntryPoint = "index.html"
		}
	}
	if AppCfg.Server == "" {
		AppCfg.Server = "server.js"
	}
	if AppCfg.MaxVM <= 0 {
		AppCfg.MaxVM = MAX_VM_POOL_SIZE
	}
	if AppCfg.MaxWS <= 0 {
		AppCfg.MaxWS = MAX_WS_CONNECTIONS
	}
	if AppCfg.MaxUploadSize <= 0 {
		AppCfg.MaxUploadSize = 10 * 1024 * 1024
	}
	if AppCfg.RateLimit <= 0 {
		AppCfg.RateLimit = 100
	}
	if AppCfg.KeepAlive <= 0 {
		AppCfg.KeepAlive = 60
	}

	return AppCfg
}