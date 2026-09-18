package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// testOAuthConfig is enough of a config for the handlers to get past their
// "not configured" guard; no request ever reaches Google in these tests.
func testOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.test/auth/google/callback",
		Scopes:       []string{"openid"},
		Endpoint:     google.Endpoint,
	}
}

// A callback carrying a state we never issued — a cached Safari redirect, or a
// deploy that wiped the in-memory states — must restart the login instead of
// dead-ending on "invalid state parameter", and must keep the destination.
func TestGoogleCallbackUnknownStateRestartsLogin(t *testing.T) {
	googleOAuth = testOAuthConfig()
	defer func() { googleOAuth = nil }()

	r := httptest.NewRequest("GET", "/auth/google/callback?state=stale&code=x", nil)
	r.AddCookie(&http.Cookie{Name: loginRedirectCookie, Value: "%2Fadmin%2Fbookings"})
	w := httptest.NewRecorder()
	handleGoogleCallback(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 back into the login", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/auth/google?redirect=%2Fadmin%2Fbookings&n=") {
		t.Errorf("Location = %q, want the login with the destination and a cache-busting nonce", loc)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", w.Header().Get("Cache-Control"))
	}
	if cookieByName(w.Result().Cookies(), loginRetryCookie) == nil {
		t.Error("retry cookie not set, so a second failure would loop")
	}
}

// The retry is one-shot: a second failure shows a page with a way out.
func TestGoogleCallbackSecondFailureIsNotALoop(t *testing.T) {
	googleOAuth = testOAuthConfig()
	defer func() { googleOAuth = nil }()

	r := httptest.NewRequest("GET", "/auth/google/callback?state=stale&code=x", nil)
	r.AddCookie(&http.Cookie{Name: loginRedirectCookie, Value: "%2Fadmin"})
	r.AddCookie(&http.Cookie{Name: loginRetryCookie, Value: "1"})
	w := httptest.NewRecorder()
	handleGoogleCallback(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `href="/admin"`) {
		t.Errorf("no link back to /admin in the error page: %s", body)
	}
	if c := cookieByName(w.Result().Cookies(), loginRetryCookie); c == nil || c.MaxAge >= 0 {
		t.Error("retry cookie not cleared, so the next attempt starts already retried")
	}
}

// The login entry point must not be cacheable, or Safari replays one authorise
// URL — and one spent state — forever.
func TestGoogleLoginIsNotCacheable(t *testing.T) {
	googleOAuth = testOAuthConfig()
	defer func() { googleOAuth = nil }()

	w := httptest.NewRecorder()
	handleGoogleLogin(w, httptest.NewRequest("GET", "/auth/google?redirect=%2Fadmin", nil))

	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307 to Google", w.Code)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", w.Header().Get("Cache-Control"))
	}
	c := cookieByName(w.Result().Cookies(), loginRedirectCookie)
	if c == nil || c.Value != "%2Fadmin" {
		t.Fatalf("redirect cookie = %+v, want the escaped destination", c)
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Error("redirect cookie must be SameSite=Lax to survive the return from Google")
	}
}

// Offsite redirect targets are dropped, and clear any earlier destination.
func TestGoogleLoginRejectsOpenRedirect(t *testing.T) {
	googleOAuth = testOAuthConfig()
	defer func() { googleOAuth = nil }()

	w := httptest.NewRecorder()
	handleGoogleLogin(w, httptest.NewRequest("GET", "/auth/google?redirect=//evil.example", nil))

	c := cookieByName(w.Result().Cookies(), loginRedirectCookie)
	if c == nil || c.MaxAge >= 0 || c.Value != "" {
		t.Fatalf("redirect cookie = %+v, want cleared", c)
	}
}

// Anonymous admin GETs bounce to the login, and that bounce must not be cached
// either — a cached copy keeps sending a signed-in user back to Google.
func TestRequireAdminBounceIsNotCacheable(t *testing.T) {
	w := httptest.NewRecorder()
	requireAdmin(func(http.ResponseWriter, *http.Request) {})(w, httptest.NewRequest("GET", "/admin", nil))

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", w.Header().Get("Cache-Control"))
	}
}
