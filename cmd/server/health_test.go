package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"localithelp/db"
)

func TestHealth(t *testing.T) {
	if err := db.Open(filepath.Join(t.TempDir(), "health.db")); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handleHealth(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("healthy: got %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}

	db.Close()
	rec = httptest.NewRecorder()
	handleHealth(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed db: got %d, want 503", rec.Code)
	}
}
