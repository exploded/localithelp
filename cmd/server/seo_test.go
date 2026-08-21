package main

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mchugh.com.au/db"
)

// seoTestSetup opens a temp DB, loads templates, sets site config and returns the real router.
func seoTestSetup(t *testing.T) *http.ServeMux {
	t.Helper()
	if err := db.Open(filepath.Join(t.TempDir(), "seo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var err error
	pages, err = loadTemplates("../../templates")
	if err != nil {
		t.Fatal(err)
	}
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test", OnsiteFee: 80, BlockRate: 30, SeniorsPct: 20, Suburbs: suburbs, Areas: suburbList, IndexNowKey: "testindexnowkey0123456789abcdef0"}
	return newMux("../..")
}

func get(mux http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Host = "example.test"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestRobots(t *testing.T) {
	mux := seoTestSetup(t)
	rr := get(mux, "/robots.txt")
	if rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("robots: %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	body := rr.Body.String()
	for _, want := range []string{
		"User-agent: *", "Disallow: /admin", "Disallow: /invoice/", "Sitemap: https://example.test/sitemap.xml",
		"User-agent: GPTBot", "User-agent: OAI-SearchBot", "User-agent: ChatGPT-User",
		"User-agent: ClaudeBot", "User-agent: Claude-User", "User-agent: Claude-SearchBot",
		"User-agent: PerplexityBot", "User-agent: Google-Extended",
		"Allow: /api/pricing", "https://example.test/llms.txt",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("robots.txt missing %q:\n%s", want, body)
		}
	}
}

func TestSitemap(t *testing.T) {
	mux := seoTestSetup(t)
	rr := get(mux, "/sitemap.xml")
	if rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("sitemap: %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	var set sitemapSet
	if err := xml.Unmarshal(rr.Body.Bytes(), &set); err != nil {
		t.Fatalf("sitemap not well-formed: %v\n%s", err, rr.Body.String())
	}
	if len(set.URLs) < 80 {
		t.Errorf("expected ~86 URLs, got %d", len(set.URLs))
	}
	seen := map[string]bool{}
	for _, u := range set.URLs {
		if !strings.HasPrefix(u.Loc, "https://example.test/") {
			t.Errorf("loc %q not absolute on BaseURL", u.Loc)
		}
		if seen[u.Loc] {
			t.Errorf("duplicate loc %q", u.Loc)
		}
		seen[u.Loc] = true
		path := strings.TrimPrefix(u.Loc, "https://example.test")
		for _, bad := range []string{"/admin", "/api/", "/auth/", "/invoice/", "/quote/verify", "/quote/sent", "/book/thanks"} {
			if strings.HasPrefix(path, bad) {
				t.Errorf("sitemap must not list %q", path)
			}
		}
		if strings.HasSuffix(path, "/") && path != "/" {
			t.Errorf("trailing slash in sitemap: %q", path)
		}
		// Every listed URL must be a real 200 page — never a redirect or 404.
		if r := get(mux, path); r.Code != 200 {
			t.Errorf("GET %s → %d (sitemap entries must be 200)", path, r.Code)
		}
	}
	for _, must := range []string{"/", "/services", "/software-development", "/pricing", "/fix-it-yourself", "/areas", "/areas/donvale", "/services/email-outlook", "/book"} {
		if !seen["https://example.test"+must] {
			t.Errorf("sitemap missing %s", must)
		}
	}
	if seen["https://example.test/services/software-development"] {
		t.Error("sitemap lists /services/software-development, which 301s — use Service.URL()")
	}
}

func TestNotFoundAndTrailingSlash(t *testing.T) {
	mux := seoTestSetup(t)
	rr := get(mux, "/nope")
	if rr.Code != 404 {
		t.Errorf("/nope → %d, want 404", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "</html>") || !strings.Contains(rr.Body.String(), "/fix-it-yourself") {
		t.Errorf("404 page not branded:\n%.300s", rr.Body.String())
	}
	for _, c := range []struct{ path, want string }{
		{"/services/", "/services"},
		{"/fix-it-yourself/", "/fix-it-yourself"},
		{"/areas/?x=1", "/areas?x=1"},
	} {
		rr := get(mux, c.path)
		if rr.Code != 301 || rr.Header().Get("Location") != c.want {
			t.Errorf("%s → %d %s, want 301 %s", c.path, rr.Code, rr.Header().Get("Location"), c.want)
		}
	}
	// Unknown service/guide/suburb slugs use the branded page too.
	for _, p := range []string{"/services/nope", "/fix-it-yourself/nope", "/areas/nope"} {
		if rr := get(mux, p); rr.Code != 404 || !strings.Contains(rr.Body.String(), "</html>") {
			t.Errorf("%s → %d, want branded 404", p, rr.Code)
		}
	}
	if rr := get(mux, "/favicon.ico"); rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "image/") {
		t.Errorf("/favicon.ico → %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
}

func TestCanonicalHost(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := canonicalHost(inner)
	cases := []struct {
		host string
		code int
		loc  string
	}{
		{"example.test", 204, ""},
		{"EXAMPLE.test", 204, ""},
		{"www.example.test", 301, "https://example.test/p?q=1"},
		{"example.test:443", 301, "https://example.test/p?q=1"},
		{"localhost:8181", 204, ""},
		{"127.0.0.1:8181", 204, ""},
		{"192.168.1.20:8181", 204, ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/p?q=1", nil)
		req.Host = c.host
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != c.code || rr.Header().Get("Location") != c.loc {
			t.Errorf("host %s → %d %q, want %d %q", c.host, rr.Code, rr.Header().Get("Location"), c.code, c.loc)
		}
	}
}

func TestSuburbs(t *testing.T) {
	known := map[string]bool{}
	for _, l := range lgaOrder {
		known[l] = true
	}
	slugs, blurbs, descs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	pc := regexp.MustCompile(`^3\d{3}$`)
	slugRe := regexp.MustCompile(`^[a-z0-9-]+$`)
	for i := range suburbList {
		s := &suburbList[i]
		if slugs[s.Slug] || !slugRe.MatchString(s.Slug) {
			t.Errorf("bad/duplicate slug %q", s.Slug)
		}
		slugs[s.Slug] = true
		if !pc.MatchString(s.Postcode) {
			t.Errorf("%s: bad postcode %q", s.Name, s.Postcode)
		}
		if !known[s.LGA] {
			t.Errorf("%s: unknown LGA %q", s.Name, s.LGA)
		}
		if s.DriveMin <= 0 || s.DriveMin > 40 {
			t.Errorf("%s: odd DriveMin %d", s.Name, s.DriveMin)
		}
		if len(s.Blurb) < 150 || blurbs[s.Blurb] {
			t.Errorf("%s: blurb short or duplicated", s.Name)
		}
		blurbs[s.Blurb] = true
		if len(s.MetaDesc) < 60 || len(s.MetaDesc) > 170 || descs[s.MetaDesc] || !strings.Contains(s.MetaDesc, s.Name) {
			t.Errorf("%s: meta description len=%d dup=%v hasName=%v", s.Name, len(s.MetaDesc), descs[s.MetaDesc], strings.Contains(s.MetaDesc, s.Name))
		}
		descs[s.MetaDesc] = true
		if got, ok := findSuburb(s.Slug); !ok || got != s {
			t.Errorf("findSuburb(%q) failed", s.Slug)
		}
		if got, ok := findSuburbByName(strings.ToUpper(s.Name)); !ok || got != s {
			t.Errorf("findSuburbByName(%q) failed", s.Name)
		}
		if n := s.Nearby(); len(n) == 0 || len(n) > 6 {
			t.Errorf("%s: nearby=%d", s.Name, len(n))
		}
		// A photo credit must point at a real file; the file itself is optional (own photos need no credit).
		_, statErr := os.Stat(filepath.Join("../../static/img/areas", s.Slug+".jpg"))
		if s.Photo != nil {
			if statErr != nil {
				t.Errorf("%s: has Photo credit but static/img/areas/%s.jpg is missing", s.Name, s.Slug)
			}
			if s.Photo.Artist == "" || s.Photo.Licence == "" || !strings.HasPrefix(s.Photo.Source, "https://") {
				t.Errorf("%s: incomplete photo credit %+v", s.Name, *s.Photo)
			}
			if strings.HasPrefix(s.Photo.Licence, "CC") && s.Photo.LicenceURL == "" {
				t.Errorf("%s: CC licence needs a LicenceURL", s.Name)
			}
		}
	}
	if len(suburbs) != len(suburbList) {
		t.Errorf("suburbs names slice out of sync: %d vs %d", len(suburbs), len(suburbList))
	}
	if len(groupSuburbs()) < 5 {
		t.Errorf("expected several LGA groups")
	}
}

// TestJSONLD renders the structured-data pages and checks every ld+json block parses.
func TestJSONLD(t *testing.T) {
	mux := seoTestSetup(t)
	re := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)
	pathsWant := map[string][]string{
		"/":                                  {"LocalBusiness", "WebSite"},
		"/services":                          {"BreadcrumbList"},
		"/services/email-outlook":            {"BreadcrumbList", "Service"},
		"/software-development":              {"BreadcrumbList", "Service"},
		"/pricing":                           {"BreadcrumbList", "Service", "FAQPage"},
		"/fix-it-yourself":                   {"BreadcrumbList"},
		"/fix-it-yourself/" + guides[0].Slug: {"BreadcrumbList", "Article"},
		"/areas":                             {"BreadcrumbList"},
		"/areas/donvale":                     {"BreadcrumbList", "Service"},
	}
	for path, wantTypes := range pathsWant {
		rr := get(mux, path)
		if rr.Code != 200 {
			t.Errorf("%s → %d", path, rr.Code)
			continue
		}
		body := rr.Body.String()
		blocks := re.FindAllStringSubmatch(body, -1)
		if len(blocks) == 0 {
			t.Errorf("%s: no JSON-LD", path)
			continue
		}
		joined := ""
		for _, b := range blocks {
			var v any
			if err := json.Unmarshal([]byte(b[1]), &v); err != nil {
				t.Errorf("%s: invalid JSON-LD: %v\n%s", path, err, b[1])
			}
			joined += b[1]
		}
		for _, ty := range wantTypes {
			if !strings.Contains(joined, `"@type":"`+ty+`"`) && !strings.Contains(joined, `"@type": "`+ty+`"`) {
				t.Errorf("%s: JSON-LD missing @type %s", path, ty)
			}
		}
		// Basic head hygiene on every indexable page.
		for _, want := range []string{"<title>", `name="description"`, `rel="canonical" href="https://example.test` + path + `"`, `property="og:image"`, `property="og:title"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: head missing %s", path, want)
			}
		}
		if strings.Contains(body, `name="robots" content="noindex"`) {
			t.Errorf("%s: indexable page is noindex", path)
		}
	}
	// Home page LocalBusiness: concrete priceRange and a booking action.
	// (Match slash-free fragments — html/template escapes "/" inside <script>.)
	home := get(mux, "/").Body.String()
	for _, want := range []string{"ReserveAction", "$80 flat visit fee plus $30 per 15 minutes (AUD)", `"Reservation"`} {
		if !strings.Contains(home, want) {
			t.Errorf("home JSON-LD missing %s", want)
		}
	}
	// Suburb photo: page with a file gets the figure + credit + its own og:image; page without falls back.
	body := get(mux, "/areas/donvale").Body.String()
	for _, want := range []string{`<figure class="area-photo">`, `src="/static/img/areas/donvale.jpg"`, `property="og:image" content="https://example.test/static/img/areas/donvale.jpg"`, `Wikimedia Commons`, `"image": "https:\/\/example.test\/static\/img\/areas\/donvale.jpg"`} { // html/template escapes "/" inside <script>
		if !strings.Contains(body, want) {
			t.Errorf("/areas/donvale missing %s", want)
		}
	}
	if rr := get(mux, "/static/img/areas/donvale.jpg"); rr.Code != 200 {
		t.Errorf("suburb photo not served: %d", rr.Code)
	}
	if noPhoto := get(mux, "/areas/vermont").Body.String(); strings.Contains(noPhoto, "area-photo") || !strings.Contains(noPhoto, `og:image" content="https://example.test/static/img/og.png"`) {
		t.Errorf("/areas/vermont should fall back to the site og:image and render no figure")
	}
	// Service links hard-coded on the area page must resolve.
	for _, m := range regexp.MustCompile(`href="(/services/[a-z0-9-]+)"`).FindAllStringSubmatch(body, -1) {
		if rr := get(mux, m[1]); rr.Code != 200 {
			t.Errorf("area page links to %s → %d", m[1], rr.Code)
		}
	}
}
