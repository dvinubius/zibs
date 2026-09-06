package main

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

func runExpiryCleanup(ctx context.Context, store *linkStore, interval time.Duration, logger *slog.Logger, metrics *metrics) {
	cleanup := func() {
		started := time.Now()
		deleted, err := store.deleteExpired(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				metrics.expiryCleanupDuration.WithLabelValues("error").Observe(time.Since(started).Seconds())
				logger.Error("expiry cleanup failed",
					"event", "expiry_cleanup_failed",
					"error_category", "database",
				)
			}
			return
		}
		metrics.expiredLinksDeleted.Add(float64(deleted))
		metrics.expiryCleanupDuration.WithLabelValues("success").Observe(time.Since(started).Seconds())
		logger.Info("expiry cleanup completed", "event", "expiry_cleanup_completed", "deleted", deleted)
	}

	select {
	case <-ctx.Done():
		return
	default:
		cleanup()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
