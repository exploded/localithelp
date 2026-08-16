package main

import (
	"strings"
	"testing"

	"mchugh.com.au/db"
)

func TestMailTemplatesRender(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test", Phone: "0400 000 000"}
	b := &db.Booking{Name: "Ann <b>Bold</b>", Phone: "0400 111 222", Email: "ann@example.test",
		Suburb: "Donvale", ServiceSlug: "email-outlook", Issue: "Outlook won't open\nsecond line", PreferredTime: "Tomorrow arvo", IP: "1.2.3.4"}
	for _, name := range []string{"booking-admin", "booking-customer"} {
		out, err := renderMail(name, map[string]any{"ID": int64(7), "B": b, "ServiceTitle": "Email & Outlook", "Suspicious": true})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(out, "<b>Bold</b>") {
			t.Errorf("%s: user input not escaped", name)
		}
		if !strings.Contains(out, "https://example.test") || !strings.Contains(out, "Ann") {
			t.Errorf("%s: missing expected content:\n%s", name, out)
		}
	}
	q := &db.Quote{ID: 3, Name: "Bob", Email: "bob@example.test", TotalCost: 199.5, Description: "An app"}
	if _, err := renderMail("quote-paid", map[string]any{"Q": q}); err != nil {
		t.Fatalf("quote-paid: %v", err)
	}
	// nil mailer is a no-op everywhere
	mail = nil
	notifyBooking(1, b, false)
	notifyQuotePaid(q)
}
