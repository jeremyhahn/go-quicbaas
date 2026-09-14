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
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// dialRefused builds the error shape net/http actually returns when the
// control plane is not listening: url.Error → net.OpError → os.SyscallError
// → syscall.ECONNREFUSED.
func dialRefused() error {
	return &url.Error{
		Op:  "Post",
		URL: "https://control-plane.test:18080/oauth2/token",
		Err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
		},
	}
}

// dnsFailure builds the error shape returned when the control plane's
// hostname does not resolve yet.
func dnsFailure() error {
	return &url.Error{
		Op:  "Post",
		URL: "https://control-plane.test:18080/oauth2/token",
		Err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{Err: "no such host", Name: "control-plane.test", IsNotFound: true},
		},
	}
}

// tlsVerifyFailure builds the error shape returned when the presented
// server certificate does not chain to the configured CA bundle.
func tlsVerifyFailure() error {
	return &url.Error{
		Op:  "Post",
		URL: "https://control-plane.test:18080/oauth2/token",
		Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
	}
}

func TestClassifyEnrollmentError_Buckets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		err   error
		class EnrollmentErrorClass
	}{
		{
			name:  "token exchange connection refused",
			err:   &EnrollmentTokenExchangeError{Op: "post token request", Cause: dialRefused()},
			class: EnrollmentTransient,
		},
		{
			name:  "register connection refused",
			err:   &EnrollmentRejectedError{Status: 0, Cause: dialRefused()},
			class: EnrollmentTransient,
		},
		{
			name:  "dns not found",
			err:   &EnrollmentRejectedError{Status: 0, Cause: dnsFailure()},
			class: EnrollmentTransient,
		},
		{
			name:  "dial deadline exceeded",
			err:   &EnrollmentTokenExchangeError{Op: "post token request", Cause: context.DeadlineExceeded},
			class: EnrollmentTransient,
		},
		{
			name:  "network unreachable",
			err:   &EnrollmentRejectedError{Cause: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH}},
			class: EnrollmentTransient,
		},
		{
			name:  "bad gateway",
			err:   &EnrollmentRejectedError{Status: 502, Detail: "bad gateway"},
			class: EnrollmentTransient,
		},
		{
			name:  "service unavailable",
			err:   &EnrollmentTokenExchangeError{Op: "post token request", Status: 503, Detail: "starting up"},
			class: EnrollmentTransient,
		},
		{
			name:  "gateway timeout",
			err:   &EnrollmentRejectedError{Status: 504, Detail: "upstream timeout"},
			class: EnrollmentTransient,
		},
		{
			name:  "invalid_client",
			err:   &EnrollmentTokenExchangeError{Op: "post token request", Status: 401, Detail: `{"error":"invalid_client"}`},
			class: EnrollmentTerminal,
		},
		{
			name:  "forbidden",
			err:   &EnrollmentRejectedError{Status: 403, Detail: "tenant not permitted"},
			class: EnrollmentTerminal,
		},
		{
			name:  "bad request",
			err:   &EnrollmentRejectedError{Status: 400, Detail: "malformed registration"},
			class: EnrollmentTerminal,
		},
		{
			name:  "tls verification",
			err:   &EnrollmentTokenExchangeError{Op: "post token request", Cause: tlsVerifyFailure()},
			class: EnrollmentTerminal,
		},
		{
			name:  "tls client construction",
			err:   &EnrollmentTLSError{Op: "load agent keypair", Cause: os.ErrNotExist},
			class: EnrollmentTerminal,
		},
		{
			name:  "credentials read",
			err:   &EnrollmentCredentialsReadError{Path: "/x/credentials.json", Cause: os.ErrPermission},
			class: EnrollmentTerminal,
		},
		{
			name:  "credentials save",
			err:   &EnrollmentCredentialsSaveError{Path: "/x/credentials.json", Cause: os.ErrPermission},
			class: EnrollmentTerminal,
		},
		{
			name:  "nil config",
			err:   ErrConfigNil,
			class: EnrollmentTerminal,
		},
		{
			name:  "missing config field",
			err:   &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "server_addr"},
			class: EnrollmentTerminal,
		},
		{
			name:  "context canceled",
			err:   &EnrollmentTokenExchangeError{Op: "post token request", Cause: context.Canceled},
			class: EnrollmentTerminal,
		},
		{
			name:  "unrecognized failure defaults to terminal",
			err:   errors.New("boom"),
			class: EnrollmentTerminal,
		},
		{
			name:  "nil error is terminal",
			err:   nil,
			class: EnrollmentTerminal,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyEnrollmentError(tc.err); got != tc.class {
				t.Fatalf("ClassifyEnrollmentError(%v) = %s, want %s", tc.err, got, tc.class)
			}
		})
	}
}

