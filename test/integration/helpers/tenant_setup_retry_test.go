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
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
		// A pause longer than what is left of the window: the second
		// failure comes 250ms in, with 50ms of the window left, so no
		// third call may start and the helper returns when the window ends.
		window := 300 * time.Millisecond
		shrinkTenantSetupRetry(t, window, 250*time.Millisecond)
		calls := 0
		start := time.Now()
		err := retryTenantSetup(context.Background(), "provision", "t3", func() error {
			calls++
			return badStatus(503, "unavailable")
		})
		elapsed := time.Since(start)
		var bad *AuthBadStatusError
		if !errors.As(err, &bad) || bad.Status != 503 {
			t.Fatalf("err = %v, want the 503", err)
		}
		if calls != 2 {
			t.Fatalf("calls = %d, want 2: no call may start after the window", calls)
		}
		if elapsed < window {
			t.Fatalf("returned after %v, before the window ended", elapsed)
		}
		if elapsed > window+150*time.Millisecond {
			t.Fatalf("returned after %v, past the %v window", elapsed, window)
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

// tenantServer starts a TLS server that answers each request to path with
// the next of replies (the last one repeats), counts the requests, and makes
// NewHTTPClient trust it for the test.
func tenantServer(t *testing.T, method, path string, replies ...func(http.ResponseWriter)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method || r.URL.Path != path {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
			return
		}
		n := int(calls.Add(1))
		if n > len(replies) {
			n = len(replies)
		}
		replies[n-1](w)
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	integrationCAOnce.Do(func() {})
	oldPool, oldErr := integrationCAPool, integrationCAErr
	integrationCAPool, integrationCAErr = pool, nil
	t.Cleanup(func() { integrationCAPool, integrationCAErr = oldPool, oldErr })
	return srv, &calls
}

func reply(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestProvisionTenantRetriesLeaderLoss(t *testing.T) {
	shrinkTenantSetupRetry(t, 2*time.Second, 10*time.Millisecond)
	const path = "/api/v1/tenants/t1/provision"

	t.Run("retries until the provision succeeds", func(t *testing.T) {
		// The successful reply carries no admin cert, so the helper stops
		// at decoding it: that step is reached only once the provision
		// call itself succeeded after the leader-loss replies.
		srv, calls := tenantServer(t, http.MethodPost, path,
			reply(http.StatusInternalServerError, internalBody),
			reply(http.StatusServiceUnavailable, `{"error":"unavailable"}`),
			reply(http.StatusOK, `{}`))
		_, err := ProvisionTenant(context.Background(), srv.URL, "t1", "system-token")
		var perr *TenantProvisionError
		if !errors.As(err, &perr) || perr.Step != "decode provision response" {
			t.Fatalf("err = %v, want the decode step after a successful provision", err)
		}
		var missing *MissingAdminCertError
		if !errors.As(err, &missing) {
			t.Fatalf("err = %v, want MissingAdminCertError", err)
		}
		if got := calls.Load(); got != 3 {
			t.Fatalf("provision requests = %d, want 3", got)
		}
	})

	t.Run("other status fails at once", func(t *testing.T) {
		srv, calls := tenantServer(t, http.MethodPost, path,
			reply(http.StatusConflict, `{"error":"tenant exists"}`))
		_, err := ProvisionTenant(context.Background(), srv.URL, "t1", "system-token")
		var perr *TenantProvisionError
		if !errors.As(err, &perr) || perr.Step != "provision" {
			t.Fatalf("err = %v, want the provision step", err)
		}
		var bad *AuthBadStatusError
		if !errors.As(err, &bad) || bad.Status != http.StatusConflict {
			t.Fatalf("err = %v, want the 409", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("provision requests = %d, want 1", got)
		}
	})
}

func TestDeleteTenantRetriesLeaderLoss(t *testing.T) {
	shrinkTenantSetupRetry(t, 2*time.Second, 10*time.Millisecond)
	const path = "/api/v1/tenants/t1"

	t.Run("retries until the delete succeeds", func(t *testing.T) {
		srv, calls := tenantServer(t, http.MethodDelete, path,
			reply(http.StatusInternalServerError, `{"error":"service: not leader for shard 1"}`),
			reply(http.StatusUnauthorized, `{"error":"platformtoken: liveness check failed: service: not leader for shard 1"}`),
			reply(http.StatusNoContent, ``))
		if err := DeleteTenant(context.Background(), srv.URL, "t1", "system-token"); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got := calls.Load(); got != 3 {
			t.Fatalf("delete requests = %d, want 3", got)
		}
	})

	t.Run("other status fails at once", func(t *testing.T) {
		srv, calls := tenantServer(t, http.MethodDelete, path,
			reply(http.StatusForbidden, `{"error":"forbidden"}`))
		err := DeleteTenant(context.Background(), srv.URL, "t1", "system-token")
		var derr *TenantDeleteError
		if !errors.As(err, &derr) {
			t.Fatalf("err = %v, want TenantDeleteError", err)
		}
		var bad *AuthBadStatusError
		if !errors.As(err, &bad) || bad.Status != http.StatusForbidden {
			t.Fatalf("err = %v, want the 403", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("delete requests = %d, want 1", got)
		}
	})
}
