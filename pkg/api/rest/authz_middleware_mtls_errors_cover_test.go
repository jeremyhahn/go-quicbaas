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

//go:build rest && iam

package rest

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestExtractJWTClaims_InvalidJSON verifies that a JWT whose payload is valid
// base64url but not valid JSON surfaces a JWTUnmarshalError that wraps the
// underlying json error.
func TestExtractJWTClaims_InvalidJSON(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))
	// "not-json-payload" is valid base64url but decodes to bytes that are not a
	// JSON object, so json.Unmarshal into a map fails.
	payload := base64.RawURLEncoding.EncodeToString([]byte("not-json-payload"))
	sig := base64.RawURLEncoding.EncodeToString([]byte("dummy-sig"))
	token := header + "." + payload + "." + sig

	_, err := extractJWTClaims(token)
	if err == nil {
		t.Fatal("expected error for invalid JSON payload, got nil")
	}

	unmarshalErr, ok := err.(*JWTUnmarshalError)
	if !ok {
		t.Fatalf("expected *JWTUnmarshalError, got %T", err)
	}
	if unmarshalErr.Error() == "" {
		t.Error("JWTUnmarshalError.Error() returned empty string")
	}
	if unmarshalErr.Unwrap() == nil {
		t.Error("JWTUnmarshalError.Unwrap() returned nil")
	}
}

// TestJWTErrorTypes_ErrorAndUnwrap exercises the Error() and Unwrap() methods of
// the JWT error types to lock in their string and cause semantics.
func TestJWTErrorTypes_ErrorAndUnwrap(t *testing.T) {
	formatErr := &InvalidJWTFormatError{}
	if got := formatErr.Error(); got != "invalid JWT format: expected three dot-separated parts" {
		t.Errorf("InvalidJWTFormatError.Error() = %q", got)
	}

	cause := &InvalidJWTFormatError{}

	decodeErr := &JWTDecodeError{Cause: cause}
	if !strings.Contains(decodeErr.Error(), "base64url-decode") {
		t.Errorf("JWTDecodeError.Error() = %q, want it to mention base64url-decode", decodeErr.Error())
	}
	if decodeErr.Unwrap() != cause {
		t.Error("JWTDecodeError.Unwrap() did not return the wrapped cause")
	}

	unmarshalErr := &JWTUnmarshalError{Cause: cause}
	if !strings.Contains(unmarshalErr.Error(), "unmarshal JWT payload") {
		t.Errorf("JWTUnmarshalError.Error() = %q, want it to mention unmarshal JWT payload", unmarshalErr.Error())
	}
	if unmarshalErr.Unwrap() != cause {
		t.Error("JWTUnmarshalError.Unwrap() did not return the wrapped cause")
	}
}
