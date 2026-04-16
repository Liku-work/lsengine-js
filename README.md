# LS Engine v6 (Go Port)

**LS Engine** is an optimized web server written in Go, designed to run web applications with advanced features such as WebSockets, dynamic JS module imports, a JavaScript engine (Goja), sharded JSON storage, enhanced security, and real-time metrics.

## Project Structure

```
lsengine/
├── api/
│   └── routes.go               # API route definitions
├── cmd/
│   └── main.go                 # Main entry point
├── internal/
│   ├── cache/                  # Cache system (memory/Redis)
│   ├── cluster/                # Cluster management (workers)
│   ├── config/                 # Configuration loading and validation
│   ├── imports/                # JS import processor (import {x} from ...)
│   ├── jsondb/                 # JSON database with sharding and B-Tree
│   ├── metrics/                # Internal metrics and Prometheus
│   ├── middleware/             # Middleware (CORS, rate limit, logging)
│   ├── security/               # Path, content and policy validation
│   ├── server/                 # Main HTTP server
│   ├── utils/                  # Common utilities
│   ├── vm/                     # Goja VM pool (JavaScript)
│   └── websocket/              # WebSocket connection management
├── app.json                    # Main application configuration
├── go.mod                      # Go dependencies
├── go.sum                      # Dependency checksums
├── start.bat                   # Compilation script for Windows
├── start.sh                    # Compilation script for Linux
└── index.html                  # Default page (entry point)
```

## Key Features

- HTTP server with Gzip compression, CORS, configurable timeouts.
- WebSockets with broadcast, ping/pong, connection limits.
- JavaScript engine (Goja) with sandbox, VM pool and native APIs (ls.app, ls.http, ls.crypto, etc.).
- Dynamic JS module import using <script to-call>, <script ls>, <script ls-ws> and import {x} from "file.js".
- Sharded JSON database (SQLite per shard) with B-Tree indexes and paginated queries.
- Security: path validation, remote import blocking, input sanitization, CSP, HSTS.
- Metrics: real-time statistics, /metrics endpoint (JSON or Prometheus).
- Cluster mode (workers) for better scalability.
- Distributed cache (Redis) or in-memory.
- Optional hot reload (configurable).

## Prerequisites

- Go 1.21 or higher (recommended 1.25+)
- Git (optional, for cloning the repository)
- Make (optional, if using a Makefile)
- Redis (optional, only for distributed cache)

## Compilation and Execution

### Windows (cmd / PowerShell)

1. Open a terminal in the project root.
2. Run the compilation script:

```batch
start.bat
```

3. If compilation is successful, `lsengine.exe` will be generated.
4. Run the server:

```batch
lsengine.exe
```

You can also compile manually:

```batch
go mod tidy
go build -ldflags="-s -w" -o lsengine.exe ./cmd/main.go
```

### Linux / macOS (bash)

1. Give execution permissions to the script (first time only):

```bash
chmod +x start.sh
```

2. Run the compilation script:

```bash
./start.sh
```

3. The binary `lsengine` (no extension) will be generated.
4. Run the server:

```bash
./lsengine
```

Manual compilation:

```bash
go mod tidy
go build -ldflags="-s -w" -o lsengine ./cmd/main.go
```

## Quick Test

Once the server is running, open your browser at:

```
http://localhost:1505
```

Useful endpoints:

- http://localhost:1505/health -> Health status
- http://localhost:1505/metrics -> Metrics (JSON)
- http://localhost:1505/metrics?format=prometheus -> Metrics for Prometheus
- http://localhost:1505/upload -> File upload (POST multipart)
- http://localhost:1505/stream -> Server-Sent Events (keepalive)
- http://localhost:1505/ws -> WebSocket

## Main Dependencies

| Package | Usage |
|--------|-------|
| github.com/dop251/goja | JavaScript engine |
| github.com/gorilla/websocket | WebSockets |
| github.com/prometheus/client_golang | Prometheus metrics |
| github.com/go-redis/redis/v8 | Distributed cache |
| github.com/mattn/go-sqlite3 | Sharded JSON storage |
| golang.org/x/time/rate | Rate limiting |
| golang.org/x/sync | Singleflight, semaphores |

## Configuration (app.json)

The `app.json` file is automatically generated on first run. Key values include:

```json
{
  "port": ":1505",
  "maxVM": 500,
  "maxWS": 10000,
  "compression": true,
  "rateLimit": 100,
  "cache": {
    "enabled": true,
    "type": "memory",
    "size": 1000,
    "ttl": 300000000000
  },
  "security": {
    "csp": "default-src 'self'",
    "hsts": true
  },
  "importSecurity": {
    "allowRemoteImports": false,
    "allowedSubdirs": ["js", "modules", "src", "lib"]
  }
}
```

You can modify `app.json` without recompiling.

## Common Issues and Solutions

### go: command not found
Install Go from golang.org and make sure it is in your PATH.

### Port already in use
Change the port in `app.json` or using the `PORT` environment variable:

```bash
export PORT=8080   # Linux/macOS
set PORT=8080      # Windows cmd
```

### Error cannot find package
Run:

```bash
go mod tidy
go mod download
```

### Compilation fails on Windows with "gcc"
The driver `mattn/go-sqlite3` requires GCC (e.g., TDM-GCC or MinGW-w64).  
Alternative: compile with the tag `-tags sqlite_omit_load_extension`.

## License

MIT – see the `LICENSE` file (if applicable). By default, the project is distributed under the MIT license.

## Author

LS User – LS Engine v6 (Go Port)
