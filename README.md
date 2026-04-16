# LS Engine v6 version 1.0.0 beta (Go Port)

**LS Engine** es un servidor web optimizado, escrito en Go, disenado para ejecutar aplicaciones web con caracteristicas avanzadas como WebSockets, importacion dinamica de modulos JS, motor JavaScript (Goja), almacenamiento JSON shardeado, seguridad reforzada y metricas en tiempo real.

## Estructura del Proyecto

```
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
```

## Caracteristicas Principales

- Servidor HTTP con soporte para compresion Gzip, CORS, timeouts configurables.
- WebSockets con broadcast, ping/pong, limite de conexiones.
- Motor JavaScript (Goja) con sandbox, pool de VMs y APIs nativas (ls.app, ls.http, ls.crypto, etc.).
- Importacion dinamica de modulos JS mediante <script to-call>, <script ls>, <script ls-ws> e import {x} from "archivo.js".
- Base de datos JSON shardeada (SQLite por shard) con indices B-Tree y consultas paginadas.
- Seguridad: validacion de rutas, bloqueo de imports remotos, sanitizacion de entradas, CSP, HSTS.
- Metricas: estadisticas en tiempo real, endpoint /metrics (JSON o Prometheus).
- Modo cluster (workers) para mayor escalabilidad.
- Cache distribuida (Redis) o en memoria.
- Hot reload opcional (configurable).

## Requisitos Previos

- Go 1.21 o superior (recomendado 1.25+)
- Git (opcional, para clonar el repositorio)
- Make (opcional, si usas Makefile)
- Redis (opcional, solo si usas cache distribuida)

## Compilacion y Ejecucion

### Windows (cmd / PowerShell)

1. Abre una terminal en la raiz del proyecto.
2. Ejecuta el script de compilacion:

```batch
start.bat
```

3. Si la compilacion es exitosa, se generara lsengine.exe.
4. Ejecuta el servidor:

```batch
lsengine.exe
```

Tambien puedes compilar manualmente:

```batch
go mod tidy
go build -ldflags="-s -w" -o lsengine.exe ./cmd/main.go
```

### Linux / macOS (bash)

1. Da permisos de ejecucion al script (solo la primera vez):

```bash
chmod +x start.sh
```

2. Ejecuta el script de compilacion:

```bash
./start.sh
```

3. Se generara el binario lsengine (sin extension).
4. Ejecuta el servidor:

```bash
./lsengine
```

Compilacion manual:

```bash
go mod tidy
go build -ldflags="-s -w" -o lsengine ./cmd/main.go
```

## Prueba Rapida

Una vez que el servidor este corriendo, abre tu navegador en:

```
http://localhost:1505
```

Endpoints utiles:

- http://localhost:1505/health -> Estado de salud
- http://localhost:1505/metrics -> Metricas (JSON)
- http://localhost:1505/metrics?format=prometheus -> Metricas para Prometheus
- http://localhost:1505/upload -> Subida de archivos (POST multipart)
- http://localhost:1505/stream -> Server-Sent Events (keepalive)
- http://localhost:1505/ws -> WebSocket

## Dependencias Principales

| Paquete | Uso |
|--------|------|
| github.com/dop251/goja | Motor JavaScript |
| github.com/gorilla/websocket | WebSockets |
| github.com/prometheus/client_golang | Metricas Prometheus |
| github.com/go-redis/redis/v8 | Cache distribuida |
| github.com/mattn/go-sqlite3 | Almacenamiento JSON shardeado |
| golang.org/x/time/rate | Rate limiting |
| golang.org/x/sync | Singleflight, semaforos |

## Configuracion (app.json)

El archivo app.json se genera automaticamente en la primera ejecucion. Algunos valores clave:

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

Puedes modificar app.json sin necesidad de recompilar.

## Solucion de Problemas Comunes

### go: command not found
Instala Go desde golang.org y asegurate de que este en el PATH.

### Puerto en uso
Cambia el puerto en app.json o mediante la variable de entorno PORT:

```bash
export PORT=8080   # Linux/macOS
set PORT=8080      # Windows cmd
```

### Error cannot find package
Ejecuta:

```bash
go mod tidy
go mod download
```

### La compilacion falla en Windows con "gcc"
El driver mattn/go-sqlite3 requiere GCC (por ejemplo, TDM-GCC o MinGW-w64).  
Alternativa: usa compilacion con etiqueta -tags sqlite_omit_load_extension.

## Licencia

MIT – ver archivo LICENSE (si aplica). Por defecto, el proyecto se distribuye bajo licencia MIT.

## Autor

LS User – Proyecto LS Engine v6.0 (Go Port)

## Contribuciones

Las contribuciones son bienvenidas. Por favor, abre un issue o pull request para mejorar el motor.
