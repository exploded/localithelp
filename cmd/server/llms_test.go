package main

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestLlmsTxt(t *testing.T) {
	mux := seoTestSetup(t)
	rr := get(mux, "/llms.txt")
	if rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("llms.txt: %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	body := rr.Body.String()
	if !strings.HasPrefix(body, "# Local IT Help\n") {
		t.Errorf("llms.txt must start with the H1:\n%.100s", body)
	}
	if !strings.Contains(body, "\n> ") && !strings.HasPrefix(body, "> ") {
		t.Error("llms.txt missing the blockquote summary")
	}
	// Pricing facts, service area and agent guidance must all be present.
	for _, want := range []string{
		"$80", "$30", "$200", "20%", softwareHourly,
		"Donvale", "me@example.test",
		"no booking API", "issue, name, phone, email, preferred_time",
		"Do NOT include an address",
		"https://example.test/book?service=",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("llms.txt missing %q", want)
		}
	}
	// Every service and every software package must be listed.
	for i := range services {
		if !strings.Contains(body, services[i].Title) {
			t.Errorf("llms.txt missing service %q", services[i].Title)
		}
		if !strings.Contains(body, services[i].Slug) {
			t.Errorf("llms.txt missing slug %q", services[i].Slug)
		}
	}
	for i := range softwarePackages {
		if !strings.Contains(body, softwarePackages[i].Name) {
			t.Errorf("llms.txt missing package %q", softwarePackages[i].Name)
		}
	}
	// Every internal link must resolve to a real 200 page (drift-proofing,
	// mirrors TestSitemap). Query strings are stripped before fetching.
	re := regexp.MustCompile(`https://example\.test(/[a-zA-Z0-9\-/]*)`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		path := m[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		if r := get(mux, path); r.Code != 200 {
			t.Errorf("llms.txt links to %s → %d", path, r.Code)
		}
	}
	if len(seen) < 15 {
		t.Errorf("expected many internal links, got %d", len(seen))
	}
}

func TestAPIPricing(t *testing.T) {
	mux := seoTestSetup(t)
	rr := get(mux, "/api/pricing")
	if rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("api/pricing: %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("api/pricing missing CORS header")
	}
	var p pricingResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatalf("api/pricing not valid JSON: %v", err)
	}
	if p.Currency != "AUD" || p.GSTIncluded {
		t.Errorf("currency/gst wrong: %+v", p)
	}
	if p.OnsiteFee != 80 || p.BlockRatePer15 != 30 || p.OneHourTotal != 200 || p.SeniorsPct != 20 {
		t.Errorf("pricing numbers wrong: %+v", p)
	}
	if !p.NoFixNoFee || !p.TravelIncluded {
		t.Errorf("guarantees wrong: %+v", p)
	}
	if len(p.Services) != len(services) {
		t.Errorf("services: got %d, want %d", len(p.Services), len(services))
	}
	if len(p.SoftwarePackages) != len(softwarePackages) {
		t.Errorf("packages: got %d, want %d", len(p.SoftwarePackages), len(softwarePackages))
	}
	if len(p.ServiceArea) != len(suburbs) {
		t.Errorf("service area: got %d, want %d", len(p.ServiceArea), len(suburbs))
	}
	if p.BookURL != "https://example.test/book" {
		t.Errorf("book_url = %q", p.BookURL)
	}
	for _, s := range p.Services {
		if !strings.HasPrefix(s.URL, "https://example.test/") {
			t.Errorf("service %s URL not absolute: %q", s.Slug, s.URL)
		}
	}
}

func TestBookPrefill(t *testing.T) {
	mux := seoTestSetup(t)

	// Valid params are echoed into the form.
	body := get(mux, "/book?service=email-outlook&name=Jane&phone=0400000000&email=jane%40example.com&issue=Printer+jam+since+update&preferred_time=Tue+am").Body.String()
	for _, want := range []string{
		`value="Jane"`, `value="0400000000"`, `value="jane@example.com"`, `value="Tue am"`,
		`value="email-outlook" selected`,
		`>Printer jam since update</textarea>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("prefilled form missing %s", want)
		}
	}

	// Unknown service slug is blanked — only the placeholder option is selected.
	body = get(mux, "/book?service=not-a-real-slug").Body.String()
	if !strings.Contains(body, `<option value="" selected>`) || strings.Contains(body, `"not-a-real-slug"`) {
		t.Error("unknown service slug should fall back to the empty option")
	}

	// Address params must be ignored: the address gate depends on autocomplete.
	body = get(mux, "/book?address=1+Evil+St&addr_street=1+Evil+St&addr_suburb=Donvale&addr_state=VIC&addr_postcode=3111").Body.String()
	if strings.Contains(body, "Evil") {
		t.Error("address query params must never be echoed")
	}
	for _, want := range []string{
		`name="addr_street" value=""`, `name="addr_suburb" value=""`,
		`name="addr_state" value=""`, `name="addr_postcode" value=""`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hidden address field not empty: %s", want)
		}
	}

	// Over-length values are capped to the form's maxlength.
	long := strings.Repeat("a", 150)
	body = get(mux, "/book?name="+long).Body.String()
	if !strings.Contains(body, `value="`+strings.Repeat("a", 100)+`"`) || strings.Contains(body, strings.Repeat("a", 101)) {
		t.Error("name should be capped at 100 runes")
	}

	// Injected markup must render escaped, never as live HTML.
	probe := url.QueryEscape(`"><script>alert(1)</script>`)
	body = get(mux, "/book?name="+probe).Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("prefill value rendered unescaped")
	}
}
