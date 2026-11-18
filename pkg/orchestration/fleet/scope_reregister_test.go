// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package fleet

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const invalidScopeBody = `{"error":"invalid_scope","error_description":"scope not granted"}`

// TestHTTPHeartbeatSender_ForbiddenTriggersReRegister asserts a 403 on the
// heartbeat causes exactly one re-registration, after which the retried
// heartbeat succeeds (#675).
func TestHTTPHeartbeatSender_ForbiddenTriggersReRegister(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rereg := &staticReregisterer{errs: []error{nil}}
	tokens := &staticToken{tokens: []string{"tok-1", "tok-2"}}
	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr:   srv.URL,
		TenantID:     "tenant-a",
		HTTPClient:   tlsClientFromServer(srv),
		Tokens:       tokens,
		Logger:       &testLogger{t: t},
		Reregisterer: rereg,
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	if err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{}); err != nil {
		t.Fatalf("SendHeartbeat after re-register should succeed: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server hits: got %d, want 2 (403, then 200)", got)
	}
	if got := rereg.calls.Load(); got != 1 {
		t.Errorf("re-registrations: got %d, want 1", got)
	}
}

// TestHTTPHeartbeatSender_ForbiddenWithoutReregistererSurfaces asserts a 403
// is reported as a send error carrying the status when no Reregisterer is
// wired.
func TestHTTPHeartbeatSender_ForbiddenWithoutReregistererSurfaces(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})

	err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	var se *HeartbeatSendError
	if !errors.As(err, &se) || se.Status != http.StatusForbidden {
		t.Fatalf("expected HeartbeatSendError with status=403, got %v", err)
	}
}

// TestHTTPHeartbeatSender_TransientErrorsDoNotReRegister asserts 5xx and
// network failures are not scope problems and never re-register.
func TestHTTPHeartbeatSender_TransientErrorsDoNotReRegister(t *testing.T) {
	t.Parallel()

	t.Run("5xx", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)
		rereg := &staticReregisterer{errs: []error{nil}}
		sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
			ServerAddr:   srv.URL,
			TenantID:     "tenant-a",
			HTTPClient:   tlsClientFromServer(srv),
			Tokens:       &staticToken{tokens: []string{"tok-1"}},
			Logger:       &testLogger{t: t},
			Reregisterer: rereg,
		})

		err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
		var se *HeartbeatSendError
		if !errors.As(err, &se) || se.Status != http.StatusServiceUnavailable {
			t.Fatalf("expected HeartbeatSendError with status=503, got %v", err)
		}
		if got := rereg.calls.Load(); got != 0 {
			t.Errorf("5xx must not re-register; got %d calls", got)
		}
	})

	t.Run("network", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		client := tlsClientFromServer(srv)
		addr := srv.URL
		srv.Close() // connections to a closed server fail at the transport

		rereg := &staticReregisterer{errs: []error{nil}}
		sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
			ServerAddr:   addr,
			TenantID:     "tenant-a",
			HTTPClient:   client,
			Tokens:       &staticToken{tokens: []string{"tok-1"}},
			Logger:       &testLogger{t: t},
			Reregisterer: rereg,
		})

		if err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{}); err == nil {
			t.Fatal("expected a transport error")
		}
		if got := rereg.calls.Load(); got != 0 {
			t.Errorf("network error must not re-register; got %d calls", got)
		}
	})
}

// TestCachingTokenSource_InvalidScopeTriggersReenrollAndRetry asserts an
// invalid_scope token error re-registers once and the retried exchange
// returns the recovered token (#675).
func TestCachingTokenSource_InvalidScopeTriggersReenrollAndRetry(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		var refreshCalls atomic.Int32
		rereg := &staticReregisterer{errs: []error{nil}}
		src := &CachingTokenSource{
			Refresh: func(_ context.Context) (string, time.Time, error) {
				if refreshCalls.Add(1) == 1 {
					return "", time.Time{}, &EnrollmentTokenExchangeError{
						Op: "token response", Status: status, Detail: invalidScopeBody,
					}
				}
				return "fresh-jwt", time.Now().Add(time.Hour), nil
			},
			Reregisterer: rereg,
			Logger:       &testLogger{t: t},
		}

		tok, err := src.Token(context.Background())
		if err != nil || tok != "fresh-jwt" {
			t.Fatalf("status %d: Token = %q, %v; want fresh-jwt", status, tok, err)
		}
		if got := rereg.calls.Load(); got != 1 {
			t.Errorf("status %d: re-registrations: got %d, want 1", status, got)
		}
		if got := refreshCalls.Load(); got != 2 {
			t.Errorf("status %d: Refresh calls: got %d, want 2", status, got)
		}
	}
}

// TestIsInvalidScopeTokenExchangeError covers the matcher's positive and
// negative inputs.
func TestIsInvalidScopeTokenExchangeError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"compact body", &EnrollmentTokenExchangeError{Detail: invalidScopeBody}, true},
		{"spaced body", &EnrollmentTokenExchangeError{Detail: `{"error": "invalid_scope"}`}, true},
		{"other code", &EnrollmentTokenExchangeError{Detail: invalidClientBody}, false},
		{"non-exchange error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := isInvalidScopeTokenExchangeError(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
