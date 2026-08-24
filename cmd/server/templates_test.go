package main

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"localithelp/db"
)

// TestTemplatesRender executes every page template with representative data so a
// broken field reference fails in CI rather than as a 500 in production.
func TestTemplatesRender(t *testing.T) {
	var err error
	pages, err = loadTemplates("../../templates")
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	site = siteConfig{
		BaseURL: "https://example.test", Phone: "0400 000 000", PhoneHref: "tel:+61400000000",
		Email: "test@example.test", OnsiteFee: 80, BlockRate: 30, SeniorsPct: 20, Suburbs: suburbs,
		ReviewURL: "https://g.page/r/TEST/review",
	}

	svc := featuredService()
	if svc == nil {
		t.Fatal("no featured service")
	}
	email, _ := findService("email-outlook")
	booked := db.Booking{ID: 1, CustomerID: 9, Name: "B <b>x</b>", Phone: "1", Email: "b@x", Suburb: "Donvale", ServiceSlug: "email-outlook",
		Issue: "Broken", Status: "booked", StartAt: time.Date(2026, 8, 20, 9, 30, 0, 0, db.Melbourne), DurationMin: 60, CreatedAt: time.Now(), ParentBookingID: 1}
	cust := &db.Customer{ID: 9, Name: "Ann", Email: "ann@x", Phone: "0400 000 000", Suburb: "Donvale", CreatedAt: time.Now()}
	inv := sampleInvoiceView(db.InvoiceSent).Inv

	cases := map[string]any{
		"home": struct {
			Services []Service
			Featured *Service
		}{services, svc},
		"services": struct {
			Services []Service
			Featured *Service
		}{services, svc},
		"service": struct {
			S       *Service
			Related []*Service
			Guides  []*Guide
		}{email, relatedServices(email), guidesForService(email.Slug)},
		"software-development": struct {
			S        *Service
			Packages []Package
			Hourly   string
			Related  []*Service
		}{svc, softwarePackages, softwareHourly, relatedServices(svc)},
		"book":        bookPageData{Services: services, Form: bookForm{}, Errors: map[string]string{"name": "x", "contact": "y"}, TS: 1},
		"pricing":     pricingPageData{Packages: softwarePackages, Hourly: softwareHourly, HourTotal: 200, SeniorsHour: 160},
		"book-thanks": nil,
		"guides":      struct{ Groups []guideGroup }{groupGuides()},
		"guide": struct {
			G      *Guide
			Others []*Guide
		}{&guides[0], []*Guide{&guides[1]}},
		"portfolio": nil,
		"404":       nil,
		"areas": struct {
			Groups []suburbGroup
			Count  int
		}{groupSuburbs(), len(suburbList)},
		"area": areaPageData(&suburbList[0]),
		"quote": quotePageData{TurnstileSiteKey: "1x00000000000000000000AA", BaseCost: 2000, Groups: []db.OptionGroup{{
			Name: "feature_email", Label: "Email", Hint: "h",
			Options: []db.Option{{Value: "none", Name: "None", Cost: 0, CostLabel: "Free", IsDefault: true}},
		}}},
		"quote-sent":    map[string]any{"Email": "a@b.co"},
		"quote-result":  map[string]any{"AIEstimate": template.HTML("<p>x</p>"), "Name": "A"},
		"admin-options": adminOptionsData{GroupsJSON: "[]", BaseCost: 2000},
		"admin": adminDashData{
			Quotes: []db.Quote{{ID: 1, Status: "paid", CreatedAt: time.Now()}},
			Week:   bookingRows([]db.Booking{booked}), NewCount: 2, Outstanding: 12345},
		"admin-bookings": adminBookingsData{Flash: flash{OK: "saved"}, Status: "new", Statuses: db.BookingStatuses,
			Counts: map[string]int{"new": 2}, Rows: bookingRows([]db.Booking{booked, {ID: 2, Name: "Unscheduled", PreferredTime: "arvo", Status: "new"}})},
		"admin-booking": adminBookingData{Flash: flash{Err: "oops"}, B: &booked, Row: newBookingRow(booked), Cust: cust,
			StartValue: "2026-08-20T09:30", Durations: durationChoices, Invoices: []db.Invoice{*inv}, Parent: &booked,
			Children: bookingRows([]db.Booking{booked}), Statuses: db.BookingStatuses, CanInvoice: true, FollowupText: "Return"},
		"admin-booking-nocust": adminBookingData{B: &db.Booking{ID: 3, Name: "X", Status: "new"}, Row: newBookingRow(db.Booking{ID: 3}), Durations: durationChoices, StartValue: "2026-08-20T09:30"},
		"admin-booking-new": adminBookingNewData{Flash: flash{OK: "x"},
			Form:   phoneBookingForm{Name: "Ann", Address: "1 Example Ct, Donvale VIC 3111", AddrStreet: "1 Example Ct", AddrSuburb: "Donvale", AddrState: "VIC", AddrPostcode: "3111"},
			Errors: map[string]string{"contact": "y", "address": "z"}, Services: services},
		"admin-calendar": calendarData{WeekStart: time.Now(), WeekLabel: "w", Prev: "p", Next: "n", ThisWeek: "t", Hours: []int{7, 8},
			SlotPx: 14, ColPx: 672, ForID: 1, ForName: "Ann",
			Days: []calDay{{Date: time.Now(), Label: "Mon 17", IsToday: true, Slots: []calSlot{{Time: "07:00", OnHour: true, Href: "/x"}, {Time: "07:15"}},
				Events: []calEvent{{Row: newBookingRow(booked), Top: 10, Height: 56, Href: "/y", Label: "9:30 am – 10:30 am"}}}}},
		"admin-invoices": adminInvoicesData{Status: "sent", Statuses: []string{"draft", "sent"}, Rows: []invoiceRow{{Invoice: *inv, Ref: "INV-1001", CustName: "Ann", Issued: "20 Aug 2026"}}, TotalOut: 500},
		"admin-invoice": func() adminInvoiceData {
			v := sampleInvoiceView(db.InvoiceDraft)
			return adminInvoiceData{V: v, Ref: "INV-1001", Total: "$229.50", TotalDollars: "229.50", Editable: true, CanSend: true, CanPay: true, CanVoid: true,
				Lines: []invoiceLine{{"Fee", "1", "80.00", "$80.00", "$80.00"}}, Blank: 2, DueValue: "2026-08-27", PaidValue: "2026-08-20", Methods: db.PaymentMethods,
				When: "Thu 20 Aug", BlockRate: "30", OnsiteFee: "80", SeniorsPct: "20", HasCustEmail: true, PublicURL: v.PublicURL, BankConfigured: true,
				ReviewOn: true}
		}(),
		"admin-invoice-sent": func() adminInvoiceData {
			v := sampleInvoiceView(db.InvoicePaid)
			return adminInvoiceData{V: v, Ref: "INV-1001", Total: "$229.50", TotalDollars: "229.50", CanResend: false,
				Lines: []invoiceLine{{"Fee", "1", "80.00", "$80.00", "$80.00"}}, Methods: db.PaymentMethods, PaymentMethod: "Zeller payment link", PublicURL: v.PublicURL,
				CanResendRcpt: true, HasCustEmail: true, ReviewOn: true, ReviewAsked: "24 Aug 2026"}
		}(),
		"admin-review-card": func() adminReviewCardData {
			qr, err := reviewQRDataURI(256)
			if err != nil {
				t.Fatalf("review qr: %v", err)
			}
			return adminReviewCardData{Configured: true, QR: qr, ShortURL: reviewShortURL(), Display: stripScheme(reviewShortURL()), Target: site.ReviewURL}
		}(),
		"admin-review-card-empty": adminReviewCardData{},
		"admin-customers":         adminCustomersData{Query: "ann", Rows: []db.Customer{*cust}},
		"admin-customer":          adminCustomerData{C: cust, Bookings: bookingRows([]db.Booking{booked}), Invoices: []invoiceRow{{Invoice: *inv, Ref: "INV-1001", CustName: "Ann", Issued: "20 Aug 2026"}}},
		"invoice-public": func() invoicePublicData {
			v := sampleInvoiceView(db.InvoiceSent)
			return invoicePublicData{V: v, Ref: "INV-1001", Total: "$229.50", Lines: []invoiceLine{{"Fee", "1", "$80.00", "$80.00", "$80.00"}}, Issued: "20 Aug 2026", Due: "27 Aug 2026", When: "Thu 20 Aug"}
		}(),
		"invoice-public-paid": func() invoicePublicData {
			v := sampleInvoiceView(db.InvoicePaid)
			return invoicePublicData{V: v, Ref: "INV-1001", Total: "$229.50", Paid: "20 Aug 2026", Method: "Cash", Paidish: true}
		}(),
	}
	// Variants (name suffix after the page) reuse the same template with different data.
	pageOf := func(name string) string {
		for _, p := range []string{"admin-booking-nocust", "admin-invoice-sent", "invoice-public-paid", "admin-review-card-empty"} {
			if name == p {
				return name[:strings.LastIndex(name, "-")]
			}
		}
		return name
	}
	for page := range pages {
		if _, ok := cases[page]; !ok {
			t.Errorf("page %q has no render test case", page)
		}
	}
	for name, data := range cases {
		name := pageOf(name)
		tmpl, ok := pages[name]
		if !ok {
			t.Errorf("page %q not loaded", name)
			continue
		}
		var buf bytes.Buffer
		pd := pageData{Site: site, Path: "/" + name, PageData: data, User: &db.User{Name: "U", Email: "u@x"}, IsAdmin: true, CSRF: "csrf-token"}
		if err := tmpl.ExecuteTemplate(&buf, "base", pd); err != nil {
			t.Errorf("render %q: %v", name, err)
			continue
		}
		if !strings.Contains(buf.String(), "</html>") {
			t.Errorf("render %q: incomplete output", name)
		}
	}
}

