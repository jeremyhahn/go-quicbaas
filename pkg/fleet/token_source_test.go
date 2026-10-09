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
	"sync/atomic"
	"testing"
	"time"
)

// invalidClientBody is the literal OAuth2 error body the control plane emits
// (pkg/iam/oidc.go) when the presented client_secret no longer matches
// the stored hash for the client_id. The token source's
// shouldReregister hook matches against this exact wire shape.
const invalidClientBody = `{"error":"invalid_client","error_description":"client authentication failed"}`

// invalidClientTokenError returns the *EnrollmentTokenExchangeError
// shape RefreshTokenFn produces when the control plane rejects the
// /oauth2/token POST with a 401 + invalid_client OAuth2 error body.
// Tests use this to drive CachingTokenSource.Token() down the
// re-enrollment recovery path.
func invalidClientTokenError() error {
	return &EnrollmentTokenExchangeError{
		Op:     "token response",
		Status: http.StatusUnauthorized,
		Detail: invalidClientBody,
	}
}

// TestCachingTokenSource_InvalidClientTriggersReenrollAndRetry asserts
// that when the cached client_secret is no longer trusted by the
// control plane (401 invalid_client on /oauth2/token), the token source:
//
//  1. Calls Reregisterer.Reregister(ctx) to mint fresh credentials.
//  2. Retries the Refresh once with the new credentials.
//  3. Returns the recovered access_token to the caller, transparently.
//
// This is symmetrical to the 404-on-heartbeat re-register path: a
// transient credentials wipe at the control plane must self-heal without
// operator intervention.
func TestCachingTokenSource_InvalidClientTriggersReenrollAndRetry(t *testing.T) {
	t.Parallel()

	var refreshCalls atomic.Int32
	rereg := &staticReregisterer{errs: []error{nil}}
	src := &CachingTokenSource{
		Refresh: func(_ context.Context) (string, time.Time, error) {
			n := refreshCalls.Add(1)
			if n == 1 {
				return "", time.Time{}, invalidClientTokenError()
			}
			return "fresh-jwt", time.Now().Add(time.Hour), nil
		},
		Reregisterer: rereg,
		Logger:       &testLogger{t: t},
	}

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after re-enroll should succeed: %v", err)
	}
	if tok != "fresh-jwt" {
		t.Errorf("token: got %q, want %q", tok, "fresh-jwt")
	}
	if got := refreshCalls.Load(); got != 2 {
		t.Errorf("expected 2 Refresh calls (initial 401, then post-re-enroll retry), got %d", got)
	}
	if got := rereg.calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 Reregister call, got %d", got)
	}
}

// TestCachingTokenSource_InvalidClientReregisterBudgetExhausted asserts
// that maxConsecutiveReregisterFailures consecutive Reregister
// failures surface a typed *ReregisterFailedError that unwraps to
// ErrNodeReRegisterFailed. The daemon turns this into a clean exit
// so systemd restarts the unit and the next process retries from a
// clean slate.
func TestCachingTokenSource_InvalidClientReregisterBudgetExhausted(t *testing.T) {
	t.Parallel()

	regErr := errors.New("control plane refused enrollment")
	rereg := &staticReregisterer{errs: []error{regErr, regErr, regErr}}
	src := &CachingTokenSource{
		Refresh: func(_ context.Context) (string, time.Time, error) {
			return "", time.Time{}, invalidClientTokenError()
		},
		Reregisterer: rereg,
		Logger:       &testLogger{t: t},
	}

	var lastErr error
	for i := 0; i < int(maxConsecutiveReregisterFailures); i++ {
		// Each call must hit Refresh — invalidate the cache up front
		// so the recovery path runs every iteration. (After a failed
		// re-enroll the cache is already empty, but explicit
		// invalidation keeps the test independent of internal state.)
		src.Invalidate()
		_, lastErr = src.Token(context.Background())
	}
	if !errors.Is(lastErr, ErrNodeReRegisterFailed) {
		t.Fatalf("after %d re-register failures expected ErrNodeReRegisterFailed, got %v",
			maxConsecutiveReregisterFailures, lastErr)
	}
	var failed *ReregisterFailedError
	if !errors.As(lastErr, &failed) {
		t.Fatalf("expected *ReregisterFailedError, got %T", lastErr)
	}
	if failed.Attempts != int(maxConsecutiveReregisterFailures) {
		t.Errorf("Attempts: got %d, want %d", failed.Attempts, maxConsecutiveReregisterFailures)
	}
	if rereg.calls.Load() != maxConsecutiveReregisterFailures {
		t.Errorf("Reregister calls: got %d, want %d",
			rereg.calls.Load(), maxConsecutiveReregisterFailures)
	}
}

