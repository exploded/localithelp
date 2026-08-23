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

	"localithelp/db"
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
	mux.HandleFunc("GET /admin/bookings/new", requireAdmin(handleAdminBookingNew))
	mux.HandleFunc("POST /admin/bookings/new", requireAdmin(handleAdminBookingCreate))
	mux.HandleFunc("GET /admin/bookings/{id}", requireAdmin(handleAdminBooking))
	mux.HandleFunc("POST /admin/bookings/{id}/schedule", requireAdmin(handleAdminBookingSchedule))
	mux.HandleFunc("POST /admin/bookings/{id}/status", requireAdmin(handleAdminBookingStatus))
	mux.HandleFunc("POST /admin/bookings/{id}/notes", requireAdmin(handleAdminBookingNotes))
	mux.HandleFunc("POST /admin/bookings/{id}/followup", requireAdmin(handleAdminBookingFollowup))
	mux.HandleFunc("POST /admin/bookings/{id}/address", requireAdmin(handleAdminBookingAddress))
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
	mux.HandleFunc("POST /admin/customers/{id}/bookings", requireAdmin(handleAdminCustomerBooking))

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

	// 1. Public enquiry (timestamp old enough to not be flagged). Without a
	// picked address (no addr_* fields) the form re-renders with an error.
	rr := do("POST", "/book", url.Values{
		"name": {"Zoë O'Brien"}, "phone": {"0400 000 001"}, "email": {"Zoe@Example.test"},
		"address": {"12 Sample St typed by hand"},
		"service": {"email-outlook"}, "issue": {"Outlook keeps asking for my password and won't send."},
		"preferred_time": {"Tue morning"}, "ts": {itoa(time.Now().Unix() - 10)},
	}, false)
	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "pick the address") {
		t.Fatalf("enquiry without picked address should 422: %d", rr.Code)
	}
	rr = do("POST", "/book", url.Values{
		"name": {"Zoë O'Brien"}, "phone": {"0400 000 001"}, "email": {"Zoe@Example.test"},
		"address": {"12 Sample St, Donvale VIC 3111"}, "addr_street": {"12 Sample St"},
		"addr_suburb": {"Donvale"}, "addr_state": {"VIC"}, "addr_postcode": {"3111"},
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
	if b.Address != "12 Sample St, Donvale VIC 3111" || b.Suburb != "Donvale" {
		t.Fatalf("enquiry address not stored: %q / %q", b.Address, b.Suburb)
	}
	// The new customer's blank address is filled from the enquiry.
	if c, _ := db.GetCustomer(b.CustomerID); c.Address != "12 Sample St, Donvale VIC 3111" || c.Suburb != "Donvale" {
		t.Fatalf("customer address not filled from enquiry: %+v", c)
	}
	bpath := "/admin/bookings/" + itoa(bid)
	if l := get("/admin/bookings"); !strings.Contains(l, "Zoë") {
		t.Fatalf("bookings list missing content: %s", l)
	}
	if p := get(bpath); !strings.Contains(p, "Schedule the visit") {
		t.Fatalf("booking page missing content: %s", p)
	}
	// Address: hand-typed (no structured addr_* fields) is rejected; a picked
	// suggestion saves to the customer and syncs the booking's suburb.
	rr = do("POST", bpath+"/address", url.Values{"address": {"12 Nowhere St"}}, true)
	if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || !strings.Contains(loc, "err=") {
		t.Fatalf("hand-typed address should be rejected: %d %s", rr.Code, loc)
	}
	// Outside Victoria is outside the service area.
	rr = do("POST", bpath+"/address", url.Values{
		"address": {"1 Pitt St, Sydney NSW 2000"}, "addr_street": {"1 Pitt St"},
		"addr_suburb": {"Sydney"}, "addr_state": {"NSW"}, "addr_postcode": {"2000"},
	}, true)
	if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || !strings.Contains(loc, "err=") {
		t.Fatalf("interstate address should be rejected: %d %s", rr.Code, loc)
	}
	post(bpath+"/address", url.Values{
		"address": {"1 Example Ct, Doncaster East VIC 3109"}, "addr_street": {"1 Example Ct"},
		"addr_suburb": {"Doncaster East"}, "addr_state": {"VIC"}, "addr_postcode": {"3109"},
	})
	if nb, _ := db.GetBooking(bid); nb.Address != "1 Example Ct, Doncaster East VIC 3109" || nb.Suburb != "Doncaster East" {
		t.Fatalf("booking address not saved: %q / %q", nb.Address, nb.Suburb)
	}
	// The admin edit overwrites the customer's address too (invoices read it).
	if c, _ := db.GetCustomer(b.CustomerID); c.Address != "1 Example Ct, Doncaster East VIC 3109" || c.Suburb != "Doncaster East" {
		t.Fatalf("customer address not synced: %+v", c)
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
	// Edit: add hardware and a seniors discount (negative unit), paste link.
	post(invPath+"/items", url.Values{
		"desc": {"Visit fee — includes travel", "Labour — 5 × 15 min", "Crucial 1 TB SSD", "Seniors Card discount — 20%", ""},
		"qty":  {"1", "5", "1", "1", "1"},
		"unit": {"80", "30", "129.95", "-58.00", ""},
		"due":  {"2026-09-01"}, "notes": {"Thanks!"}, "payment_link": {"https://pay.example/zeller/abc"},
	})
	inv, _ := db.GetInvoice(invID)
	if inv.TotalCents != 8000+15000+12995-5800 || inv.PaymentLink != "https://pay.example/zeller/abc" || inv.Status != db.InvoiceDraft {
		t.Fatalf("after edit: %+v", inv)
	}
	// The editor renders the negative unit bare ("-58.00") and it must survive a
	// re-save unchanged (fmtCents-style "-$58.00" once round-tripped the editor).
	if pg := get(invPath); !strings.Contains(pg, `value="-58.00"`) || !strings.Contains(pg, "-$58.00") {
		t.Fatalf("discount line not rendered:\n%s", pg)
	}
	post(invPath+"/items", url.Values{
		"desc": {"Visit fee — includes travel", "Labour — 5 × 15 min", "Crucial 1 TB SSD", "Seniors Card discount — 20%", ""},
		"qty":  {"1", "5", "1", "1", "1"},
		"unit": {"80", "30", "129.95", "-$58.00", ""},
		"due":  {"2026-09-01"}, "notes": {"Thanks!"}, "payment_link": {"https://pay.example/zeller/abc"},
	})
	if inv, _ = db.GetInvoice(invID); inv.TotalCents != 8000+15000+12995-5800 {
		t.Fatalf("negative unit did not round-trip: %+v", inv)
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
	if pub.Code != 200 || !strings.Contains(pub.Body.String(), "INV-1000") || !strings.Contains(pub.Body.String(), "https://pay.example/zeller/abc") || !strings.Contains(pub.Body.String(), "$301.95") || !strings.Contains(pub.Body.String(), "-$58.00") {
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
	// Paid: the receipt can be re-emailed; status stays paid.
	if page := get(invPath); !strings.Contains(page, "Resend receipt") {
		t.Fatal("paid invoice page should offer Resend receipt")
	}
	if loc := post(invPath+"/send", nil); !strings.Contains(loc, "Receipt") {
		t.Fatalf("resend receipt: %s", loc)
	}
	if inv, _ = db.GetInvoice(invID); inv.Status != db.InvoicePaid {
		t.Fatalf("resend receipt changed status: %s", inv.Status)
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
	// New booking straight from the customer page: unscheduled, linked, uses saved details.
	loc = post(cpath+"/bookings", url.Values{"service": {"email-outlook"}, "issue": {"Outlook won't open"}})
	nid := lastSeg(strings.SplitN(loc, "?", 2)[0])
	nb, _ := db.GetBooking(nid)
	if nb == nil || nb.CustomerID != b.CustomerID || nb.Status != db.BookingNew || nb.ServiceSlug != "email-outlook" ||
		nb.Name != "Zoë O'Brien" || nb.Email != "zoe@example.test" || nb.Suburb != "Donvale" || !nb.StartAt.IsZero() {
		t.Fatalf("customer booking: %+v", nb)
	}
	if !strings.Contains(get(cpath), "/admin/bookings/"+itoa(nid)) {
		t.Fatal("customer page should list the new booking")
	}
	// Unknown service slug is dropped, not rejected.
	loc = post(cpath+"/bookings", url.Values{"service": {"nope"}, "issue": {"x"}})
	if nb, _ = db.GetBooking(lastSeg(strings.SplitN(loc, "?", 2)[0])); nb == nil || nb.ServiceSlug != "" {
		t.Fatalf("bad slug should be blanked: %+v", nb)
	}
	// Cancel the follow-up with an email (nil mailer).
	post("/admin/bookings/"+itoa(fid)+"/status", url.Values{"status": {"cancelled"}, "notify": {"1"}, "reason": {"customer away"}})
	if fb, _ = db.GetBooking(fid); fb.Status != db.BookingCancelled {
		t.Fatal("cancel")
	}
	// 8. Phone booking: /admin/bookings/new takes a caller's details directly.
	if p := get("/admin/bookings/new"); !strings.Contains(p, "Take a booking over the phone") {
		t.Fatalf("new-booking form missing content: %s", p)
	}
	// No name and no contact details → form re-renders with errors.
	if rr := do("POST", "/admin/bookings/new", url.Values{"csrf": {"csrf1"}, "issue": {"?"}}, true); rr.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rr.Body.String(), "caller&#39;s name") {
		t.Fatalf("empty phone booking should 422: %d %s", rr.Code, rr.Body.String())
	}
	// A hand-typed address (no picked addr_* fields) is rejected.
	if rr := do("POST", "/admin/bookings/new", url.Values{"csrf": {"csrf1"},
		"name": {"Rex Kramer"}, "phone": {"0400 000 099"}, "address": {"9 Typed St, Ringwood"},
	}, true); rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "pick the address") {
		t.Fatalf("hand-typed address should 422: %d", rr.Code)
	}
	// A brand-new caller with a picked address creates a new customer with the
	// address filled in; the booking carries the address and its suburb.
	loc = post("/admin/bookings/new", url.Values{
		"name": {"Rex Kramer"}, "phone": {"0400 000 099"},
		"address": {"9 Sample St, Ringwood VIC 3134"}, "addr_street": {"9 Sample St"},
		"addr_suburb": {"Ringwood"}, "addr_state": {"VIC"}, "addr_postcode": {"3134"},
		"service": {"email-outlook"}, "issue": {"NBN dropouts every evening"},
	})
	pid := lastSeg(strings.SplitN(loc, "?", 2)[0])
	pb, _ := db.GetBooking(pid)
	if pb == nil || pb.CustomerID == 0 || pb.CustomerID == b.CustomerID || pb.Status != db.BookingNew ||
		pb.Mode != "onsite" || pb.ServiceSlug != "email-outlook" || pb.Suburb != "Ringwood" ||
		pb.Address != "9 Sample St, Ringwood VIC 3134" || !pb.StartAt.IsZero() {
		t.Fatalf("phone booking: %+v", pb)
	}
	if pc, _ := db.GetCustomer(pb.CustomerID); pc.Address != "9 Sample St, Ringwood VIC 3134" || pc.Suburb != "Ringwood" {
		t.Fatalf("phone booking customer address: %+v", pc)
	}
	// A repeat caller (same phone digits) is linked to the existing customer;
	// no address given is fine, a blank issue gets a placeholder, and the
	// saved customer address is not overwritten.
	loc = post("/admin/bookings/new", url.Values{"name": {"Zoë O'Brien"}, "phone": {"0400 000 001"}, "suburb": {"Donvale"}})
	pb, _ = db.GetBooking(lastSeg(strings.SplitN(loc, "?", 2)[0]))
	if pb == nil || pb.CustomerID != b.CustomerID || pb.Issue != "Phone enquiry" || pb.Address != "" {
		t.Fatalf("repeat caller not linked: %+v", pb)
	}
	if pc, _ := db.GetCustomer(b.CustomerID); pc.Address != "1 Test St" {
		t.Fatalf("repeat caller should not change address: %+v", pc)
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
