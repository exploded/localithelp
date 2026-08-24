package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"localithelp/db"
)

// Google Calendar sync. Bookings are pushed to a dedicated secondary calendar
// on the admin's account; everything else on that account comes back as
// anonymous busy intervals for the admin calendar view. See "Google Calendar
// sync" in the README.
//
// The connect flow is deliberately separate from admin sign-in: signing in asks
// only for openid/email/profile, and the calendar scope is granted once from
// /admin/calendar/settings.

// calendarScope covers events, the calendar list and free/busy on the user's
// own calendars.
const calendarScope = "https://www.googleapis.com/auth/calendar"

// calendarName is the secondary calendar the app creates and writes to.
const calendarName = "Local IT Help"

// gcalTimeout bounds every Google call. The admin calendar page must never hang
// on Google, so the busy query uses a shorter budget (see busyTimeout).
const (
	gcalTimeout = 10 * time.Second
	busyTimeout = 3 * time.Second
)

// gcalOAuth is the OAuth config for the calendar scope. It shares the client
// credentials with sign-in but uses its own redirect URI, so consent for the
// calendar never leaks into the login flow. nil when GOOGLE_CLIENT_ID is unset.
var gcalOAuth *oauth2.Config

func initGCalOAuth() {
	cid := os.Getenv("GOOGLE_CLIENT_ID")
	if cid == "" {
		return
	}
	gcalOAuth = &oauth2.Config{
		ClientID:     cid,
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  site.BaseURL + "/auth/google/calendar/callback",
		Scopes:       []string{calendarScope},
		Endpoint:     google.Endpoint,
	}
}

// ── API surface ──

// gcalEvent is the subset of a Google Calendar event the app reads and writes.
type gcalEvent struct {
	ID                 string           `json:"id,omitempty"`
	Summary            string           `json:"summary,omitempty"`
	Description        string           `json:"description,omitempty"`
	Location           string           `json:"location,omitempty"`
	Status             string           `json:"status,omitempty"`
	Start              *gcalEventTime   `json:"start,omitempty"`
	End                *gcalEventTime   `json:"end,omitempty"`
	Reminders          *gcalReminders   `json:"reminders,omitempty"`
	ExtendedProperties *gcalExtProps    `json:"extendedProperties,omitempty"`
	Source             *gcalEventSource `json:"source,omitempty"`
}

type gcalEventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type gcalReminders struct {
	UseDefault bool `json:"useDefault"`
}

type gcalExtProps struct {
	Private map[string]string `json:"private,omitempty"`
}

type gcalEventSource struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// gcalCalendar is one entry from the user's calendar list.
type gcalCalendar struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Primary  bool   `json:"primary"`
	Selected bool   `json:"selected"`
	Deleted  bool   `json:"deleted"`
	Role     string `json:"accessRole"`
}

// busyInterval is a single busy block from freebusy.query. Titles are never
// requested, so nothing but the times crosses over.
type busyInterval struct {
	Start time.Time
	End   time.Time
}

// calendarAPI is the slice of the Calendar v3 API the app uses. The tests
// substitute a fake.
type calendarAPI interface {
	InsertEvent(ctx context.Context, calendarID string, ev *gcalEvent) (string, error)
	PatchEvent(ctx context.Context, calendarID, eventID string, ev *gcalEvent) error
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
	ListCalendars(ctx context.Context) ([]gcalCalendar, error)
	CreateCalendar(ctx context.Context, summary, timeZone string) (string, error)
	FreeBusy(ctx context.Context, calendarIDs []string, from, to time.Time) ([]busyInterval, error)
}

// errNotFound is returned when Google reports 404/410 for an event. Deleting an
// event that is already gone counts as success.
var errNotFound = fmt.Errorf("calendar: event not found")

// ── HTTP implementation ──

const calendarBase = "https://www.googleapis.com/calendar/v3"

type gcalClient struct{ hc *http.Client }

// newCalendarClient builds an authorised client from the stored refresh token.
// oauth2 refreshes the access token itself, so nothing else is cached.
func newCalendarClient(g *db.GoogleCalendar) (calendarAPI, error) {
	if gcalOAuth == nil {
		return nil, fmt.Errorf("calendar: GOOGLE_CLIENT_ID is not set")
	}
	if g == nil || g.RefreshToken == "" {
		return nil, fmt.Errorf("calendar: not connected")
	}
	src := gcalOAuth.TokenSource(context.Background(), &oauth2.Token{RefreshToken: g.RefreshToken})
	return &gcalClient{hc: oauth2.NewClient(context.Background(), src)}, nil
}

// do performs one API call. body is JSON-encoded when non-nil; out is decoded
// when non-nil.
func (c *gcalClient) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, calendarBase+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return errNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("calendar: %s %s: %s: %s", method, path, resp.Status, googleErrMessage(msg))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// googleErrMessage pulls the human-readable message out of a Google error body,
// falling back to the raw text.
func googleErrMessage(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return strings.TrimSpace(string(body))
}