// TestClassifyEnrollmentError_ReasonNamesTheBucket asserts the operator
// facing reason string identifies why the daemon made the call, so the
// journal line is self-explanatory without reading the source.
func TestClassifyEnrollmentError_ReasonNamesTheBucket(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"refused", &EnrollmentRejectedError{Cause: dialRefused()}, "connection refused"},
		{"dns", &EnrollmentRejectedError{Cause: dnsFailure()}, "dns resolution failed"},
		{"unavailable", &EnrollmentRejectedError{Status: 503}, "control plane unavailable"},
		{"invalid_client", &EnrollmentTokenExchangeError{Status: 401, Detail: `{"error":"invalid_client"}`}, "invalid_client"},
		{"rejected", &EnrollmentRejectedError{Status: 403}, "rejected"},
		{"tls", &EnrollmentTLSError{Op: "load agent keypair", Cause: os.ErrNotExist}, "tls"},
		{"unknown", errors.New("boom"), "unrecognized"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, reason := classifyEnrollment(tc.err)
			if !strings.Contains(reason, tc.want) {
				t.Fatalf("reason %q does not contain %q", reason, tc.want)
			}
		})
	}
}

func TestEnrollmentErrorClass_String(t *testing.T) {
	t.Parallel()
	if got := EnrollmentTransient.String(); got != "transient" {
		t.Errorf("transient: got %q", got)
	}
	if got := EnrollmentTerminal.String(); got != "terminal" {
		t.Errorf("terminal: got %q", got)
	}
	if got := EnrollmentErrorClass(99).String(); got != "unknown" {
		t.Errorf("unknown: got %q", got)
	}
}

func TestEnrollBackoff_WindowGrowsAndCaps(t *testing.T) {
	t.Parallel()
	b := enrollBackoff{base: time.Second, max: 8 * time.Second}
	want := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		8 * time.Second,
		8 * time.Second,
	}
	for i, w := range want {
		if got := b.window(i + 1); got != w {
			t.Errorf("window(%d) = %s, want %s", i+1, got, w)
		}
	}
	// A pathological attempt counter must not overflow into a negative
	// or tiny duration; the cap holds for every input.
	for _, attempt := range []int{0, -5, 62, 63, 64, 1 << 20} {
		got := b.window(attempt)
		if got < time.Second || got > 8*time.Second {
			t.Errorf("window(%d) = %s, want within [1s, 8s]", attempt, got)
		}
	}
}

func TestEnrollBackoff_DelayIsJitteredWithinWindow(t *testing.T) {
	t.Parallel()
	b := enrollBackoff{base: 100 * time.Millisecond, max: 400 * time.Millisecond}
	distinct := map[time.Duration]struct{}{}
	for attempt := 1; attempt <= 8; attempt++ {
		window := b.window(attempt)
		for i := 0; i < 64; i++ {
			d, err := b.delay(attempt)
			if err != nil {
				t.Fatalf("delay(%d): %v", attempt, err)
			}
			if d < window/2 || d > window {
				t.Fatalf("delay(%d) = %s, want within [%s, %s]", attempt, d, window/2, window)
			}
			if d > b.max {
				t.Fatalf("delay(%d) = %s exceeds cap %s", attempt, d, b.max)
			}
			distinct[d] = struct{}{}
		}
	}
	// Jitter must actually vary; a fixed schedule re-synchronizes a
	// whole fleet onto the same retry instant.
	if len(distinct) < 8 {
		t.Fatalf("delay produced only %d distinct values; jitter is not applied", len(distinct))
	}
}

