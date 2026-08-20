package main

import (
	"encoding/xml"
	"fmt"
	"html"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// ── robots.txt ──

// robotsDisallow lists paths crawlers should skip: nothing here is content, and
// several are token/secret or thank-you pages that are noindex anyway.
var robotsDisallow = []string{
	"/admin", "/api/", "/auth/", "/invoice/", "/quote/verify", "/quote/sent", "/book/thanks",
}

func handleRobots(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for _, p := range robotsDisallow {
		b.WriteString("Disallow: " + p + "\n")
	}
	b.WriteString("\nSitemap: " + site.BaseURL + "/sitemap.xml\n")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(b.String()))
}

// ── sitemap.xml ──

// sitemapURLs is the single source of truth for every indexable public path.
// Everything here must return 200 (never a redirect) — TestSitemap enforces it.
func sitemapURLs() []string {
	paths := []string{"/", "/services", "/software-development", "/fix-it-yourself", "/areas", "/book", "/quote", "/portfolio"}
	for i := range services {
		p := services[i].URL()
		if p == "/software-development" {
			continue // already listed as a static page
		}
		paths = append(paths, p)
	}
	for i := range guides {
		paths = append(paths, "/fix-it-yourself/"+guides[i].Slug)
	}
	for i := range suburbList {
		paths = append(paths, suburbList[i].URL())
	}
	return paths
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func handleSitemap(w http.ResponseWriter, r *http.Request) {
	set := sitemapSet{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, p := range sitemapURLs() {
		set.URLs = append(set.URLs, sitemapURL{Loc: site.BaseURL + p})
	}
	out, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		log.Printf("sitemap: %v", err)
		http.Error(w, "sitemap error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(xml.Header))
	w.Write(out)
	w.Write([]byte("\n"))
}

// ── canonical host ──

// canonicalHost 301s any request whose Host doesn't match BASE_URL's host (e.g.
// www.localithelp.com.au → localithelp.com.au) so there is exactly one indexable origin.
// Local/loopback hosts and requests with no Host are passed through untouched so
// dev and tests are unaffected. Caddy forwards the original Host header.
func canonicalHost(next http.Handler) http.Handler {
	want := ""
	if u, err := url.Parse(site.BaseURL); err == nil {
		want = u.Host
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want != "" && r.Host != "" && !strings.EqualFold(r.Host, want) && !isLocalHost(r.Host) {
			http.Redirect(w, r, site.BaseURL+r.URL.RequestURI(), http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified())
}

// ── 404 ──

// handleNotFound is the catch-all: trailing slashes redirect to the bare path
// (so /services/ → /services) and everything else gets the branded 404 page.
func handleNotFound(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Path; len(p) > 1 && strings.HasSuffix(p, "/") {
		u := *r.URL
		u.Path = strings.TrimRight(p, "/")
		http.Redirect(w, r, u.RequestURI(), http.StatusMovedPermanently)
		return
	}
	notFound(w, r)
}

// notFound renders the branded 404 page with the correct status. Use this in
// place of http.NotFound for public routes.
func notFound(w http.ResponseWriter, r *http.Request) {
	tmpl, ok := pages["404"]
	if !ok {
		http.NotFound(w, r)
		return
	}
	pd := pageData{Site: site, Path: r.URL.Path}
	if sess := getSession(r); sess != nil {
		pd.User, pd.CSRF = sess.User, sess.CSRF
		pd.IsAdmin = isAdmin(sess.User)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := tmpl.ExecuteTemplate(w, "base", pd); err != nil {
		log.Printf("render 404: %v", err)
	}
}

// ── favicon ──

// handleFavicon serves /favicon.ico from static/img so the root request stops 404ing.
func handleFavicon(dir string) http.HandlerFunc {
	path := filepath.Join(dir, "static", "img", "favicon.ico")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		http.ServeFile(w, r, path)
	}
}

// ── template helpers for structured data ──

// crumb is one BreadcrumbList item.
type crumb struct {
	Name string
	URL  string
}

// breadcrumbTrail is what the "ld-breadcrumbs" partial renders.
type breadcrumbTrail struct {
	BaseURL string
	Items   []crumb
}

// crumbs builds a breadcrumb trail from alternating name/path arguments, e.g.
// (crumbs "Home" "/" "Services" "/services" .PageData.S.Title .PageData.S.URL).
func crumbs(pairs ...string) breadcrumbTrail {
	t := breadcrumbTrail{BaseURL: site.BaseURL}
	for i := 0; i+1 < len(pairs); i += 2 {
		t.Items = append(t.Items, crumb{Name: pairs[i], URL: pairs[i+1]})
	}
	return t
}

// stripTags removes HTML tags from authored guide steps for plain-text JSON-LD.
func stripTags(s any) string {
	var str string
	switch v := s.(type) {
	case template.HTML:
		str = string(v)
	case string:
		str = v
	default:
		str = fmt.Sprint(v)
	}
	var b strings.Builder
	in := false
	for _, c := range str {
		switch {
		case c == '<':
			in = true
		case c == '>':
			in = false
		case !in:
			b.WriteRune(c)
		}
	}
	return html.UnescapeString(strings.Join(strings.Fields(b.String()), " "))
}