// TestCachingTokenSource_NonInvalidClient401DoesNotReenroll asserts
// that a 401 carrying a different OAuth2 error code (e.g.
// invalid_grant) does NOT trigger client-side re-enrollment. The
// recovery path is reserved for the specific case where the control plane
// repudiates the cached client_secret; other auth failures must
// propagate to the caller unchanged so the operator sees the actual
// cause (misconfigured scope set, malformed grant, etc.).
func TestCachingTokenSource_NonInvalidClient401DoesNotReenroll(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "invalid_grant",
			body: `{"error":"invalid_grant","error_description":"unsupported grant type"}`,
		},
		{
			name: "empty_body",
			body: ``,
		},
		{
			name: "non_json_body",
			body: `<html>401 Unauthorized</html>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rereg := &staticReregisterer{errs: []error{nil}}
			src := &CachingTokenSource{
				Refresh: func(_ context.Context) (string, time.Time, error) {
					return "", time.Time{}, &EnrollmentTokenExchangeError{
						Op:     "token response",
						Status: http.StatusUnauthorized,
						Detail: tc.body,
					}
				},
				Reregisterer: rereg,
				Logger:       &testLogger{t: t},
			}

			_, err := src.Token(context.Background())
			if !errors.Is(err, ErrEnrollmentTokenExchange) {
				t.Fatalf("expected propagated ErrEnrollmentTokenExchange, got %v", err)
			}
			if rereg.calls.Load() != 0 {
				t.Errorf("re-enroll must NOT trigger for %s; got %d calls",
					tc.name, rereg.calls.Load())
			}
		})
	}
}

// TestCachingTokenSource_NoReregistererPropagates401 asserts that a
// CachingTokenSource configured without a Reregisterer hook (e.g.
// during early bootstrap or in tests) propagates the 401 unchanged
// instead of panicking or hanging.
func TestCachingTokenSource_NoReregistererPropagates401(t *testing.T) {
	t.Parallel()
	src := &CachingTokenSource{
		Refresh: func(_ context.Context) (string, time.Time, error) {
			return "", time.Time{}, invalidClientTokenError()
		},
	}

	_, err := src.Token(context.Background())
	if !errors.Is(err, ErrEnrollmentTokenExchange) {
		t.Fatalf("expected propagated token-exchange error, got %v", err)
	}
}

// TestCachingTokenSource_SetReregisterer asserts the post-construction
// setter wires the hook in and a subsequent invalid_client failure
// drives the re-enroll path. Tests the symmetry with
// HTTPHeartbeatSender.Reregisterer wiring through agent_serve.go.
func TestCachingTokenSource_SetReregisterer(t *testing.T) {
	t.Parallel()

	var refreshCalls atomic.Int32
	rereg := &staticReregisterer{errs: []error{nil}}
	src := &CachingTokenSource{
		Refresh: func(_ context.Context) (string, time.Time, error) {
			n := refreshCalls.Add(1)
			if n == 1 {
				return "", time.Time{}, invalidClientTokenError()
			}
			return "recovered", time.Now().Add(time.Hour), nil
		},
	}
	src.SetReregisterer(rereg)
	src.SetLogger(&testLogger{t: t})

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "recovered" {
		t.Errorf("token: got %q, want %q", tok, "recovered")
	}
	if rereg.calls.Load() != 1 {
		t.Errorf("Reregister calls: got %d, want 1", rereg.calls.Load())
	}
}

// TestCachingTokenSource_NonTokenExchangeErrorPropagatesUnchanged
// asserts that an arbitrary error from Refresh (network, decode, etc.)
// that is NOT an *EnrollmentTokenExchangeError never triggers
// re-enrollment. Only the control plane's explicit invalid_client signal is
// treated as evidence that the cached credentials are dead.
func TestCachingTokenSource_NonTokenExchangeErrorPropagatesUnchanged(t *testing.T) {
	t.Parallel()

	rawErr := errors.New("dial tcp: connection refused")
	rereg := &staticReregisterer{errs: []error{nil}}
	src := &CachingTokenSource{
		Refresh: func(_ context.Context) (string, time.Time, error) {
			return "", time.Time{}, rawErr
		},
		Reregisterer: rereg,
		Logger:       &testLogger{t: t},
	}

	_, err := src.Token(context.Background())
	if !errors.Is(err, rawErr) {
		t.Fatalf("expected propagated raw error, got %v", err)
	}
	if rereg.calls.Load() != 0 {
		t.Errorf("re-enroll must NOT trigger for non-401 errors, got %d calls", rereg.calls.Load())
	}
}
