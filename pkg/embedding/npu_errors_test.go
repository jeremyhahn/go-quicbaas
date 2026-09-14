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

// No build tag: npu_errors.go is untagged so its contract holds in both vector
// and non-vector builds, and so must its tests.
package embedding

import (
	"errors"
	"strings"
	"testing"
)

// An error message is what an operator reads at 2am, so the text is part of the
// contract and is asserted rather than assumed.

func TestNPUUnavailableErrorMessage(t *testing.T) {
	cause := errors.New("connection refused")
	err := &NPUUnavailableError{Endpoint: "http://npu:8077", Err: cause}

	msg := err.Error()
	for _, want := range []string{"sidecar unavailable", "http://npu:8077", "connection refused"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}

	if !errors.Is(err, ErrNPUUnavailable) {
		t.Error("must match ErrNPUUnavailable")
	}
	if !errors.Is(err, cause) {
		t.Error("must unwrap to the transport cause")
	}
}

func TestNPUUnavailableErrorWithoutCause(t *testing.T) {
	err := &NPUUnavailableError{Endpoint: "http://npu:8077"}

	if strings.HasSuffix(err.Error(), ": ") {
		t.Errorf("message %q has a dangling separator when there is no cause", err.Error())
	}
	if !errors.Is(err, ErrNPUUnavailable) {
		t.Error("must still match the sentinel without a cause")
	}
	// The NPU sentinel plus the shared provider-unavailable one; no cause.
	if got := err.Unwrap(); len(got) != 2 {
		t.Errorf("Unwrap() returned %d errors, want 2 when there is no cause", len(got))
	}
}

// TestNPUUnavailableMatchesTheSharedSentinel is the property that keeps a down
// sidecar out of the "internal error" bucket. The service layer maps
// ErrProviderUnavailable to ErrUnavailable; without this, an unreachable sidecar
// would surface to API callers as a 500 and mislead monitoring.
func TestNPUUnavailableMatchesTheSharedSentinel(t *testing.T) {
	err := &NPUUnavailableError{
		Endpoint: "http://npu:8077",
		Err:      errors.New("connection refused"),
	}

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Error("an unreachable sidecar must match ErrProviderUnavailable, not fall through to internal")
	}
	if !errors.Is(err, ErrNPUUnavailable) {
		t.Error("the NPU-specific sentinel must still match")
	}
}

func TestNPUSidecarErrorStatusMapping(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		unavailable bool
		rateLimited bool
	}{
		{"internal", 500, true, false},
		{"bad gateway", 502, true, false},
		{"unavailable", 503, true, false},
		{"too many requests", 429, false, true},
		{"bad request", 400, false, false},
		{"unauthorized", 401, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &NPUSidecarError{Endpoint: "e", StatusCode: tc.status}

			if !errors.Is(err, ErrNPUSidecar) {
				t.Error("must always match ErrNPUSidecar")
			}
			if got := errors.Is(err, ErrProviderUnavailable); got != tc.unavailable {
				t.Errorf("errors.Is(ErrProviderUnavailable) = %v, want %v for status %d",
					got, tc.unavailable, tc.status)
			}
			if got := errors.Is(err, ErrRateLimitExceeded); got != tc.rateLimited {
				t.Errorf("errors.Is(ErrRateLimitExceeded) = %v, want %v for status %d",
					got, tc.rateLimited, tc.status)
			}
		})
	}
}

