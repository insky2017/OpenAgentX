package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPeriodicRecoveryRetriesAtIntervalAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log bytes.Buffer
	var calls atomic.Int64
	observed := make(chan time.Time, 8)
	done := make(chan struct{})
	const interval = 30 * time.Millisecond
	go func() {
		defer close(done)
		reconcilePeriodically(ctx, interval, func(context.Context) error {
			calls.Add(1)
			observed <- time.Now()
			return errors.New("injected recovery failure")
		}, slog.New(slog.NewTextHandler(&log, nil)))
	}()
	var first time.Time
	for i := 0; i < 3; i++ {
		select {
		case when := <-observed:
			if i == 0 {
				first = when
			}
			if i == 2 && when.Sub(first) < interval {
				t.Fatal("failed recovery retried without waiting for the interval")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("periodic recovery did not retry")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery loop did not stop")
	}
	if !strings.Contains(log.String(), "periodic lease recovery failed") || !strings.Contains(log.String(), "injected recovery failure") {
		t.Fatalf("missing recovery failure diagnostic: %s", log.String())
	}
	if got := calls.Load(); got < 3 || got > 4 {
		t.Fatalf("unexpected recovery call count: %d", got)
	}
}

func TestPeriodicRecoveryCancellationReachesInFlightTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int64
	go func() {
		defer close(done)
		reconcilePeriodically(ctx, time.Millisecond, func(callContext context.Context) error {
			if calls.Add(1) != 1 {
				return errors.New("unexpected overlapping or repeated recovery")
			}
			close(started)
			<-callContext.Done()
			return callContext.Err()
		}, slog.Default())
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("recovery did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight recovery did not receive cancellation")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("recovery continued after shutdown: %d calls", got)
	}
}
