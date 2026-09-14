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

package agent

import (
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// enrollment.go error types
// =============================================================================

func TestEnrollmentRejectedError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("transport failure")
	cases := []struct {
		name    string
		err     *EnrollmentRejectedError
		wantIn  []string
		unwraps error
	}{
		{
			name:    "status and detail and cause",
			err:     &EnrollmentRejectedError{Status: 401, Detail: "invalid_token", Cause: cause},
			wantIn:  []string{"status=401", "invalid_token", "transport failure"},
			unwraps: ErrEnrollmentRejected,
		},
		{
			name:    "zero status no cause",
			err:     &EnrollmentRejectedError{Status: 0, Detail: "missing field"},
			wantIn:  []string{"missing field"},
			unwraps: ErrEnrollmentRejected,
		},
		{
			name:    "empty detail and cause",
			err:     &EnrollmentRejectedError{Status: 403},
			wantIn:  []string{"status=403"},
			unwraps: ErrEnrollmentRejected,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, want := range tc.wantIn {
				if !strings.Contains(msg, want) {
					t.Errorf("Error() = %q missing %q", msg, want)
				}
			}
			if !errors.Is(tc.err, tc.unwraps) {
				t.Errorf("Unwrap: errors.Is failed for sentinel %v", tc.unwraps)
			}
		})
	}
}

func TestEnrollmentRequestError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("bad url")
	err := &EnrollmentRequestError{Op: "build enroll url", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "build enroll url") {
		t.Errorf("Error() missing op: %q", msg)
	}
	if !strings.Contains(msg, "bad url") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, ErrEnrollmentRejected) {
		t.Error("Unwrap should return ErrEnrollmentRejected")
	}
	// Unwrap exposes the sentinel and the cause: the enrollment classifier
	// triages on the transport failure underneath the rejection.
	unwrapped := err.Unwrap()
	if len(unwrapped) != 2 || unwrapped[0] != ErrEnrollmentRejected || unwrapped[1] != cause {
		t.Errorf("Unwrap() = %v, want [ErrEnrollmentRejected, cause]", unwrapped)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is should reach the wrapped cause")
	}
}

func TestEnrollmentCredentialsReadError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("unexpected EOF")
	err := &EnrollmentCredentialsReadError{Path: "/var/agent/credentials.json", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "/var/agent/credentials.json") {
		t.Errorf("Error() missing path: %q", msg)
	}
	if !strings.Contains(msg, "unexpected EOF") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, ErrEnrollmentCredentialsRead) {
		t.Error("errors.Is(err, ErrEnrollmentCredentialsRead) should be true")
	}
}

func TestEnrollmentCredentialsSaveError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("disk full")
	err := &EnrollmentCredentialsSaveError{Path: "/var/agent/credentials.json", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "/var/agent/credentials.json") {
		t.Errorf("Error() missing path: %q", msg)
	}
	if !strings.Contains(msg, "disk full") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, ErrEnrollmentCredentialsSave) {
		t.Error("errors.Is(err, ErrEnrollmentCredentialsSave) should be true")
	}
}

func TestEnrollmentTokenExchangeError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("timeout")
	cases := []struct {
		name   string
		err    *EnrollmentTokenExchangeError
		wantIn []string
	}{
		{
			name:   "all fields set",
			err:    &EnrollmentTokenExchangeError{Op: "build token request", Status: 401, Detail: "invalid_client", Cause: cause},
			wantIn: []string{"build token request", "status=401", "invalid_client", "timeout"},
		},
		{
			name:   "no cause",
			err:    &EnrollmentTokenExchangeError{Op: "exchange", Status: 503},
			wantIn: []string{"exchange", "status=503"},
		},
		{
			name:   "empty op and status",
			err:    &EnrollmentTokenExchangeError{Detail: "some detail"},
			wantIn: []string{"some detail"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, want := range tc.wantIn {
				if !strings.Contains(msg, want) {
					t.Errorf("Error() = %q missing %q", msg, want)
				}
			}
			if !errors.Is(tc.err, ErrEnrollmentTokenExchange) {
				t.Error("errors.Is sentinel failed")
			}
		})
	}
}

// =============================================================================
// metrics.go error types
// =============================================================================

func TestMetricsRegisterError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("descriptor conflict")
	err := &MetricsRegisterError{Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "descriptor conflict") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !strings.Contains(msg, "metrics") {
		t.Errorf("Error() missing 'metrics' prefix: %q", msg)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) via Unwrap should be true")
	}
}

// =============================================================================
// runtime.go error types
// =============================================================================

func TestPersistSessionIOError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("permission denied")
	err := &PersistSessionIOError{Op: "mkdir", Path: "/data/sessions", Err: cause}

	msg := err.Error()
	if !strings.Contains(msg, "mkdir") {
		t.Errorf("Error() missing op: %q", msg)
	}
	if !strings.Contains(msg, "/data/sessions") {
		t.Errorf("Error() missing path: %q", msg)
	}
	if !strings.Contains(msg, "permission denied") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, cause) {
		t.Error("Unwrap should chain to original cause")
	}
}

func TestPersistSessionEncodeError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("json: unsupported value")
	err := &PersistSessionEncodeError{SessionID: "sess-abc", Err: cause}

	msg := err.Error()
	if !strings.Contains(msg, "sess-abc") {
		t.Errorf("Error() missing session ID: %q", msg)
	}
	if !strings.Contains(msg, "json: unsupported value") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, cause) {
		t.Error("Unwrap should chain to original cause")
	}
}

func TestPersistSessionDecodeError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("unexpected EOF")
	err := &PersistSessionDecodeError{Path: "/sessions/abc.json", Err: cause}

	msg := err.Error()
	if !strings.Contains(msg, "/sessions/abc.json") {
		t.Errorf("Error() missing path: %q", msg)
	}
	if !strings.Contains(msg, "unexpected EOF") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, cause) {
		t.Error("Unwrap should chain to original cause")
	}
}

func TestPersistedSessionCorruptError_Error(t *testing.T) {
	t.Parallel()
	err := &PersistedSessionCorruptError{Count: 3}
	msg := err.Error()
	if !strings.Contains(msg, "3") {
		t.Errorf("Error() missing count: %q", msg)
	}
	if !strings.Contains(msg, "corrupt") {
		t.Errorf("Error() missing 'corrupt': %q", msg)
	}
}

// =============================================================================
// spawn_server.go error types
// =============================================================================

func TestSpawnServerBindError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("address already in use")
	err := &SpawnServerBindError{Addr: "127.0.0.1:9090", Err: cause}

	msg := err.Error()
	if !strings.Contains(msg, "127.0.0.1:9090") {
		t.Errorf("Error() missing addr: %q", msg)
	}
	if !strings.Contains(msg, "address already in use") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, cause) {
		t.Error("Unwrap should chain to original cause")
	}
}

func TestSpawnServerServeError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("listener closed")
	err := &SpawnServerServeError{Err: cause}

	msg := err.Error()
	if !strings.Contains(msg, "listener closed") {
		t.Errorf("Error() missing cause: %q", msg)
	}
	if !errors.Is(err, cause) {
		t.Error("Unwrap should chain to original cause")
	}
}
