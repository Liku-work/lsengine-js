#!/bin/bash

# LS Engine Compiler for Linux

echo "========================================"
echo "   COMPILANDO LS ENGINE v6.0"
echo "========================================"
echo ""

# Verificar Go
if ! command -v go &> /dev/null; then
    echo "[ERROR] Go no esta instalado"
    exit 1
fi

# Mostrar version
echo "[1/5] Go version:"
go version
echo ""

# Limpiar cache de compilacion
echo "[2/5] Limpiando cache..."
go clean -cache
go clean -testcache
echo ""

# Limpiar cache de modulos
echo "[3/5] Limpiando mod cache..."
go clean -modcache
echo ""

# Descargar y limpiar dependencias
echo "[4/5] Actualizando dependencias..."
go mod tidy
go mod verify
echo ""

# Compilar
echo "[5/5] Compilando..."
go build -ldflags="-s -w" -o lsengine ./cmd/main.go

if [ $? -eq 0 ]; then
    echo ""
    echo "========================================"
    echo "   COMPILACION EXITOSA!"
    echo "========================================"
    echo ""
    echo "Archivo: lsengine"
    echo "Tamanio: $(ls -lh lsengine | awk '{print $5}')"
    echo ""
    echo "Para ejecutar: ./lsengine"
else
    echo ""
    echo "========================================"
    echo "   ERROR EN COMPILACION"
    echo "========================================"
    echo "Revisa los errores arriba"
    exit 1
fi

echo ""
