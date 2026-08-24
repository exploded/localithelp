package main

import (
	"encoding/base64"
	"html/template"
	"log"
	"net/http"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// ── Google reviews ──
//
// Everything here is gated on site.ReviewURL (REVIEW_URL): empty and the route
// isn't registered, the admin card 404s and the receipt email keeps its old
// wording. The QR encodes our own /review, never the Google URL directly, so
// the destination can change without reprinting a single card.

// handleReview redirects to the Google Business Profile review form. Only
// registered when REVIEW_URL is set (see newMux).
func handleReview(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, site.ReviewURL, http.StatusFound)
}

// reviewShortURL is the address printed on the card and encoded in the QR.
func reviewShortURL() string { return site.BaseURL + "/review" }

// reviewQRDataURI renders the short URL as a PNG data: URI, so the card is a
// single self-contained page that prints without a second request. Medium error
// correction leaves enough redundancy for a printed card that picks up a scuff.
func reviewQRDataURI(size int) (template.URL, error) {
	png, err := qrcode.Encode(reviewShortURL(), qrcode.Medium, size)
	if err != nil {
		return "", err
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)), nil
}

// stripScheme trims https:// for display — "localithelp.com.au/review" is what
// you read out to a customer, not the scheme.
func stripScheme(u string) string {
	return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
}

type adminReviewCardData struct {
	Configured bool // REVIEW_URL set; false renders setup instructions instead
	QR         template.URL
	ShortURL   string
	Display    string // short URL without the scheme, for reading aloud
	Target     string
}

// handleAdminReviewCard renders the printable/scannable card. It stays reachable
// when REVIEW_URL is unset — the nav link is always there, so an empty config
// should explain itself rather than 404.
func handleAdminReviewCard(w http.ResponseWriter, r *http.Request) {
	d := adminReviewCardData{Configured: site.ReviewURL != ""}
	if !d.Configured {
		render(w, r, "admin-review-card", d)
		return
	}
	qr, err := reviewQRDataURI(512)
	if err != nil {
		log.Printf("review qr: %v", err)
		http.Error(w, "failed to render the QR code", http.StatusInternalServerError)
		return
	}
	short := reviewShortURL()
	d.QR, d.ShortURL, d.Display, d.Target = qr, short, stripScheme(short), site.ReviewURL
	render(w, r, "admin-review-card", d)
}
