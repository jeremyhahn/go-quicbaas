// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package quicbaas

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRetryDo_SucceedsAfterFailures proves the retry re-exports are usable
// end-to-end through the quicbaas SDK alias surface: an operation that fails
// twice succeeds on the third attempt under a constant backoff.
func TestRetryDo_SucceedsAfterFailures(t *testing.T) {
	attempts := 0
	cfg := &RetryConfig{
		MaxAttempts:   3,
		Backoff:       &ConstantBackoff{Delay: time.Millisecond},
		RetryableFunc: func(err error) bool { return true },
	}
	err := RetryDo(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RetryDo: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

// TestRetryDo_ExhaustsAttempts verifies the error path: a persistently
// failing (but retryable) operation stops after MaxAttempts and surfaces
// the failure.
func TestRetryDo_ExhaustsAttempts(t *testing.T) {
	attempts := 0
	permanent := errors.New("permanent")
	cfg := &RetryConfig{
		MaxAttempts:   2,
		Backoff:       &ConstantBackoff{Delay: time.Millisecond},
		RetryableFunc: func(err error) bool { return true },
	}
	err := RetryDo(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return permanent
	})
	if err == nil {
		t.Fatal("expected error after exhausting attempts, got nil")
	}
	if !errors.Is(err, permanent) {
		t.Errorf("expected wrapped permanent error, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

// TestRetryDo_NonRetryableFailsFast pins the default predicate semantics:
// without a RetryableFunc, a plain (non-QRDBError) failure is treated as
// non-retryable and returns after a single attempt.
func TestRetryDo_NonRetryableFailsFast(t *testing.T) {
	attempts := 0
	permanent := errors.New("permanent")
	cfg := &RetryConfig{
		MaxAttempts: 3,
		Backoff:     &ConstantBackoff{Delay: time.Millisecond},
	}
	err := RetryDo(context.Background(), cfg, func(ctx context.Context) error {
		attempts++
		return permanent
	})
	if !errors.Is(err, permanent) {
		t.Errorf("expected permanent error, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (fail fast on non-retryable)", attempts)
	}
}
