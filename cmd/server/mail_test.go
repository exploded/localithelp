package main

import (
	"html/template"
	"strings"
	"testing"

	"localithelp/db"
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
	q := &db.Quote{ID: 3, Name: "Bob <i>x</i>", Email: "bob@example.test", TotalCost: 199.5, Description: "An app",
		AIEstimate: "<p><strong>$2,000–$3,000</strong></p>", VerifyToken: "tok"}
	link := "https://example.test/quote/verify?t=tok"
	data := map[string]any{"Q": q, "Link": link, "Estimate": template.HTML(q.AIEstimate)}
	for _, name := range []string{"quote-verify", "quote-ready-admin", "quote-ready-customer"} {
		out, err := renderMail(name, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(out, "<i>x</i>") {
			t.Errorf("%s: user input not escaped", name)
		}
		if !strings.Contains(out, link) {
			t.Errorf("%s: missing link", name)
		}
		if name != "quote-verify" && !strings.Contains(out, "<strong>$2,000") {
			t.Errorf("%s: estimate HTML should be rendered, not escaped:\n%s", name, out)
		}
	}
	// nil mailer is a no-op everywhere
	mail = nil
	notifyBooking(1, b, false)
	sendQuoteVerification(q, link)
	notifyQuoteVerified(q, link)
}

func TestValidEmail(t *testing.T) {
	for in, want := range map[string]bool{
		"":                       false,
		"bob":                    false,
		"bob@example.test":       true,
		"Bob <bob@example.test>": false, // display-name form is not a plain address
		"a@b":                    true,
		"x@example.test\n":       false,
	} {
		if got := validEmail(in); got != want {
			t.Errorf("validEmail(%q) = %v, want %v", in, got, want)
		}
	}
}
