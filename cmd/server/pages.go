package main

import (
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"localithelp/db"
)

// ── Services ──

func handleServices(w http.ResponseWriter, r *http.Request) {
	render(w, r, "services", struct {
		Services []Service
		Featured *Service
	}{services, featuredService()})
}

func handleService(w http.ResponseWriter, r *http.Request) {
	s, ok := findService(r.PathValue("slug"))
	if !ok {
		notFound(w, r)
		return
	}
	if s.Featured {
		http.Redirect(w, r, s.URL(), http.StatusMovedPermanently)
		return
	}
	render(w, r, "service", struct {
		S       *Service
		Related []*Service
		Guides  []*Guide
	}{s, relatedServices(s), guidesForService(s.Slug)})
}

func handleSoftwareDev(w http.ResponseWriter, r *http.Request) {
	s := featuredService()
	if s == nil {
		notFound(w, r)
		return
	}
	render(w, r, "software-development", struct {
		S        *Service
		Packages []Package
		Hourly   string
		Related  []*Service
	}{s, softwarePackages, softwareHourly, relatedServices(s)})
}

// ── Pricing ──

type pricingPageData struct {
	Packages    []Package
	Hourly      string
	HourTotal   int // one-hour visit: fee + 4 blocks
	SeniorsHour int // the same hour with the Seniors Card discount
}

func handlePricing(w http.ResponseWriter, r *http.Request) {
	hour := site.OnsiteFee + 4*site.BlockRate
	render(w, r, "pricing", pricingPageData{
		Packages:    softwarePackages,
		Hourly:      softwareHourly,
		HourTotal:   hour,
		SeniorsHour: hour - hour*site.SeniorsPct/100,
	})
}

// ── Fix it yourself guides ──

// guideGroup is one category on the /fix-it-yourself index.
type guideGroup struct {
	Kicker string
	Guides []*Guide
}

// guideCategoryOrder controls the order of categories on the index; unknown kickers go last.
var guideCategoryOrder = []string{"Security", "Wi-Fi", "Printing", "Email", "Phone", "Windows", "Data"}

func groupGuides() []guideGroup {
	byKicker := map[string]*guideGroup{}
	var groups []*guideGroup
	for _, k := range guideCategoryOrder {
		g := &guideGroup{Kicker: k}
		byKicker[k] = g
		groups = append(groups, g)
	}
	for i := range guides {
		g, ok := byKicker[guides[i].Kicker]
		if !ok {
			g = &guideGroup{Kicker: guides[i].Kicker}
			byKicker[guides[i].Kicker] = g
			groups = append(groups, g)
		}
		g.Guides = append(g.Guides, &guides[i])
	}
	var out []guideGroup
	for _, g := range groups {
		if len(g.Guides) > 0 {
			out = append(out, *g)
		}
	}
	return out
}

func handleGuides(w http.ResponseWriter, r *http.Request) {
	render(w, r, "guides", struct{ Groups []guideGroup }{groupGuides()})
}

func handleGuide(w http.ResponseWriter, r *http.Request) {
	g, ok := findGuide(r.PathValue("slug"))
	if !ok {
		notFound(w, r)
		return
	}
	var others []*Guide
	for i := range guides {
		if guides[i].Slug != g.Slug && guides[i].Kicker == g.Kicker {
			others = append(others, &guides[i])
		}
	}
	render(w, r, "guide", struct {
		G      *Guide
		Others []*Guide
	}{g, others})
}

// ── Service areas ──

func handleAreas(w http.ResponseWriter, r *http.Request) {
	render(w, r, "areas", struct {
		Groups []suburbGroup
		Count  int
	}{groupSuburbs(), len(suburbList)})
}

func handleArea(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	s, ok := findSuburb(slug)
	if !ok {
		// Suburbs dropped when the service area was tightened keep their link
		// equity by pointing at the index rather than 404ing.
		if retiredSuburbs[slug] {
			http.Redirect(w, r, "/areas", http.StatusMovedPermanently)
			return
		}
		notFound(w, r)
		return
	}
	render(w, r, "area", areaPageData(s))
}

type areaPage struct {
	A        *Suburb
	Image    string // "/static/img/areas/{slug}.jpg" when the file exists, else ""
	Services []Service
	Guides   []*Guide
	Nearby   []*Suburb
}

