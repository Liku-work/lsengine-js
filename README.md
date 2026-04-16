LS Engine v6 version 1.0.0 beta (Go Port)
LS Engine is an optimized web server, written in Go, designed to run web applications with advanced features such as WebSockets, dynamic import of JS modules, JavaScript engine (Goja), sharded JSON storage, hardened security, and real-time metrics.

Project Structure
lsengine/
├── api/
│   └── routes.go               # Definicion de rutas de la API
├── cmd/
│   └── main.go                 # Punto de entrada principal
├── internal/
│   ├── cache/                  # Sistema de cache (memoria/Redis)
│   ├── cluster/                # Gestion de cluster (workers)
│   ├── config/                 # Carga y validacion de configuracion
│   ├── imports/                # Procesador de imports JS (import {x} from ...)
│   ├── jsondb/                 # Base de datos JSON con sharding y B-Tree
│   ├── metrics/                # Metricas internas y Prometheus
│   ├── middleware/             # Middleware (CORS, rate limit, logging)
│   ├── security/               # Validacion de rutas, contenidos y politicas
│   ├── server/                 # Servidor HTTP principal
│   ├── utils/                  # Utilidades comunes
│   ├── vm/                     # Pool de maquinas virtuales Goja (JS)
│   └── websocket/              # Gestion de conexiones WebSocket
├── app.json                    # Configuracion principal de la app
├── go.mod                      # Dependencias Go
├── go.sum                      # Checksums de dependencias
├── start.bat                   # Script de compilacion para Windows
├── start.sh                    # Script de compilacion para Linux
└── index.html                  # Pagina por defecto (entry point)
Main Features
HTTP server with support for Gzip compression, CORS, configurable timeouts.
WebSockets with broadcast, ping/pong, connection limit.
JavaScript engine (Goja) with sandbox, VM pool and native APIs (ls.app, ls.http, ls.crypto, etc.).
Dynamic import of JS modules using <script to-call>, <script ls>, <script ls-ws> and import {x} from "archivo.js".
Sharded JSON database (SQLite per shard) with B-Tree indexes and paginated queries.
Security: route validation, remote import blocking, input sanitization, CSP, HSTS.
Metrics: real-time statistics, endpoint /metrics (JSON or Prometheus).
Cluster mode (workers) for greater scalability.
Distributed cache (Redis) or in memory.
Hot reload opcional (configurable).
Prerequisites
Go 1.21 or higher (1.25+ recommended)
Git (optional, to clone the repository)
Make (optional, if you use Makefile)
Redis (optional, only if you use distributed caching)
Compilation and Execution
Windows (cmd / PowerShell)
Open a terminal in the project root directory.
Run the compilation script:
start.bat
If the compilation is successful, lsengine.exe will be generated.
Run the server:
lsengine.exe
You can also compile manually:

go mod tidy
go build -ldflags="-s -w" -o lsengine.exe ./cmd/main.go
Linux / macOS (bash)
Grant execution permissions to the script (only the first time):
chmod +x start.sh
Run the compilation script:
./start.sh
The lsengine binary will be generated (without extension).
Run the server:
./lsengine
Manual compilation:

go mod tidy
go build -ldflags="-s -w" -o lsengine ./cmd/main.go
Rapid Test
Once the server is running, open your browser to:

http://localhost:1505
Useful endpoints:

http://localhost:1505/health -> Health status
http://localhost:1505/metrics -> Metricas (JSON)
http://localhost:1505/metrics?format=prometheus -> Metricas para Prometheus
http://localhost:1505/upload -> Uploading files (multipart POST)
http://localhost:1505/stream -> Server-Sent Events (keepalive)
http://localhost:1505/ws -> WebSocket
Main Departments
Package	Use
github.com/dop251/goja	Motor JavaScript
github.com/gorilla/websocket	WebSockets
github.com/prometheus/client_golang	Metricas Prometheus
github.com/go-redis/redis/v8	Distributed cache
github.com/mattn/go-sqlite3	sharded JSON storage
golang.org/x/time/rate	Rate limiting
golang.org/x/sync	Singleflight, semaforos
Configuration (app.json)
The app.json file is automatically generated on the first run. Some key values:

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
You can modify app.json without recompiling.

Solving Common Problems
go: command not found
Install Go from golang.org and make sure it's in your PATH.

Port in use
Change the port in app.json or using the PORT environment variable:

export PORT=8080   # Linux/macOS
set PORT=8080      # Windows cmd
Error cannot find package
Execute:

go mod tidy
go mod download
The compilation fails on Windows with "gcc"
The mattn/go-sqlite3 driver requires GCC (e.g., TDM-GCC or MinGW-w64).
Alternative: use compilation with the `-tags` attribute: `sqlite_omit_load_extension`.

License
MIT – see LICENSE file (if applicable). By default, the project is distributed under the MIT license.

Author
LS User – Proyecto LS Engine v6.0 (Go Port)

Contributions
Contributions are welcome. Please open an issue or pull request to help improve the engine.
