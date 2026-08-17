package db

import (
	"strings"
	"time"
	_ "time/tzdata" // static binary must not depend on the server's zoneinfo
)

// Melbourne is the business time zone. Booking start times are stored as
// local text ('YYYY-MM-DD HH:MM') in this zone.
var Melbourne = mustLoad("Australia/Melbourne")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("AEST", 10*3600)
	}
	return loc
}

const (
	startAtLayout = "2006-01-02 15:04"
	dateLayout    = "2006-01-02"
)

// ParseStartAt parses a stored booking start ('YYYY-MM-DD HH:MM', Melbourne
// local). Empty or malformed input yields the zero time.
func ParseStartAt(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation(startAtLayout, s, Melbourne)
	if err != nil {
		return time.Time{}
	}
	return t
}

// FormatStartAt renders t in Melbourne local time as stored text; the zero
// time renders as "" (unscheduled).
func FormatStartAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(Melbourne).Format(startAtLayout)
}

// ParseDate parses a stored 'YYYY-MM-DD' local date; empty → zero time.
func ParseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation(dateLayout, s, Melbourne)
	if err != nil {
		return time.Time{}
	}
	return t
}

// FormatDate renders t as a 'YYYY-MM-DD' Melbourne local date; zero → "".
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(Melbourne).Format(dateLayout)
}

// Today returns midnight today in Melbourne.
func Today() time.Time {
	now := time.Now().In(Melbourne)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, Melbourne)
}

// NormalizePhone reduces a phone number to digits only for matching
// ("+61 400 000 000" and "0400 000 000" both become "0400000000").
func NormalizePhone(p string) string {
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	d := b.String()
	// Fold the +61 international form onto the local 0-prefixed form so both match.
	if strings.HasPrefix(d, "61") && len(d) == 11 {
		d = "0" + d[2:]
	}
	return d
}
