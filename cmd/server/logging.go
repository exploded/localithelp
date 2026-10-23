package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/exploded/monitor/pkg/logship"
)

// setupLogging ships WARN and above to the monitor portal when MONITOR_URL and
// MONITOR_API_KEY are set, so an error here shows up on monitor's app-logs page
// and raises a Discord alert instead of sitting in journalctl. Plain log.Printf
// lines keep going to stderr at INFO and are not shipped. Call it after the
// .env is loaded; the returned func flushes anything still buffered.
func setupLogging() func() {
	monitorURL := os.Getenv("MONITOR_URL")
	monitorKey := os.Getenv("MONITOR_API_KEY")
	if monitorURL == "" || monitorKey == "" {
		// Keep slog's default handler, which writes through the log package
		// exactly as before.
		return func() {}
	}
	ship := logship.New(logship.Options{
		Endpoint: monitorURL + "/api/logs",
		APIKey:   monitorKey,
		App:      "localithelp",
		Level:    slog.LevelWarn,
	})
	slog.SetDefault(slog.New(logship.Multi(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
		ship,
	)))
	// WARN so it ships: it proves shipping works after a deploy without raising
	// an alert (only ERROR does).
	slog.Warn("localithelp started, log shipping active", "endpoint", monitorURL+"/api/logs")
	return ship.Shutdown
}

// logErrorf logs a failure someone should look at. It ships to monitor and
// raises an alert.
func logErrorf(format string, args ...any) {
	slog.Error(fmt.Sprintf(format, args...))
}

// logWarnf logs something worth seeing on monitor's app-logs page that is not
// worth an alert: a client that went away, a degraded fallback, missing config.
func logWarnf(format string, args ...any) {
	slog.Warn(fmt.Sprintf(format, args...))
}
