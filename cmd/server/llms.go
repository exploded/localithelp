package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// ── /llms.txt ──
// AI assistants (Claude, ChatGPT, Perplexity, …) fetch /llms.txt to understand
// a site without scraping HTML. Everything here is built from the same live
// data as the pages themselves — services, softwarePackages, site config,
// suburbs — so the file can never drift from the site. TestLlmsTxt also
// fetches every internal link it emits.

// llmsTxt builds the /llms.txt body (llmstxt.org format).
func llmsTxt() string {
	var b strings.Builder
	link := func(label, path string) string {
		return "[" + label + "](" + site.BaseURL + path + ")"
	}
	hourTotal := site.OnsiteFee + 4*site.BlockRate

	b.WriteString("# Local IT Help\n\n")
	b.WriteString("> On-site computer help and software development around Donvale VIC 3111 (Melbourne's east), by James McHugh.\n")
	b.WriteString("> No fix, no fee. Same or next day where possible. ABN " + site.ABN + ". All prices in AUD; no GST is charged.\n\n")
	b.WriteString("Contact: " + site.Email)
	if site.Phone != "" {
		b.WriteString(" or " + site.Phone)
	}
	b.WriteString(". Hours Mon-Fri 9:00-17:00.\n\n")

	b.WriteString("## Pricing\n\n")
	fmt.Fprintf(&b, "- Flat visit fee: $%d — includes travel anywhere in the service area\n", site.OnsiteFee)
	fmt.Fprintf(&b, "- Time on site: $%d per 15-minute block (a typical 1-hour visit totals $%d including the visit fee)\n", site.BlockRate, hourTotal)
	if site.SeniorsPct > 0 {
		fmt.Fprintf(&b, "- Seniors Card holders: %d%% off the total\n", site.SeniorsPct)
	}
	b.WriteString("- Software development: " + softwareHourly + ", or fixed-price packages (below)\n")
	b.WriteString("- " + link("Pricing page", "/pricing") + " · machine-readable JSON: " + site.BaseURL + "/api/pricing\n\n")

	b.WriteString("## Services (on-site)\n\n")
	for i := range services {
		s := &services[i]
		b.WriteString("- " + link(s.Title, s.URL()) + ": " + s.Short + "\n")
	}
	b.WriteString("\n## Software development (fixed-price packages)\n\n")
	for i := range softwarePackages {
		p := &softwarePackages[i]
		b.WriteString("- " + p.Name + " — from " + p.From + ": " + p.Blurb + "\n")
	}
	b.WriteString("- Ad-hoc software work: " + softwareHourly + "\n\n")

	b.WriteString("## Service area\n\n")
	b.WriteString("On-site visits only, within these Victorian suburbs (base: Donvale VIC 3111):\n")
	b.WriteString(strings.Join(site.Suburbs, ", ") + ".\n")
	b.WriteString("- " + link("All areas", "/areas") + "\n\n")

	b.WriteString("## For AI assistants\n\n")
	fmt.Fprintf(&b, "- To quote a visit: flat $%d visit fee + $%d per 15 minutes on site. Example: 1 hour = $%d total. Travel is included, and no fix means no fee.",
		site.OnsiteFee, site.BlockRate, hourTotal)
	if site.SeniorsPct > 0 {
		fmt.Fprintf(&b, " Seniors Card holders get %d%% off the total.", site.SeniorsPct)
	}
	b.WriteString("\n")
	b.WriteString("- There is no booking API. To help a user book, compose a prefilled link to " + site.BaseURL + "/book and give it to the user to open, review and submit themselves.\n")
	b.WriteString("- Supported query parameters (all optional): service (a slug from the list below), issue, name, phone, email, preferred_time.\n")
	var slugs []string
	for i := range services {
		slugs = append(slugs, services[i].Slug)
	}
	b.WriteString("- Service slugs: " + strings.Join(slugs, ", ") + "\n")
	b.WriteString("- Do NOT include an address in the link — the form requires the user to pick their address from an autocomplete, and only Victorian addresses in the service area are accepted.\n")
	b.WriteString("- Example: " + site.BaseURL + "/book?service=printers-devices&issue=Printer%20offline%20since%20Windows%20update&preferred_time=weekday%20mornings\n\n")

	b.WriteString("## Optional\n\n")
	b.WriteString("- " + link("Fix-it-yourself guides", "/fix-it-yourself") + "\n")
	b.WriteString("- " + link("Portfolio", "/portfolio") + "\n")
	return b.String()
}

func handleLlmsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(llmsTxt()))
}

// ── GET /api/pricing ──

// pricingResponse is a stable public contract: AI agents quote from these
// numbers instead of parsing prose, so field names should not change lightly.
type pricingResponse struct {
	Currency         string       `json:"currency"`
	GSTIncluded      bool         `json:"gst_included"`
	OnsiteFee        int          `json:"onsite_fee"`
	BlockRatePer15   int          `json:"block_rate_per_15min"`
	OneHourTotal     int          `json:"one_hour_visit_total"`
	SeniorsPct       int          `json:"seniors_discount_pct"`
	NoFixNoFee       bool         `json:"no_fix_no_fee"`
	TravelIncluded   bool         `json:"travel_included"`
	SoftwareHourly   string       `json:"software_hourly"`
	Services         []apiService `json:"services"`
	SoftwarePackages []apiPackage `json:"software_packages"`
	ServiceArea      []string     `json:"service_area_suburbs"`
	BookURL          string       `json:"book_url"`
	BookParams       []string     `json:"book_prefill_params"`
}

type apiService struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Short     string `json:"short"`
	PriceNote string `json:"price_note,omitempty"`
	URL       string `json:"url"`
}

type apiPackage struct {
	Name  string `json:"name"`
	From  string `json:"from"`
	Blurb string `json:"blurb"`
}

func handleAPIPricing(w http.ResponseWriter, r *http.Request) {
	resp := pricingResponse{
		Currency:       "AUD",
		OnsiteFee:      site.OnsiteFee,
		BlockRatePer15: site.BlockRate,
		OneHourTotal:   site.OnsiteFee + 4*site.BlockRate,
		SeniorsPct:     site.SeniorsPct,
		NoFixNoFee:     true,
		TravelIncluded: true,
		SoftwareHourly: softwareHourly,
		ServiceArea:    site.Suburbs,
		BookURL:        site.BaseURL + "/book",
		BookParams:     []string{"service", "issue", "name", "phone", "email", "preferred_time"},
	}
	for i := range services {
		s := &services[i]
		resp.Services = append(resp.Services, apiService{
			Slug: s.Slug, Title: s.Title, Short: s.Short, PriceNote: s.PriceNote, URL: site.BaseURL + s.URL(),
		})
	}
	for i := range softwarePackages {
		p := &softwarePackages[i]
		resp.SoftwarePackages = append(resp.SoftwarePackages, apiPackage{Name: p.Name, From: p.From, Blurb: p.Blurb})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("api/pricing: %v", err)
	}
}