func (c *gcalClient) InsertEvent(ctx context.Context, calendarID string, ev *gcalEvent) (string, error) {
	var out gcalEvent
	err := c.do(ctx, http.MethodPost, "/calendars/"+url.PathEscape(calendarID)+"/events", ev, &out)
	return out.ID, err
}

func (c *gcalClient) PatchEvent(ctx context.Context, calendarID, eventID string, ev *gcalEvent) error {
	return c.do(ctx, http.MethodPatch,
		"/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID), ev, nil)
}

func (c *gcalClient) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	return c.do(ctx, http.MethodDelete,
		"/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID), nil, nil)
}

func (c *gcalClient) ListCalendars(ctx context.Context) ([]gcalCalendar, error) {
	var out struct {
		Items []gcalCalendar `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/users/me/calendarList?minAccessRole=freeBusyReader&maxResults=250", nil, &out); err != nil {
		return nil, err
	}
	live := make([]gcalCalendar, 0, len(out.Items))
	for _, cal := range out.Items {
		if !cal.Deleted {
			live = append(live, cal)
		}
	}
	return live, nil
}

func (c *gcalClient) CreateCalendar(ctx context.Context, summary, timeZone string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]string{"summary": summary, "timeZone": timeZone}
	err := c.do(ctx, http.MethodPost, "/calendars", body, &out)
	return out.ID, err
}

func (c *gcalClient) FreeBusy(ctx context.Context, calendarIDs []string, from, to time.Time) ([]busyInterval, error) {
	if len(calendarIDs) == 0 {
		return nil, nil
	}
	type item struct {
		ID string `json:"id"`
	}
	items := make([]item, 0, len(calendarIDs))
	for _, id := range calendarIDs {
		items = append(items, item{ID: id})
	}
	body := map[string]any{
		"timeMin": from.UTC().Format(time.RFC3339),
		"timeMax": to.UTC().Format(time.RFC3339),
		"items":   items,
	}
	var out struct {
		Calendars map[string]struct {
			Busy []struct {
				Start string `json:"start"`
				End   string `json:"end"`
			} `json:"busy"`
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"calendars"`
	}
	if err := c.do(ctx, http.MethodPost, "/freeBusy", body, &out); err != nil {
		return nil, err
	}
	var busy []busyInterval
	for _, cal := range out.Calendars {
		for _, b := range cal.Busy {
			st, err1 := time.Parse(time.RFC3339, b.Start)
			en, err2 := time.Parse(time.RFC3339, b.End)
			if err1 != nil || err2 != nil || !en.After(st) {
				continue
			}
			busy = append(busy, busyInterval{Start: st, End: en})
		}
	}
	return busy, nil
}

// ── Connect / disconnect ──

// gcalClientFor builds a client for the stored connection. Overridable in tests.
var gcalClientFor = func(g *db.GoogleCalendar) (calendarAPI, error) { return newCalendarClient(g) }

func handleGCalConnect(w http.ResponseWriter, r *http.Request) {
	if gcalOAuth == nil {
		redirectMsg(w, r, "/admin/calendar/settings", "err", "Google OAuth is not configured on this server.")
		return
	}
	state := generateSessionToken()
	oauthStates.Store(state, time.Now().Add(10*time.Minute))
	// access_type=offline + prompt=consent guarantees a refresh token even when
	// the account has authorised this client before.
	authURL := gcalOAuth.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("include_granted_scopes", "true"),
		oauth2.SetAuthURLParam("login_hint", adminEmail()))
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func handleGCalCallback(w http.ResponseWriter, r *http.Request) {
	const back = "/admin/calendar/settings"
	if gcalOAuth == nil {
		http.Error(w, "Google OAuth is not configured", http.StatusServiceUnavailable)
		return
	}
	val, ok := oauthStates.LoadAndDelete(r.URL.Query().Get("state"))
	if !ok {
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}
	if time.Now().After(val.(time.Time)) {
		http.Error(w, "state expired", http.StatusBadRequest)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		redirectMsg(w, r, back, "err", "Google declined the request: "+errParam)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), gcalTimeout)
	defer cancel()

	tok, err := gcalOAuth.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		logGCal("token exchange", err)
		redirectMsg(w, r, back, "err", "Could not complete the Google connection.")
		return
	}
	if tok.RefreshToken == "" {
		redirectMsg(w, r, back, "err", "Google did not return a refresh token. Remove this app at myaccount.google.com/permissions and connect again.")
		return
	}

	// The connected account must be the admin's own — a different account would
	// push visits into someone else's diary.
	email, err := googleAccountEmail(ctx, gcalOAuth.Client(ctx, tok))
	if err != nil {
		logGCal("userinfo", err)
		redirectMsg(w, r, back, "err", "Could not read the Google account details.")
		return
	}
	if !strings.EqualFold(email, adminEmail()) {
		redirectMsg(w, r, back, "err", "That is "+email+", not the admin account ("+adminEmail()+"). Sign out of Google and try again.")
		return
	}

	// Find (or create) the dedicated secondary calendar. A separate calendar
	// keeps bookings colour-coded, easy to hide, and excludable from busy times.
	api, err := newCalendarClient(&db.GoogleCalendar{RefreshToken: tok.RefreshToken})
	if err != nil {
		logGCal("client", err)
		redirectMsg(w, r, back, "err", err.Error())
		return
	}
	calID, err := findOrCreateBookingCalendar(ctx, api)
	if err != nil {
		logGCal("find/create calendar", err)
		redirectMsg(w, r, back, "err", "Connected, but could not set up the "+calendarName+" calendar: "+err.Error())
		return
	}
	if err := db.SaveGoogleCalendar(email, tok.RefreshToken, calID, calendarName); err != nil {
		logGCal("save connection", err)
		redirectMsg(w, r, back, "err", "Could not save the connection.")
		return
	}

	// Push existing visits straight away so the calendar is useful immediately.
	go resyncAllBookings()
	redirectMsg(w, r, back, "ok", "Connected as "+email+". Existing visits are syncing now.")
}

