package main

import (
	"log"
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
	clickCookie  = "lih_click"
	srcCookieAge = 30 * 24 * 60 * 60 // 30 days, in seconds
	adFieldMax   = 100               // runes kept from any one ad parameter
)

// trackSource records an attribution marker when one arrives on a page view,
// and logs the detail behind a paid click. Nothing else about the request
// changes, and a failure to log is never allowed to reach the visitor.
func trackSource(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assets aren't landing pages: a marker on /static/ is noise, and
		// this middleware wraps the whole mux.
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/static/") {
			if s := detectSource(r); s != "" {
				setAttrCookie(w, srcCookie, s, srcCookieAge)

				// Both cookies move together. A later non-ad touch wins the
				// source, so it has to drop the click too - otherwise a
				// booking reads "via Card or flyer" with an ad keyword
				// hanging off it.
				if c := detectAdClick(r); c != nil {
					if tok, err := recordAdClick(readCookie(r, clickCookie), *c); err != nil {
						log.Printf("attribution: record ad click: %v", err)
					} else {
						setAttrCookie(w, clickCookie, tok, srcCookieAge)
					}
				} else if readCookie(r, clickCookie) != "" {
					setAttrCookie(w, clickCookie, "", -1)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// setAttrCookie writes one of the attribution cookies. A negative age expires
// it.
func setAttrCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   age,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(site.BaseURL, "https://"),
	})
}

func readCookie(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
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

// adClick is the detail behind one paid click, read off the landing page URL.
// Google appends gclid itself; the rest arrives from the campaign's final URL
// suffix, so every field can legitimately be empty.
type adClick struct {
	Source   string
	GCLID    string
	Keyword  string // the Ads keyword that matched, not what the visitor typed
	Campaign string
	Landing  string
}

// detectAdClick returns the ad detail carried by a page view, or nil when the
// visit wasn't a paid click. Pure, like detectSource: it reads the URL and
// nothing else.
func detectAdClick(r *http.Request) *adClick {
	qs := r.URL.Query()
	gclid := qs.Get("gclid")
	if gclid == "" && qs.Get("gad_source") == "" && !strings.EqualFold(qs.Get("utm_medium"), "cpc") {
		return nil
	}
	return &adClick{
		Source:   db.SourceGoogleAds,
		GCLID:    sanitiseText(gclid, adFieldMax),
		Keyword:  sanitiseText(qs.Get("utm_term"), adFieldMax),
		Campaign: sanitiseText(qs.Get("utm_campaign"), adFieldMax),
		Landing:  sanitiseText(r.URL.Path, adFieldMax),
	}
}

// recordAdClick stores a click and returns the token that points at it. prev
// is the token the visitor already carries: a reload of the same click reuses
// it rather than logging the click twice, which keeps clicks-per-booking
// honest. It's a var so tests can run the middleware without a database.
var recordAdClick = func(prev string, c adClick) (string, error) {
	if prev != "" {
		if old, err := db.GetAdClickByToken(prev); err == nil && old != nil &&
			old.GCLID == c.GCLID && old.BookingID == 0 {
			return prev, nil
		}
	}
	token := generateSessionToken()
	return token, db.RecordAdClick(db.AdClick{
		Token: token, Source: c.Source, GCLID: c.GCLID,
		Keyword: c.Keyword, Campaign: c.Campaign, Landing: c.Landing,
	})
}

// sanitiseText keeps a free-text ad parameter safe to store and print. Unlike
// sanitiseToken it keeps spaces and case, because a keyword reads as
// "computer technician near me", and it caps length because utm_term arrives
// from the URL and a long one is free storage for anyone who wants it.
func sanitiseText(v string, max int) string {
	clean := strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f { // control characters, tabs and newlines included
			return ' '
		}
		return c
	}, v)
	out := []rune(strings.Join(strings.Fields(clean), " "))
	if len(out) > max {
		out = out[:max]
	}
	return string(out)
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
	if v := sanitiseToken(readCookie(r, srcCookie)); v != "" {
		return v
	}
	return db.SourceDirect
}

// linkBookingClick ties the booking just created to the click that produced
// it, and spends the cookie either way so a second booking from the same
// browser can't claim the same click.
func linkBookingClick(w http.ResponseWriter, r *http.Request, bookingID int64) {
	token := readCookie(r, clickCookie)
	if token == "" {
		return
	}
	setAttrCookie(w, clickCookie, "", -1)
	if ok, err := db.LinkAdClick(token, bookingID); err != nil {
		log.Printf("attribution: link ad click to booking #%d: %v", bookingID, err)
	} else if !ok {
		log.Printf("attribution: ad click for booking #%d was unknown or already claimed", bookingID)
	}
}
