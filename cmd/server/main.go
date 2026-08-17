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
	"mchugh.com.au/db"
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
	initTurnstile()

	if err := db.Open(filepath.Join(dir, "quotes.db")); err != nil {
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
	} else {
		log.Println("GOOGLE_CLIENT_ID not set — Google login disabled")
	}

	mux := newMux(dir)

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, canonicalHost(mux)); err != nil {
		log.Fatal(err)
	}
}

// newMux registers every route. Kept separate from main so tests can drive the
// real router (see TestSitemap).
func newMux(dir string) *http.ServeMux {
	mux := http.NewServeMux()

	// Static files with cache headers
	fs := http.FileServer(http.Dir(filepath.Join(dir, "static")))
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(fs)))

	mux.HandleFunc("GET /robots.txt", handleRobots)
	mux.HandleFunc("GET /sitemap.xml", handleSitemap)
	mux.HandleFunc("GET /favicon.ico", handleFavicon(dir))

	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("GET /services", handleServices)
	mux.HandleFunc("GET /services/{slug}", handleService)
	mux.HandleFunc("GET /software-development", handleSoftwareDev)
	mux.HandleFunc("GET /fix-it-yourself", handleGuides)
	mux.HandleFunc("GET /fix-it-yourself/{slug}", handleGuide)
	mux.HandleFunc("GET /areas", handleAreas)
	mux.HandleFunc("GET /areas/{slug}", handleArea)
	mux.HandleFunc("GET /book", handleBookForm)
	mux.HandleFunc("POST /book", handleBookSubmit)
	mux.HandleFunc("GET /book/thanks", handleBookThanks)
	mux.HandleFunc("GET /portfolio", handlePortfolio)
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
	mux.HandleFunc("POST /api/admin/save", requireAdmin(handleAdminSave))
	mux.HandleFunc("GET /admin/bookings", requireAdmin(handleAdminBookings))
	mux.HandleFunc("GET /admin/bookings/{id}", requireAdmin(handleAdminBooking))
	mux.HandleFunc("POST /admin/bookings/{id}/schedule", requireAdmin(handleAdminBookingSchedule))
	mux.HandleFunc("POST /admin/bookings/{id}/status", requireAdmin(handleAdminBookingStatus))
	mux.HandleFunc("POST /admin/bookings/{id}/notes", requireAdmin(handleAdminBookingNotes))
	mux.HandleFunc("POST /admin/bookings/{id}/followup", requireAdmin(handleAdminBookingFollowup))
	mux.HandleFunc("GET /admin/calendar", requireAdmin(handleAdminCalendar))
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

func loadTemplates(dir string) (map[string]*template.Template, error) {
	funcMap := template.FuncMap{
		"safeHTML":  func(s string) template.HTML { return template.HTML(s) },
		"hasPrefix": strings.HasPrefix,
		"add":       func(a, b int) int { return a + b },
		"mul":       func(a, b int) int { return a * b },
		"money":     fmtCents,
		"seq":       seq,
		"crumbs":    crumbs,
		"stripTags": stripTags,
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
	BaseURL   string       // canonical origin, no trailing slash, e.g. https://mchugh.com.au
	Phone     string       // display phone, empty hides all phone UI
	PhoneHref template.URL // tel: link (+61 form); template.URL so html/template keeps the tel: scheme
	Email     string       // contact email
	OnsiteFee int          // onsite service fee (AUD)
	BlockRate int          // per 15-minute block (AUD)
	Suburbs   []string     // service area (display names)
	Areas     []Suburb     // service area with slugs, for /areas/{slug} links
	ABN       string       // shown on invoices
	BankName  string       // bank transfer details on invoices (BSB empty = hidden)
	BankBSB   string
	BankAcct  string
}

var site siteConfig

func initSiteConfig(port string) {
	base := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if base == "" {
		if os.Getenv("PROD") != "" {
			base = "https://mchugh.com.au"
		} else {
			base = "http://localhost:" + port
		}
	}
	email := os.Getenv("CONTACT_EMAIL")
	if email == "" {
		email = "james@mchugh.com.au"
	}
	site = siteConfig{
		BaseURL:   base,
		Phone:     strings.TrimSpace(os.Getenv("PHONE")),
		Email:     email,
		OnsiteFee: envInt("ONSITE_FEE", 80),
		BlockRate: envInt("BLOCK_RATE", 30),
		Suburbs:   suburbs,
		Areas:     suburbList,
		ABN:       envOr("ABN", "14 723 053 435"),
		BankName:  envOr("BANK_ACCOUNT_NAME", "James McHugh"),
		BankBSB:   strings.TrimSpace(os.Getenv("BANK_BSB")),
		BankAcct:  strings.TrimSpace(os.Getenv("BANK_ACCOUNT_NO")),
	}
	site.PhoneHref = template.URL(telHref(site.Phone))
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

func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		h.ServeHTTP(w, r)
	})
}
