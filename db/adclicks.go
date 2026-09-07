package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"localithelp/db/sqlc"
)

// Ad clicks record what a paid click carried when it landed, so a booking can
// be traced back to the keyword that bought it. The click is written on the
// landing page view and claimed later, when and if the visitor books - most
// never do, and those unclaimed rows are the point: they're the denominator
// for "how many clicks does a job cost?".

// AdClick is one paid click on its way through the site.
type AdClick struct {
	ID        int64
	Token     string // the lih_click cookie value that points back here
	Source    string // same vocabulary as Booking.Source
	GCLID     string
	Keyword   string // the Ads keyword that matched, not what was typed
	Campaign  string
	Landing   string // path only
	BookingID int64  // 0 until the click turns into a booking
	CreatedAt time.Time
}

func sqlcAdClick(r sqlc.AdClick) AdClick {
	return AdClick{
		ID: r.ID, Token: r.Token, Source: r.Source, GCLID: r.Gclid, Keyword: r.Keyword,
		Campaign: r.Campaign, Landing: r.Landing, BookingID: r.BookingID, CreatedAt: parseUTC(r.CreatedAt),
	}
}

// RecordAdClick stores a click. The caller mints the token.
func RecordAdClick(c AdClick) error {
	return q.InsertAdClick(context.Background(), sqlc.InsertAdClickParams{
		Token: c.Token, Source: c.Source, Gclid: c.GCLID,
		Keyword: c.Keyword, Campaign: c.Campaign, Landing: c.Landing,
	})
}

// LinkAdClick points a stored click at the booking it produced. It reports
// false when the token is unknown or the click was already spent on an earlier
// booking, so a second booking from the same browser can't steal the credit.
func LinkAdClick(token string, bookingID int64) (bool, error) {
	n, err := q.LinkAdClick(context.Background(), sqlc.LinkAdClickParams{BookingID: bookingID, Token: token})
	return n == 1, err
}

// GetAdClickByToken returns the click a cookie points at, or nil when there
// isn't one.
func GetAdClickByToken(token string) (*AdClick, error) {
	r, err := q.GetAdClickByToken(context.Background(), token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := sqlcAdClick(r)
	return &c, nil
}

// GetAdClickByBooking returns the click a booking came from, or nil when the
// booking wasn't the result of one.
func GetAdClickByBooking(bookingID int64) (*AdClick, error) {
	if bookingID == 0 {
		return nil, nil
	}
	r, err := q.GetAdClickByBooking(context.Background(), bookingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := sqlcAdClick(r)
	return &c, nil
}

// CountAdClicksSince counts clicks recorded on or after the given UTC time -
// the "32 clicks, 1 booking" denominator.
func CountAdClicksSince(t time.Time) (int, error) {
	n, err := q.CountAdClicksSince(context.Background(), t.UTC().Format("2006-01-02 15:04:05"))
	return int(n), err
}
