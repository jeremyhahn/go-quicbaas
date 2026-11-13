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

// Enrollment failure triage.
//
// A daemon that treats every enrollment failure as fatal turns a control
// plane which simply has not started yet into an unbounded supervisor
// restart loop: the process lives a few milliseconds, exits 1, and the
// unit sits in activating (auto-restart) forever, which reads as almost
// healthy and never trips an alert.
//
// The two failure modes deserve opposite handling:
//
//   - TRANSIENT — nothing about the agent's configuration is wrong; the
//     other end is not answering yet. Waiting fixes it, so the daemon
//     waits, announcing each wait so the journal shows a daemon that is
//     waiting rather than one that is wedged.
//   - TERMINAL — the agent will never succeed as configured. Exiting is
//     correct: the unit lands in failed, where monitoring can see it.
//
// Anything unrecognized is TERMINAL. A silent retry on a misconfiguration
// hides the misconfiguration, which is the more expensive mistake.

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// EnrollmentErrorClass triages an enrollment failure into the bucket that
// decides whether the daemon retries or exits.
type EnrollmentErrorClass uint8

const (
	// EnrollmentTerminal marks a failure retrying cannot fix. It is the
	// zero value deliberately: an error nobody classified must not be
	// mistaken for one that is safe to retry forever.
	EnrollmentTerminal EnrollmentErrorClass = iota

	// EnrollmentTransient marks a failure that reflects the control plane
	// being unreachable right now rather than the agent being wrong.
	EnrollmentTransient
)

// String returns the operator-facing class label.
func (c EnrollmentErrorClass) String() string {
	switch c {
	case EnrollmentTerminal:
		return "terminal"
	case EnrollmentTransient:
		return "transient"
	default:
		return "unknown"
	}
}

// ClassifyEnrollmentError reports whether err is worth retrying. See
// classifyEnrollment for the rule table and the reason strings.
func ClassifyEnrollmentError(err error) EnrollmentErrorClass {
	class, _ := classifyEnrollment(err)
	return class
}

// enrollmentRule pairs a predicate over an enrollment error with the class
// it implies and the reason string the daemon logs.
type enrollmentRule struct {
	class  EnrollmentErrorClass
	reason func(error) string
	match  func(error) bool
}

// constReason adapts a fixed string to the enrollmentRule reason signature.
func constReason(s string) func(error) string {
	return func(error) string { return s }
}

// transientHTTPStatus lists the response codes that mean "the control
// plane is not ready", as opposed to "the control plane read your request
// and said no". Only the gateway/unavailable family qualifies: a 500 can
// just as easily be a deterministic server-side bug that no amount of
// waiting resolves, so it stays terminal.
var transientHTTPStatus = map[int]string{
	502: "bad gateway",
	503: "service unavailable",
	504: "gateway timeout",
}

// transientSyscalls lists the connect(2)/read(2) failures that mean the
// peer is absent or the path to it is not up yet. Every one of them is a
// condition that a control plane finishing its own boot resolves.
var transientSyscalls = map[syscall.Errno]string{
	syscall.ECONNREFUSED:  "connection refused",
	syscall.ECONNRESET:    "connection reset",
	syscall.ECONNABORTED:  "connection aborted",
	syscall.EHOSTUNREACH:  "host unreachable",
	syscall.EHOSTDOWN:     "host down",
	syscall.ENETUNREACH:   "network unreachable",
	syscall.ENETDOWN:      "network down",
	syscall.ETIMEDOUT:     "connect timed out",
	syscall.EPIPE:         "broken pipe",
	syscall.EADDRNOTAVAIL: "local address unavailable",
}

