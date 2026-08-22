package main

import (
	"bytes"
	"strings"
	"testing"
)

func testSite() siteConfig {
	return siteConfig{
		BaseURL: "https://example.test", Email: "test@example.test",
		OnsiteFee: 80, BlockRate: 30, Suburbs: suburbs,
		Analytics: true, TagID: "G-TEST12345", GA4ID: "G-TEST12345",
		AdsID: "AW-123456789", AdsBookLabel: "bookLabel", AdsCallLabel: "callLabel",
	}
}

func renderPage(t *testing.T, page string, data any, admin bool) string {
	t.Helper()
	var buf bytes.Buffer
	pd := pageData{Site: site, Path: "/" + page, PageData: data, IsAdmin: admin}
	if err := pages[page].ExecuteTemplate(&buf, "base", pd); err != nil {
		t.Fatalf("render %s: %v", page, err)
	}
	return buf.String()
}

// TestGoogleTag covers the three states that matter: tag configured, admin
// signed in (never tagged, so my own visits don't count as traffic or
// conversions), and no IDs set at all.
func TestGoogleTag(t *testing.T) {
	var err error
	if pages, err = loadTemplates("../../templates"); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	saved := site
	defer func() { site = saved }()

	site = testSite()
	out := renderPage(t, "book-thanks", nil, false)
	for _, want := range []string{
		"googletagmanager.com/gtag/js?id=G-TEST12345",
		"gtag('config', 'G-TEST12345')",
		"gtag('config', 'AW-123456789')",
		"phone_call_click",
		"send_to: 'AW-123456789/callLabel'", // tel: click conversion
		"gtag('event', 'booking_request')",
		"send_to: 'AW-123456789/bookLabel'", // booking conversion
	} {
		if !strings.Contains(out, want) {
			t.Errorf("configured page missing %q", want)
		}
	}

	admin := renderPage(t, "book-thanks", nil, true)
	for _, unwanted := range []string{"googletagmanager", "send_to", "booking_request"} {
		if strings.Contains(admin, unwanted) {
			t.Errorf("admin page should not contain %q", unwanted)
		}
	}

	site = siteConfig{BaseURL: "https://example.test", Email: "e@x.test", Suburbs: suburbs}
	if off := renderPage(t, "book-thanks", nil, false); strings.Contains(off, "googletagmanager") {
		t.Error("unconfigured site should render no Google tag")
	}
}

// TestSameAsJSONLD checks the profile URLs reach the LocalBusiness block, which
// is how Google links the site to the Business Profile.
func TestSameAsJSONLD(t *testing.T) {
	var err error
	if pages, err = loadTemplates("../../templates"); err != nil {
		t.Fatalf("load templates: %v", err)
	}
	saved := site
	defer func() { site = saved }()

	home := struct {
		Services []Service
		Featured *Service
	}{services, featuredService()}

	site = testSite()
	if out := renderPage(t, "home", home, false); strings.Contains(out, `"sameAs"`) {
		t.Error("empty SameAs should emit no sameAs key")
	}

	site.SameAs = []string{"https://maps.example/place/one", "https://profile.example/two"}
	out := renderPage(t, "home", home, false)
	if !strings.Contains(out, `"sameAs"`) {
		t.Fatal("sameAs key missing")
	}
	for _, want := range []string{"maps.example", "profile.example"} {
		if !strings.Contains(out, want) {
			t.Errorf("sameAs missing %q", want)
		}
	}
}

func TestTagToken(t *testing.T) {
	for in, want := range map[string]string{
		"":              "",
		"  abc_1-Z  ":   "abc_1-Z",
		"G-ABC123":      "G-ABC123",
		"bad value":     "", // space
		"a';alert(1)//": "", // would break out of the tag's JS string
		"lab/el":        "", // send_to separator
	} {
		if got := tagToken(in); got != want {
			t.Errorf("tagToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTagID(t *testing.T) {
	cases := []struct{ in, prefix, want string }{
		{"G-ABC123", "G-", "G-ABC123"},
		{"AW-123456789", "AW-", "AW-123456789"},
		{"AW-123456789", "G-", ""}, // pasted into the wrong variable
		{"UA-12345-1", "G-", ""},   // Universal Analytics, long dead
		{"", "G-", ""},
	}
	for _, c := range cases {
		if got := tagID(c.in, c.prefix); got != c.want {
			t.Errorf("tagID(%q, %q) = %q, want %q", c.in, c.prefix, got, c.want)
		}
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" a , ,b,, c ")
	want := []string{"a", "b", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("splitList = %v, want %v", got, want)
	}
	if splitList("") != nil {
		t.Error("splitList(\"\") should be nil")
	}
}
