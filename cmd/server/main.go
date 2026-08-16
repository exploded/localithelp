package main

import (
	"bufio"
	"bytes"
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

// pendingQuotes stores form data keyed by Stripe session ID
var pendingQuotes sync.Map

// userSessions stores active user session tokens mapped to the logged-in user.
var userSessions sync.Map // token -> *userSession

type userSession struct {
	User   *db.User
	Expiry time.Time
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

	mux := http.NewServeMux()

	// Static files with cache headers
	fs := http.FileServer(http.Dir(filepath.Join(dir, "static")))
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(fs)))

	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("GET /services", handleServices)
	mux.HandleFunc("GET /services/{slug}", handleService)
	mux.HandleFunc("GET /software-development", handleSoftwareDev)
	mux.HandleFunc("GET /fix-it-yourself", handleGuides)
	mux.HandleFunc("GET /fix-it-yourself/{slug}", handleGuide)
	mux.HandleFunc("GET /book", handleBookForm)
	mux.HandleFunc("POST /book", handleBookSubmit)
	mux.HandleFunc("GET /book/thanks", handleBookThanks)
	mux.HandleFunc("GET /portfolio", handlePortfolio)
	mux.HandleFunc("GET /quote", handleQuote)
	mux.HandleFunc("GET /quote/success", handleQuoteSuccess)
	mux.HandleFunc("GET /quote/cancel", handleQuoteCancel)
	mux.HandleFunc("GET /my-quotes", handleMyQuotes)
	mux.HandleFunc("POST /api/create-checkout", handleCreateCheckout)

	// Google auth routes
	mux.HandleFunc("GET /auth/google", handleGoogleLogin)
	mux.HandleFunc("GET /auth/google/callback", handleGoogleCallback)
	mux.HandleFunc("POST /auth/logout", handleUserLogout)

	// Admin routes (protected by Google sign-in + ADMIN_EMAIL check)
	mux.HandleFunc("GET /admin", handleAdmin)
	mux.HandleFunc("POST /api/admin/save", handleAdminSave)

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func loadTemplates(dir string) (map[string]*template.Template, error) {
	funcMap := template.FuncMap{
		"safeHTML":  func(s string) template.HTML { return template.HTML(s) },
		"hasPrefix": strings.HasPrefix,
		"add":       func(a, b int) int { return a + b },
		"mul":       func(a, b int) int { return a * b },
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
	User     *db.User   // nil if not logged in
	IsAdmin  bool       // true if logged-in user's email matches ADMIN_EMAIL
	LoginURL string     // "/auth/google" or empty if OAuth not configured
	Site     siteConfig // global site settings (phone, pricing, base URL, suburbs)
	Path     string     // current request path (for nav active state + canonical)
	PageData any        // page-specific data
}

func render(w http.ResponseWriter, r *http.Request, page string, data any) {
	tmpl, ok := pages[page]
	if !ok {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}
	user := getLoggedInUser(r)
	pd := pageData{
		User:     user,
		IsAdmin:  isAdmin(user),
		Site:     site,
		Path:     r.URL.Path,
		PageData: data,
	}
	if googleOAuth != nil {
		pd.LoginURL = "/auth/google"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", pd); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// getLoggedInUser returns the logged-in user from the session cookie, or nil.
func getLoggedInUser(r *http.Request) *db.User {
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
	return sess.User
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

func handleMyQuotes(w http.ResponseWriter, r *http.Request) {
	user := getLoggedInUser(r)
	if user == nil {
		http.Redirect(w, r, "/auth/google?redirect=/my-quotes", http.StatusSeeOther)
		return
	}

	quotes, err := db.ListPaidByUser(user.ID)
	if err != nil {
		log.Printf("load paid quotes: %v", err)
		http.Error(w, "failed to load quotes", http.StatusInternalServerError)
		return
	}

	render(w, r, "my-quotes", quotes)
}

func handleQuote(w http.ResponseWriter, r *http.Request) {
	user := getLoggedInUser(r)
	if user == nil {
		http.Redirect(w, r, "/auth/google?redirect=/quote", http.StatusSeeOther)
		return
	}

	groups, err := db.ListOptionGroups()
	if err != nil {
		log.Printf("load options: %v", err)
		http.Error(w, "failed to load options", http.StatusInternalServerError)
		return
	}
	baseCost, err := db.GetBaseCost()
	if err != nil {
		log.Printf("load base cost: %v", err)
	}

	drafts, err := db.ListDraftsByUser(user.ID)
	if err != nil {
		log.Printf("load drafts: %v", err)
	}

	// If loading a specific draft, fetch its full data
	var draftJSON template.JS
	if draftID := r.URL.Query().Get("draft"); draftID != "" {
		var id int64
		fmt.Sscanf(draftID, "%d", &id)
		if id > 0 {
			draft, err := db.GetQuote(id)
			if err == nil && draft.UserID == user.ID && draft.Status == "draft" {
				dj, _ := json.Marshal(draft.Features)
				draftData := map[string]any{
					"name":        draft.Name,
					"mobile":      draft.Mobile,
					"address":     draft.Address,
					"description": draft.Description,
					"features":    json.RawMessage(dj),
					"id":          draft.ID,
				}
				djBytes, _ := json.Marshal(draftData)
				draftJSON = template.JS(djBytes)
			}
		}
	}

	render(w, r, "quote", struct {
		StripeKey string
		Groups    []db.OptionGroup
		BaseCost  int
		Drafts    []db.Quote
		DraftJSON template.JS
	}{
		StripeKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
		Groups:    groups,
		BaseCost:  baseCost,
		Drafts:    drafts,
		DraftJSON: draftJSON,
	})
}

func handleQuoteSuccess(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		render(w, r, "quote-success", map[string]any{"Error": "Missing session information."})
		return
	}

	// Verify payment with Stripe
	secretKey := os.Getenv("STRIPE_SECRET_KEY")
	paid, err := verifyStripePayment(secretKey, sessionID)
	if err != nil {
		log.Printf("stripe verification error: %v", err)
		render(w, r, "quote-success", map[string]any{"Error": "Could not verify payment. Please contact support."})
		return
	}
	if !paid {
		render(w, r, "quote-success", map[string]any{"Error": "Payment has not been completed. Please try again."})
		return
	}

	// Retrieve pending quote ID
	val, ok := pendingQuotes.LoadAndDelete(sessionID)
	if !ok {
		// Already processed or expired — show generic success
		render(w, r, "quote-success", map[string]any{"AlreadyProcessed": true})
		return
	}
	quoteID := val.(int64)

	quote, err := db.GetQuote(quoteID)
	if err != nil {
		log.Printf("failed to load quote #%d: %v", quoteID, err)
		render(w, r, "quote-success", map[string]any{"Error": "Could not load quote data."})
		return
	}

	// Build form data map for the estimate prompt
	formData := map[string]any{
		"name":        quote.Name,
		"email":       quote.Email,
		"description": quote.Description,
		"total_cost":  quote.TotalCost,
	}
	for k, v := range quote.Features {
		formData[k] = v
	}

	// Run AI estimate now that payment is confirmed
	var aiEstimate string
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		prompt := buildEstimatePrompt(formData)
		aiEstimate, err = callClaude(apiKey, prompt)
		if err != nil {
			log.Printf("claude API error after payment: %v", err)
			aiEstimate = "<p>AI estimate could not be generated at this time. James will review your requirements manually and include pricing in your proposal.</p>"
		}
	}

	// Update quote status to paid
	if err := db.UpdateQuoteStatus(quoteID, "paid", sessionID, aiEstimate); err != nil {
		log.Printf("failed to update quote #%d: %v", quoteID, err)
	} else {
		log.Printf("quote #%d marked as paid for %s", quoteID, quote.Email)
		notifyQuotePaid(quote)
	}

	render(w, r, "quote-success", map[string]any{
		"AIEstimate": template.HTML(aiEstimate),
		"Name":       quote.Name,
	})
}

func handleQuoteCancel(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/quote", http.StatusSeeOther)
}

func buildEstimatePrompt(data map[string]any) string {
	var b strings.Builder
	b.WriteString("You are a project estimation assistant for an Australian web development business (James McHugh, ABN 14 723 053 435). ")
	b.WriteString("Based on the following project requirements, provide a concise estimate including:\n")
	b.WriteString("1. A recommended total cost range (in AUD)\n")
	b.WriteString("2. Estimated turnaround time\n")
	b.WriteString("3. Brief notes on complexity factors\n\n")
	b.WriteString("Format your response as clean HTML (use <p>, <strong>, <ul>, <li> tags only). Keep it concise — max 200 words.\n\n")
	b.WriteString("Project Requirements:\n")

	desc, _ := data["description"].(string)
	if desc != "" {
		b.WriteString(fmt.Sprintf("Description: %s\n", desc))
	}

	totalCost, _ := data["total_cost"].(float64)
	b.WriteString(fmt.Sprintf("Configured base estimate: $%.0f AUD\n\n", totalCost))

	// Load feature labels dynamically from database
	groups, err := db.ListOptionGroups()
	if err != nil {
		log.Printf("buildEstimatePrompt: failed to load groups: %v", err)
	}

	b.WriteString("Selected features:\n")
	for _, g := range groups {
		val, _ := data[g.Name].(string)
		cost, _ := data[g.Name+"_cost"].(float64)
		if val != "" {
			b.WriteString(fmt.Sprintf("- %s: %s ($%.0f)\n", g.Label, val, cost))
		}
	}

	return b.String()
}

func callClaude(apiKey, prompt string) (string, error) {
	reqBody := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, result.Error.Message)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}

	return result.Content[0].Text, nil
}

