package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupTo(t *testing.T) {
	openTestDB(t)
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO bookings (name, phone, email, suburb) VALUES ('Snap', '0400 000 002', 'snap@example.test', 'Donvale')`); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "copy.db")
	for i := 0; i < 2; i++ { // second run overwrites the first
		if err := BackupTo(ctx, out); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
	}

	copy, err := sql.Open("sqlite", out)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var n int
	if err := copy.QueryRow(`SELECT count(*) FROM bookings WHERE name = 'Snap'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("copy has %d Snap rows, want 1", n)
	}

	// Claim / release round-trip used by the backup job.
	day := time.Date(2026, 8, 24, 2, 0, 0, 0, Melbourne)
	if first, err := ClaimSchedulerRun("backup", day); err != nil || !first {
		t.Fatalf("first claim: first=%v err=%v", first, err)
	}
	if first, _ := ClaimSchedulerRun("backup", day); first {
		t.Fatal("second claim should not be first")
	}
	if err := ReleaseSchedulerRun("backup", day); err != nil {
		t.Fatal(err)
	}
	if first, _ := ClaimSchedulerRun("backup", day); !first {
		t.Fatal("claim after release should be first again")
	}
}
