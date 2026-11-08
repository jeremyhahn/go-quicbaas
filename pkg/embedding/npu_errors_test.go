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
func TestNPUErrorMessages(t *testing.T) {
	transportCause := errors.New("connection refused")
	decodeCause := errors.New("unexpected EOF")
	secretCause := errors.New("permission denied")

	cases := []struct {
		name    string
		err     error
		want    []string // substrings the message must contain
		notWant []string // substrings the message must not contain
		is      []error  // errors the value must match with errors.Is
		unwraps int      // len(Unwrap()) when non-zero
	}{
		{
			name: "unavailable with cause",
			err:  &NPUUnavailableError{Endpoint: "http://npu:8077", Err: transportCause},
			want: []string{"sidecar unavailable", "http://npu:8077", "connection refused"},
			is:   []error{transportCause},
		},
		{
			// The NPU sentinel plus the shared provider-unavailable one; no cause.
			name:    "unavailable without cause",
			err:     &NPUUnavailableError{Endpoint: "http://npu:8077"},
			is:      []error{ErrNPUUnavailable},
			unwraps: 2,
		},
		{
			name: "model mismatch",
			err: &NPUModelMismatchError{
				Endpoint:  "http://npu:8077",
				Requested: "all-MiniLM-L6-v2",
				Served:    "some-other-model",
			},
			want: []string{"requested=all-MiniLM-L6-v2", "served=some-other-model"},
			is:   []error{ErrNPUModelMismatch},
		},
		{
			name: "sidecar with code and message",
			err: &NPUSidecarError{
				Endpoint:   "http://npu:8077",
				StatusCode: 503,
				Code:       "compile_failed",
				Message:    "NPU refused shape [8,512]",
			},
			want: []string{"status=503", "code=compile_failed", "refused shape"},
			is:   []error{ErrNPUSidecar},
		},
		{
			// A bare status is a complete error; the message must not invent detail.
			name:    "sidecar status only",
			err:     &NPUSidecarError{Endpoint: "http://npu:8077", StatusCode: 500},
			want:    []string{"status=500"},
			notWant: []string{"code="},
		},
		{
			name: "malformed response with cause",
			err: &NPUMalformedResponseError{
				Endpoint: "http://npu:8077",
				Reason:   "could not decode response body",
				Err:      decodeCause,
			},
			want: []string{"malformed sidecar response", "could not decode", "unexpected EOF"},
			is:   []error{ErrNPUMalformedResponse, decodeCause},
		},
		{
			name:    "malformed response without cause",
			err:     &NPUMalformedResponseError{Endpoint: "e", Reason: "bad arity"},
			is:      []error{ErrNPUMalformedResponse},
			unwraps: 1,
		},
		{
			// It wraps the shared sentinel so callers written against the general
			// embedding contract keep working.
			name: "dimensions mismatch",
			err: &NPUDimensionsMismatchError{
				Endpoint: "http://npu:8077",
				Expected: 384,
				Actual:   768,
				Index:    3,
			},
			want: []string{"expected=384", "actual=768", "index=3"},
			is:   []error{ErrDimensionsMismatch},
		},
		{
			name: "result count",
			err:  &NPUResultCountError{Endpoint: "http://npu:8077", Expected: 8, Actual: 7},
			want: []string{"expected 8 embeddings, got 7"},
			is:   []error{ErrNPUMalformedResponse},
		},
		{
			name: "invalid input in a batch",
			err:  &NPUInvalidInputError{Index: 4, Reason: "text must not be empty"},
			want: []string{"index 4"},
			is:   []error{ErrInvalidInput},
		},
		{
			// -1 means a single-text call, where an index would be meaningless.
			name:    "invalid input in a single-text call",
			err:     &NPUInvalidInputError{Index: -1, Reason: "text must not be empty"},
			notWant: []string{"index"},
		},
		{
			name: "config with field and reason",
			err: &NPUConfigError{
				Field:    "secrets",
				Reason:   "APIKeyRef is set but no resolver was supplied",
				sentinel: ErrNPUSecretResolverRequired,
			},
			want: []string{"field=secrets", "no resolver"},
			is:   []error{ErrNPUSecretResolverRequired},
		},
		{
			name:    "config without field or reason",
			err:     &NPUConfigError{sentinel: ErrNPUEndpointRequired},
			notWant: []string{"field="},
		},
		{
			name: "secret resolution with cause",
			err: &NPUSecretResolutionError{
				Engine: "secrets",
				Path:   "tenants/acme/npu",
				Err:    secretCause,
			},
			want: []string{"engine=secrets", "path=tenants/acme/npu", "permission denied"},
			is:   []error{ErrNPUSecretResolution, secretCause},
		},
		{
			name:    "secret resolution without cause",
			err:     &NPUSecretResolutionError{Engine: "secrets", Path: "p"},
			unwraps: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if strings.HasSuffix(msg, ": ") {
				t.Errorf("message %q has a dangling separator", msg)
			}
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not contain %q", msg, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(msg, notWant) {
					t.Errorf("message %q must not contain %q", msg, notWant)
				}
			}
			for _, target := range tc.is {
				if !errors.Is(tc.err, target) {
					t.Errorf("errors.Is(%v) = false, want true", target)
				}
			}
			if tc.unwraps == 0 {
				return
			}
			multi, ok := tc.err.(interface{ Unwrap() []error })
			if !ok {
				t.Fatalf("%T does not unwrap to multiple errors", tc.err)
			}
			if got := len(multi.Unwrap()); got != tc.unwraps {
				t.Errorf("Unwrap() returned %d errors, want %d", got, tc.unwraps)
			}
		})
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