// enrollmentRules is evaluated in order and the first match wins.
//
// Terminal rules come first on purpose. A TLS verification failure or an
// authorization rejection can be reached through the same *url.Error a
// dial failure travels in, and misreading one of those as "the server is
// not up yet" would bury a real misconfiguration under an endless wait.
var enrollmentRules = []enrollmentRule{
	{
		// A cancelled context is the operator asking the daemon to stop.
		// Retrying would ignore the request.
		class:  EnrollmentTerminal,
		reason: constReason("shutdown requested: context canceled"),
		match:  func(err error) bool { return errors.Is(err, context.Canceled) },
	},
	{
		// A config defect cannot be fixed by the far end coming up.
		class:  EnrollmentTerminal,
		reason: constReason("invalid agent configuration"),
		match: func(err error) bool {
			return errors.Is(err, ErrConfigNil) || errors.Is(err, ErrConfigFieldRequired)
		},
	},
	{
		// Missing/corrupt key material, or a server certificate that does
		// not chain to the configured CA. Both are operator-fixable only.
		class:  EnrollmentTerminal,
		reason: certificateReason,
		match:  isCertificateFailure,
	},
	{
		// RFC 6749 invalid_client: the control plane read the credentials
		// and rejected them. Waiting changes nothing.
		class:  EnrollmentTerminal,
		reason: constReason("credentials rejected: invalid_client"),
		match:  isInvalidClientTokenExchangeError,
	},
	{
		// The credentials file could not be read or written. Retrying a
		// permission or corruption problem hides it.
		class:  EnrollmentTerminal,
		reason: constReason("credentials store unusable"),
		match: func(err error) bool {
			return errors.Is(err, ErrEnrollmentCredentialsRead) ||
				errors.Is(err, ErrEnrollmentCredentialsSave)
		},
	},
	{
		// The control plane answered, but only to say it is not ready.
		class:  EnrollmentTransient,
		reason: statusReason("control plane unavailable"),
		match: func(err error) bool {
			_, ok := transientHTTPStatus[enrollmentHTTPStatus(err)]
			return ok
		},
	},
	{
		// Any other answered request is a decision about this agent.
		class:  EnrollmentTerminal,
		reason: statusReason("control plane rejected the agent"),
		match:  func(err error) bool { return enrollmentHTTPStatus(err) != 0 },
	},
	{
		// The control plane's name does not resolve. At boot this is
		// routinely a resolver that has not started yet.
		class:  EnrollmentTransient,
		reason: constReason("dns resolution failed"),
		match: func(err error) bool {
			var dnsErr *net.DNSError
			return errors.As(err, &dnsErr)
		},
	},
	{
		// connect(2) said nobody is listening, or the route is not up.
		class:  EnrollmentTransient,
		reason: syscallReason,
		match:  func(err error) bool { _, ok := transientSyscall(err); return ok },
	},
	{
		// The dial or the request ran out of time. The control plane is
		// slow, saturated, or absent behind a black-holing firewall.
		class:  EnrollmentTransient,
		reason: constReason("request timed out"),
		match:  isTimeoutFailure,
	},
	{
		// Any remaining network-layer failure: the socket never carried a
		// request, so nothing about the agent's identity was judged.
		class:  EnrollmentTransient,
		reason: constReason("network unreachable"),
		match: func(err error) bool {
			var opErr *net.OpError
			return errors.As(err, &opErr)
		},
	},
}

// classifyEnrollment triages err and returns the class plus a short
// operator-facing reason naming why the daemon made that call.
func classifyEnrollment(err error) (EnrollmentErrorClass, string) {
	if err == nil {
		return EnrollmentTerminal, "no error reported"
	}
	for _, rule := range enrollmentRules {
		if rule.match(err) {
			return rule.class, rule.reason(err)
		}
	}
	return EnrollmentTerminal, "unrecognized enrollment failure"
}

// enrollmentHTTPStatus returns the HTTP status the control plane answered
// with, or 0 when the request never got a response. Both enrollment legs
// (register and token exchange) record their status on a typed error.
func enrollmentHTTPStatus(err error) int {
	var rejected *EnrollmentRejectedError
	if errors.As(err, &rejected) && rejected.Status != 0 {
		return rejected.Status
	}
	var exchange *EnrollmentTokenExchangeError
	if errors.As(err, &exchange) && exchange.Status != 0 {
		return exchange.Status
	}
	return 0
}

// statusReason builds a reason string that carries the answered status so
// the journal line explains itself without a second lookup.
func statusReason(prefix string) func(error) string {
	return func(err error) string {
		status := enrollmentHTTPStatus(err)
		label := transientHTTPStatus[status]
		reason := prefix + ": status=" + strconv.Itoa(status)
		if label != "" {
			reason += " (" + label + ")"
		}
		return reason
	}
}

// transientSyscall extracts the retryable errno underneath err, if any.
func transientSyscall(err error) (string, bool) {
	for errno, label := range transientSyscalls {
		if errors.Is(err, errno) {
			return label, true
		}
	}
	return "", false
}

// syscallReason names the errno that made the dial fail.
func syscallReason(err error) string {
	if label, ok := transientSyscall(err); ok {
		return label
	}
	return "network unreachable"
}

// isCertificateFailure reports whether err is an X.509 / TLS trust problem
// or a failure to assemble the daemon's own mTLS client.
func isCertificateFailure(err error) bool {
	var tlsSetup *EnrollmentTLSError
	if errors.As(err, &tlsSetup) {
		return true
	}
	var verify *tls.CertificateVerificationError
	if errors.As(err, &verify) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var hostname x509.HostnameError
	if errors.As(err, &hostname) {
		return true
	}
	var invalid x509.CertificateInvalidError
	return errors.As(err, &invalid)
}

// certificateReason distinguishes "we could not build our own identity"
// from "we did not trust theirs", because the two have different fixes.
func certificateReason(err error) string {
	var tlsSetup *EnrollmentTLSError
	if errors.As(err, &tlsSetup) {
		return "tls client identity unusable: " + tlsSetup.Op
	}
	return "tls server certificate not trusted"
}

