// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package secrets

// borrow.go is the protected-memory read path for secret material
// (go-quicbaas#954, security-review.md §6). Get returns a secret's fields as
// ordinary Go strings inside a map, so the plaintext is unprotected and
// unwipeable before any caller sees it. GetWithBytes never builds that map:
//
//  1. The transport answers the internal GetData verb with the stored payload
//     as a []byte (*service.GetSecretResponse) — never decoded into strings.
//  2. The requested member is located in that payload by a scanner that works
//     on bytes, and its JSON string escapes are undone into one []byte.
//  3. That value is sealed into protected storage — the qrdb SecretValue the
//     SDK re-exports from sdk/go/secret, the owned surface at this layer —
//     and every plain buffer that held it is wiped at once.
//  4. The caller borrows the plaintext for the duration of fn only; the
//     protected storage is destroyed when GetWithBytes returns.

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/secret"
)

// getDataMethod is the internal wire verb behind GetWithBytes. It carries the
// same getSecretRequest as Get, but every transport answers it with the
// stored payload as bytes (*service.GetSecretResponse) instead of decoding it
// into a map of strings.
const getDataMethod = "GetData"

// getWithBytesOp is the operation name GetWithBytes errors carry.
const getWithBytesOp = "secrets.GetWithBytes"

// GetWithBytes lends one field of the secret at path to fn without the
// plaintext ever becoming an ordinary Go string. See Client.GetWithBytes.
func (c *client) GetWithBytes(ctx context.Context, path, field string, fn func([]byte) error, opts ...GetOption) error {
	if path == "" {
		return errors.NewInvalidInput(getWithBytesOp, "path is required")
	}
	if fn == nil {
		return errors.NewInvalidInput(getWithBytesOp, "borrow function is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput(getWithBytesOp, "context is required")
	}

	options := &getOptions{}
	for _, opt := range opts {
		opt(options)
	}

	resp, err := c.transport.Request(ctx, serviceName, getDataMethod, &getSecretRequest{
		Path:    path,
		Version: options.version,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.NewNotFound(getWithBytesOp, "secret not found")
	}
	svcResp, ok := resp.(*service.GetSecretResponse)
	if !ok {
		// Any other shape may already hold the plaintext somewhere this
		// method cannot wipe; refuse it rather than lend from it.
		return errors.NewInternal(getWithBytesOp, "invalid response type")
	}

	// Take sole ownership of the payload and wipe it on every path out.
	payload := svcResp.Data
	svcResp.Data = nil
	defer wipe(payload)

	if len(payload) == 0 {
		return errors.NewNotFound(getWithBytesOp, "secret has no data")
	}

	value, owned, err := selectField(payload, field)
	if err != nil {
		return err
	}
	if owned {
		defer wipe(value)
	}

	protected, err := secret.NewSecretValue(path, value, "")
	if err != nil {
		// A value that cannot be sealed is never lent out in the clear.
		return errors.Wrap(err, errors.CodeInternal, getWithBytesOp, "seal the secret value")
	}
	defer protected.Clear()
	size := len(value)

	// The plain copies are no longer needed once the value is sealed; wipe
	// them before the caller's code runs rather than after.
	if owned {
		wipe(value)
	}
	wipe(payload)

	return protected.WithBytes(func(b []byte) error {
		// NewSecretValue reports a refused seal only by holding nothing,
		// which WithBytes would lend as an empty slice. Never hand the
		// caller that in place of the secret.
		if len(b) != size {
			return errors.NewInternal(getWithBytesOp, "protected storage refused the secret")
		}
		return fn(b)
	})
}

// selectField returns the bytes GetWithBytes lends for field. An empty field
// selects the whole stored payload verbatim (owned is false: the result
// aliases payload). A named field must be a top-level member of the payload's
// JSON object whose value is a JSON string; its unescaped bytes are returned
// in a new buffer the caller must wipe (owned is true).
//
// No error returned here quotes the payload: a JSON syntax error message
// would echo the offending character of the secret.
func selectField(payload []byte, field string) (value []byte, owned bool, err error) {
	if field == "" {
		return payload, false, nil
	}
	if !json.Valid(payload) {
		return nil, false, errors.NewPreconditionFailed(getWithBytesOp,
			"stored secret is not JSON; pass an empty field to borrow the whole payload")
	}
	start, end, found, err := findMember(payload, field)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, errors.NewNotFound(getWithBytesOp, "secret has no field "+field)
	}
	raw := payload[start:end]
	if raw[0] != '"' {
		return nil, false, errors.NewInvalidInput(getWithBytesOp, "secret field "+field+" is not a JSON string")
	}
	return unescapeJSONString(raw), true, nil
}

// findMember locates the value of the top-level member named field in the
// JSON object payload and returns its byte span. payload must already be
// valid JSON (json.Valid), which lets the scanner stay a simple skipper. When
// a name repeats, the last occurrence wins — the same member encoding/json,
// and therefore Get, would report.
func findMember(payload []byte, field string) (start, end int, found bool, err error) {
	i := skipSpace(payload, 0)
	if payload[i] != '{' {
		return 0, 0, false, errors.NewPreconditionFailed(getWithBytesOp,
			"stored secret is not a JSON object; pass an empty field to borrow the whole payload")
	}
	i = skipSpace(payload, i+1)
	if payload[i] == '}' {
		return 0, 0, false, nil
	}
	want := []byte(field)
	for {
		keyEnd := skipString(payload, i)
		name := unescapeJSONString(payload[i:keyEnd])
		match := bytes.Equal(name, want)
		i = skipSpace(payload, keyEnd)
		i = skipSpace(payload, i+1) // past ':'
		valueEnd := skipValue(payload, i)
		if match {
			start, end, found = i, valueEnd, true
		}
		i = skipSpace(payload, valueEnd)
		if payload[i] == '}' {
			return start, end, found, nil
		}
		i = skipSpace(payload, i+1) // past ','
	}
}

// skipSpace returns the index of the first non-whitespace byte at or after i.
func skipSpace(b []byte, i int) int {
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// skipString returns the index just past the JSON string starting at b[i].
func skipString(b []byte, i int) int {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return i
}

// skipValue returns the index just past the JSON value starting at b[i].
func skipValue(b []byte, i int) int {
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				i = skipString(b, i)
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return i
	default:
		// Number, true, false or null: runs to the next delimiter.
		for i < len(b) {
			switch b[i] {
			case ',', '}', ']', ' ', '\t', '\n', '\r':
				return i
			}
			i++
		}
		return i
	}
}

// unescapeJSONString decodes the quoted JSON string raw (quotes included,
// already validated) into a new buffer. The buffer is sized once from raw —
// an unescaped string is never longer than its quoted form — so decoding
// never reallocates and leaves no unwiped partial copy behind. Invalid UTF-16
// surrogates decode to U+FFFD, as encoding/json does.
func unescapeJSONString(raw []byte) []byte {
	s := raw[1 : len(raw)-1]
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		switch s[i+1] {
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			r := hex4(s[i+2 : i+6])
			i += 6
			if utf16.IsSurrogate(r) {
				if i+6 <= len(s) && s[i] == '\\' && s[i+1] == 'u' {
					if pair := utf16.DecodeRune(r, hex4(s[i+2:i+6])); pair != utf8.RuneError {
						out = utf8.AppendRune(out, pair)
						i += 6
						continue
					}
				}
				r = utf8.RuneError
			}
			out = utf8.AppendRune(out, r)
			continue
		default: // '"', '\\', '/'
			out = append(out, s[i+1])
		}
		i += 2
	}
	return out
}

// hex4 decodes four validated hexadecimal digits.
func hex4(h []byte) rune {
	var r rune
	for _, c := range h {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		default:
			r |= rune(c - 'A' + 10)
		}
	}
	return r
}

// wipe zeroes b in place. KeepAlive keeps the stores from being treated as
// dead writes to memory nothing reads again.
func wipe(b []byte) {
	clear(b)
	runtime.KeepAlive(b)
}
