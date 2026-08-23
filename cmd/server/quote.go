package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	netmail "net/mail"
	"os"
	"strings"
	"time"

	"localithelp/db"
)

// ── Software quote flow ──
//
//	GET  /quote            public configurator (Cloudflare Turnstile on the form)
//	POST /api/quote        validate + Turnstile → store a *pending* quote → email a verification link
//	GET  /quote/sent       "check your inbox"
//	GET  /quote/verify?t=  confirm the email → generate the AI estimate once → notify admin + customer → show it
//
// No login and no payment. Abuse controls: Turnstile on submit, the email must be
// verified before any AI call is made, and only one verified quote is allowed per
// email address. A resubmission from an address that only has an unconfirmed
// (pending) quote replaces it and re-sends the link.

// quoteVerifyTTL is how long a verification link stays valid.
const quoteVerifyTTL = 48 * time.Hour

// quotePageData is the page-specific data for the public quote configurator.
type quotePageData struct {
	TurnstileSiteKey string
	Groups           []db.OptionGroup
	BaseCost         int
}

func handleQuote(w http.ResponseWriter, r *http.Request) {
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
	render(w, r, "quote", quotePageData{
		TurnstileSiteKey: turnstileSiteKey,
		Groups:           groups,
		BaseCost:         baseCost,
	})
}

// handleQuoteSubmit receives the configurator JSON, checks Turnstile, stores a
// pending quote and emails the customer a verification link.
func handleQuoteSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var formData map[string]any
	if err := json.NewDecoder(r.Body).Decode(&formData); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	str := func(k string) string { s, _ := formData[k].(string); return strings.TrimSpace(s) }

	name := str("name")
	email := strings.ToLower(str("email"))
	description := str("description")
	switch {
	case name == "" || len(name) > 120:
		jsonError(w, "Please enter your name", http.StatusBadRequest)
		return
	case !validEmail(email):
		jsonError(w, "Please enter a valid email address", http.StatusBadRequest)
		return
	case description == "" || len(description) > 5000:
		jsonError(w, "Please describe your project", http.StatusBadRequest)
		return
	}

	if !verifyTurnstile(str("cf-turnstile-response"), clientIP(r)) {
		jsonError(w, "Verification failed — please tick the box and try again", http.StatusBadRequest)
		return
	}

	// One verified quote per customer (each one costs an AI call). The owner's
	// own addresses are exempt so the flow can be tested repeatedly.
	if !isOwnerEmail(email) {
		dup, err := db.HasVerifiedQuote(email)
		if err != nil {
			log.Printf("quote: check email %s: %v", email, err)
			jsonError(w, "Something went wrong, please try again", http.StatusInternalServerError)
			return
		}
		if dup {
			jsonError(w, "You already have a quote from us for this email address, and we generate one per customer. Check your inbox for it, or email "+site.Email+" to discuss a new project.", http.StatusConflict)
			return
		}
	}
	// A fresh submission supersedes any earlier unconfirmed attempt.
	if err := db.DeletePendingQuotes(email); err != nil {
		log.Printf("quote: delete pending for %s: %v", email, err)
	}

	totalCost, _ := formData["total_cost"].(float64)
	features := make(map[string]any)
	for k, v := range formData {
		if strings.HasPrefix(k, "feature_") {
			features[k] = v
		}
	}
	quote := &db.Quote{
		Name:        name,
		Email:       email,
		Mobile:      str("mobile"),
		Address:     str("address"),
		Description: description,
		TotalCost:   totalCost,
		Features:    features,
		VerifyToken: generateSessionToken(),
	}
	id, err := db.InsertQuote(quote)
	if err != nil {
		log.Printf("quote: insert: %v", err)
		jsonError(w, "Something went wrong, please try again", http.StatusInternalServerError)
		return
	}
	quote.ID = id

	link := site.BaseURL + "/quote/verify?t=" + quote.VerifyToken
	if mail.Enabled() {
		sendQuoteVerification(quote, link)
	} else {
		// Local dev without SES: surface the link in the log so the flow can be exercised.
		log.Printf("quote #%d: email disabled — verification link: %s", id, link)
	}
	log.Printf("quote #%d pending for %s (%s)", id, email, clientIP(r))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "sent", "email": email})
}

