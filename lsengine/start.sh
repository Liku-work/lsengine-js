@echo off
title LS Engine Compiler
color 0A

echo ========================================
echo    COMPILANDO LS ENGINE v6.0
echo ========================================
echo.

:: Verificar Go
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Go no esta instalado
    pause
    exit /b 1
)

:: Mostrar version
echo [1/5] Go version:
go version
echo.

:: Limpiar cache de compilacion
echo [2/5] Limpiando cache...
go clean -cache
go clean -testcache
echo.

:: Limpiar cache de modulos
echo [3/5] Limpiando mod cache...
go clean -modcache
echo.

:: Descargar y limpiar dependencias
echo [4/5] Actualizando dependencias...
go mod tidy
go mod verify
echo.

:: Compilar
echo [5/5] Compilando...
go build -ldflags="-s -w" -o lsengine.exe ./cmd/main.go

if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo    COMPILACION EXITOSA!
    echo ========================================
    echo.
    echo Archivo: lsengine.exe
    echo Tamanio: 
    dir lsengine.exe | find "lsengine.exe"
    echo.
    echo Para ejecutar: lsengine.exe
) else (
    echo.
    echo ========================================
    echo    ERROR EN COMPILACION
    echo ========================================
    echo Revisa los errores arriba
)

echo.
pause