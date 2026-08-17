package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mchugh.com.au/db"
)

// requireAdmin gates a handler behind Google sign-in + the ADMIN_EMAIL check.
// Anonymous GETs bounce through Google and come back; anything else gets a
// 401/403. Mutating requests must also carry the session's CSRF token, either
// as the "csrf" form field or the X-CSRF-Token header.
func requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := getSession(r)
		if sess == nil {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				http.Redirect(w, r, "/auth/google?redirect="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
				return
			}
			http.Error(w, "sign in required", http.StatusUnauthorized)
			return
		}
		if !isAdmin(sess.User) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			tok := r.Header.Get("X-CSRF-Token")
			if tok == "" {
				// ParseForm is a no-op for JSON bodies and cheap for forms; the
				// 64 KiB cap matches the booking form.
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
				if err := r.ParseForm(); err == nil {
					tok = r.FormValue("csrf")
				}
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(sess.CSRF)) != 1 {
				http.Error(w, "invalid or missing CSRF token — reload the page and try again", http.StatusForbidden)
				return
			}
		}
		h(w, r)
	}
}

// pathID parses the {id} path value as an int64 (0 when absent/invalid).
func pathID(r *http.Request) int64 {
	n, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return n
}

// ── Formatting helpers shared by templates, mail and PDF ──

// fmtCents renders integer cents as "$1,234.50".
func fmtCents(c int64) string {
	neg := c < 0
	if neg {
		c = -c
	}
	d, f := c/100, c%100
	s := strconv.FormatInt(d, 10)
	var out []byte
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(ch))
	}
	res := "$" + string(out) + fmt.Sprintf(".%02d", f)
	if neg {
		return "-" + res
	}
	return res
}

// dollarsToCents parses a user-typed dollar amount ("129.95", "$1,200") to cents.
func dollarsToCents(s string) (int64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(s), "$"), ",", ""))
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	var f int64
	if frac != "" {
		if len(frac) > 2 {
			frac = frac[:2]
		}
		for len(frac) < 2 {
			frac += "0"
		}
		if f, err = strconv.ParseInt(frac, 10, 64); err != nil {
			return 0, err
		}
	}
	c := w*100 + f
	if neg {
		c = -c
	}
	return c, nil
}

// seq returns [0, n) for template loops.
func seq(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// startOfWeek returns the Monday 00:00 (Melbourne) on or before t.
func startOfWeek(t time.Time) time.Time {
	t = t.In(db.Melbourne)
	wd := int(t.Weekday()) // Sunday = 0
	if wd == 0 {
		wd = 7
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, db.Melbourne)
	return d.AddDate(0, 0, -(wd - 1))
}

// fmtWhen renders a booking start like "Thu 20 Aug, 9:30 am" ("" when unscheduled).
func fmtWhen(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(db.Melbourne)
	return t.Format("Mon 2 Jan") + ", " + fmtClock(t)
}

// fmtWhenLong renders "Thursday 20 August 2026, 9:30 am".
func fmtWhenLong(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(db.Melbourne)
	return t.Format("Monday 2 January 2006") + ", " + fmtClock(t)
}

// fmtClock renders "9:30 am" / "2:00 pm".
func fmtClock(t time.Time) string {
	return strings.ToLower(t.In(db.Melbourne).Format("3:04 pm"))
}

// fmtDate renders "20 Aug 2026" ("" for zero).
func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(db.Melbourne).Format("2 Jan 2006")
}

// fmtDuration renders minutes as "45 min" / "1 hr" / "1 hr 30 min".
func fmtDuration(min int) string {
	h, m := min/60, min%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d hr", h)
	default:
		return fmt.Sprintf("%d hr %d min", h, m)
	}
}
