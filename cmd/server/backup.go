package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"localithelp/backup"
	"localithelp/db"
)

// ── Nightly database backup ──
//
// Configured by BACKUP_S3_BUCKET / BACKUP_AWS_ACCESS_KEY_ID /
// BACKUP_AWS_SECRET_ACCESS_KEY (region from AWS_REGION). A separate IAM user
// from the mailer, with PutObject only — see scripts/s3-backup-setup.sh. When
// unset the uploader is nil and the job is a silent no-op.

// objectPutter is the slice of *backup.Uploader the scheduler needs; tests
// substitute a fake.
type objectPutter interface {
	Enabled() bool
	Bucket() string
	Upload(ctx context.Context, key, contentType string, r io.Reader, size int64) error
}

var backups objectPutter = (*backup.Uploader)(nil)

func initBackup() {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "ap-southeast-2"
	}
	u := backup.New(region, os.Getenv("BACKUP_S3_BUCKET"), os.Getenv("BACKUP_AWS_ACCESS_KEY_ID"), os.Getenv("BACKUP_AWS_SECRET_ACCESS_KEY"))
	backups = u
	if u.Enabled() {
		log.Printf("backup: nightly DB backup to s3://%s (region %s)", u.Bucket(), region)
	} else {
		log.Printf("backup: not configured (set BACKUP_S3_BUCKET + BACKUP_AWS_ACCESS_KEY_ID/SECRET) — no DB backups")
	}
}

// backupHour is the earliest local hour the nightly backup runs.
const backupHour = 2

// backupKey is the S3 object key for a given local date.
func backupKey(day time.Time) string {
	return day.Format("app/2006/01/localithelp-2006-01-02.db.gz")
}

// backupDatabase uploads a gzip'd snapshot of app.db once a day, on the first
// tick at or after 02:00. The scheduler_runs row is claimed first so two ticks
// can't both upload; on failure the claim is released so the next tick retries,
// and the admin is emailed once per failure.
func backupDatabase(now time.Time) bool {
	if !backups.Enabled() || now.Hour() < backupHour {
		return false
	}
	first, err := db.ClaimSchedulerRun("backup", now)
	if err != nil {
		log.Printf("scheduler: claim backup: %v", err)
		return false
	}
	if !first {
		return false
	}
	key := backupKey(now)
	size, err := runBackup(key)
	if err != nil {
		log.Printf("scheduler: backup: %v", err)
		if rerr := db.ReleaseSchedulerRun("backup", now); rerr != nil {
			log.Printf("scheduler: release backup claim: %v", rerr)
		}
		notifyBackupFailed(key, err)
		return false
	}
	log.Printf("scheduler: backup uploaded s3://%s/%s (%d bytes)", backups.Bucket(), key, size)
	return true
}

// runBackup snapshots the DB to a temp file, gzips it in memory and uploads
// it. Returns the compressed size.
func runBackup(key string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tmp := filepath.Join(os.TempDir(), "localithelp-backup.db")
	defer os.Remove(tmp)
	if err := db.BackupTo(ctx, tmp); err != nil {
		return 0, err
	}
	raw, err := os.ReadFile(tmp)
	if err != nil {
		return 0, fmt.Errorf("read snapshot: %w", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	if err := gz.Close(); err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	size := int64(buf.Len())
	if err := backups.Upload(ctx, key, "application/gzip", &buf, size); err != nil {
		return 0, err
	}
	return size, nil
}

// notifyBackupFailed emails the admin so a broken backup never goes unnoticed.
func notifyBackupFailed(key string, cause error) {
	text := fmt.Sprintf("The nightly database backup failed.\n\nObject: s3://%s/%s\nError: %v\n\nThe scheduler retries every 15 minutes; you'll get another email if the next attempt fails too. Check the service log: journalctl -u localithelp\n",
		backups.Bucket(), key, cause)
	html, err := renderMail("backup-failed", map[string]any{"Bucket": backups.Bucket(), "Key": key, "Error": cause.Error()})
	if err != nil {
		log.Printf("email: render backup-failed: %v", err)
		return
	}
	if err := sendNow(notifyEmail, "DB backup failed", html, text, ""); err != nil {
		log.Printf("email: backup-failed: %v", err)
	}
}
