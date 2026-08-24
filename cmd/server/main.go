package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"localithelp/db"
)

var pages map[string]*template.Template

// userSessions stores active user session tokens mapped to the logged-in user.
var userSessions sync.Map // token -> *userSession

type userSession struct {
	User   *db.User
	Expiry time.Time
	CSRF   string // per-session token required on admin form POSTs
}

var googleOAuth *oauth2.Config

// oauthStates stores CSRF state tokens for in-flight OAuth flows.
var oauthStates sync.Map // state -> time.Time

func main() {
	dir := "."
	if d := os.Getenv("APP_DIR"); d != "" {
		dir = d
	}

	loadEnv(filepath.Join(dir, ".env"))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	initSiteConfig(port)
	initMail()
	initBackup()
	initTurnstile()

	if err := db.Open(filepath.Join(dir, "app.db")); err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	var err error
	pages, err = loadTemplates(filepath.Join(dir, "templates"))
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	// Google OAuth2 config
	if cid := os.Getenv("GOOGLE_CLIENT_ID"); cid != "" {
		googleOAuth = &oauth2.Config{
			ClientID:     cid,
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  site.BaseURL + "/auth/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		}
		log.Println("Google OAuth2 configured")
		initGCalOAuth()
	} else {
		log.Println("GOOGLE_CLIENT_ID not set — Google login disabled")
	}

	mux := newMux(dir)
	startScheduler()

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, canonicalHost(mux)); err != nil {
		log.Fatal(err)
	}
}