// handleCreateCheckout creates a Stripe Checkout session
func handleCreateCheckout(w http.ResponseWriter, r *http.Request) {
	user := getLoggedInUser(r)
	if user == nil {
		jsonError(w, "login required", http.StatusUnauthorized)
		return
	}

	secretKey := os.Getenv("STRIPE_SECRET_KEY")
	if secretKey == "" {
		jsonError(w, "Payment is not configured", http.StatusServiceUnavailable)
		return
	}

	var formData map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&formData); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	customerEmail := user.Email

	baseURL := site.BaseURL

	params := fmt.Sprintf(
		"mode=payment"+
			"&success_url=%s/quote/success?session_id={CHECKOUT_SESSION_ID}"+
			"&cancel_url=%s/quote/cancel"+
			"&customer_email=%s"+
			"&line_items[0][price_data][currency]=aud"+
			"&line_items[0][price_data][unit_amount]=500"+
			"&line_items[0][price_data][product_data][name]=Web+App+Quote+-+Detailed+Proposal"+
			"&line_items[0][quantity]=1",
		baseURL, baseURL, customerEmail,
	)

	req, err := http.NewRequest("POST", "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(params))
	if err != nil {
		jsonError(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(secretKey, "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		jsonError(w, "failed to reach payment provider", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var stripeResp struct {
		ID    string `json:"id"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&stripeResp); err != nil {
		jsonError(w, "invalid payment response", http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != 200 {
		log.Printf("stripe error: %s", stripeResp.Error.Message)
		jsonError(w, "Payment error: "+stripeResp.Error.Message, http.StatusBadRequest)
		return
	}

	// Save as draft quote in DB
	name, _ := formData["name"].(string)
	mobile, _ := formData["mobile"].(string)
	address, _ := formData["address"].(string)
	description, _ := formData["description"].(string)
	totalCost, _ := formData["total_cost"].(float64)

	features := make(map[string]any)
	for k, v := range formData {
		if strings.HasPrefix(k, "feature_") {
			features[k] = v
		}
	}

	quoteID, err := db.InsertQuote(&db.Quote{
		UserID:          user.ID,
		Name:            name,
		Email:           customerEmail,
		Mobile:          mobile,
		Address:         address,
		Description:     description,
		TotalCost:       totalCost,
		Features:        features,
		StripeSessionID: stripeResp.ID,
		Status:          "draft",
	})
	if err != nil {
		log.Printf("failed to save draft quote: %v", err)
	}

	pendingQuotes.Store(stripeResp.ID, quoteID)
	log.Printf("checkout session %s created for %s (draft #%d)", stripeResp.ID, customerEmail, quoteID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"session_id": stripeResp.ID})
}

// verifyStripePayment checks whether a Stripe checkout session has been paid.
func verifyStripePayment(secretKey, sessionID string) (bool, error) {
	req, err := http.NewRequest("GET", "https://api.stripe.com/v1/checkout/sessions/"+sessionID, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(secretKey, "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var session struct {
		PaymentStatus string `json:"payment_status"`
		Error         struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return false, err
	}

	if resp.StatusCode != 200 {
		return false, fmt.Errorf("stripe API error %d: %s", resp.StatusCode, session.Error.Message)
	}

	return session.PaymentStatus == "paid", nil
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
	if redir := r.URL.Query().Get("redirect"); redir != "" {
		oauthRedirects.Store(state, redir)
	}
	opts := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline}
	if r.URL.Query().Get("prompt") == "select_account" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}
	url := googleOAuth.AuthCodeURL(state, opts...)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
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

// isAdmin returns true if the user is logged in and their email matches ADMIN_EMAIL.
func isAdmin(user *db.User) bool {
	if user == nil {
		return false
	}
	adminEmail := os.Getenv("ADMIN_EMAIL")
	return adminEmail != "" && strings.EqualFold(user.Email, adminEmail)
}

func handleAdmin(w http.ResponseWriter, r *http.Request) {
	user := getLoggedInUser(r)
	if user == nil {
		// Not signed in — redirect to Google sign-in
		http.Redirect(w, r, "/auth/google", http.StatusSeeOther)
		return
	}
	if !isAdmin(user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	groupsJSON, err := db.OptionGroupsJSON()
	if err != nil {
		log.Printf("admin: load groups: %v", err)
		http.Error(w, "failed to load options", http.StatusInternalServerError)
		return
	}
	baseCost, err := db.GetBaseCost()
	if err != nil {
		log.Printf("admin: load base cost: %v", err)
	}
	quotes, err := db.ListQuotes()
	if err != nil {
		log.Printf("admin: load quotes: %v", err)
	}
	bookings, err := db.ListBookings()
	if err != nil {
		log.Printf("admin: load bookings: %v", err)
	}

	render(w, r, "admin", struct {
		GroupsJSON template.JS
		BaseCost   int
		Quotes     []db.Quote
		Bookings   []db.Booking
	}{
		GroupsJSON: template.JS(groupsJSON),
		BaseCost:   baseCost,
		Quotes:     quotes,
		Bookings:   bookings,
	})
}

func handleAdminSave(w http.ResponseWriter, r *http.Request) {
	user := getLoggedInUser(r)
	if !isAdmin(user) {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

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
	Suburbs   []string     // service area
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
		email = "james@mchugh.au"
	}
	site = siteConfig{
		BaseURL:   base,
		Phone:     strings.TrimSpace(os.Getenv("PHONE")),
		Email:     email,
		OnsiteFee: envInt("ONSITE_FEE", 80),
		BlockRate: envInt("BLOCK_RATE", 30),
		Suburbs:   suburbs,
	}
	site.PhoneHref = template.URL(telHref(site.Phone))
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
