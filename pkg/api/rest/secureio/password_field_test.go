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
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// TestPasswordFieldUnmarshalJSON exercises the happy path and the
// documented edge cases. Table-driven so each case is a named subtest
// that can be run in isolation via `go test -run`.
func TestPasswordFieldUnmarshalJSON(t *testing.T) {
	t.Parallel()

	plaintext := []byte("hunter2-supersecret")
	encoded := base64.StdEncoding.EncodeToString(plaintext)

	tests := []struct {
		name       string
		input      string
		wantBytes  []byte
		wantEmpty  bool
		wantErrTyp error
	}{
		{
			name:      "happy_path",
			input:     `"` + encoded + `"`,
			wantBytes: plaintext,
		},
		{
			name:      "explicit_null_is_empty",
			input:     `null`,
			wantEmpty: true,
		},
		{
			name:      "empty_string_is_empty",
			input:     `""`,
			wantEmpty: true,
		},
		{
			name:       "invalid_base64",
			input:      `"!!!not-base64!!!"`,
			wantErrTyp: &MalformedPasswordError{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var f PasswordField
			err := json.Unmarshal([]byte(tc.input), &f)
			defer f.Destroy()
			if tc.wantErrTyp != nil {
				if err == nil {
					t.Fatalf("expected error type %T, got nil", tc.wantErrTyp)
				}
				var got *MalformedPasswordError
				if !errors.As(err, &got) {
					t.Fatalf("error type mismatch: got %T, want %T", err, tc.wantErrTyp)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantEmpty {
				if f.Size() != 0 {
					t.Fatalf("expected empty buffer, got size %d", f.Size())
				}
				return
			}
			if f.Buffer() == nil {
				t.Fatalf("expected populated buffer, got nil")
			}
			if f.Size() != len(tc.wantBytes) {
				t.Fatalf("size mismatch: got %d, want %d", f.Size(), len(tc.wantBytes))
			}
			if string(f.Buffer().Bytes()) != string(tc.wantBytes) {
				t.Fatalf("bytes mismatch: got %q, want %q", f.Buffer().Bytes(), tc.wantBytes)
			}
		})
	}
}

// TestPasswordFieldDestroyIdempotent guards the documented contract
// that Destroy is safe to call repeatedly and on the zero value. This
// matters because handlers `defer f.Destroy()` unconditionally even
// on decode-error paths.
func TestPasswordFieldDestroyIdempotent(t *testing.T) {
	t.Parallel()
	var zero PasswordField
	zero.Destroy()
	zero.Destroy()

	var f PasswordField
	if err := json.Unmarshal([]byte(`"aGVsbG8="`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	f.Destroy()
	f.Destroy()
	if f.Size() != 0 {
		t.Fatalf("expected size 0 after Destroy, got %d", f.Size())
	}
}

// TestPasswordFieldHasNoStringFields guards the core invariant of the
// type: plaintext may never land in a Go string. If a future refactor
// adds a string-typed field the reflection walk below catches it and
// the test fails loudly.
func TestPasswordFieldHasNoStringFields(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(PasswordField{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() == reflect.String {
			t.Fatalf("PasswordField.%s is a string — plaintext must stay in mlocked buffers only", f.Name)
		}
	}
}

// TestMalformedPasswordErrorUnwrap ensures the typed error chains
// through errors.Unwrap so callers can match the underlying base64
// failure via errors.Is / errors.As.
func TestMalformedPasswordErrorUnwrap(t *testing.T) {
	t.Parallel()
	inner := errors.New("boom")
	e := &MalformedPasswordError{Reason: "x", Err: inner}
	if !errors.Is(e, inner) {
		t.Fatalf("expected errors.Is to match the wrapped error")
	}
	if e.Error() == "" {
		t.Fatalf("expected non-empty Error()")
	}
}