func TestEnrollmentTerminalError_MessageAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := &EnrollmentTokenExchangeError{Status: 401, Detail: `{"error":"invalid_client"}`}
	err := &EnrollmentTerminalError{Reason: "credentials rejected: invalid_client", Cause: cause}
	msg := err.Error()
	for _, want := range []string{"terminal", "invalid_client", "not retrying"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	if !errors.Is(err, ErrEnrollmentTokenExchange) {
		t.Error("terminal error must unwrap to the underlying cause")
	}
	if !errors.Is(err, ErrEnrollmentTerminal) {
		t.Error("terminal error must match the ErrEnrollmentTerminal sentinel")
	}
}

func TestEnrollmentAbandonedError_MessageAndUnwrap(t *testing.T) {
	t.Parallel()
	last := &EnrollmentRejectedError{Cause: dialRefused()}
	err := &EnrollmentAbandonedError{Attempts: 7, Cause: context.Canceled, Last: last}
	msg := err.Error()
	for _, want := range []string{"7", "connection refused"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("abandoned error must unwrap to the context cause")
	}
	if !errors.Is(err, ErrEnrollmentRejected) {
		t.Error("abandoned error must unwrap to the last enrollment failure")
	}
	// A nil Last must not panic and must still carry the context cause.
	bare := &EnrollmentAbandonedError{Attempts: 1, Cause: context.DeadlineExceeded}
	if !errors.Is(bare, context.DeadlineExceeded) {
		t.Error("abandoned error without a last failure must unwrap to the context cause")
	}
	if bare.Error() == "" {
		t.Error("abandoned error message must not be empty")
	}
}

// TestEnrollmentTypedErrors_ExposeTheirCause pins the multi-unwrap
// contract the classifier depends on: the sentinel stays matchable and
// the transport cause becomes reachable through errors.Is / errors.As.
func TestEnrollmentTypedErrors_ExposeTheirCause(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"rejected", &EnrollmentRejectedError{Cause: dialRefused()}, ErrEnrollmentRejected},
		{"request", &EnrollmentRequestError{Op: "build register url", Cause: dialRefused()}, ErrEnrollmentRejected},
		{"exchange", &EnrollmentTokenExchangeError{Op: "post", Cause: dialRefused()}, ErrEnrollmentTokenExchange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(tc.err, tc.sentinel) {
				t.Errorf("%s must still match its sentinel", tc.name)
			}
			if !errors.Is(tc.err, syscall.ECONNREFUSED) {
				t.Errorf("%s must expose its transport cause", tc.name)
			}
			var opErr *net.OpError
			if !errors.As(tc.err, &opErr) {
				t.Errorf("%s must expose its *net.OpError cause", tc.name)
			}
		})
	}
	// A nil cause must not break sentinel matching or panic on unwrap.
	bare := &EnrollmentRejectedError{Status: 403}
	if !errors.Is(bare, ErrEnrollmentRejected) {
		t.Error("cause-free rejection must still match its sentinel")
	}
	if errors.Is(bare, syscall.ECONNREFUSED) {
		t.Error("cause-free rejection must not match an unrelated error")
	}
}