func TestNPUModelMismatchErrorMessage(t *testing.T) {
	err := &NPUModelMismatchError{
		Endpoint:  "http://npu:8077",
		Requested: "all-MiniLM-L6-v2",
		Served:    "some-other-model",
	}

	msg := err.Error()
	for _, want := range []string{"requested=all-MiniLM-L6-v2", "served=some-other-model"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	if !errors.Is(err, ErrNPUModelMismatch) {
		t.Error("must match ErrNPUModelMismatch")
	}
}

func TestNPUSidecarErrorMessage(t *testing.T) {
	err := &NPUSidecarError{
		Endpoint:   "http://npu:8077",
		StatusCode: 503,
		Code:       "compile_failed",
		Message:    "NPU refused shape [8,512]",
	}

	msg := err.Error()
	for _, want := range []string{"status=503", "code=compile_failed", "refused shape"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	if !errors.Is(err, ErrNPUSidecar) {
		t.Error("must match ErrNPUSidecar")
	}
}

func TestNPUSidecarErrorStatusOnly(t *testing.T) {
	// A bare status is a complete error; the message must not invent detail.
	err := &NPUSidecarError{Endpoint: "http://npu:8077", StatusCode: 500}

	msg := err.Error()
	if !strings.Contains(msg, "status=500") {
		t.Errorf("message %q must carry the status", msg)
	}
	if strings.Contains(msg, "code=") {
		t.Errorf("message %q must not report an empty code", msg)
	}
}

func TestNPUMalformedResponseErrorMessage(t *testing.T) {
	cause := errors.New("unexpected EOF")
	err := &NPUMalformedResponseError{
		Endpoint: "http://npu:8077",
		Reason:   "could not decode response body",
		Err:      cause,
	}

	msg := err.Error()
	for _, want := range []string{"malformed sidecar response", "could not decode", "unexpected EOF"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	if !errors.Is(err, ErrNPUMalformedResponse) || !errors.Is(err, cause) {
		t.Error("must match both the sentinel and the cause")
	}
}

func TestNPUMalformedResponseErrorWithoutCause(t *testing.T) {
	err := &NPUMalformedResponseError{Endpoint: "e", Reason: "bad arity"}
	if got := err.Unwrap(); len(got) != 1 {
		t.Errorf("Unwrap() returned %d errors, want 1", len(got))
	}
	if !errors.Is(err, ErrNPUMalformedResponse) {
		t.Error("must match the sentinel")
	}
}

func TestNPUDimensionsMismatchErrorMessage(t *testing.T) {
	err := &NPUDimensionsMismatchError{
		Endpoint: "http://npu:8077",
		Expected: 384,
		Actual:   768,
		Index:    3,
	}

	msg := err.Error()
	for _, want := range []string{"expected=384", "actual=768", "index=3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	// It wraps the shared sentinel so callers written against the general
	// embedding contract keep working.
	if !errors.Is(err, ErrDimensionsMismatch) {
		t.Error("must match the shared ErrDimensionsMismatch")
	}
}

func TestNPUResultCountErrorMessage(t *testing.T) {
	err := &NPUResultCountError{Endpoint: "http://npu:8077", Expected: 8, Actual: 7}

	msg := err.Error()
	if !strings.Contains(msg, "expected 8 embeddings, got 7") {
		t.Errorf("message %q must report both counts", msg)
	}
	if !errors.Is(err, ErrNPUMalformedResponse) {
		t.Error("must match ErrNPUMalformedResponse")
	}
}

func TestNPUInvalidInputErrorMessage(t *testing.T) {
	batch := &NPUInvalidInputError{Index: 4, Reason: "text must not be empty"}
	if !strings.Contains(batch.Error(), "index 4") {
		t.Errorf("batch message %q must name the offending index", batch.Error())
	}

	// -1 means a single-text call, where an index would be meaningless.
	single := &NPUInvalidInputError{Index: -1, Reason: "text must not be empty"}
	if strings.Contains(single.Error(), "index") {
		t.Errorf("single-text message %q must not mention an index", single.Error())
	}

	if !errors.Is(batch, ErrInvalidInput) {
		t.Error("must match the shared ErrInvalidInput")
	}
}

func TestNPUConfigErrorMessage(t *testing.T) {
	err := &NPUConfigError{
		Field:    "secrets",
		Reason:   "APIKeyRef is set but no resolver was supplied",
		sentinel: ErrNPUSecretResolverRequired,
	}

	msg := err.Error()
	for _, want := range []string{"field=secrets", "no resolver"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	if !errors.Is(err, ErrNPUSecretResolverRequired) {
		t.Error("must match the configured sentinel")
	}
}

func TestNPUConfigErrorDefaultsToEndpointSentinel(t *testing.T) {
	// A zero sentinel must still produce a usable error rather than panicking.
	err := &NPUConfigError{Field: "base_url", Reason: "must not be empty"}

	if !errors.Is(err, ErrNPUEndpointRequired) {
		t.Error("must default to ErrNPUEndpointRequired")
	}
	if !strings.Contains(err.Error(), "endpoint is required") {
		t.Errorf("message %q must reflect the default sentinel", err.Error())
	}
}

func TestNPUConfigErrorWithoutFieldOrReason(t *testing.T) {
	err := &NPUConfigError{sentinel: ErrNPUEndpointRequired}
	if strings.Contains(err.Error(), "field=") {
		t.Errorf("message %q must not report an empty field", err.Error())
	}
}

func TestNPUSecretResolutionErrorMessage(t *testing.T) {
	cause := errors.New("permission denied")
	err := &NPUSecretResolutionError{
		Engine: "secrets",
		Path:   "tenants/acme/npu",
		Err:    cause,
	}

	msg := err.Error()
	for _, want := range []string{"engine=secrets", "path=tenants/acme/npu", "permission denied"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	if !errors.Is(err, ErrNPUSecretResolution) || !errors.Is(err, cause) {
		t.Error("must match both the sentinel and the cause")
	}
}

func TestNPUSecretResolutionErrorWithoutCause(t *testing.T) {
	err := &NPUSecretResolutionError{Engine: "secrets", Path: "p"}
	if got := err.Unwrap(); len(got) != 1 {
		t.Errorf("Unwrap() returned %d errors, want 1", len(got))
	}
}

// TestNPUSentinelsAreDistinct guards against a copy-paste that would make two
// failure modes indistinguishable to errors.Is.
func TestNPUSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrNPUEndpointRequired,
		ErrNPUUnavailable,
		ErrNPUSidecar,
		ErrNPUMalformedResponse,
		ErrNPUSecretResolution,
		ErrNPUSecretResolverRequired,
		ErrNPUModelMismatch,
	}

	for i, a := range sentinels {
		if a == nil {
			t.Fatalf("sentinel %d is nil", i)
		}
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinels %d and %d are not distinct: %v / %v", i, j, a, b)
			}
		}
	}
}
