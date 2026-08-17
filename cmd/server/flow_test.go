package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mchugh.com.au/db"
)

// TestBookingToInvoiceFlow drives the admin handlers end to end against a temp
// SQLite file: enquiry → customer linked → schedule → done → invoice draft →
// edit lines → send → public view/PDF → mark paid → follow-up booking.
func TestBookingToInvoiceFlow(t *testing.T) {
	if err := db.Open(filepath.Join(t.TempDir(), "flow.db")); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var err error
	pages, err = loadTemplates("../../templates")
	if err != nil {
		t.Fatal(err)
	}
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test", OnsiteFee: 80, BlockRate: 30, Suburbs: suburbs,
		ABN: "14 723 053 435", BankName: "James McHugh", BankBSB: "000-000", BankAcct: "12345678"}
	mail = nil

	sessTok := "sess-" + generateSessionToken()
	userSessions.Store(sessTok, &userSession{User: &db.User{Email: "james67@gmail.com"}, Expiry: time.Now().Add(time.Hour), CSRF: "csrf1"})
	defer userSessions.Delete(sessTok)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /book", handleBookSubmit)
	mux.HandleFunc("GET /invoice/{token}", handleInvoicePublic)
	mux.HandleFunc("GET /invoice/{token}/pdf", handleInvoicePublicPDF)
	mux.HandleFunc("GET /admin", requireAdmin(handleAdmin))
	mux.HandleFunc("GET /admin/bookings", requireAdmin(handleAdminBookings))
	mux.HandleFunc("GET /admin/bookings/{id}", requireAdmin(handleAdminBooking))
	mux.HandleFunc("POST /admin/bookings/{id}/schedule", requireAdmin(handleAdminBookingSchedule))
	mux.HandleFunc("POST /admin/bookings/{id}/status", requireAdmin(handleAdminBookingStatus))
	mux.HandleFunc("POST /admin/bookings/{id}/notes", requireAdmin(handleAdminBookingNotes))
	mux.HandleFunc("POST /admin/bookings/{id}/followup", requireAdmin(handleAdminBookingFollowup))
	mux.HandleFunc("GET /admin/calendar", requireAdmin(handleAdminCalendar))
	mux.HandleFunc("GET /admin/invoices", requireAdmin(handleAdminInvoices))
	mux.HandleFunc("POST /admin/invoices/new", requireAdmin(handleAdminInvoiceNew))
	mux.HandleFunc("GET /admin/invoices/{id}", requireAdmin(handleAdminInvoice))
	mux.HandleFunc("GET /admin/invoices/{id}/pdf", requireAdmin(handleAdminInvoicePDF))
	mux.HandleFunc("POST /admin/invoices/{id}/items", requireAdmin(handleAdminInvoiceItems))
	mux.HandleFunc("POST /admin/invoices/{id}/send", requireAdmin(handleAdminInvoiceSend))
	mux.HandleFunc("POST /admin/invoices/{id}/paid", requireAdmin(handleAdminInvoicePaid))
	mux.HandleFunc("POST /admin/invoices/{id}/void", requireAdmin(handleAdminInvoiceVoid))
	mux.HandleFunc("GET /admin/customers", requireAdmin(handleAdminCustomers))
	mux.HandleFunc("GET /admin/customers/{id}", requireAdmin(handleAdminCustomer))
	mux.HandleFunc("POST /admin/customers/{id}", requireAdmin(handleAdminCustomerSave))

	do := func(method, path string, form url.Values, admin bool) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if form == nil && admin && method == "POST" {
			form = url.Values{}
		}
		if form != nil {
			if admin {
				form.Set("csrf", "csrf1")
			}
			req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.RemoteAddr = "203.0.113.5:1234"
		if admin {
			req.AddCookie(&http.Cookie{Name: "user_session", Value: sessTok})
		}
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}
	get := func(path string) string {
		t.Helper()
		rr := do("GET", path, nil, true)
		if rr.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, rr.Code, rr.Body.String())
		}
		return rr.Body.String()
	}
	post := func(path string, form url.Values) string { // returns redirect location
		t.Helper()
		rr := do("POST", path, form, true)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("POST %s: %d %s", path, rr.Code, rr.Body.String())
		}
		loc := rr.Header().Get("Location")
		if strings.Contains(loc, "err=") {
			t.Fatalf("POST %s redirected with error: %s", path, loc)
		}
		return loc
	}

	// 1. Public enquiry (timestamp old enough to not be flagged).
	rr := do("POST", "/book", url.Values{
		"name": {"Zoë O'Brien"}, "phone": {"0400 000 001"}, "email": {"Zoe@Example.test"}, "suburb": {"Donvale"},
		"service": {"email-outlook"}, "issue": {"Outlook keeps asking for my password and won't send."},
		"preferred_time": {"Tue morning"}, "ts": {itoa(time.Now().Unix() - 10)},
	}, false)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("enquiry: %d %s", rr.Code, rr.Body.String())
	}
	bs, _ := db.ListBookings()
	if len(bs) != 1 || bs[0].CustomerID == 0 {
		t.Fatalf("enquiry not linked to a customer: %+v", bs)
	}
	b := bs[0]
	bid := b.ID
	bpath := "/admin/bookings/" + itoa(bid)
	if l := get("/admin/bookings"); !strings.Contains(l, "Zoë") {
		t.Fatalf("bookings list missing content: %s", l)
	}
	if p := get(bpath); !strings.Contains(p, "Schedule the visit") {
		t.Fatalf("booking page missing content: %s", p)
	}
	get("/admin")
	get("/admin/customers?q=zoe")

	// 2. Schedule via calendar-prefilled time, with confirmation email (nil mailer = ok).
	if !strings.Contains(get("/admin/calendar?for="+itoa(bid)), "?start=") {
		t.Fatal("calendar has no pick links")
	}
	loc := post(bpath+"/schedule", url.Values{"start": {"2026-08-25T09:30"}, "duration": {"75"}, "notify": {"1"}})
	if !strings.Contains(loc, "ok=") {
		t.Fatalf("schedule: %s", loc)
	}
	b2, _ := db.GetBooking(bid)
	if b2.Status != db.BookingBooked || b2.DurationMin != 75 || fmtWhen(b2.StartAt) != "Tue 25 Aug, 9:30 am" {
		t.Fatalf("after schedule: %+v", b2)
	}
	if !strings.Contains(get("/admin/calendar?week=2026-08-25"), "9:30 am – 10:45 am") {
		t.Fatal("calendar does not show the event")
	}
	// Reschedule + notes + done.
	post(bpath+"/schedule", url.Values{"start": {"2026-08-25T14:00"}, "duration": {"60"}})
	post(bpath+"/notes", url.Values{"notes": {"Reset app password; installed SSD."}})
	post(bpath+"/status", url.Values{"status": {"done"}})
	b2, _ = db.GetBooking(bid)
	if b2.Status != db.BookingDone || b2.AdminNotes == "" {
		t.Fatalf("after done: %+v", b2)
	}

	// 3. Invoice draft, prefilled from duration (60 min → 4 units).
	loc = post("/admin/invoices/new", url.Values{"booking": {itoa(bid)}})
	invPath := strings.SplitN(loc, "?", 2)[0]
	invID := lastSeg(invPath)
	page := get(invPath)
	if !strings.Contains(page, "INV-1000") || !strings.Contains(page, "Labour — 4 × 15 min") || !strings.Contains(page, "$200.00") {
		t.Fatalf("draft page:\n%s", page)
	}
	// Creating again reuses the draft.
	if loc2 := post("/admin/invoices/new", url.Values{"booking": {itoa(bid)}}); !strings.HasPrefix(loc2, invPath) {
		t.Fatalf("second create should reuse draft: %s", loc2)
	}
	// Edit: add hardware, paste link.
	post(invPath+"/items", url.Values{
		"desc": {"Onsite service fee", "Labour — 5 × 15 min", "Crucial 1 TB SSD", ""},
		"qty":  {"1", "5", "1", "1"},
		"unit": {"80", "30", "129.95", ""},
		"due":  {"2026-09-01"}, "notes": {"Thanks!"}, "payment_link": {"https://pay.example/zeller/abc"},
	})
	inv, _ := db.GetInvoice(invID)
	if inv.TotalCents != 8000+15000+12995 || inv.PaymentLink != "https://pay.example/zeller/abc" || inv.Status != db.InvoiceDraft {
		t.Fatalf("after edit: %+v", inv)
	}
	// Bad link rejected.
	if rr := do("POST", invPath+"/items", url.Values{"csrf": {"csrf1"}, "desc": {"x"}, "qty": {"1"}, "unit": {"1"}, "payment_link": {"javascript:alert(1)"}}, true); !strings.Contains(rr.Header().Get("Location"), "err=") {
		t.Fatal("javascript link accepted")
	}

	// 4. Send → sent, booking invoiced; public page + PDFs work; resend allowed.
	post(invPath+"/send", nil)
	inv, _ = db.GetInvoice(invID)
	b2, _ = db.GetBooking(bid)
	if inv.Status != db.InvoiceSent || inv.IssuedAt.IsZero() || b2.Status != db.BookingInvoiced {
		t.Fatalf("after send: inv=%+v booking=%s", inv, b2.Status)
	}
	if rr := do("POST", invPath+"/items", url.Values{"csrf": {"csrf1"}, "desc": {"x"}, "qty": {"1"}, "unit": {"1"}}, true); !strings.Contains(rr.Header().Get("Location"), "err=") {
		t.Fatal("sent invoice should not be editable")
	}
	pub := do("GET", "/invoice/"+inv.ViewToken, nil, false)
	if pub.Code != 200 || !strings.Contains(pub.Body.String(), "INV-1000") || !strings.Contains(pub.Body.String(), "https://pay.example/zeller/abc") || !strings.Contains(pub.Body.String(), "$359.95") {
		t.Fatalf("public invoice: %d\n%s", pub.Code, pub.Body.String())
	}
	if do("GET", "/invoice/"+strings.Repeat("b", 64), nil, false).Code != 404 || do("GET", "/invoice/short", nil, false).Code != 404 {
		t.Fatal("bad tokens should 404")
	}
	if p := do("GET", "/invoice/"+inv.ViewToken+"/pdf", nil, false); p.Code != 200 || !strings.HasPrefix(p.Body.String(), "%PDF-") || p.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("public pdf: %d", p.Code)
	}
	if p := do("GET", invPath+"/pdf", nil, true); p.Code != 200 {
		t.Fatalf("admin pdf: %d", p.Code)
	}
	post(invPath+"/send", nil) // resend
	get("/admin/invoices?status=sent")

	// 5. Mark paid with receipt → paid, booking paid; void refused afterwards.
	post(invPath+"/paid", url.Values{"method": {db.PayZellerLink}, "ref": {"Z-1"}, "notify": {"1"}})
	inv, _ = db.GetInvoice(invID)
	b2, _ = db.GetBooking(bid)
	if inv.Status != db.InvoicePaid || inv.PaymentMethod != db.PayZellerLink || b2.Status != db.BookingPaid {
		t.Fatalf("after paid: inv=%+v booking=%s", inv, b2.Status)
	}
	if rr := do("POST", invPath+"/void", url.Values{"csrf": {"csrf1"}}, true); !strings.Contains(rr.Header().Get("Location"), "err=") {
		t.Fatal("void of paid invoice should fail")
	}
	pub = do("GET", "/invoice/"+inv.ViewToken, nil, false)
	if !strings.Contains(pub.Body.String(), "Receipt") {
		t.Fatal("public page should show receipt")
	}

	// 6. Follow-up booking on the same customer, unscheduled; second invoice numbers 1001.
	loc = post(bpath+"/followup", url.Values{"issue": {"Return the repaired laptop"}})
	fid := lastSeg(strings.SplitN(loc, "?", 2)[0])
	fb, _ := db.GetBooking(fid)
	if fb.CustomerID != b.CustomerID || fb.ParentBookingID != bid || fb.Status != db.BookingNew {
		t.Fatalf("followup: %+v", fb)
	}
	loc = post("/admin/invoices/new", url.Values{"booking": {itoa(fid)}})
	inv2, _ := db.GetInvoice(lastSeg(strings.SplitN(loc, "?", 2)[0]))
	if inv2.Number != 1001 {
		t.Fatalf("second invoice number: %d", inv2.Number)
	}
	post("/admin/invoices/"+itoa(inv2.ID)+"/void", nil)

	// 7. Customer edit and search.
	cpath := "/admin/customers/" + itoa(b.CustomerID)
	post(cpath, url.Values{"name": {"Zoë O'Brien"}, "email": {"zoe@example.test"}, "phone": {"0400 000 001"}, "address": {"1 Test St"}, "suburb": {"Donvale"}, "notes": {"Lovely dog."}})
	if !strings.Contains(get(cpath), "1 Test St") || !strings.Contains(get("/admin/customers?q=0400"), "Zoë") {
		t.Fatal("customer edit/search")
	}
	// Cancel the follow-up with an email (nil mailer).
	post("/admin/bookings/"+itoa(fid)+"/status", url.Values{"status": {"cancelled"}, "notify": {"1"}, "reason": {"customer away"}})
	if fb, _ = db.GetBooking(fid); fb.Status != db.BookingCancelled {
		t.Fatal("cancel")
	}
	// Anonymous admin access is redirected; CSRF-less POST refused.
	if rr := do("GET", "/admin/bookings", nil, false); rr.Code != http.StatusSeeOther {
		t.Fatalf("anon admin: %d", rr.Code)
	}
	if rr := do("POST", bpath+"/notes", url.Values{"notes": {"x"}}, false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon post: %d", rr.Code)
	}
}

var reID = regexp.MustCompile(`(\d+)$`)

func lastSeg(path string) int64 {
	m := reID.FindString(path)
	var n int64
	for _, c := range m {
		n = n*10 + int64(c-'0')
	}
	return n
}

func itoa(n int64) string { return fmtInt(n) }

func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