func areaPageData(s *Suburb) areaPage {
	return areaPage{A: s, Image: areaImage(s), Services: services, Guides: areaGuides(s.Slug), Nearby: s.Nearby()}
}

// areaGuides picks the four guides to link from a suburb page. Every area page
// used to link the same first four of the catalogue, which left the rest with
// almost no internal links; starting at a hash of the slug spreads the links
// across the whole catalogue while keeping each page's list stable.
func areaGuides(slug string) []*Guide {
	if len(guides) == 0 {
		return nil
	}
	h := fnv.New32a()
	h.Write([]byte(slug))
	start := int(h.Sum32() % uint32(len(guides)))
	n := 4
	if n > len(guides) {
		n = len(guides)
	}
	out := make([]*Guide, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, &guides[(start+i)%len(guides)])
	}
	return out
}

// staticDir is where /static/ is served from; set by newMux so handlers can check for optional assets.
var staticDir = "static"

// areaImage returns the URL of the suburb's photo if static/img/areas/{slug}.jpg exists.
// Photos are optional and file-driven: drop one in (see tools/areaphoto) and it appears.
func areaImage(s *Suburb) string {
	if _, err := os.Stat(filepath.Join(staticDir, "img", "areas", s.Slug+".jpg")); err != nil {
		return ""
	}
	return "/static/img/areas/" + s.Slug + ".jpg"
}

// ── Booking ──

type bookForm struct {
	Name          string
	Phone         string
	Email         string
	Address       string // the visible autocomplete field, as typed or picked
	AddrStreet    string // hidden structured parts, filled only when a suggestion is picked
	AddrSuburb    string
	AddrState     string
	AddrPostcode  string
	Service       string
	Issue         string
	PreferredTime string
}

type bookPageData struct {
	Services []Service
	Form     bookForm
	Errors   map[string]string
	TS       int64
}

func handleBookForm(w http.ResponseWriter, r *http.Request) {
	// Prefill from query params so AI assistants can hand users a ready-made
	// booking link (documented in /llms.txt). Caps mirror the form maxlengths.
	// Address fields are deliberately never prefilled: the address must come
	// through the autocomplete so the service-area gate stays meaningful.
	q := r.URL.Query()
	pre := func(key string, max int) string {
		v := strings.TrimSpace(q.Get(key))
		if runes := []rune(v); len(runes) > max {
			v = string(runes[:max])
		}
		return v
	}
	f := bookForm{
		Name:          pre("name", 100),
		Phone:         pre("phone", 40),
		Email:         pre("email", 120),
		Service:       q.Get("service"),
		Issue:         pre("issue", 2000),
		PreferredTime: pre("preferred_time", 200),
	}
	if _, ok := findService(f.Service); !ok {
		f.Service = ""
	}
	render(w, r, "book", bookPageData{
		Services: services,
		Form:     f,
		Errors:   map[string]string{},
		TS:       time.Now().Unix(),
	})
}

func handleBookThanks(w http.ResponseWriter, r *http.Request) {
	render(w, r, "book-thanks", nil)
}

// bookingHits tracks recent booking submissions per IP for a simple rate limit.
var bookingHits sync.Map // ip -> []time.Time

const (
	bookingRateWindow = time.Hour
	bookingRateMax    = 5
)

