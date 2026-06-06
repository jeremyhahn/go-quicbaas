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

package errors

import (
	"errors"
	"fmt"
	"testing"
)

// TestNewPasswordLoginUnsupported verifies the constructor produces an error
// that is recognised by both IsPasswordLoginUnsupported and the inherited
// IsUnimplemented classifier, carries the originating op, and surfaces the
// migration guidance.
func TestNewPasswordLoginUnsupported(t *testing.T) {
	err := NewPasswordLoginUnsupported("auth.Login")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !IsPasswordLoginUnsupported(err) {
		t.Error("IsPasswordLoginUnsupported = false, want true")
	}
	if !IsUnimplemented(err) {
		t.Error("IsUnimplemented = false, want true (embedded QBError code)")
	}
	if err.Op != "auth.Login" {
		t.Errorf("Op = %q, want %q", err.Op, "auth.Login")
	}
	msg := err.Error()
	for _, want := range []string{"auth.Login", "BeginLogin/FinishLogin", "mtls", "fido2", "piv"} {
		if !contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
}

// TestIsPasswordLoginUnsupported_Negatives verifies the predicate rejects
// unrelated errors and nil, so it cannot be used to misclassify a different
// failure as the retired password path.
func TestIsPasswordLoginUnsupported_Negatives(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"invalid input", NewInvalidInput("op", "bad")},
		{"plain unimplemented", NewUnimplemented("op", "nope")},
		{"foreign error", errors.New("boom")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsPasswordLoginUnsupported(tc.err) {
				t.Errorf("IsPasswordLoginUnsupported(%v) = true, want false", tc.err)
			}
		})
	}
}

// TestPasswordLoginUnsupported_Wrapped verifies the predicate sees through
// error wrapping, so callers that decorate the error still classify it.
func TestPasswordLoginUnsupported_Wrapped(t *testing.T) {
	base := NewPasswordLoginUnsupported("auth.Login")
	wrapped := fmt.Errorf("login failed: %w", base)
	if !IsPasswordLoginUnsupported(wrapped) {
		t.Error("IsPasswordLoginUnsupported did not see through wrapping")
	}
	// errors.Is must also match the typed sentinel through the wrap.
	if !errors.Is(wrapped, base) {
		t.Error("errors.Is did not match the wrapped PasswordLoginUnsupportedError")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
