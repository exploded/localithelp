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

type Quote struct {
	ID              int64
	UserID          int64
	Name            string
	Email           string
	Mobile          string
	Address         string
	Description     string
	TotalCost       float64
	Features        map[string]any
	AIEstimate      string
	StripeSessionID string
	Status          string // "draft" or "paid"
	CreatedAt       time.Time
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

	// Run embedded schema (idempotent CREATE TABLE IF NOT EXISTS statements)
	if _, err := conn.Exec(schemaSQL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	// Migrate existing quotes table (add columns if missing)
	for _, stmt := range []string{
		"ALTER TABLE quotes ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE quotes ADD COLUMN status TEXT NOT NULL DEFAULT 'paid'",
	} {
		conn.Exec(stmt) // ignore "duplicate column" errors
	}

	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	q = sqlc.New(conn)

	if err := initOptions(); err != nil {
		return fmt.Errorf("init options: %w", err)
	}
	return nil
}

func Close() error {
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// ── Quotes ──

func InsertQuote(quote *Quote) (int64, error) {
	ctx := context.Background()

	featJSON, err := json.Marshal(quote.Features)
	if err != nil {
		return 0, fmt.Errorf("marshal features: %w", err)
	}

	status := quote.Status
	if status == "" {
		status = "draft"
	}

	if err := q.InsertQuote(ctx, sqlc.InsertQuoteParams{
		UserID:          quote.UserID,
		Name:            quote.Name,
		Email:           quote.Email,
		Mobile:          quote.Mobile,
		Address:         quote.Address,
		Description:     quote.Description,
		TotalCost:       quote.TotalCost,
		Features:        string(featJSON),
		AiEstimate:      quote.AIEstimate,
		StripeSessionID: quote.StripeSessionID,
		Status:          status,
	}); err != nil {
		return 0, fmt.Errorf("insert quote: %w", err)
	}

	row, err := q.GetLastQuote(ctx)
	if err != nil {
		return 0, fmt.Errorf("get last quote: %w", err)
	}
	return row.ID, nil
}

func GetQuote(id int64) (*Quote, error) {
	row, err := q.GetQuote(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return sqlcQuoteToQuote(row), nil
}

func ListDraftsByUser(userID int64) ([]Quote, error) {
	rows, err := q.ListDraftsByUser(context.Background(), userID)
	if err != nil {
		return nil, err
	}
	quotes := make([]Quote, len(rows))
	for i, r := range rows {
		quotes[i] = Quote{
			ID:          r.ID,
			UserID:      r.UserID,
			Name:        r.Name,
			Email:       r.Email,
			Description: r.Description,
			TotalCost:   r.TotalCost,
			Status:      r.Status,
		}
		quotes[i].CreatedAt, _ = time.Parse("2006-01-02 15:04:05", r.CreatedAt)
	}
	return quotes, nil
}

func UpdateQuoteStatus(id int64, status, stripeSessionID, aiEstimate string) error {
	return q.UpdateQuoteStatus(context.Background(), sqlc.UpdateQuoteStatusParams{
		ID:              id,
		Status:          status,
		StripeSessionID: stripeSessionID,
		AiEstimate:      aiEstimate,
	})
}

func ListPaidByUser(userID int64) ([]Quote, error) {
	rows, err := conn.QueryContext(context.Background(),
		`SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, stripe_session_id, status, created_at
		 FROM quotes WHERE user_id = ? AND status = 'paid'
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var quotes []Quote
	for rows.Next() {
		var q sqlc.Quote
		if err := rows.Scan(&q.ID, &q.UserID, &q.Name, &q.Email, &q.Mobile, &q.Address, &q.Description, &q.TotalCost, &q.Features, &q.AiEstimate, &q.StripeSessionID, &q.Status, &q.CreatedAt); err != nil {
			return nil, err
		}
		quotes = append(quotes, *sqlcQuoteToQuote(q))
	}
	return quotes, rows.Err()
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
		ID:              r.ID,
		UserID:          r.UserID,
		Name:            r.Name,
		Email:           r.Email,
		Mobile:          r.Mobile,
		Address:         r.Address,
		Description:     r.Description,
		TotalCost:       r.TotalCost,
		AIEstimate:      r.AiEstimate,
		StripeSessionID: r.StripeSessionID,
		Status:          r.Status,
	}
	json.Unmarshal([]byte(r.Features), &quote.Features)
	quote.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", r.CreatedAt)
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

// ── Bookings ──

type Booking struct {
	ID            int64
	Name          string
	Phone         string
	Email         string
	Suburb        string
	ServiceSlug   string
	Mode          string // onsite | remote | either
	Issue         string
	PreferredTime string
	Status        string // new | contacted | booked | done | spam
	IP            string
	CreatedAt     time.Time
}

func InsertBooking(b *Booking) (int64, error) {
	ctx := context.Background()
	if err := q.InsertBooking(ctx, sqlc.InsertBookingParams{
		Name:          b.Name,
		Phone:         b.Phone,
		Email:         b.Email,
		Suburb:        b.Suburb,
		ServiceSlug:   b.ServiceSlug,
		Mode:          b.Mode,
		Issue:         b.Issue,
		PreferredTime: b.PreferredTime,
		Ip:            b.IP,
	}); err != nil {
		return 0, fmt.Errorf("insert booking: %w", err)
	}
	row, err := q.GetLastBooking(ctx)
	if err != nil {
		return 0, fmt.Errorf("get last booking: %w", err)
	}
	return row.ID, nil
}

func ListBookings() ([]Booking, error) {
	rows, err := q.ListBookings(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]Booking, len(rows))
	for i, r := range rows {
		out[i] = Booking{
			ID:            r.ID,
			Name:          r.Name,
			Phone:         r.Phone,
			Email:         r.Email,
			Suburb:        r.Suburb,
			ServiceSlug:   r.ServiceSlug,
			Mode:          r.Mode,
			Issue:         r.Issue,
			PreferredTime: r.PreferredTime,
			Status:        r.Status,
			IP:            r.Ip,
		}
		out[i].CreatedAt, _ = time.Parse("2006-01-02 15:04:05", r.CreatedAt)
	}
	return out, nil
}

func UpdateBookingStatus(id int64, status string) error {
	return q.UpdateBookingStatus(context.Background(), sqlc.UpdateBookingStatusParams{
		Status: status,
		ID:     id,
	})
}