// failingReader always errors, standing in for an entropy source that is
// unavailable.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// TestEnrollBackoff_DelayWithoutEntropy asserts that losing the jitter
// source degrades to the full window and reports why, rather than
// collapsing the wait to zero and reintroducing the spin.
func TestEnrollBackoff_DelayWithoutEntropy(t *testing.T) {
	t.Parallel()
	boom := errors.New("entropy pool drained")
	b := enrollBackoff{
		base:    2 * time.Second,
		max:     8 * time.Second,
		entropy: failingReader{err: boom},
	}
	d, err := b.delay(3)
	if err == nil {
		t.Fatal("delay must report an unusable jitter source")
	}
	if !errors.Is(err, boom) {
		t.Errorf("delay err must wrap the reader failure, got %v", err)
	}
	var jitterErr *EnrollmentJitterError
	if !errors.As(err, &jitterErr) {
		t.Errorf("delay err: got %T, want *EnrollmentJitterError", err)
	}
	if want := b.window(3); d != want {
		t.Errorf("delay = %s, want the full window %s", d, want)
	}
	if !strings.Contains(err.Error(), "jitter unavailable") {
		t.Errorf("error message %q does not explain the degradation", err.Error())
	}
}

// TestEnrollBackoff_DelayZeroWindow covers the degenerate configuration
// where the window is too small to split, which must not divide by zero
// or return a negative wait.
func TestEnrollBackoff_DelayZeroWindow(t *testing.T) {
	t.Parallel()
	b := enrollBackoff{base: time.Nanosecond, max: time.Nanosecond}
	d, err := b.delay(1)
	if err != nil {
		t.Fatalf("delay: %v", err)
	}
	if d != time.Nanosecond {
		t.Errorf("delay = %s, want %s", d, time.Nanosecond)
	}
}

// TestClassifyEnrollmentError_CertificateShapes covers the remaining
// X.509 failure shapes an agent meets in the field: a certificate whose
// SANs do not cover the dialed host, and one that is expired or otherwise
// structurally invalid. Both are operator-fixable, so both are terminal.
func TestClassifyEnrollmentError_CertificateShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "hostname mismatch",
			err: &EnrollmentRejectedError{Cause: &url.Error{
				Op:  "Post",
				URL: "https://control-plane.test:18080/",
				Err: x509.HostnameError{Host: "control-plane.test"},
			}},
		},
		{
			name: "expired certificate",
			err: &EnrollmentTokenExchangeError{Op: "post token request", Cause: &url.Error{
				Op:  "Post",
				URL: "https://control-plane.test:18080/oauth2/token",
				Err: x509.CertificateInvalidError{Reason: x509.Expired},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, reason := classifyEnrollment(tc.err)
			if class != EnrollmentTerminal {
				t.Errorf("class: got %s, want terminal", class)
			}
			if !strings.Contains(reason, "tls server certificate not trusted") {
				t.Errorf("reason %q does not name the trust failure", reason)
			}
		})
	}
}

// TestEnrollmentTerminalError_WithoutCause covers the degenerate case
// where the classifier reported a reason but no underlying error.
func TestEnrollmentTerminalError_WithoutCause(t *testing.T) {
	t.Parallel()
	err := &EnrollmentTerminalError{Reason: "unrecognized enrollment failure"}
	if !errors.Is(err, ErrEnrollmentTerminal) {
		t.Error("cause-free terminal error must still match its sentinel")
	}
	if !strings.Contains(err.Error(), "unrecognized enrollment failure") {
		t.Errorf("message %q does not carry the reason", err.Error())
	}
}

// TestEnrollBackoff_WindowClampsABaseAboveTheCap pins the guard for a
// misordered pair reaching enrollBackoff directly: the first wait is the
// cap, never the larger base.
func TestEnrollBackoff_WindowClampsABaseAboveTheCap(t *testing.T) {
	t.Parallel()
	b := enrollBackoff{base: time.Minute, max: time.Second}
	if got := b.window(1); got != time.Second {
		t.Errorf("window(1) = %s, want the cap %s", got, time.Second)
	}
}
