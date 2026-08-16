package main

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"mchugh.com.au/db"
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
		Email: "test@example.test", OnsiteFee: 80, BlockRate: 30, Suburbs: suburbs,
	}

	svc := featuredService()
	if svc == nil {
		t.Fatal("no featured service")
	}
	email, _ := findService("email-outlook")

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
		}{email, relatedServices(email)},
		"software-development": struct {
			S        *Service
			Packages []Package
			Hourly   string
			Related  []*Service
		}{svc, softwarePackages, softwareHourly, relatedServices(svc)},
		"book":        bookPageData{Services: services, Form: bookForm{}, Errors: map[string]string{"name": "x", "contact": "y"}, TS: 1},
		"book-thanks": nil,
		"guides":      struct{ Groups []guideGroup }{groupGuides()},
		"guide": struct {
			G      *Guide
			Others []*Guide
		}{&guides[0], []*Guide{&guides[1]}},
		"portfolio":     nil,
		"quote-success": map[string]any{"AIEstimate": template.HTML("<p>x</p>"), "Name": "A"},
		"my-quotes":     []db.Quote{{ID: 1, Name: "A", CreatedAt: time.Now()}},
		"admin": struct {
			GroupsJSON template.JS
			BaseCost   int
			Quotes     []db.Quote
			Bookings   []db.Booking
		}{"[]", 2000, []db.Quote{{ID: 1, Status: "paid", CreatedAt: time.Now()}}, []db.Booking{{ID: 1, Name: "B", Phone: "1", Email: "b@x", Status: "new", CreatedAt: time.Now()}}},
	}
	for name, data := range cases {
		tmpl, ok := pages[name]
		if !ok {
			t.Errorf("page %q not loaded", name)
			continue
		}
		var buf bytes.Buffer
		pd := pageData{Site: site, Path: "/" + name, PageData: data, User: &db.User{Name: "U", Email: "u@x"}, IsAdmin: true}
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
