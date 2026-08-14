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

package secureio

import (
	"encoding/base64"
	"errors"
	"testing"
)

// TestPasswordFieldNilReceiver exercises every method's nil-receiver
// guard. Handlers may hold a (*PasswordField)(nil) before a request is
// decoded, and the documented contract is that all accessors are safe
// on that value rather than panicking.
func TestPasswordFieldNilReceiver(t *testing.T) {
	t.Parallel()

	var f *PasswordField

	if got := f.Buffer(); got != nil {
		t.Fatalf("Buffer() on nil receiver = %v, want nil", got)
	}
	if got := f.Size(); got != 0 {
		t.Fatalf("Size() on nil receiver = %d, want 0", got)
	}
	// Destroy must not panic on a nil receiver.
	f.Destroy()

	err := f.UnmarshalJSON([]byte(`"aGVsbG8="`))
	if err == nil {
		t.Fatalf("UnmarshalJSON on nil receiver = nil, want MalformedPasswordError")
	}
	var malformed *MalformedPasswordError
	if !errors.As(err, &malformed) {
		t.Fatalf("UnmarshalJSON on nil receiver returned %T, want *MalformedPasswordError", err)
	}
	if malformed.Reason != "nil receiver" {
		t.Fatalf("Reason = %q, want %q", malformed.Reason, "nil receiver")
	}
}

// TestPasswordFieldUnmarshalEmptyData covers the zero-length input
// guard, which json.Unmarshal never produces but a direct caller can.
// The contract is a no-op leaving the buffer nil.
func TestPasswordFieldUnmarshalEmptyData(t *testing.T) {
	t.Parallel()

	var f PasswordField
	if err := f.UnmarshalJSON(nil); err != nil {
		t.Fatalf("UnmarshalJSON(nil) = %v, want nil", err)
	}
	if f.Buffer() != nil {
		t.Fatalf("Buffer() = %v after empty input, want nil", f.Buffer())
	}
	if err := f.UnmarshalJSON([]byte{}); err != nil {
		t.Fatalf("UnmarshalJSON([]byte{}) = %v, want nil", err)
	}
}

// TestPasswordFieldUnmarshalNotAString covers inputs that are valid
// JSON tokens but not JSON strings, plus a single unquoted byte that
// fails the minimum-length guard. Each must yield a typed error.
func TestPasswordFieldUnmarshalNotAString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "json_number", input: `12345`},
		{name: "json_object", input: `{"x":1}`},
		{name: "single_byte", input: `x`},
		{name: "leading_quote_only", input: `"abc`},
		{name: "trailing_quote_only", input: `abc"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var f PasswordField
			defer f.Destroy()
			err := f.UnmarshalJSON([]byte(tc.input))
			if err == nil {
				t.Fatalf("UnmarshalJSON(%q) = nil, want error", tc.input)
			}
			var malformed *MalformedPasswordError
			if !errors.As(err, &malformed) {
				t.Fatalf("UnmarshalJSON(%q) returned %T, want *MalformedPasswordError", tc.input, err)
			}
			if malformed.Reason != "password must be a JSON string" {
				t.Fatalf("Reason = %q, want %q", malformed.Reason, "password must be a JSON string")
			}
		})
	}
}

// TestPasswordFieldUnmarshalEmptyAfterDecode covers the branch where a
// quoted token holds 1..3 base64 characters, for which the decoded
// length rounds down to zero bytes. That is treated as a malformed
// password rather than a silent empty value.
func TestPasswordFieldUnmarshalEmptyAfterDecode(t *testing.T) {
	t.Parallel()

	var f PasswordField
	defer f.Destroy()
	err := f.UnmarshalJSON([]byte(`"A"`))
	if err == nil {
		t.Fatalf("UnmarshalJSON(\"A\") = nil, want error")
	}
	var malformed *MalformedPasswordError
	if !errors.As(err, &malformed) {
		t.Fatalf("returned %T, want *MalformedPasswordError", err)
	}
	if malformed.Reason != "password empty after decode" {
		t.Fatalf("Reason = %q, want %q", malformed.Reason, "password empty after decode")
	}
}

// TestMalformedPasswordErrorNoWrappedErr covers the Error() branch that
// formats the message when no underlying decoder failure is attached.
func TestMalformedPasswordErrorNoWrappedErr(t *testing.T) {
	t.Parallel()

	e := &MalformedPasswordError{Reason: "standalone reason"}
	want := "secureio: malformed password: standalone reason"
	if got := e.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if e.Unwrap() != nil {
		t.Fatalf("Unwrap() = %v, want nil", e.Unwrap())
	}
}

// TestPasswordFieldUnmarshalExactFit covers the n == maxLen branch where
// the base64 token decodes to exactly the predicted length so no
// right-sizing copy is needed. A plaintext whose length is a multiple
// of 3 encodes without padding and triggers this path.
func TestPasswordFieldUnmarshalExactFit(t *testing.T) {
	t.Parallel()

	// 3 bytes -> 4 base64 chars, no padding, decoded len == predicted.
	plaintext := []byte("abc")
	encoded := base64.StdEncoding.EncodeToString(plaintext)
	if len(encoded) != 4 || encoded[len(encoded)-1] == '=' {
		t.Fatalf("test precondition: expected unpadded 4-char encoding, got %q", encoded)
	}

	var f PasswordField
	defer f.Destroy()
	if err := f.UnmarshalJSON([]byte(`"` + encoded + `"`)); err != nil {
		t.Fatalf("UnmarshalJSON exact-fit = %v, want nil", err)
	}
	if f.Buffer() == nil {
		t.Fatalf("Buffer() = nil, want populated")
	}
	if f.Size() != len(plaintext) {
		t.Fatalf("Size() = %d, want %d", f.Size(), len(plaintext))
	}
	if string(f.Buffer().Bytes()) != string(plaintext) {
		t.Fatalf("Bytes() = %q, want %q", f.Buffer().Bytes(), plaintext)
	}
}
