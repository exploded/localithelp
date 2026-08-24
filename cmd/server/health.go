package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"localithelp/db"
)

// handleHealth is the uptime-monitor endpoint. 200 "ok" when the process is
// serving and the database answers; 503 otherwise. Never cached so the
// monitor sees the live state.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := db.Ping(ctx); err != nil {
		log.Printf("health: db ping failed: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("db unavailable\n"))
		return
	}
	w.Write([]byte("ok\n"))
}
