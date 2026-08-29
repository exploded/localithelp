package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"localithelp/db"
)

// TestDetectSource covers the markers a visit can carry, and the ones it can't.
func TestDetectSource(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}

	cases := []struct {
		name, url, referer, want string
	}{
		{"google ads gclid", "/?gclid=abc123", "", db.SourceGoogleAds},
		{"google ads gad_source", "/services?gad_source=1", "", db.SourceGoogleAds},
		{"cpc medium", "/?utm_medium=cpc&utm_source=bing", "", db.SourceGoogleAds},
		{"utm tag", "/?utm_source=qr-card", "", "qr-card"},
		{"google organic", "/", "https://www.google.com/search?q=computer+help", db.SourceGoogleSearch},
		{"external referrer", "/", "https://www.bing.com/search", "bing.com"},
		{"own site", "/book", "https://example.test/pricing", ""},
		{"no signal", "/pricing", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.url, nil)
			if c.referer != "" {
				r.Header.Set("Referer", c.referer)
			}
			if got := detectSource(r); got != c.want {
				t.Errorf("detectSource() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestTrackSourceCookie checks the marker survives the walk from the landing
// page to the booking form, and that a later unmarked visit doesn't erase it.
func TestTrackSourceCookie(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}
	h := trackSource(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?gclid=abc123", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != srcCookie || cookies[0].Value != db.SourceGoogleAds {
		t.Fatalf("expected a %s cookie holding %q, got %+v", srcCookie, db.SourceGoogleAds, cookies)
	}

	// A later page view with no marker must not overwrite the ad click.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/pricing", nil))
	if got := rec2.Result().Cookies(); len(got) != 0 {
		t.Errorf("unmarked visit set a cookie: %+v", got)
	}

	// The booking POST reads the marker back.
	post := httptest.NewRequest(http.MethodPost, "/book", nil)
	post.AddCookie(cookies[0])
	if got := bookingSource(post); got != db.SourceGoogleAds {
		t.Errorf("bookingSource() = %q, want %q", got, db.SourceGoogleAds)
	}

	// No cookie at all is a direct visit, never an empty string.
	if got := bookingSource(httptest.NewRequest(http.MethodPost, "/book", nil)); got != db.SourceDirect {
		t.Errorf("bookingSource() with no cookie = %q, want %q", got, db.SourceDirect)
	}
}