// newMux registers every route. Kept separate from main so tests can drive the
// real router (see TestSitemap).
func newMux(dir string) *http.ServeMux {
	mux := http.NewServeMux()
	staticDir = filepath.Join(dir, "static")

	// Static files with cache headers
	fs := http.FileServer(http.Dir(staticDir))
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(fs)))

	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /robots.txt", handleRobots)
	mux.HandleFunc("GET /sitemap.xml", handleSitemap)
	mux.HandleFunc("GET /llms.txt", handleLlmsTxt)
	if site.IndexNowKey != "" {
		mux.HandleFunc("GET /"+site.IndexNowKey+".txt", handleIndexNowKey)
	}
	if site.ReviewURL != "" {
		mux.HandleFunc("GET /review", handleReview)
	}
	mux.HandleFunc("GET /api/pricing", handleAPIPricing)
	mux.HandleFunc("GET /favicon.ico", handleFavicon(dir))

	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("GET /services", handleServices)
	mux.HandleFunc("GET /services/{slug}", handleService)
	mux.HandleFunc("GET /software-development", handleSoftwareDev)
	mux.HandleFunc("GET /pricing", handlePricing)
	mux.HandleFunc("GET /fix-it-yourself", handleGuides)
	mux.HandleFunc("GET /fix-it-yourself/{slug}", handleGuide)
	mux.HandleFunc("GET /areas", handleAreas)
	mux.HandleFunc("GET /areas/{slug}", handleArea)
	mux.HandleFunc("GET /book", handleBookForm)
	mux.HandleFunc("POST /book", handleBookSubmit)
	mux.HandleFunc("GET /book/address-search", handleBookAddressSearch)
	mux.HandleFunc("GET /book/thanks", handleBookThanks)
	mux.HandleFunc("GET /portfolio", handlePortfolio)
	mux.HandleFunc("GET /privacy", handlePrivacy)
	mux.HandleFunc("GET /terms", handleTerms)
	mux.HandleFunc("GET /quote", handleQuote)
	mux.HandleFunc("POST /api/quote", handleQuoteSubmit)
	mux.HandleFunc("GET /quote/sent", handleQuoteSent)
	mux.HandleFunc("GET /quote/verify", handleQuoteVerify)

	// Google auth routes — only used to gate /admin; customers never sign in.
	mux.HandleFunc("GET /auth/google", handleGoogleLogin)
	mux.HandleFunc("GET /auth/google/callback", handleGoogleCallback)
	mux.HandleFunc("POST /auth/logout", handleUserLogout)

	// Public tokenised invoice/receipt views (no login; the token is the secret).
	mux.HandleFunc("GET /invoice/{token}", handleInvoicePublic)
	mux.HandleFunc("GET /invoice/{token}/pdf", handleInvoicePublicPDF)

	// Admin routes (protected by Google sign-in + ADMIN_EMAIL check; POSTs need the session CSRF token)
	mux.HandleFunc("GET /admin", requireAdmin(handleAdmin))
	mux.HandleFunc("GET /admin/options", requireAdmin(handleAdminOptions))
	mux.HandleFunc("GET /admin/review-card", requireAdmin(handleAdminReviewCard))
	mux.HandleFunc("POST /api/admin/save", requireAdmin(handleAdminSave))
	mux.HandleFunc("GET /admin/bookings", requireAdmin(handleAdminBookings))
	mux.HandleFunc("GET /admin/bookings/new", requireAdmin(handleAdminBookingNew))
	mux.HandleFunc("POST /admin/bookings/new", requireAdmin(handleAdminBookingCreate))
	mux.HandleFunc("GET /admin/bookings/{id}", requireAdmin(handleAdminBooking))
	mux.HandleFunc("POST /admin/bookings/{id}/schedule", requireAdmin(handleAdminBookingSchedule))
	mux.HandleFunc("POST /admin/bookings/{id}/status", requireAdmin(handleAdminBookingStatus))
	mux.HandleFunc("POST /admin/bookings/{id}/notes", requireAdmin(handleAdminBookingNotes))
	mux.HandleFunc("POST /admin/bookings/{id}/followup", requireAdmin(handleAdminBookingFollowup))
	mux.HandleFunc("POST /admin/bookings/{id}/address", requireAdmin(handleAdminBookingAddress))
	mux.HandleFunc("GET /admin/address-search", requireAdmin(handleAdminAddressSearch))
	mux.HandleFunc("GET /admin/calendar", requireAdmin(handleAdminCalendar))
	mux.HandleFunc("GET /admin/calendar/settings", requireAdmin(handleAdminCalendarSettings))
	mux.HandleFunc("POST /admin/calendar/connect", requireAdmin(handleGCalConnect))
	mux.HandleFunc("POST /admin/calendar/disconnect", requireAdmin(handleGCalDisconnect))
	mux.HandleFunc("POST /admin/calendar/busy", requireAdmin(handleAdminCalendarBusy))
	mux.HandleFunc("POST /admin/calendar/resync", requireAdmin(handleAdminCalendarResync))
	mux.HandleFunc("GET /auth/google/calendar/callback", requireAdmin(handleGCalCallback))
	mux.HandleFunc("GET /admin/invoices", requireAdmin(handleAdminInvoices))
	mux.HandleFunc("POST /admin/invoices/new", requireAdmin(handleAdminInvoiceNew))
	mux.HandleFunc("GET /admin/invoices/{id}", requireAdmin(handleAdminInvoice))
	mux.HandleFunc("GET /admin/invoices/{id}/pdf", requireAdmin(handleAdminInvoicePDF))
	mux.HandleFunc("POST /admin/invoices/{id}/items", requireAdmin(handleAdminInvoiceItems))
	mux.HandleFunc("POST /admin/invoices/{id}/link", requireAdmin(handleAdminInvoiceLink))
	mux.HandleFunc("POST /admin/invoices/{id}/send", requireAdmin(handleAdminInvoiceSend))
	mux.HandleFunc("POST /admin/invoices/{id}/paid", requireAdmin(handleAdminInvoicePaid))
	mux.HandleFunc("POST /admin/invoices/{id}/void", requireAdmin(handleAdminInvoiceVoid))
	mux.HandleFunc("GET /admin/customers", requireAdmin(handleAdminCustomers))
	mux.HandleFunc("GET /admin/customers/{id}", requireAdmin(handleAdminCustomer))
	mux.HandleFunc("POST /admin/customers/{id}", requireAdmin(handleAdminCustomerSave))
	mux.HandleFunc("POST /admin/customers/{id}/bookings", requireAdmin(handleAdminCustomerBooking))

	// Catch-all: trailing-slash redirects + branded 404.
	mux.HandleFunc("/", handleNotFound)
	return mux
}

