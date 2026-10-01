package main

import (
	"context"
	"log/slog"
	"time"
)

// reconcilePeriodically never overlaps recovery transactions or retries errors
// in a tight loop. The startup reconciliation remains a separate fail-closed
// check; ongoing failures are diagnosed and retried at the next interval.
func reconcilePeriodically(ctx context.Context, interval time.Duration, reconcile func(context.Context) error, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if err := reconcile(ctx); err != nil && ctx.Err() == nil {
				logger.Error("periodic lease recovery failed", "error", err)
			}
		}
	}
}
