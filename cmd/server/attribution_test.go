package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"localithelp/db"
)

// cookieByName finds a Set-Cookie by name. Never index the slice: an ad click
// sets two cookies and a plain visit one, so position means nothing.
func cookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// stubAdClicks replaces the recorder so the middleware runs without a
// database, and hands back the clicks it was asked to store.
func stubAdClicks(t *testing.T) *[]adClick {
	t.Helper()
	prev := recordAdClick
	var got []adClick
	recordAdClick = func(_ string, c adClick) (string, error) {
		got = append(got, c)
		return "tok-1", nil
	}
	t.Cleanup(func() { recordAdClick = prev })
	return &got
}

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

// TestDetectAdClick covers what a paid click carries, and that an unpaid visit
// carries nothing.
func TestDetectAdClick(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}

	t.Run("full suffix", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet,
			"/computer-help/donvale?gclid=abc123&utm_source=google&utm_medium=cpc"+
				"&utm_campaign=22334455&utm_term=computer+technician+near+me", nil)
		c := detectAdClick(r)
		if c == nil {
			t.Fatal("detectAdClick() = nil, want a click")
		}
		if c.Source != db.SourceGoogleAds || c.GCLID != "abc123" ||
			c.Keyword != "computer technician near me" || c.Campaign != "22334455" ||
			c.Landing != "/computer-help/donvale" {
			t.Errorf("detectAdClick() = %+v", *c)
		}
	})

	t.Run("gclid alone", func(t *testing.T) {
		c := detectAdClick(httptest.NewRequest(http.MethodGet, "/?gclid=abc123", nil))
		if c == nil || c.GCLID != "abc123" || c.Keyword != "" {
			t.Errorf("detectAdClick() = %+v, want a click with no keyword", c)
		}
	})

	t.Run("cpc without gclid", func(t *testing.T) {
		if c := detectAdClick(httptest.NewRequest(http.MethodGet, "/?utm_medium=cpc", nil)); c == nil {
			t.Error("detectAdClick() = nil, want a click")
		}
	})

	for _, url := range []string{"/pricing", "/?utm_source=qr-card", "/", "/?utm_term=stray"} {
		t.Run("not an ad "+url, func(t *testing.T) {
			if c := detectAdClick(httptest.NewRequest(http.MethodGet, url, nil)); c != nil {
				t.Errorf("detectAdClick(%q) = %+v, want nil", url, *c)
			}
		})
	}

	t.Run("oversized keyword is truncated", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/?gclid=x&utm_term="+strings.Repeat("a", 10000), nil)
		c := detectAdClick(r)
		if c == nil {
			t.Fatal("detectAdClick() = nil, want a click")
		}
		if len([]rune(c.Keyword)) != adFieldMax {
			t.Errorf("keyword kept %d runes, want %d", len([]rune(c.Keyword)), adFieldMax)
		}
	})
}

// TestTrackSourceCookie checks the marker survives the walk from the landing
// page to the booking form, and that a later unmarked visit doesn't erase it.
func TestTrackSourceCookie(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}
	clicks := stubAdClicks(t)
	h := trackSource(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?gclid=abc123&utm_term=laptop+help", nil))
	cookies := rec.Result().Cookies()
	src := cookieByName(cookies, srcCookie)
	click := cookieByName(cookies, clickCookie)
	if src == nil || src.Value != db.SourceGoogleAds {
		t.Fatalf("expected a %s cookie holding %q, got %+v", srcCookie, db.SourceGoogleAds, cookies)
	}
	if click == nil || click.Value != "tok-1" {
		t.Fatalf("expected a %s cookie holding the click token, got %+v", clickCookie, cookies)
	}
	if len(*clicks) != 1 || (*clicks)[0].Keyword != "laptop help" {
		t.Errorf("recorded clicks = %+v", *clicks)
	}

	// A later page view with no marker must not overwrite the ad click.
	rec2 := httptest.NewRecorder()
	unmarked := httptest.NewRequest(http.MethodGet, "/pricing", nil)
	unmarked.AddCookie(click)
	h.ServeHTTP(rec2, unmarked)
	if got := rec2.Result().Cookies(); len(got) != 0 {
		t.Errorf("unmarked visit set a cookie: %+v", got)
	}

	// Assets aren't landing pages, marker or not.
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/static/css/styles.css?gclid=abc123", nil))
	if got := rec3.Result().Cookies(); len(got) != 0 {
		t.Errorf("static asset set a cookie: %+v", got)
	}
	if len(*clicks) != 1 {
		t.Errorf("static asset recorded a click: %+v", *clicks)
	}

	// A later non-ad marker takes the source AND drops the click, so the two
	// can never disagree about where the booking came from.
	rec4 := httptest.NewRecorder()
	card := httptest.NewRequest(http.MethodGet, "/?utm_source=qr-card", nil)
	card.AddCookie(click)
	h.ServeHTTP(rec4, card)
	after := rec4.Result().Cookies()
	if c := cookieByName(after, srcCookie); c == nil || c.Value != "qr-card" {
		t.Errorf("card visit left source = %+v, want qr-card", c)
	}
	if c := cookieByName(after, clickCookie); c == nil || c.MaxAge >= 0 {
		t.Errorf("card visit left click cookie = %+v, want it expired", c)
	}

	// The booking POST reads the marker back.
	post := httptest.NewRequest(http.MethodPost, "/book", nil)
	post.AddCookie(src)
	if got := bookingSource(post); got != db.SourceGoogleAds {
		t.Errorf("bookingSource() = %q, want %q", got, db.SourceGoogleAds)
	}

	// No cookie at all is a direct visit, never an empty string.
	if got := bookingSource(httptest.NewRequest(http.MethodPost, "/book", nil)); got != db.SourceDirect {
		t.Errorf("bookingSource() with no cookie = %q, want %q", got, db.SourceDirect)
	}
}

// TestRecordAdClickDedupe checks that reloading the same ad landing page
// doesn't log the click twice: clicks-per-booking is only worth reading if it
// counts clicks rather than page views.
func TestRecordAdClickDedupe(t *testing.T) {
	if err := db.Open(filepath.Join(t.TempDir(), "clicks.db")); err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	c := adClick{Source: db.SourceGoogleAds, GCLID: "abc123", Keyword: "laptop help", Landing: "/"}
	first, err := recordAdClick("", c)
	if err != nil {
		t.Fatalf("recordAdClick: %v", err)
	}
	again, err := recordAdClick(first, c)
	if err != nil {
		t.Fatalf("recordAdClick (reload): %v", err)
	}
	if again != first {
		t.Errorf("reload minted a new token %q, want %q", again, first)
	}

	// A different click is a different row.
	other, err := recordAdClick(first, adClick{Source: db.SourceGoogleAds, GCLID: "def456", Landing: "/"})
	if err != nil {
		t.Fatalf("recordAdClick (second click): %v", err)
	}
	if other == first {
		t.Error("a new gclid reused the old token")
	}

	// Once the click is spent on a booking, a fresh visit starts a new row.
	if ok, err := db.LinkAdClick(first, 7); err != nil || !ok {
		t.Fatalf("LinkAdClick = %v, %v", ok, err)
	}
	third, err := recordAdClick(first, c)
	if err != nil {
		t.Fatalf("recordAdClick (after booking): %v", err)
	}
	if third == first {
		t.Error("a click already spent on a booking was reused")
	}
}
