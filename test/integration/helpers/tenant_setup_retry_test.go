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

//go:build integration

package helpers

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "i/o timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }

const internalBody = `{"error":"internal: the request could not be completed","code":"internal"}`

func badStatus(status int, body string) error {
	return &AuthBadStatusError{URL: "https://example.test/api", Status: status, Body: body}
}

func TestLeaderLossError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"503", badStatus(503, "unavailable"), true},
		{"500 internal body", badStatus(500, internalBody), true},
		{"500 not leader", badStatus(500, "service: not leader for shard 1"), true},
		{"401 not leader", badStatus(401, `{"error":"platformtoken: liveness check failed: service: not leader for shard 1, leader unknown"}`), true},
		{"wrapped 500 internal", &TenantDeleteError{TenantID: "t", Cause: badStatus(500, internalBody)}, true},
		{"deadline in AuthError", &AuthError{Op: "post", URL: "https://example.test", Cause: context.DeadlineExceeded}, true},
		{"net timeout", &AuthError{Op: "post", URL: "https://example.test", Cause: fakeTimeoutError{}}, true},
		{"nil", nil, false},
		{"404", badStatus(404, "not found"), false},
		{"409", badStatus(409, "conflict"), false},
		{"500 other body", badStatus(500, "boom"), false},
		{"401 invalid token", badStatus(401, "invalid token"), false},
		{"plain error", errors.New("something else"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := leaderLossError(tc.err); got != tc.want {
				t.Fatalf("leaderLossError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func shrinkTenantSetupRetry(t *testing.T, window, pause time.Duration) {
	t.Helper()
	oldWindow, oldPause := tenantSetupRetryWindow, tenantSetupRetryPause
	tenantSetupRetryWindow, tenantSetupRetryPause = window, pause
	t.Cleanup(func() {
		tenantSetupRetryWindow, tenantSetupRetryPause = oldWindow, oldPause
	})
}

func TestTenantSetupRetry(t *testing.T) {
	shrinkTenantSetupRetry(t, 2*time.Second, 10*time.Millisecond)

	t.Run("recovers after leader loss", func(t *testing.T) {
		calls := 0
		err := retryTenantSetup(context.Background(), "provision", "t1", func() error {
			calls++
			if calls <= 2 {
				return badStatus(500, internalBody)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if calls != 3 {
			t.Fatalf("calls = %d, want 3", calls)
		}
	})

	t.Run("other error fails at once", func(t *testing.T) {
		calls := 0
		want := badStatus(404, "not found")
		err := retryTenantSetup(context.Background(), "delete", "t2", func() error {
			calls++
			return want
		})
		if err != want {
			t.Fatalf("err = %v, want %v", err, want)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
	})

	t.Run("window ends with last error", func(t *testing.T) {
		calls := 0
		start := time.Now()
		err := retryTenantSetup(context.Background(), "provision", "t3", func() error {
			calls++
			return badStatus(503, "unavailable")
		})
		var bad *AuthBadStatusError
		if !errors.As(err, &bad) || bad.Status != 503 {
			t.Fatalf("err = %v, want the 503", err)
		}
		if calls < 2 {
			t.Fatalf("calls = %d, want retries", calls)
		}
		if elapsed := time.Since(start); elapsed < 2*time.Second {
			t.Fatalf("returned after %v, before the window ended", elapsed)
		}
	})

	t.Run("cancelled ctx stops retrying", func(t *testing.T) {
		shrinkTenantSetupRetry(t, 2*time.Second, 500*time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		start := time.Now()
		err := retryTenantSetup(ctx, "delete", "t4", func() error {
			calls++
			cancel()
			return badStatus(503, "unavailable")
		})
		var bad *AuthBadStatusError
		if !errors.As(err, &bad) || bad.Status != 503 {
			t.Fatalf("err = %v, want the 503", err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("waited %v after ctx was cancelled", elapsed)
		}
	})
}
