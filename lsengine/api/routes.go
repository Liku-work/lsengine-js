// api/routes.go
package api

import (
	"net/http"

	"lsengine/internal/middleware"
)

func SetupAPIRoutes(router *middleware.Router) {
	router.GET("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	router.GET("/api/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"LS Engine","version":"6.0"}`))
	})

	router.GET("/api/ws-stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ws_active":0}`))
	})
}