// dict builds a map from key/value pairs for passing multiple values to a
// template partial: {{template "price-card" (dict "Site" .Site "Kicker" "…")}}.
// Errors (odd arg count, non-string key) fail the template render, so mistakes
// surface in TestTemplatesRender rather than as silently missing data.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments (%d)", len(pairs))
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		k, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is %T, want string", i/2, pairs[i])
		}
		m[k] = pairs[i+1]
	}
	return m, nil
}

func loadTemplates(dir string) (map[string]*template.Template, error) {
	funcMap := template.FuncMap{
		"safeHTML":  func(s string) template.HTML { return template.HTML(s) },
		"hasPrefix": strings.HasPrefix,
		"add":       func(a, b int) int { return a + b },
		"mul":       func(a, b int) int { return a * b },
		"money":     fmtCents,
		"dict":      dict,
		"seq":       seq,
		"crumbs":    crumbs,
		"stripTags": stripTags,
		"asset":     asset,
	}
	base := template.New("").Funcs(funcMap)
	base = template.Must(base.ParseGlob(filepath.Join(dir, "layouts", "*.html")))

	pagesDir := filepath.Join(dir, "pages")
	result := make(map[string]*template.Template)

	entries, err := os.ReadDir(pagesDir)
	if err != nil {
		return nil, fmt.Errorf("read pages dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
			continue
		}
		name := e.Name()[:len(e.Name())-5] // strip .html
		clone := template.Must(base.Clone())
		result[name] = template.Must(clone.ParseFiles(filepath.Join(pagesDir, e.Name())))
	}
	return result, nil
}

// pageData is the base template data passed to every page.
type pageData struct {
	User     *db.User   // nil if not logged in (only the admin ever signs in)
	IsAdmin  bool       // true if logged-in user's email matches ADMIN_EMAIL
	Site     siteConfig // global site settings (phone, pricing, base URL, suburbs)
	Path     string     // current request path (for nav active state + canonical)
	CSRF     string     // admin session CSRF token for form POSTs ("" when not signed in)
	PageData any        // page-specific data
}