// handleQuoteSent shows the "check your inbox" page after submission.
func handleQuoteSent(w http.ResponseWriter, r *http.Request) {
	render(w, r, "quote-sent", map[string]any{"Email": r.URL.Query().Get("e")})
}

// handleQuoteVerify is the target of the emailed link. The first visit confirms
// the address and generates the estimate; later visits just show the saved quote.
func handleQuoteVerify(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, "quote-result", map[string]any{"Error": msg})
	}
	token := r.URL.Query().Get("t")
	if len(token) != 64 {
		fail("That link isn't valid. Please use the link from your email, or request a new quote.")
		return
	}
	quote, err := db.GetQuoteByVerifyToken(token)
	if errors.Is(err, sql.ErrNoRows) {
		fail("That link isn't valid or has been replaced by a newer request. Please request a new quote.")
		return
	}
	if err != nil {
		log.Printf("quote verify: %v", err)
		fail("Something went wrong loading your quote. Please try again shortly.")
		return
	}

	if quote.Status != "pending" {
		// Already confirmed — the link doubles as a permalink to the quote.
		render(w, r, "quote-result", map[string]any{
			"Name": quote.Name, "AIEstimate": template.HTML(quote.AIEstimate), "Returning": true,
		})
		return
	}
	if time.Since(quote.CreatedAt) > quoteVerifyTTL {
		fail("That link has expired. Please request a new quote and confirm within 48 hours.")
		return
	}

	estimate := generateEstimate(quote)
	first, err := db.MarkQuoteVerified(quote.ID, estimate)
	if err != nil {
		log.Printf("quote #%d: mark verified: %v", quote.ID, err)
		fail("Something went wrong saving your quote. Please try again shortly.")
		return
	}
	if first {
		quote.Status = "verified"
		quote.AIEstimate = estimate
		log.Printf("quote #%d verified for %s", quote.ID, quote.Email)
		notifyQuoteVerified(quote, site.BaseURL+"/quote/verify?t="+quote.VerifyToken)
	}
	render(w, r, "quote-result", map[string]any{
		"Name": quote.Name, "AIEstimate": template.HTML(estimate),
	})
}

// generateEstimate asks Claude for an estimate; on any failure it returns a
// friendly placeholder so the customer still gets a page and James still gets
// the request.
func generateEstimate(quote *db.Quote) string {
	const fallback = "<p>The instant AI estimate could not be generated right now. James will review your requirements personally and include pricing in your proposal.</p>"
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fallback
	}
	formData := map[string]any{
		"description": quote.Description,
		"total_cost":  quote.TotalCost,
	}
	for k, v := range quote.Features {
		formData[k] = v
	}
	estimate, err := callClaude(apiKey, buildEstimatePrompt(formData))
	if err != nil {
		log.Printf("quote #%d: claude: %v", quote.ID, err)
		return fallback
	}
	return estimate
}

func buildEstimatePrompt(data map[string]any) string {
	var b strings.Builder
	b.WriteString("You are a project estimation assistant for an Australian web development business (Local IT Help, ABN 14 723 053 435). ")
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
	// Sonnet 5 thinks by default and max_tokens covers thinking + answer, so
	// give it headroom; the estimate itself is ~200 words. Low effort keeps
	// this quick task cheap.
	reqBody := map[string]any{
		"model":         "claude-sonnet-5",
		"max_tokens":    4096,
		"output_config": map[string]any{"effort": "low"},
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

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Error      struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, result.Error.Message)
	}

	if result.StopReason == "refusal" {
		return "", fmt.Errorf("Claude declined the request")
	}

	// Content may start with thinking blocks; return the first text block.
	for _, c := range result.Content {
		if c.Type == "text" && c.Text != "" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("empty response from Claude (stop_reason=%s)", result.StopReason)
}

// validEmail reports whether s looks like a single, plain email address.
func validEmail(s string) bool {
	if s == "" || len(s) > 254 {
		return false
	}
	addr, err := netmail.ParseAddress(s)
	return err == nil && addr.Address == s
}
