// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"errors"
	"strings"
	"testing"
)

// TestError_Error_AllShapes verifies the four rendering branches of
// Error.Error(): with/without Msg crossed with with/without Cause.
func TestError_Error_AllShapes(t *testing.T) {
	cause := errors.New("boom")
	cases := []struct {
		name      string
		err       *Error
		wantParts []string
	}{
		{
			name:      "msg and cause",
			err:       &Error{Op: "pkg.Op", Kind: ErrKindTransport, Msg: "send failed", Cause: cause},
			wantParts: []string{"pkg.Op", "send failed", "transport", "boom"},
		},
		{
			name:      "cause only",
			err:       &Error{Op: "pkg.Op", Kind: ErrKindTransport, Cause: cause},
			wantParts: []string{"pkg.Op", "transport", "boom"},
		},
		{
			name:      "msg only",
			err:       &Error{Op: "pkg.Op", Kind: ErrKindInvalidInput, Msg: "bad arg"},
			wantParts: []string{"pkg.Op", "bad arg", "invalid_input"},
		},
		{
			name:      "kind only",
			err:       &Error{Op: "pkg.Op", Kind: ErrKindNotFound},
			wantParts: []string{"pkg.Op", "not_found"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			for _, part := range tc.wantParts {
				if !strings.Contains(got, part) {
					t.Fatalf("Error() = %q, missing %q", got, part)
				}
			}
		})
	}
}

// TestError_Error_Nil renders an empty string for a nil receiver so a
// nil *Error coerced to error never panics on Error().
func TestError_Error_Nil(t *testing.T) {
	var e *Error
	if got := e.Error(); got != "" {
		t.Fatalf("nil Error() = %q, want empty", got)
	}
}

// TestError_Unwrap returns the wrapped cause and nil for a nil receiver.
func TestError_Unwrap(t *testing.T) {
	cause := errors.New("inner")
	e := &Error{Op: "x", Kind: ErrKindStorage, Cause: cause}
	if got := e.Unwrap(); !errors.Is(got, cause) {
		t.Fatalf("Unwrap() = %v, want %v", got, cause)
	}
	if !errors.Is(e, cause) {
		t.Fatalf("errors.Is should match wrapped cause")
	}
	var nilErr *Error
	if nilErr.Unwrap() != nil {
		t.Fatalf("nil Unwrap should be nil")
	}
}

// TestIsKind_Match matches a kind at the top of the chain.
func TestIsKind_Match(t *testing.T) {
	if !IsKind(&Error{Kind: ErrKindRateLimited}, ErrKindRateLimited) {
		t.Fatalf("direct match failed")
	}
}

// TestIsKind_Nested matches a kind buried under wrapping.
func TestIsKind_Nested(t *testing.T) {
	inner := &Error{Kind: ErrKindProtocol, Msg: "inner"}
	outer := wrapError("op", ErrKindTransport, inner, "outer")
	if !IsKind(outer, ErrKindProtocol) {
		t.Fatalf("nested match for protocol failed")
	}
	if !IsKind(outer, ErrKindTransport) {
		t.Fatalf("nested match for transport failed")
	}
}

// TestIsKind_NoMatch returns false for nil, unrelated kinds, and plain
// errors that terminate the chain without an *Error.
func TestIsKind_NoMatch(t *testing.T) {
	if IsKind(nil, ErrKindInternal) {
		t.Fatalf("nil should not match")
	}
	if IsKind(&Error{Kind: ErrKindStorage}, ErrKindConflict) {
		t.Fatalf("wrong kind should not match")
	}
	// A plain error wrapped under an *Error: the loop walks to the plain
	// error, which is not an *Error and does not Unwrap, so the search
	// terminates with false.
	plain := errors.New("not typed")
	wrapped := wrapError("op", ErrKindUnauthorized, plain, "")
	if IsKind(wrapped, ErrKindNotFound) {
		t.Fatalf("should not match kind not present in chain")
	}
	// And a bare plain error never matches.
	if IsKind(plain, ErrKindUnauthorized) {
		t.Fatalf("plain error should not match any kind")
	}
}

// TestNewError_Fields populates the struct without a cause.
func TestNewError_Fields(t *testing.T) {
	e := newError("op", ErrKindParse, "msg")
	if e.Op != "op" || e.Kind != ErrKindParse || e.Msg != "msg" || e.Cause != nil {
		t.Fatalf("unexpected newError: %+v", e)
	}
}
