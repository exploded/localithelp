package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestReviewRoute covers the whole gate: with REVIEW_URL set /review redirects
// to the Google form; without it the route isn't registered at all, so nothing
// leaks a broken link.
func TestReviewRoute(t *testing.T) {
	const target = "https://g.page/r/TEST/review"
	site = siteConfig{BaseURL: "https://example.test", ReviewURL: target}

	rec := httptest.NewRecorder()
	newMux("../..").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/review", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != target {
		t.Errorf("/review = %d %q, want 302 %q", rec.Code, rec.Header().Get("Location"), target)
	}

	site.ReviewURL = ""
	rec = httptest.NewRecorder()
	newMux("../..").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/review", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/review unconfigured = %d, want 404", rec.Code)
	}
}

// TestHTTPSURL: a malformed REVIEW_URL must disable the feature rather than
// render a link html/template would neuter anyway.
func TestHTTPSURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://g.page/r/X/review":           "https://g.page/r/X/review",
		"  https://g.page/r/X/review":         "https://g.page/r/X/review",
		"http://g.page/r/X/review":            "",
		"javascript:alert(1)":                 "",
		"":                                    "",
		`https://x/" onmouseover=`:            "",
		"https://" + strings.Repeat("a", 600): "",
	} {
		if got := httpsURL(in); got != want {
			t.Errorf("httpsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReviewQR(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test", ReviewURL: "https://g.page/r/TEST/review"}
	if got := reviewShortURL(); got != "https://example.test/review" {
		t.Errorf("reviewShortURL = %q", got)
	}
	uri, err := reviewQRDataURI(256)
	if err != nil {
		t.Fatalf("reviewQRDataURI: %v", err)
	}
	if !strings.HasPrefix(string(uri), "data:image/png;base64,iVBOR") || len(uri) < 500 {
		t.Errorf("QR data URI looks wrong (%d bytes): %.60s", len(uri), uri)
	}
	if got := stripScheme("https://example.test/review"); got != "example.test/review" {
		t.Errorf("stripScheme = %q", got)
	}
}
