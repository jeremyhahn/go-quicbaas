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

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// PasswordField is a JSON field type whose UnmarshalJSON decodes a
// base64-encoded JSON string directly into an mlocked *secure.Buffer.
// The plaintext bytes NEVER become a Go string at any point on the
// unmarshal path:
//
//  1. json.Unmarshal invokes UnmarshalJSON with the raw token
//     (including the surrounding quotes).
//  2. The method strips the quotes from the []byte — no string
//     conversion occurs.
//  3. base64.StdEncoding.Decode writes directly into the mlocked
//     Buffer's Bytes() slice.
//  4. The Buffer is right-sized: on a short decode the method
//     allocates a fresh Buffer of exactly n bytes and destroys the
//     original.
//
// Callers MUST invoke Destroy when finished with the field (typically
// via a handler-level defer). Destroy is idempotent and safe to call
// on a zero value, so the defer runs on every code path including
// decode errors.
type PasswordField struct {
	buf *secure.Buffer
}

// MalformedPasswordError is returned when UnmarshalJSON receives input
// that cannot be interpreted as a base64-encoded JSON string.
type MalformedPasswordError struct {
	// Reason is a short, operator-visible description of the failure.
	Reason string

	// Err is the underlying decoder failure when one is available.
	Err error
}

// Error implements the error interface.
func (e *MalformedPasswordError) Error() string {
	if e.Err != nil {
		return "secureio: malformed password: " + e.Reason + ": " + e.Err.Error()
	}
	return "secureio: malformed password: " + e.Reason
}

// Unwrap exposes the underlying decoder failure to errors.Is/errors.As.
func (e *MalformedPasswordError) Unwrap() error { return e.Err }

// UnmarshalJSON decodes a JSON-quoted base64 string directly into an
// mlocked Buffer. Empty input (zero-length data or JSON null) is a
// no-op and leaves the field's buffer nil.
func (f *PasswordField) UnmarshalJSON(data []byte) error {
	if f == nil {
		return &MalformedPasswordError{Reason: "nil receiver"}
	}
	if len(data) == 0 {
		return nil
	}
	if len(data) == 4 && data[0] == 'n' && data[1] == 'u' && data[2] == 'l' && data[3] == 'l' {
		return nil
	}
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return &MalformedPasswordError{Reason: "password must be a JSON string"}
	}
	encoded := data[1 : len(data)-1]
	if len(encoded) == 0 {
		return nil
	}
	maxLen := base64.StdEncoding.DecodedLen(len(encoded))
	if maxLen <= 0 {
		return &MalformedPasswordError{Reason: "password empty after decode"}
	}
	raw, err := secure.NewBuffer(maxLen)
	if err != nil {
		return err
	}
	//secure:unborrowed allocates-protected base64.Decode writes directly into the newly allocated protected buffer.
	n, err := base64.StdEncoding.Decode(raw.Bytes(), encoded)
	if err != nil {
		raw.Destroy()
		return &MalformedPasswordError{Reason: "base64 decode failed", Err: err}
	}
	if n == maxLen {
		f.buf = raw
		return nil
	}
	trimmed, err := secure.NewBuffer(n)
	if err != nil {
		raw.Destroy()
		return err
	}
	//secure:unborrowed allocates-protected copy moves bytes between protected buffers before the source is destroyed.
	copy(trimmed.Bytes(), raw.Bytes()[:n])
	raw.Destroy()
	f.buf = trimmed
	return nil
}

// Buffer exposes the underlying mlocked *secure.Buffer for handlers
// that forward it to the authentication pipeline. Returns nil when
// the field was not populated.
func (f *PasswordField) Buffer() *secure.Buffer {
	if f == nil {
		return nil
	}
	return f.buf
}

// Size reports the number of plaintext bytes currently held. Returns
// 0 for a zero-value field.
func (f *PasswordField) Size() int {
	if f == nil || f.buf == nil {
		return 0
	}
	return f.buf.Size()
}

// Destroy wipes the underlying Buffer. Safe to call on the zero value
// and idempotent — the SDK Buffer.Destroy is itself idempotent.
func (f *PasswordField) Destroy() {
	if f == nil {
		return
	}
	if f.buf != nil {
		f.buf.Destroy()
		f.buf = nil
	}
}