// isTimeoutFailure reports whether err is a deadline expiry rather than a
// refusal. A parent context that is already done is caught by the retry
// loop's own cancellation check, so a timeout here means the dial or the
// request itself ran long.
func isTimeoutFailure(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// enrollBackoff is the capped exponential schedule the daemon waits on
// between transient enrollment attempts.
type enrollBackoff struct {
	base time.Duration
	max  time.Duration

	// entropy supplies the jitter. Nil means crypto/rand.Reader. Tests
	// inject a failing reader to exercise the degraded path.
	entropy io.Reader
}

// window returns the deterministic ceiling for the given 1-based attempt:
// base doubled once per prior attempt, clamped at max. The doubling loop
// exits as soon as the cap is reached, so an unbounded attempt counter
// costs a handful of iterations and can never overflow into a negative
// duration.
func (b enrollBackoff) window(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	w := b.base
	for i := 1; i < attempt; i++ {
		if w >= b.max {
			return b.max
		}
		w *= 2
	}
	if w > b.max {
		return b.max
	}
	return w
}

// delay returns the actual wait before the given attempt: half the window
// fixed plus up to half jittered. Equal jitter keeps the schedule making
// progress while stopping a whole fleet of agents that lost the same
// control plane from re-converging on one retry instant.
//
// A jitter source that cannot produce a value yields the full window and a
// non-nil error. The caller logs it: the schedule is still capped and still
// makes progress, it has only lost its fleet decorrelation, which is worth
// reporting but not worth failing enrollment over.
func (b enrollBackoff) delay(attempt int) (time.Duration, error) {
	window := b.window(attempt)
	half := window / 2
	if half <= 0 {
		return window, nil
	}
	source := b.entropy
	if source == nil {
		source = cryptorand.Reader
	}
	n, err := cryptorand.Int(source, big.NewInt(int64(half)+1))
	if err != nil {
		return window, &EnrollmentJitterError{Cause: err}
	}
	return half + time.Duration(n.Int64()), nil
}

// EnrollmentJitterError reports that the enrollment backoff could not read
// its jitter source. It is informational: the caller still waits, it just
// waits the full window.
type EnrollmentJitterError struct {
	Cause error
}

// Error implements the error interface.
func (e *EnrollmentJitterError) Error() string {
	return "agent: enrollment backoff jitter unavailable: " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *EnrollmentJitterError) Unwrap() error { return e.Cause }

// EnrollmentTerminalError reports an enrollment failure the daemon refused
// to retry. It names the class and the reason so the journal line explains
// the exit without a source dive, and unwraps to the original cause.
type EnrollmentTerminalError struct {
	// Reason is the operator-facing explanation from classifyEnrollment.
	Reason string
	// Cause is the underlying enrollment failure.
	Cause error
}

// Error implements the error interface.
func (e *EnrollmentTerminalError) Error() string {
	var sb strings.Builder
	sb.WriteString(ErrEnrollmentTerminal.Error())
	sb.WriteString(" (")
	sb.WriteString(e.Reason)
	sb.WriteString("); not retrying")
	if e.Cause != nil {
		sb.WriteString(": ")
		sb.WriteString(e.Cause.Error())
	}
	return sb.String()
}

// Unwrap exposes both the sentinel and the original cause so callers can
// match either.
func (e *EnrollmentTerminalError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrEnrollmentTerminal}
	}
	return []error{ErrEnrollmentTerminal, e.Cause}
}

// EnrollmentAbandonedError reports that the daemon stopped waiting out a
// transient failure because its context ended — a shutdown signal, or a
// deadline the caller imposed on startup.
type EnrollmentAbandonedError struct {
	// Attempts is how many enrollment attempts were made.
	Attempts int
	// Cause is the context error that ended the wait.
	Cause error
	// Last is the final transient failure observed, if any.
	Last error
}

// Error implements the error interface.
func (e *EnrollmentAbandonedError) Error() string {
	var sb strings.Builder
	sb.WriteString(ErrEnrollmentAbandoned.Error())
	sb.WriteString(" after ")
	sb.WriteString(strconv.Itoa(e.Attempts))
	sb.WriteString(" attempt(s)")
	if e.Cause != nil {
		sb.WriteString(": ")
		sb.WriteString(e.Cause.Error())
	}
	if e.Last != nil {
		sb.WriteString("; last failure: ")
		sb.WriteString(e.Last.Error())
	}
	return sb.String()
}

// Unwrap exposes the sentinel, the context cause, and the last observed
// failure so callers can match any of them.
func (e *EnrollmentAbandonedError) Unwrap() []error {
	errs := []error{ErrEnrollmentAbandoned}
	if e.Cause != nil {
		errs = append(errs, e.Cause)
	}
	if e.Last != nil {
		errs = append(errs, e.Last)
	}
	return errs
}

// Enrollment retry sentinels.
var (
	// ErrEnrollmentTerminal marks an enrollment failure the daemon
	// classified as unfixable by retrying.
	ErrEnrollmentTerminal = errors.New("agent: enrollment failed terminally")

	// ErrEnrollmentAbandoned marks an enrollment retry loop that was cut
	// short by context cancellation rather than by a decision about the
	// failure itself.
	ErrEnrollmentAbandoned = errors.New("agent: enrollment abandoned")
)
