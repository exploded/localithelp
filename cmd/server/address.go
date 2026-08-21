package main

// Address autocomplete, backed by Mappify (mappify.io) — the same service
// a-part uses. The user types a few characters, we proxy the query to Mappify
// and return JSON suggestions; picking one fills hidden structured fields so
// the submit handlers can insist on a complete street + suburb + state +
// postcode. Only Victorian addresses are suggested or accepted — anything else
// is outside the service area. Two endpoints share the lookup: an admin one
// (behind requireAdmin) and a public one for /book with a per-IP rate limit so
// the paid API key can't be drained anonymously. Needs MAPPIFY_API_KEY in
// .env; without it searches return no suggestions.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

type addrSuggestion struct {
	Label    string `json:"label"` // "12 Smith St, Donvale VIC 3111"
	Street   string `json:"street"`
	Suburb   string `json:"suburb"`
	State    string `json:"state"`
	Postcode string `json:"postcode"`
}

var mappifyClient = &http.Client{Timeout: 6 * time.Second}

var rePostcode = regexp.MustCompile(`^\d{4}$`)

func handleAdminAddressSearch(w http.ResponseWriter, r *http.Request) {
	serveAddressSearch(w, r)
}

// handleBookAddressSearch is the public autocomplete for /book, rate-limited
// per IP because each lookup spends paid Mappify quota.
func handleBookAddressSearch(w http.ResponseWriter, r *http.Request) {
	if addressRateLimited(clientIP(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	serveAddressSearch(w, r)
}

func serveAddressSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 3 {
		io.WriteString(w, "[]")
		return
	}
	out, err := mappifySuggestions(q)
	if err != nil {
		log.Printf("mappify: %v", err)
		io.WriteString(w, "[]")
		return
	}
	json.NewEncoder(w).Encode(out)
}

// mappifySuggestions queries Mappify and returns up to 8 Victorian address
// suggestions. Returns an empty (non-nil) slice when nothing matches.
func mappifySuggestions(query string) ([]addrSuggestion, error) {
	key := os.Getenv("MAPPIFY_API_KEY")
	if key == "" {
		return []addrSuggestion{}, nil
	}
	body, _ := json.Marshal(map[string]any{
		"streetAddress": query, "formatCase": true, "boostPrefix": true, "apiKey": key,
	})
	resp, err := mappifyClient.Post("https://mappify.io/api/rpc/address/autocomplete", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var mr struct {
		Result []struct {
			StreetAddress string `json:"streetAddress"`
			Suburb        string `json:"suburb"`
			State         string `json:"state"`
			PostCode      string `json:"postCode"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]addrSuggestion, 0, 8)
	for _, a := range mr.Result {
		// Victorian addresses only — everything else is outside the service area.
		if !strings.EqualFold(strings.TrimSpace(a.State), "VIC") {
			continue
		}
		street := a.StreetAddress
		// Mappify repeats ", SUBURB STATE POSTCODE" at the end of streetAddress.
		suffix := ", " + a.Suburb + " " + a.State + " " + a.PostCode
		if strings.HasSuffix(strings.ToUpper(street), strings.ToUpper(suffix)) {
			street = street[:len(street)-len(suffix)]
		}
		suburb := suburbDisplayName(a.Suburb)
		out = append(out, addrSuggestion{
			Label:  street + ", " + suburb + " VIC " + a.PostCode,
			Street: street, Suburb: suburb, State: "VIC", Postcode: a.PostCode,
		})
		if len(out) == 8 {
			break
		}
	}
	return out, nil
}

// validAddressParts reports whether the hidden addr_* fields describe a
// complete Victorian address. reason is a user-facing error when not.
func validAddressParts(street, suburb, state, postcode string) (reason string, ok bool) {
	if street == "" || suburb == "" || state == "" || !rePostcode.MatchString(postcode) {
		return "Please pick the address from the suggestions — it needs a street, suburb, state and postcode.", false
	}
	if state != "VIC" {
		return "That address is in " + state + " — outside our service area (Victoria only).", false
	}
	return "", true
}

// suburbDisplayName maps Mappify's ALL-CAPS suburb to the catalogue's display
// name when it's in the service area, otherwise just title-cases it.
func suburbDisplayName(s string) string {
	s = strings.TrimSpace(s)
	if sb, ok := findSuburbByName(s); ok {
		return sb.Name
	}
	words := strings.Fields(strings.ToLower(s))
	for i, wd := range words {
		rs := []rune(wd)
		rs[0] = unicode.ToUpper(rs[0])
		words[i] = string(rs)
	}
	return strings.Join(words, " ")
}

// ── Public search rate limit ──

var addressHits sync.Map // ip -> []time.Time

const (
	addressRateWindow = time.Hour
	addressRateMax    = 200 // ~10-30 keystroke lookups per address; plenty for real users
)

func addressRateLimited(ip string) bool {
	now := time.Now()
	var recent []time.Time
	if v, ok := addressHits.Load(ip); ok {
		for _, t := range v.([]time.Time) {
			if now.Sub(t) < addressRateWindow {
				recent = append(recent, t)
			}
		}
	}
	if len(recent) >= addressRateMax {
		addressHits.Store(ip, recent)
		return true
	}
	addressHits.Store(ip, append(recent, now))
	return false
}