func bookingRateLimited(ip string) bool {
	now := time.Now()
	var recent []time.Time
	if v, ok := bookingHits.Load(ip); ok {
		for _, t := range v.([]time.Time) {
			if now.Sub(t) < bookingRateWindow {
				recent = append(recent, t)
			}
		}
	}
	if len(recent) >= bookingRateMax {
		bookingHits.Store(ip, recent)
		return true
	}
	bookingHits.Store(ip, append(recent, now))
	return false
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func handleBookSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	trim := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }

	f := bookForm{
		Name:          trim("name"),
		Phone:         trim("phone"),
		Email:         trim("email"),
		Address:       trim("address"),
		AddrStreet:    trim("addr_street"),
		AddrSuburb:    trim("addr_suburb"),
		AddrState:     strings.ToUpper(trim("addr_state")),
		AddrPostcode:  trim("addr_postcode"),
		Service:       trim("service"),
		Issue:         trim("issue"),
		PreferredTime: trim("preferred_time"),
	}
	ip := clientIP(r)

	// Honeypot: real users never see this field. Pretend success, save nothing.
	if trim("website") != "" {
		log.Printf("booking: honeypot hit from %s", ip)
		http.Redirect(w, r, "/book/thanks", http.StatusSeeOther)
		return
	}
	// Timestamp sanity: form rendered less than 3s ago or more than a day ago is suspicious.
	ts, _ := strconv.ParseInt(trim("ts"), 10, 64)
	age := time.Since(time.Unix(ts, 0))
	suspicious := ts == 0 || age < 3*time.Second || age > 24*time.Hour

	if bookingRateLimited(ip) {
		http.Error(w, "Too many requests — please call or email instead.", http.StatusTooManyRequests)
		return
	}

	errs := map[string]string{}
	if n := len([]rune(f.Name)); n < 2 || n > 100 {
		errs["name"] = "Please enter your name."
	}
	if f.Phone == "" && f.Email == "" {
		errs["contact"] = "Please give a phone number or an email address so I can get back to you."
	}
	if f.Email != "" && (!strings.Contains(f.Email, "@") || len(f.Email) > 120) {
		errs["email"] = "That email address doesn't look right."
	}
	if len(f.Phone) > 40 {
		errs["phone"] = "That phone number looks too long."
	}
	if n := len([]rune(f.Issue)); n < 10 {
		errs["issue"] = "Tell me a little about the problem (at least a sentence)."
	} else if n > 2000 {
		errs["issue"] = "Please keep the description under 2,000 characters."
	}
	if reason, ok := validAddressParts(f.AddrStreet, f.AddrSuburb, f.AddrState, f.AddrPostcode); !ok {
		errs["address"] = reason
	}
	if len([]rune(f.PreferredTime)) > 200 {
		errs["preferred_time"] = "Please keep this short."
	}
	if _, ok := findService(f.Service); !ok {
		f.Service = ""
	}

	if len(errs) > 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, "book", bookPageData{
			Services: services,
			Form:     f,
			Errors:   errs,
			TS:       time.Now().Unix(),
		})
		return
	}

	fullAddress := f.AddrStreet + ", " + f.AddrSuburb + " " + f.AddrState + " " + f.AddrPostcode
	if len(fullAddress) > 200 {
		fullAddress = fullAddress[:200]
	}
	customerID, err := db.FindOrCreateCustomer(f.Name, f.Email, f.Phone, f.AddrSuburb)
	if err != nil {
		log.Printf("booking: customer upsert failed: %v", err)
		// Not fatal: the booking is still recorded; the boot-time backfill will link it later.
	}
	if customerID != 0 {
		// Fill in a blank customer address, but never overwrite one from a
		// public form — the admin address editor is the authority there.
		if c, err := db.GetCustomer(customerID); err == nil && c.Address == "" {
			c.Address, c.Suburb = fullAddress, f.AddrSuburb
			if err := db.UpdateCustomer(c); err != nil {
				log.Printf("booking: fill customer address: %v", err)
			}
		}
	}
	b := &db.Booking{
		CustomerID:    customerID,
		Name:          f.Name,
		Phone:         f.Phone,
		Email:         f.Email,
		Suburb:        f.AddrSuburb,
		Address:       fullAddress,
		ServiceSlug:   f.Service,
		Mode:          "onsite",
		Issue:         f.Issue,
		PreferredTime: f.PreferredTime,
		IP:            ip,
	}
	id, err := db.InsertBooking(b)
	if err != nil {
		log.Printf("booking: insert failed: %v", err)
		http.Error(w, "Sorry — something went wrong saving your request. Please call or email instead.", http.StatusInternalServerError)
		return
	}
	if suspicious {
		if err := db.UpdateBookingStatus(id, "spam"); err != nil {
			log.Printf("booking: mark suspicious #%d: %v", id, err)
		}
	}
	log.Printf("booking #%d from %s (%s / %s) service=%s suspicious=%v", id, f.Name, f.Phone, f.Email, f.Service, suspicious)
	notifyBooking(id, b, suspicious)
	http.Redirect(w, r, "/book/thanks", http.StatusSeeOther)
}
