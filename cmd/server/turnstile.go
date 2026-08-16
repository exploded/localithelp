package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ── Cloudflare Turnstile ──
//
// Configured by TURNSTILE_SITE_KEY (rendered into the quote form) and
// TURNSTILE_SECRET_KEY (used server-side to verify the token). When the secret
// is unset the check is skipped — fine for local dev, loudly logged at startup.
// Cloudflare's always-pass test pair for development:
//   site   1x00000000000000000000AA
//   secret 1x0000000000000000000000000000000AA

var (
	turnstileSiteKey string
	turnstileSecret  string
)

func initTurnstile() {
	turnstileSiteKey = strings.TrimSpace(os.Getenv("TURNSTILE_SITE_KEY"))
	turnstileSecret = strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY"))
	switch {
	case turnstileSiteKey != "" && turnstileSecret != "":
		log.Println("turnstile: enabled")
	case turnstileSiteKey == "" && turnstileSecret == "":
		log.Println("turnstile: not configured (set TURNSTILE_SITE_KEY/TURNSTILE_SECRET_KEY) — quote form is unprotected")
	default:
		log.Println("turnstile: WARNING only one of TURNSTILE_SITE_KEY / TURNSTILE_SECRET_KEY is set — check .env")
	}
}

var turnstileClient = &http.Client{Timeout: 10 * time.Second}

// verifyTurnstile validates a widget response token with Cloudflare. It fails
// closed on network/API errors — a quote submission is not urgent and a retry
// is cheap, whereas silently letting bots through defeats the point.
func verifyTurnstile(token, remoteIP string) bool {
	if turnstileSecret == "" {
		return true // not configured
	}
	if token == "" || len(token) > 2048 {
		return false
	}
	form := url.Values{"secret": {turnstileSecret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	resp, err := turnstileClient.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", form)
	if err != nil {
		log.Printf("turnstile: siteverify: %v", err)
		return false
	}
	defer resp.Body.Close()
	var out struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Printf("turnstile: decode: %v", err)
		return false
	}
	if !out.Success {
		log.Printf("turnstile: rejected (%s): %v", remoteIP, out.ErrorCodes)
	}
	return out.Success
}
