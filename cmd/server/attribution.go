package main

import (
	"net/http"
	"net/url"
	"strings"

	"localithelp/db"
)

// Attribution answers "where did this booking come from?" on the booking
// record itself, so a job can be traced back to an ad, a search or a card
// without leaving the admin.
//
// Google appends ?gclid=… to every ad click while auto-tagging is on. The ad
// rarely lands on /book, though, so the marker is parked in a cookie that
// survives the walk across the site and is read back when the form is posted.
// Last non-direct touch wins: a later visit with no marker never overwrites an
// earlier one, so a browse-now-book-later visitor still credits the ad.

const (
	srcCookie    = "lih_src"
	srcCookieAge = 30 * 24 * 60 * 60 // 30 days, in seconds
)

// trackSource records an attribution marker when one arrives on a page view.
// It only ever writes a cookie; nothing else about the request changes.
func trackSource(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if s := detectSource(r); s != "" {
				http.SetCookie(w, &http.Cookie{
					Name:     srcCookie,
					Value:    s,
					Path:     "/",
					MaxAge:   srcCookieAge,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
					Secure:   strings.HasPrefix(site.BaseURL, "https://"),
				})
			}
		}
		next.ServeHTTP(w, r)
	})
}

// detectSource reads the marker out of a page view, or returns "" when the
// visit carries no signal worth recording.
func detectSource(r *http.Request) string {
	qs := r.URL.Query()

	// Paid click. gclid is Google's auto-tagging parameter; gad_source turns up
	// on newer campaign types. Either one means the visitor came from an ad.
	if qs.Get("gclid") != "" || qs.Get("gad_source") != "" {
		return db.SourceGoogleAds
	}
	if strings.EqualFold(qs.Get("utm_medium"), "cpc") {
		return db.SourceGoogleAds
	}

	// Hand-tagged links: the QR card, an email footer, a directory listing.
	if u := sanitiseToken(qs.Get("utm_source")); u != "" {
		return u
	}

	// Otherwise fall back to who sent them.
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	if self, err := url.Parse(site.BaseURL); err == nil &&
		strings.EqualFold(host, strings.TrimPrefix(self.Host, "www.")) {
		return "" // internal navigation, not a new touch
	}
	switch {
	case host == "google.com" || strings.HasPrefix(host, "google."):
		return db.SourceGoogleSearch
	case host == "maps.google.com" || host == "business.google.com":
		return db.SourceGoogleProfile
	}
	return sanitiseToken(host)
}

// sanitiseToken keeps a value safe to store and print: lower case, no more
// than 40 characters, and nothing but letters, digits, dots and hyphens.
func sanitiseToken(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) > 40 {
		v = v[:40]
	}
	var b strings.Builder
	for _, c := range v {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '-':
			b.WriteRune(c)
		}
	}
	return b.String()
}

// bookingSource returns the source to store against a booking submitted by
// this request, defaulting to a direct visit when nothing was ever marked.
func bookingSource(r *http.Request) string {
	if c, err := r.Cookie(srcCookie); err == nil {
		if v := sanitiseToken(c.Value); v != "" {
			return v
		}
	}
	return db.SourceDirect
}