func TestTelHref(t *testing.T) {
	for in, want := range map[string]string{
		"":                "",
		"0400 000 000":    "tel:+61400000000",
		"+61 400 000 000": "tel:+61400000000",
		"03 9000 0000":    "tel:+61390000000",
	} {
		if got := telHref(in); got != want {
			t.Errorf("telHref(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServiceCatalogue(t *testing.T) {
	seen := map[string]bool{}
	for i := range services {
		s := &services[i]
		if seen[s.Slug] {
			t.Errorf("duplicate slug %q", s.Slug)
		}
		seen[s.Slug] = true
		if s.Title == "" || s.Short == "" || s.Intro == "" || len(s.Problems) == 0 || s.MetaTitle == "" || s.MetaDesc == "" {
			t.Errorf("service %q missing required copy", s.Slug)
		}
		for _, r := range s.Related {
			if _, ok := findService(r); !ok {
				t.Errorf("service %q relates to unknown slug %q", s.Slug, r)
			}
		}
	}
}

func TestGuides(t *testing.T) {
	seen := map[string]bool{}
	for i := range guides {
		g := &guides[i]
		if seen[g.Slug] {
			t.Errorf("duplicate guide slug %q", g.Slug)
		}
		seen[g.Slug] = true
		if g.Title == "" || g.Summary == "" || len(g.Steps) == 0 || g.StopWhen == "" || g.MetaDesc == "" || g.Level == "" || g.Time == "" {
			t.Errorf("guide %q missing required copy", g.Slug)
		}
		if _, ok := findService(g.Service); !ok {
			t.Errorf("guide %q relates to unknown service %q", g.Slug, g.Service)
		}
	}
}
