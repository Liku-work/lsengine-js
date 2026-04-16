// cmd/main.go
package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lsengine/internal/config"
	"lsengine/internal/server"
)

func main() {
	log.Printf("LS Engine v6.0 starting...")
	log.Printf("Operating System: %s", runtime.GOOS)
	log.Printf("Architecture: %s", runtime.GOARCH)
	log.Printf("Go Version: %s", runtime.Version())
	log.Printf("Number of CPUs: %d", runtime.NumCPU())

	if len(os.Args) > 2 && os.Args[1] == "--worker" {
		workerID, _ := strconv.Atoi(os.Args[2])
		log.Printf("[Worker %d] Starting worker...", workerID)
		return
	}

	cfg := config.LoadAppConfig()
	projectRoot, _ := os.Getwd()

	port := cfg.Port
	if envPort := os.Getenv("PORT"); envPort != "" {
		if !strings.HasPrefix(envPort, ":") {
			envPort = ":" + envPort
		}
		port = envPort
	}

	maxRetries := 3
	var err error
	for i := 0; i < maxRetries; i++ {
		var ln net.Listener
		ln, err = net.Listen("tcp", port)
		if err == nil {
			ln.Close()
			break
		}
		if i < maxRetries-1 {
			log.Printf("Port %s is busy, retrying in 2 seconds... (attempt %d/%d)", port, i+1, maxRetries)
			time.Sleep(2 * time.Second)
		}
	}
	if err != nil {
		log.Printf("Error: Port %s is still in use after %d attempts.", port, maxRetries)
		log.Printf("Possible solutions:")
		log.Printf("  1. Close the other LS Engine instance")
		log.Printf("  2. Change the port in app.json or via the PORT environment variable")
		log.Printf("  3. Wait a few seconds and try again")
		time.Sleep(2 * time.Second)
		os.Exit(1)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	srv := server.NewHttpServer(cfg, projectRoot)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		srv.Shutdown(10)
		os.Exit(0)
	}()

	log.Printf("\n")
	log.Printf("LS Engine v6.0 (Go Port) - Optimized")
	log.Printf("App: %s v%s (%s)", cfg.Name, cfg.Version, cfg.Environment)
	log.Printf("\n")
	log.Printf("  Endpoints:")
	log.Printf("    • http://localhost%s - UI", port)
	log.Printf("    • http://localhost%s/ws - WebSocket", port)
	log.Printf("    • http://localhost%s/metrics - Metrics (JSON/Prometheus)", port)
	log.Printf("    • http://localhost%s/health - Health Check", port)
	log.Printf("    • http://localhost%s/upload - File Upload", port)
	log.Printf("    • http://localhost%s/stream - Server-Sent Events", port)
	if cfg.GraphQL != nil && cfg.GraphQL.Enabled {
		log.Printf("    • http://localhost%s%s - GraphQL", port, cfg.GraphQL.Endpoint)
	}
	log.Printf("\n")
	log.Printf("  Features:")
	log.Printf("    • To-Call:        <script to-call=\"archivo.js\"></script>")
	log.Printf("    • Native Import:  <script>import {all} from \"archivo.js\"</script>")
	log.Printf("    • LS Script:      <script src=\"archivo.js\" ls></script>")
	log.Printf("    • LS-WS Script:   <script src=\"archivo.js\" ls-ws></script>")
	log.Printf("\n")
	log.Printf("    • File: %s", cfg.Server)
	log.Printf("\n")
	log.Printf("Server running at http://localhost%s", port)

	srv.Start()
}