// findOrCreateBookingCalendar returns the id of the app's calendar, creating it
// when the account doesn't have one yet.
func findOrCreateBookingCalendar(ctx context.Context, api calendarAPI) (string, error) {
	cals, err := api.ListCalendars(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range cals {
		if strings.EqualFold(strings.TrimSpace(c.Summary), calendarName) && c.Role == "owner" {
			return c.ID, nil
		}
	}
	return api.CreateCalendar(ctx, calendarName, db.Melbourne.String())
}

// googleAccountEmail reads the signed-in account's email address.
func googleAccountEmail(ctx context.Context, hc *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo: %s", resp.Status)
	}
	var u struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	if u.Email == "" {
		return "", fmt.Errorf("userinfo: no email in response")
	}
	return u.Email, nil
}

func handleGCalDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := db.DisconnectGoogleCalendar(); err != nil {
		logGCal("disconnect", err)
		redirectMsg(w, r, "/admin/calendar/settings", "err", "Could not disconnect.")
		return
	}
	busyCache.clear()
	redirectMsg(w, r, "/admin/calendar/settings", "ok",
		"Disconnected. Visits already in Google Calendar were left there — delete them by hand if you don't want them.")
}

// adminEmail returns the Google account allowed into /admin, matching isAdmin.
func adminEmail() string {
	if e := strings.TrimSpace(os.Getenv("ADMIN_EMAIL")); e != "" {
		return e
	}
	return "james67@gmail.com"
}

// logGCal logs a calendar failure and records it for the settings page. Sync
// problems are never surfaced in the admin's save/status flash messages —
// a booking must save even when Google is unreachable.
func logGCal(what string, err error) {
	if err == nil {
		return
	}
	msg := what + ": " + err.Error()
	log.Printf("gcal: %s", msg)
	if e := db.SetGoogleCalendarError(msg); e != nil {
		log.Printf("gcal: record error: %v", e)
	}
}

// ── Busy times ──

// busyCache memoises freebusy.query results per week so paging back and forth
// through the calendar doesn't hammer Google.
var busyCache = &busyCacheT{ttl: 2 * time.Minute}

type busyCacheT struct {
	mu   sync.Mutex
	ttl  time.Duration
	key  string
	at   time.Time
	vals []busyInterval
}

func (c *busyCacheT) get(key string, now time.Time) ([]busyInterval, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != key || now.Sub(c.at) > c.ttl {
		return nil, false
	}
	return c.vals, true
}

func (c *busyCacheT) put(key string, now time.Time, vals []busyInterval) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.at, c.vals = key, now, vals
}

func (c *busyCacheT) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.vals = "", nil
}

// busyIntervals returns busy blocks from every calendar on the admin's account
// except the app's own calendar and any the admin has excluded. It returns
// (nil, nil) when sync isn't connected, and never blocks longer than
// busyTimeout — the admin calendar page renders without busy times on failure.
//
// Kept separate from the calendar view so the customer-facing slot picker
// (TODO 12) can reuse it.
func busyIntervals(from, to time.Time) ([]busyInterval, error) {
	g, err := db.GetGoogleCalendar()
	if err != nil {
		return nil, err
	}
	if !g.Connected() {
		return nil, nil
	}
	key := from.Format(time.RFC3339) + "/" + to.Format(time.RFC3339)
	if vals, ok := busyCache.get(key, time.Now()); ok {
		return vals, nil
	}

	api, err := gcalClientFor(g)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), busyTimeout)
	defer cancel()

	cals, err := api.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cals))
	for _, c := range cals {
		// Skip the app's own calendar — its events are the bookings already
		// drawn on the page — and anything the admin has excluded.
		if c.ID == g.CalendarID || g.Skipped(c.ID) {
			continue
		}
		ids = append(ids, c.ID)
	}
	busy, err := api.FreeBusy(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	busyCache.put(key, time.Now(), busy)
	return busy, nil
}
