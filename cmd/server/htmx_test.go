package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localithelp/db"
)

// TestBookingStatusHTMX covers the htmx path on the booking status buttons:
// the request must come back as the status card fragment (not a redirect, not
// a whole page), carry the hx-partial that refreshes the heading badge, and
// still fall back to a plain redirect when the HX-Request header is absent.
func TestBookingStatusHTMX(t *testing.T) {
	if err := db.Open(filepath.Join(t.TempDir(), "htmx.db")); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var err error
	pages, err = loadTemplates("../../templates")
	if err != nil {
		t.Fatal(err)
	}
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test", OnsiteFee: 80, BlockRate: 30, Suburbs: suburbs}
	mail = nil

	sessTok := "sess-" + generateSessionToken()
	userSessions.Store(sessTok, &userSession{User: &db.User{Email: "james67@gmail.com"}, Expiry: time.Now().Add(time.Hour), CSRF: "csrf1"})
	defer userSessions.Delete(sessTok)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("GET /admin/bookings/{id}", requireAdmin(handleAdminBooking))
	mux.HandleFunc("POST /admin/bookings/{id}/status", requireAdmin(handleAdminBookingStatus))

	id, err := db.InsertBooking(&db.Booking{
		Name: "Test Person", Email: "t@example.test", Suburb: "Donvale",
		Issue: "Laptop is very slow.", Mode: "onsite",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/admin/bookings/" + itoa(id) + "/status"

	// post issues a status change. htmx controls whether the HX-Request header
	// is sent, i.e. whether we take the fragment path or the redirect path.
	post := func(status string, htmx bool) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{"status": {status}, "csrf": {"csrf1"}}
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "user_session", Value: sessTok})
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	// --- htmx path: fragment back, status applied ---------------------------
	rr := post("contacted", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("htmx status post: got %d, want 200: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("htmx response contains the full page layout; it must be the fragment only")
	}
	if !strings.Contains(body, `id="status-card"`) {
		t.Error("htmx response is missing the status card swap target")
	}
	if !strings.Contains(body, `<hx-partial hx-target="#booking-badge"`) {
		t.Error("htmx response is missing the hx-partial that refreshes the heading badge")
	}
	if b, _ := db.GetBooking(id); b.Status != db.BookingContacted {
		t.Errorf("status after htmx post: got %q, want %q", b.Status, db.BookingContacted)
	}
	// The button for the status we just set must be gone, and "Back to new"
	// must have appeared — that redraw is the whole point of the swap.
	if strings.Contains(body, `value="contacted"`) {
		t.Error("swapped card still offers the status the booking is already in")
	}
	if !strings.Contains(body, `value="new"`) {
		t.Error("swapped card does not offer a way back to new")
	}

	// --- inherited attributes must survive into the fragment -----------------
	// Without :inherited, htmx 4 children do not pick these up and every swap
	// would land in the wrong place.
	if !strings.Contains(body, `hx-target:inherited="#status-card"`) {
		t.Error("fragment lost hx-target:inherited; child forms would not know where to swap")
	}
	if !strings.Contains(body, `hx-swap:inherited="outerHTML"`) {
		t.Error("fragment lost hx-swap:inherited")
	}

	// --- progressive enhancement: no HX-Request means the old redirect -------
	rr = post("done", false)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("plain form post: got %d, want 303 — the no-JS fallback is broken", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "ok=") {
		t.Errorf("plain form post redirect: got %q, want an ok= flash", loc)
	}
	if b, _ := db.GetBooking(id); b.Status != db.BookingDone {
		t.Errorf("status after plain post: got %q, want %q", b.Status, db.BookingDone)
	}

	// --- rejected status: 422 carrying the card with the error inside --------
	rr = post("invoiced", true)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rejected status: got %d, want 422", rr.Code)
	}
	body = rr.Body.String()
	if !strings.Contains(body, "can&#39;t be set by hand") {
		t.Errorf("422 response is missing the error message: %s", body)
	}
	if !strings.Contains(body, `id="status-card"`) {
		t.Error("422 response must still be a swappable card — htmx 4 swaps 4xx by default")
	}
	if b, _ := db.GetBooking(id); b.Status != db.BookingDone {
		t.Errorf("rejected status changed the booking: got %q", b.Status)
	}

	// --- the full page still renders and carries the htmx wiring ------------
	req := httptest.NewRequest("GET", "/admin/bookings/"+itoa(id), nil)
	req.AddCookie(&http.Cookie{Name: "user_session", Value: sessTok})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("booking page: %d", rr.Code)
	}
	page := rr.Body.String()
	if !strings.Contains(page, `hx-headers:inherited=`) {
		t.Error("admin page is missing hx-headers:inherited — CSRF would not reach htmx requests")
	}
	if !strings.Contains(page, "js/htmx.min.js") {
		t.Error("admin page does not load htmx")
	}
	if !strings.Contains(page, `id="booking-badge"`) {
		t.Error("admin page is missing the badge target the hx-partial swaps into")
	}
	if !strings.Contains(page, `id="status-card"`) {
		t.Error("admin page is missing the status card")
	}

	// --- public pages must stay htmx-free ------------------------------------
	// The marketing pages are the SEO surface and carry no htmx interaction, so
	// they should not pay for the script or the body attribute.
	req = httptest.NewRequest("GET", "/", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("home page: %d", rr.Code)
	}
	home := rr.Body.String()
	if strings.Contains(home, "htmx.min.js") {
		t.Error("public home page loads htmx; it should be gated on .IsAdmin")
	}
	if strings.Contains(home, "hx-headers") {
		t.Error("public home page carries hx-headers; it should be gated on .IsAdmin")
	}
}
