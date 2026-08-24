package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"localithelp/db"
)

// fakePutter records uploads and can be told to fail.
type fakePutter struct {
	keys []string
	body []byte
	fail error
}

func (f *fakePutter) Enabled() bool  { return true }
func (f *fakePutter) Bucket() string { return "test-bucket" }
func (f *fakePutter) Upload(_ context.Context, key, _ string, r io.Reader, size int64) error {
	if f.fail != nil {
		return f.fail
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return errors.New("size mismatch")
	}
	f.keys = append(f.keys, key)
	f.body = b
	return nil
}

func TestBackupJob(t *testing.T) {
	if err := db.Open(filepath.Join(t.TempDir(), "bk.db")); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test"}
	mail, notifyEmail = nil, "me@example.test"
	if _, err := db.InsertBooking(&db.Booking{Name: "Snap", Phone: "0400 111 222", Email: "snap@example.test", Suburb: "Donvale", Issue: "x", Mode: "onsite"}); err != nil {
		t.Fatal(err)
	}

	fake := &fakePutter{}
	prev := backups
	backups = fake
	defer func() { backups = prev }()

	// Not before 02:00.
	early := time.Date(2026, 8, 24, 1, 45, 0, 0, db.Melbourne)
	if backupDatabase(early) {
		t.Fatal("backup before 02:00")
	}

	// Failure: claim released so the next tick retries; no key recorded.
	at := early.Add(30 * time.Minute)
	fake.fail = errors.New("boom")
	if backupDatabase(at) {
		t.Fatal("failed upload reported success")
	}
	fake.fail = nil
	if !backupDatabase(at) {
		t.Fatal("retry after failure should upload")
	}
	if backupDatabase(at.Add(time.Hour)) {
		t.Fatal("second upload in one day")
	}
	if len(fake.keys) != 1 || fake.keys[0] != "app/2026/08/localithelp-2026-08-24.db.gz" {
		t.Fatalf("keys = %v", fake.keys)
	}
	if _, err := os.Stat(filepath.Join(os.TempDir(), "localithelp-backup.db")); !os.IsNotExist(err) {
		t.Error("temp snapshot not cleaned up")
	}

	// The upload is a gzip'd SQLite file containing the row.
	gz, err := gzip.NewReader(bytes.NewReader(fake.body))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(out, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", out)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var n int
	if err := restored.QueryRow(`SELECT count(*) FROM bookings WHERE name = 'Snap'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("restored rows = %d, err = %v", n, err)
	}

	// Next day gets its own object; the whole tick reports it.
	if s := runScheduledJobs(at.AddDate(0, 0, 1)); !s.Backup {
		t.Fatalf("next day: %+v", s)
	}
	if len(fake.keys) != 2 {
		t.Fatalf("keys = %v", fake.keys)
	}

	// Disabled uploader: no-op, and the failure template still renders.
	backups = (*fakeDisabled)(nil)
	if backupDatabase(at.AddDate(0, 0, 2)) {
		t.Fatal("disabled uploader ran")
	}
	if _, err := renderMail("backup-failed", map[string]any{"Bucket": "b", "Key": "k", "Error": "boom"}); err != nil {
		t.Fatal(err)
	}
}

type fakeDisabled struct{}

func (*fakeDisabled) Enabled() bool  { return false }
func (*fakeDisabled) Bucket() string { return "" }
func (*fakeDisabled) Upload(context.Context, string, string, io.Reader, int64) error {
	return nil
}
