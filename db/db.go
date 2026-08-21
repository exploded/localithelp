package db

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"mchugh.com.au/db/sqlc"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

var (
	conn *sql.DB
	q    *sqlc.Queries
)

// ── Domain types (preserve existing API for main.go / templates) ──

// Quote is a software-quote request. Lifecycle:
//
//	pending  — submitted, awaiting the customer clicking the email verification link
//	verified — email confirmed, AI estimate generated, admin notified
//
// (Legacy rows from the old Stripe flow may carry "draft" / "paid".)
type Quote struct {
	ID          int64
	Name        string
	Email       string // stored lower-cased; one verified quote per email
	Mobile      string
	Address     string
	Description string
	TotalCost   float64
	Features    map[string]any
	AIEstimate  string
	VerifyToken string    // random token in the verification link; also acts as the view-quote permalink
	VerifiedAt  time.Time // zero until verified
	Status      string
	CreatedAt   time.Time
}

type User struct {
	ID        int64
	GoogleID  string
	Email     string
	Name      string
	Picture   string
	CreatedAt string
}

// OptionGroup represents a category of quote options (e.g. "Email", "SMS").
type OptionGroup struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`  // form field name, e.g. "feature_email"
	Label     string   `json:"label"` // display label, e.g. "Email"
	Hint      string   `json:"hint"`  // optional hint text below label
	SortOrder int      `json:"sort_order"`
	Options   []Option `json:"options"`
}

// Option represents a single choice within an option group.
type Option struct {
	ID        int64  `json:"id"`
	GroupID   int64  `json:"group_id"`
	Value     string `json:"value"`      // form value, e.g. "none", "send_only"
	Name      string `json:"name"`       // display name, e.g. "None", "Send only"
	Cost      int    `json:"cost"`       // cost in dollars (used for calculation)
	CostLabel string `json:"cost_label"` // display cost, e.g. "+$800", "$0/yr"
	IsDefault bool   `json:"is_default"`
	SortOrder int    `json:"sort_order"`
}

// ── Open / Close ──

func Open(path string) error {
	var err error
	conn, err = sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	conn.SetMaxOpenConns(1)

	// Migrate an existing quotes table first (add columns if missing) so the
	// schema's CREATE INDEX on newer columns succeeds. On a fresh DB the table
	// doesn't exist yet and these fail harmlessly; the schema below creates it
	// with all columns.
	for _, stmt := range []string{
		"ALTER TABLE quotes ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE quotes ADD COLUMN status TEXT NOT NULL DEFAULT 'paid'",
		"ALTER TABLE quotes ADD COLUMN verify_token TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE quotes ADD COLUMN verified_at TEXT NOT NULL DEFAULT ''",
		// bookings lifecycle columns (constant defaults only — SQLite ALTER can't use datetime('now'))
		"ALTER TABLE bookings ADD COLUMN customer_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE bookings ADD COLUMN start_at TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE bookings ADD COLUMN duration_min INTEGER NOT NULL DEFAULT 60",
		"ALTER TABLE bookings ADD COLUMN admin_notes TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE bookings ADD COLUMN parent_booking_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE bookings ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE bookings ADD COLUMN address TEXT NOT NULL DEFAULT ''",
	} {
		conn.Exec(stmt) // ignore "duplicate column" / "no such table" errors
	}

	// Run embedded schema (idempotent CREATE TABLE/INDEX IF NOT EXISTS statements)
	if _, err := conn.Exec(schemaSQL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	q = sqlc.New(conn)

	if err := initOptions(); err != nil {
		return fmt.Errorf("init options: %w", err)
	}
	if err := BackfillCustomers(); err != nil {
		return fmt.Errorf("backfill customers: %w", err)
	}
	return nil
}

// parseUTC parses the datetime('now') text SQLite stores (UTC, no zone).
func parseUTC(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04:05", s)
	return t
}

func Close() error {
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// ── Quotes ──

// InsertQuote stores a new pending quote and returns its id.
func InsertQuote(quote *Quote) (int64, error) {
	featJSON, err := json.Marshal(quote.Features)
	if err != nil {
		return 0, fmt.Errorf("marshal features: %w", err)
	}
	id, err := q.InsertQuote(context.Background(), sqlc.InsertQuoteParams{
		Name:        quote.Name,
		Email:       quote.Email,
		Mobile:      quote.Mobile,
		Address:     quote.Address,
		Description: quote.Description,
		TotalCost:   quote.TotalCost,
		Features:    string(featJSON),
		VerifyToken: quote.VerifyToken,
	})
	if err != nil {
		return 0, fmt.Errorf("insert quote: %w", err)
	}
	return id, nil
}

func GetQuote(id int64) (*Quote, error) {
	row, err := q.GetQuote(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return sqlcQuoteToQuote(row), nil
}

// GetQuoteByVerifyToken returns the quote carrying the given verification token,
// or sql.ErrNoRows.
func GetQuoteByVerifyToken(token string) (*Quote, error) {
	row, err := q.GetQuoteByVerifyToken(context.Background(), token)
	if err != nil {
		return nil, err
	}
	return sqlcQuoteToQuote(row), nil
}

// HasVerifiedQuote reports whether a verified quote already exists for email.
func HasVerifiedQuote(email string) (bool, error) {
	n, err := q.CountVerifiedByEmail(context.Background(), email)
	return n > 0, err
}

// DeletePendingQuotes removes unverified quotes for email (a resubmission
// supersedes any earlier attempt that was never confirmed).
func DeletePendingQuotes(email string) error {
	return q.DeletePendingByEmail(context.Background(), email)
}

// MarkQuoteVerified flips a pending quote to verified and stores the estimate.
// It returns false if the quote was not pending (already verified, or missing),
// so concurrent clicks on the same link only generate one notification.
func MarkQuoteVerified(id int64, aiEstimate string) (bool, error) {
	n, err := q.MarkQuoteVerified(context.Background(), sqlc.MarkQuoteVerifiedParams{
		ID:         id,
		AiEstimate: aiEstimate,
	})
	return n == 1, err
}

func ListQuotes() ([]Quote, error) {
	rows, err := q.ListQuotes(context.Background())
	if err != nil {
		return nil, err
	}
	quotes := make([]Quote, len(rows))
	for i, r := range rows {
		quotes[i] = *sqlcQuoteToQuote(r)
	}
	return quotes, nil
}

func sqlcQuoteToQuote(r sqlc.Quote) *Quote {
	quote := &Quote{
		ID:          r.ID,
		Name:        r.Name,
		Email:       r.Email,
		Mobile:      r.Mobile,
		Address:     r.Address,
		Description: r.Description,
		TotalCost:   r.TotalCost,
		AIEstimate:  r.AiEstimate,
		VerifyToken: r.VerifyToken,
		Status:      r.Status,
	}
	json.Unmarshal([]byte(r.Features), &quote.Features)
	quote.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", r.CreatedAt)
	if r.VerifiedAt != "" {
		quote.VerifiedAt, _ = time.Parse("2006-01-02 15:04:05", r.VerifiedAt)
	}
	return quote
}

// ── Users ──

func UpsertUser(googleID, email, name, picture string) (*User, error) {
	ctx := context.Background()

	if err := q.UpsertUser(ctx, sqlc.UpsertUserParams{
		GoogleID: googleID,
		Email:    email,
		Name:     name,
		Picture:  picture,
	}); err != nil {
		return nil, err
	}

	row, err := q.GetUserByGoogleID(ctx, googleID)
	if err != nil {
		return nil, err
	}
	return &User{
		ID:        row.ID,
		GoogleID:  row.GoogleID,
		Email:     row.Email,
		Name:      row.Name,
		Picture:   row.Picture,
		CreatedAt: row.CreatedAt,
	}, nil
}

// ── Option Groups ──

func ListOptionGroups() ([]OptionGroup, error) {
	ctx := context.Background()

	rows, err := q.ListOptionGroups(ctx)
	if err != nil {
		return nil, err
	}

	groups := make([]OptionGroup, len(rows))
	for i, r := range rows {
		groups[i] = OptionGroup{
			ID:        r.ID,
			Name:      r.Name,
			Label:     r.Label,
			Hint:      r.Hint,
			SortOrder: int(r.SortOrder),
		}

		opts, err := q.ListOptionsByGroupID(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		for _, o := range opts {
			groups[i].Options = append(groups[i].Options, Option{
				ID:        o.ID,
				GroupID:   o.GroupID,
				Value:     o.Value,
				Name:      o.Name,
				Cost:      int(o.Cost),
				CostLabel: o.CostLabel,
				IsDefault: o.IsDefault != 0,
				SortOrder: int(o.SortOrder),
			})
		}
	}
	return groups, nil
}

func OptionGroupsJSON() (string, error) {
	groups, err := ListOptionGroups()
	if err != nil {
		return "[]", err
	}
	b, err := json.Marshal(groups)
	if err != nil {
		return "[]", err
	}
	return string(b), nil
}

func SaveAllOptions(groups []OptionGroup) error {
	ctx := context.Background()

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	if err := qtx.DeleteAllOptions(ctx); err != nil {
		return fmt.Errorf("clear options: %w", err)
	}
	if err := qtx.DeleteAllOptionGroups(ctx); err != nil {
		return fmt.Errorf("clear groups: %w", err)
	}

	for _, g := range groups {
		if err := qtx.InsertOptionGroup(ctx, sqlc.InsertOptionGroupParams{
			Name:      g.Name,
			Label:     g.Label,
			Hint:      g.Hint,
			SortOrder: int64(g.SortOrder),
		}); err != nil {
			return fmt.Errorf("insert group %q: %w", g.Name, err)
		}

		row, err := qtx.GetLastOptionGroup(ctx)
		if err != nil {
			return fmt.Errorf("get last group: %w", err)
		}

		for _, o := range g.Options {
			isDefault := int64(0)
			if o.IsDefault {
				isDefault = 1
			}
			if err := qtx.InsertOption(ctx, sqlc.InsertOptionParams{
				GroupID:   row.ID,
				Value:     o.Value,
				Name:      o.Name,
				Cost:      int64(o.Cost),
				CostLabel: o.CostLabel,
				IsDefault: isDefault,
				SortOrder: int64(o.SortOrder),
			}); err != nil {
				return fmt.Errorf("insert option %q/%q: %w", g.Name, o.Value, err)
			}
		}
	}

	return tx.Commit()
}

// ── Settings ──

func GetBaseCost() (int, error) {
	val, err := q.GetSetting(context.Background(), "base_cost")
	if err != nil {
		return 2000, err
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 2000, nil
	}
	return n, nil
}

func SetBaseCost(cost int) error {
	return q.UpsertSetting(context.Background(), sqlc.UpsertSettingParams{
		Key:   "base_cost",
		Value: strconv.Itoa(cost),
	})
}

// ── Init ──

func initOptions() error {
	ctx := context.Background()

	count, err := q.CountOptionGroups(ctx)
	if err != nil {
		return fmt.Errorf("count groups: %w", err)
	}
	if count == 0 {
		if err := seedOptions(); err != nil {
			return err
		}
	}

	settingsCount, err := q.CountSettingByKey(ctx, "base_cost")
	if err != nil {
		return fmt.Errorf("check base_cost: %w", err)
	}
	if settingsCount == 0 {
		if err := q.UpsertSetting(ctx, sqlc.UpsertSettingParams{
			Key:   "base_cost",
			Value: "2000",
		}); err != nil {
			return fmt.Errorf("seed base_cost: %w", err)
		}
	}
	return nil
}
