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
	mux.HandleFunc("POST /admin/bookings/{id}/notes", requireAdmin(handleAdminBookingNotes))
	mux.HandleFunc("POST /admin/bookings/{id}/issue", requireAdmin(handleAdminBookingIssue))
	mux.HandleFunc("POST /admin/bookings/{id}/schedule", requireAdmin(handleAdminBookingSchedule))
	mux.HandleFunc("GET /admin/customers", requireAdmin(handleAdminCustomers))
	mux.HandleFunc("POST /admin/calendar/resync", requireAdmin(handleAdminCalendarResync))

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

	// --- the other booking cards --------------------------------------------
	// Each posts to its own endpoint and gets its own card back, plus the
	// shared flash region as an hx-partial.
	card := func(path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		form.Set("csrf", "csrf1")
		req := httptest.NewRequest("POST", "/admin/bookings/"+itoa(id)+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.AddCookie(&http.Cookie{Name: "user_session", Value: sessTok})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	rr = card("/notes", url.Values{"notes": {"Replaced the SSD."}})
	if rr.Code != http.StatusOK {
		t.Fatalf("notes: %d %s", rr.Code, rr.Body.String())
	}
	body = rr.Body.String()
	if !strings.Contains(body, `id="notes-card"`) {
		t.Error("notes response is not the notes card")
	}
	if !strings.Contains(body, `<hx-partial hx-target="#admin-flash"`) {
		t.Error("notes response carries no flash swap, so the save is silent")
	}
	if !strings.Contains(body, "Notes saved.") {
		t.Error("notes response is missing the confirmation message")
	}
	if !strings.Contains(body, "Replaced the SSD.") {
		t.Error("notes card did not come back with the saved text")
	}
	if b, _ := db.GetBooking(id); b.AdminNotes != "Replaced the SSD." {
		t.Errorf("notes not stored: %q", b.AdminNotes)
	}

	// An empty problem is rejected: 422, the card comes back, nothing stored.
	before, _ := db.GetBooking(id)
	rr = card("/issue", url.Values{"issue": {"   "}})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty issue: got %d, want 422", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "can&#39;t be empty") {
		t.Error("empty issue response is missing the error message")
	}
	if after, _ := db.GetBooking(id); after.Issue != before.Issue {
		t.Error("rejected issue was written anyway")
	}

	rr = card("/issue", url.Values{"issue": {"Fan is grinding."}})
	if rr.Code != http.StatusOK {
		t.Fatalf("issue: %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `id="issue-card"`) {
		t.Error("issue response is not the issue card")
	}

	// Scheduling flips the status to booked, so it must bring the status card
	// and the heading badge with it — three regions from the one request.
	rr = card("/schedule", url.Values{"start": {"2027-03-04T09:30"}, "duration": {"60"}})
	if rr.Code != http.StatusOK {
		t.Fatalf("schedule: %d %s", rr.Code, rr.Body.String())
	}
	body = rr.Body.String()
	for _, want := range []string{`id="schedule-card"`, `hx-target="#status-card"`, `hx-target="#booking-badge"`, `hx-target="#admin-flash"`} {
		if !strings.Contains(body, want) {
			t.Errorf("schedule response is missing %s", want)
		}
	}
	if b, _ := db.GetBooking(id); b.Status != db.BookingBooked {
		t.Errorf("schedule did not set booked: %q", b.Status)
	}

	// A bad date is rejected without touching the booking.
	rr = card("/schedule", url.Values{"start": {"not-a-date"}, "duration": {"60"}})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad schedule date: got %d, want 422", rr.Code)
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

	// --- customer search-as-you-type ----------------------------------------
	// htmx asks for the results table only; a normal visit gets the full page,
	// which matters because htmx 4 restores history with a real page request.
	getCustomers := func(q string, htmx bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/admin/customers?q="+url.QueryEscape(q), nil)
		req.AddCookie(&http.Cookie{Name: "user_session", Value: sessTok})
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	rr = getCustomers("nobody-by-this-name", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("customer search: %d", rr.Code)
	}
	frag := rr.Body.String()
	if strings.Contains(frag, "<!DOCTYPE html>") {
		t.Error("customer search returned the whole page instead of the results fragment")
	}
	if strings.Contains(frag, `id="customer-results"`) {
		t.Error("fragment includes its own swap target; innerHTML would nest a second one")
	}
	if !strings.Contains(frag, "No customers") {
		t.Errorf("empty search should say so: %s", frag)
	}

	full := getCustomers("nobody-by-this-name", false)
	if !strings.Contains(full.Body.String(), "<!DOCTYPE html>") {
		t.Error("a non-htmx customer search must still return the full page")
	}
	if !strings.Contains(full.Body.String(), `id="customer-results"`) {
		t.Error("full page is missing the search results swap target")
	}

	// --- calendar settings: flash-only response ------------------------------
	// Nothing on that page changes but the message, so the reply is just the
	// flash hx-partial and the form uses hx-swap="none".
	req = httptest.NewRequest("POST", "/admin/calendar/resync", strings.NewReader("csrf=csrf1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "user_session", Value: sessTok})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	// No calendar is connected in this test, so this is the error path.
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("resync with no calendar: got %d, want 422", rr.Code)
	}
	body = rr.Body.String()
	if !strings.Contains(body, `<hx-partial hx-target="#admin-flash"`) {
		t.Error("resync response is not a flash-only swap")
	}
	if !strings.Contains(body, "not connected") {
		t.Errorf("resync response is missing the reason: %s", body)
	}
	if strings.Contains(body, "admin-card") {
		t.Error("resync response should carry no card markup at all")
	}
}