func render(w http.ResponseWriter, r *http.Request, page string, data any) {
	tmpl, ok := pages[page]
	if !ok {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}
	sess := getSession(r)
	var user *db.User
	csrf := ""
	if sess != nil {
		user, csrf = sess.User, sess.CSRF
	}
	pd := pageData{
		User:     user,
		IsAdmin:  isAdmin(user),
		Site:     site,
		Path:     r.URL.Path,
		CSRF:     csrf,
		PageData: data,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", pd); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// getSession returns the live session for the request cookie, or nil.
func getSession(r *http.Request) *userSession {
	cookie, err := r.Cookie("user_session")
	if err != nil {
		return nil
	}
	val, ok := userSessions.Load(cookie.Value)
	if !ok {
		return nil
	}
	sess := val.(*userSession)
	if time.Now().After(sess.Expiry) {
		userSessions.Delete(cookie.Value)
		return nil
	}
	return sess
}

// getLoggedInUser returns the logged-in user from the session cookie, or nil.
func getLoggedInUser(r *http.Request) *db.User {
	if sess := getSession(r); sess != nil {
		return sess.User
	}
	return nil
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	render(w, r, "home", struct {
		Services []Service
		Featured *Service
	}{services, featuredService()})
}

func handlePortfolio(w http.ResponseWriter, r *http.Request) {
	render(w, r, "portfolio", nil)
}

// Legal pages. Both are static prose that reads its figures from siteConfig, so
// the fees and contact details can never drift from the rest of the site.
func handlePrivacy(w http.ResponseWriter, r *http.Request) {
	render(w, r, "privacy", nil)
}

func handleTerms(w http.ResponseWriter, r *http.Request) {
	render(w, r, "terms", nil)
}

// ── Google OAuth ──

// oauthRedirects stores the post-login redirect URL for each OAuth state token.
var oauthRedirects sync.Map // state -> redirect URL string

func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	if googleOAuth == nil {
		http.Error(w, "Google login is not configured", http.StatusServiceUnavailable)
		return
	}
	state := generateSessionToken()
	oauthStates.Store(state, time.Now().Add(10*time.Minute))
	// Only allow same-site relative redirects (no open redirect).
	if redir := r.URL.Query().Get("redirect"); strings.HasPrefix(redir, "/") && !strings.HasPrefix(redir, "//") {
		oauthRedirects.Store(state, redir)
	}
	opts := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline}
	if r.URL.Query().Get("prompt") == "select_account" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}
	authURL := googleOAuth.AuthCodeURL(state, opts...)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if googleOAuth == nil {
		http.Error(w, "Google login is not configured", http.StatusServiceUnavailable)
		return
	}

	// Verify CSRF state
	state := r.URL.Query().Get("state")
	val, ok := oauthStates.LoadAndDelete(state)
	if !ok {
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}
	if time.Now().After(val.(time.Time)) {
		http.Error(w, "state expired", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	code := r.URL.Query().Get("code")
	token, err := googleOAuth.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("google oauth exchange: %v", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// Fetch user info from Google
	client := googleOAuth.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		log.Printf("google userinfo: %v", err)
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var gUser struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gUser); err != nil {
		log.Printf("google userinfo decode: %v", err)
		http.Error(w, "failed to parse user info", http.StatusInternalServerError)
		return
	}

	// Upsert user in DB
	user, err := db.UpsertUser(gUser.ID, gUser.Email, gUser.Name, gUser.Picture)
	if err != nil {
		log.Printf("upsert user: %v", err)
		http.Error(w, "failed to save user", http.StatusInternalServerError)
		return
	}

	// Create session
	sessToken := generateSessionToken()
	userSessions.Store(sessToken, &userSession{
		User:   user,
		Expiry: time.Now().Add(7 * 24 * time.Hour),
		CSRF:   generateSessionToken(),
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "user_session",
		Value:    sessToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 86400,
	})

	log.Printf("user logged in: %s (%s)", user.Name, user.Email)

	// Redirect to the original page if set, otherwise home
	redirectTo := "/"
	if val, ok := oauthRedirects.LoadAndDelete(state); ok {
		redirectTo = val.(string)
	}
	http.Redirect(w, r, redirectTo, http.StatusSeeOther)
}

func handleUserLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("user_session")
	if err == nil {
		userSessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "user_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ── Admin ──

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// isAdmin returns true if the user is logged in and their email matches ADMIN_EMAIL
// (the Google account allowed into /admin; defaults to the owner's Gmail).
func isAdmin(user *db.User) bool {
	if user == nil {
		return false
	}
	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail == "" {
		adminEmail = "james67@gmail.com"
	}
	return strings.EqualFold(user.Email, adminEmail)
}

// isOwnerEmail reports whether email is one of the site owner's configured
// addresses (CONTACT_EMAIL or ADMIN_EMAIL) — used to exempt the owner from
// customer-facing limits like one-quote-per-email so flows can be tested.
func isOwnerEmail(email string) bool {
	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail == "" {
		adminEmail = "james67@gmail.com"
	}
	return strings.EqualFold(email, site.Email) || strings.EqualFold(email, adminEmail)
}

func handleAdmin(w http.ResponseWriter, r *http.Request) {
	quotes, err := db.ListQuotes()
	if err != nil {
		log.Printf("admin: load quotes: %v", err)
	}
	counts, err := db.CountBookingsByStatus()
	if err != nil {
		log.Printf("admin: count bookings: %v", err)
	}
	outstanding, err := db.SumOutstandingCents()
	if err != nil {
		log.Printf("admin: outstanding: %v", err)
	}
	weekStart := startOfWeek(db.Today())
	week, err := db.ListBookingsBetween(weekStart, weekStart.AddDate(0, 0, 7))
	if err != nil {
		log.Printf("admin: week bookings: %v", err)
	}
	render(w, r, "admin", adminDashData{
		Quotes:      quotes,
		NewCount:    counts[db.BookingNew],
		BookedCount: counts[db.BookingBooked],
		DoneCount:   counts[db.BookingDone],
		SentCount:   counts[db.BookingInvoiced],
		Outstanding: outstanding,
		Week:        bookingRows(week),
		CalendarOn:  calendarSyncConnected(),
	})
}

// adminDashData is the /admin dashboard view model.
type adminDashData struct {
	Quotes      []db.Quote
	NewCount    int
	BookedCount int
	DoneCount   int
	SentCount   int
	Outstanding int64 // cents, invoices sent but unpaid
	Week        []bookingRow
	CalendarOn  bool // Google Calendar sync is connected
}

// adminOptionsData is the /admin/options view model: the quote option groups
// and base cost, serialised for the client-side editor in static/js/admin.js.
type adminOptionsData struct {
	GroupsJSON template.JS
	BaseCost   int
}

func handleAdminOptions(w http.ResponseWriter, r *http.Request) {
	groupsJSON, err := db.OptionGroupsJSON()
	if err != nil {
		log.Printf("admin options: load groups: %v", err)
		http.Error(w, "failed to load options", http.StatusInternalServerError)
		return
	}
	baseCost, err := db.GetBaseCost()
	if err != nil {
		log.Printf("admin options: load base cost: %v", err)
	}
	render(w, r, "admin-options", adminOptionsData{
		GroupsJSON: template.JS(groupsJSON),
		BaseCost:   baseCost,
	})
}

func handleAdminSave(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Groups   []db.OptionGroup `json:"groups"`
		BaseCost int              `json:"base_cost"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		jsonError(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := db.SaveAllOptions(payload.Groups); err != nil {
		log.Printf("admin save: %v", err)
		jsonError(w, "failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := db.SetBaseCost(payload.BaseCost); err != nil {
		log.Printf("admin save base cost: %v", err)
		jsonError(w, "failed to save base cost: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ── Site config ──

// siteConfig holds global, environment-driven settings exposed to every template as .Site.
type siteConfig struct {
	BaseURL     string       // canonical origin, no trailing slash, e.g. https://localithelp.com.au
	Phone       string       // display phone, empty hides all phone UI
	PhoneHref   template.URL // tel: link (+61 form); template.URL so html/template keeps the tel: scheme
	Email       string       // contact email
	OnsiteFee   int          // flat visit fee, includes travel (AUD)
	BlockRate   int          // per 15-minute block (AUD)
	SeniorsPct  int          // Seniors Card discount, % off the total (0 hides it)
	Suburbs     []string     // service area (display names)
	Areas       []Suburb     // service area with slugs, for /areas/{slug} links
	ABN         string       // shown on invoices
	Hours       string       // trading hours for display, e.g. "Mon & Fri 11am–5pm"
	HoursLD     []string     // same hours as schema.org openingHours strings
	IndexNowKey string       // IndexNow API key; empty disables the /{key}.txt route
	ReviewURL   string       // Google review link; empty hides every review feature
	BankName    string       // bank transfer details on invoices (BSB empty = hidden)
	BankBSB     string
	BankAcct    string

	// Google tag (Analytics + Ads). Every field is optional and independently
	// gated, so the site renders no third-party script until one is configured.
	Analytics     bool     // true when the tag should render: PROD and at least one ID
	TagID         string   // ID gtag.js is loaded with (GA4 if set, else Ads)
	GA4ID         string   // GA4 measurement ID, G-XXXXXXXXXX
	AdsID         string   // Google Ads conversion ID, AW-XXXXXXXXX
	AdsBookLabel  string   // Ads conversion label — booking request submitted
	AdsQuoteLabel string   // Ads conversion label — quote request submitted
	AdsCallLabel  string   // Ads conversion label — tel: link clicked
	SameAs        []string // profile URLs (Google Business Profile, …) for JSON-LD sameAs
}

var site siteConfig

func initSiteConfig(port string) {
	base := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if base == "" {
		if os.Getenv("PROD") != "" {
			base = "https://localithelp.com.au"
		} else {
			base = "http://localhost:" + port
		}
	}
	email := os.Getenv("CONTACT_EMAIL")
	if email == "" {
		email = "james@localithelp.com.au"
	}
	site = siteConfig{
		BaseURL:     base,
		Phone:       strings.TrimSpace(os.Getenv("PHONE")),
		Email:       email,
		OnsiteFee:   envInt("ONSITE_FEE", 80),
		BlockRate:   envInt("BLOCK_RATE", 30),
		SeniorsPct:  envInt("SENIORS_DISCOUNT_PCT", 20),
		Suburbs:     suburbs,
		Areas:       suburbList,
		ABN:         envOr("ABN", "14 723 053 435"),
		Hours:       hoursDisplay(),
		HoursLD:     hoursSchema(),
		IndexNowKey: strings.TrimSpace(os.Getenv("INDEXNOW_KEY")),
		ReviewURL:   httpsURL(os.Getenv("REVIEW_URL")),
		BankName:    envOr("BANK_ACCOUNT_NAME", "James McHugh"),
		BankBSB:     strings.TrimSpace(os.Getenv("BANK_BSB")),
		BankAcct:    strings.TrimSpace(os.Getenv("BANK_ACCOUNT_NO")),

		GA4ID:         tagID(os.Getenv("GA4_ID"), "G-"),
		AdsID:         tagID(os.Getenv("GOOGLE_ADS_ID"), "AW-"),
		AdsBookLabel:  tagToken(os.Getenv("GOOGLE_ADS_BOOKING_LABEL")),
		AdsQuoteLabel: tagToken(os.Getenv("GOOGLE_ADS_QUOTE_LABEL")),
		AdsCallLabel:  tagToken(os.Getenv("GOOGLE_ADS_CALL_LABEL")),
		SameAs:        splitList(os.Getenv("SAME_AS")),
	}
	site.PhoneHref = template.URL(telHref(site.Phone))
	applySeniorsNote(site.SeniorsPct)

	// A conversion label is meaningless without the Ads ID it hangs off.
	if site.AdsID == "" {
		site.AdsBookLabel, site.AdsQuoteLabel, site.AdsCallLabel = "", "", ""
	}
	site.TagID = site.GA4ID
	if site.TagID == "" {
		site.TagID = site.AdsID
	}
	// Only tag production traffic — a local build reading a copy of the server's
	// .env must not pollute the property or report phantom conversions.
	site.Analytics = os.Getenv("PROD") != "" && site.TagID != ""
}

// httpsURL sanitises a configured external link: only https, no whitespace or
// quotes (html/template would neuter anything else anyway), reasonable length.
// Anything else comes back empty, which disables the feature that uses it.
func httpsURL(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "https://") || strings.ContainsAny(s, " \"'<>") || len(s) > 500 {
		return ""
	}
	return s
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// tagToken sanitises a Google tag ID or conversion label. Google uses only
// letters, digits, dash and underscore in both, so anything else is a typo (or
// an attempt to break out of the tag's JS string) and is dropped entirely.
func tagToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	for _, c := range v {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			log.Printf("ignoring malformed Google tag value %q", v)
			return ""
		}
	}
	return v
}

// tagID is tagToken plus a required prefix ("G-" for GA4, "AW-" for Ads), so a
// value pasted into the wrong variable is ignored rather than silently wrong.
func tagID(v, prefix string) string {
	v = tagToken(v)
	if !strings.HasPrefix(v, prefix) {
		if v != "" {
			log.Printf("ignoring Google tag ID %q: expected the %s prefix", v, prefix)
		}
		return ""
	}
	return v
}

// splitList splits a comma-separated env value into trimmed, non-empty entries.
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// telHref converts a display phone number like "0400 000 000" into "tel:+61400000000".
func telHref(phone string) string {
	if phone == "" {
		return ""
	}
	var digits strings.Builder
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			digits.WriteRune(c)
		}
	}
	d := digits.String()
	if strings.HasPrefix(d, "0") {
		d = "61" + d[1:]
	}
	if strings.HasPrefix(phone, "+") || strings.HasPrefix(d, "61") {
		return "tel:+" + d
	}
	return "tel:" + d
}

// loadEnv reads a .env file and sets environment variables (does not override existing ones).
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

// asset returns the /static/ URL for p (e.g. "js/address.js") with a
// cache-busting version derived from the file's modification time, so
// browsers pick up changed files despite the max-age on /static/.
func asset(p string) string {
	url := "/static/" + p
	if fi, err := os.Stat(filepath.Join(staticDir, filepath.FromSlash(p))); err == nil {
		url += "?v=" + strconv.FormatInt(fi.ModTime().Unix(), 10)
	}
	return url
}

func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		h.ServeHTTP(w, r)
	})
}

// openHours is the trading week in one place. The contact block, the
// LocalBusiness JSON-LD and llms.txt all render from it, so they can't drift
// apart — which is how the site came to advertise different hours from the
// Google Business Profile. Days off are simply left out.
//
// Keep in step with the Business Profile: Google treats a mismatch between a
// site and its profile as a trust signal.
var openHours = []struct {
	Code  string // schema.org two-letter day
	Label string
	Open  string // 24-hour, as schema.org wants it
	Close string
}{
	{"Mo", "Mon", "11:00", "17:00"},
	{"Tu", "Tue", "09:00", "17:00"},
	{"We", "Wed", "11:00", "17:00"},
	{"Th", "Thu", "09:00", "17:00"},
	{"Fr", "Fri", "11:00", "17:00"},
}

// hoursSchema renders one "Mo 11:00-17:00" string per open day. Per-day strings
// rather than ranges: unambiguous to parse, and a day's hours can change
// without anyone having to re-group the week.
func hoursSchema() []string {
	out := make([]string, 0, len(openHours))
	for _, h := range openHours {
		out = append(out, h.Code+" "+h.Open+"-"+h.Close)
	}
	return out
}

// hoursDisplay groups days that share an opening time into readable phrases,
// e.g. "Mon & Wed 11am–5pm · Tue, Thu & Fri 9am–5pm". Groups keep the order
// their first day falls in the week.
func hoursDisplay() string {
	type group struct {
		days []string
		span string
	}
	var groups []*group
	byspan := map[string]*group{}
	for _, h := range openHours {
		span := clock12(h.Open) + "–" + clock12(h.Close)
		g, ok := byspan[span]
		if !ok {
			g = &group{span: span}
			byspan[span] = g
			groups = append(groups, g)
		}
		g.days = append(g.days, h.Label)
	}
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		parts = append(parts, joinAnd(g.days)+" "+g.span)
	}
	return strings.Join(parts, " · ")
}

// clock12 turns "09:00" into "9am" and "17:30" into "5.30pm" — how the hours
// read everywhere else on the site.
func clock12(hhmm string) string {
	h, m, ok := strings.Cut(hhmm, ":")
	if !ok {
		return hhmm
	}
	n, err := strconv.Atoi(h)
	if err != nil {
		return hhmm
	}
	suffix := "am"
	if n >= 12 {
		suffix = "pm"
	}
	if n > 12 {
		n -= 12
	}
	if n == 0 {
		n = 12
	}
	s := strconv.Itoa(n)
	if m != "00" {
		s += "." + m
	}
	return s + suffix
}

// joinAnd renders a list the way a person would say it: "Mon, Wed & Fri".
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " & " + items[len(items)-1]
}
