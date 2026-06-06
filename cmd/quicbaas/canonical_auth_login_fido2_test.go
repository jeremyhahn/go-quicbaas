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

package main

import (
	"context"
	"errors"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// withCanonicalFIDO2AssertFunc swaps the device-drive seam for the duration of a
// test and restores it afterwards. This lets the device-free login plumbing be
// exercised without a physical authenticator; it never weakens the production
// CTAP2 path, which remains the package default.
func withCanonicalFIDO2AssertFunc(t *testing.T, fn func(ctx context.Context, optionsJSON []byte, devicePath, origin string) ([]byte, error)) {
	t.Helper()
	prev := canonicalFIDO2AssertFunc
	canonicalFIDO2AssertFunc = fn
	t.Cleanup(func() { canonicalFIDO2AssertFunc = prev })
}

// TestCanonicalFIDO2OptionsFromBegin_Present extracts the WebAuthn options JSON
// from the exact ChallengeMetadata key the server's BeginChallenge sets.
func TestCanonicalFIDO2OptionsFromBegin_Present(t *testing.T) {
	begin := &qrdbtransport.BeginLoginResponse{
		ChallengeMetadata: map[string]string{
			canonicalFIDO2OptionsKey:   `{"publicKey":{"challenge":"abc"}}`,
			canonicalFIDO2SessionIDKey: "sess-1",
		},
	}
	got, err := canonicalFIDO2OptionsFromBegin(begin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != `{"publicKey":{"challenge":"abc"}}` {
		t.Fatalf("unexpected options: %s", got)
	}
}

// TestCanonicalFIDO2OptionsFromBegin_Missing returns the typed challenge-missing
// error for nil response, nil metadata, and an empty options value.
func TestCanonicalFIDO2OptionsFromBegin_Missing(t *testing.T) {
	cases := []struct {
		name  string
		begin *qrdbtransport.BeginLoginResponse
	}{
		{"nil response", nil},
		{"nil metadata", &qrdbtransport.BeginLoginResponse{}},
		{"empty options", &qrdbtransport.BeginLoginResponse{
			ChallengeMetadata: map[string]string{canonicalFIDO2SessionIDKey: "s"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := canonicalFIDO2OptionsFromBegin(tc.begin)
			var want *MissingFIDO2ChallengeError
			if !errors.As(err, &want) {
				t.Fatalf("expected *MissingFIDO2ChallengeError, got %T (%v)", err, err)
			}
		})
	}
}

// TestCanonicalFIDO2CredentialID_RawID recovers the credential id from the
// assertion's rawId field (the qrdb FIDO2 factor lookup key).
func TestCanonicalFIDO2CredentialID_RawID(t *testing.T) {
	assertion := []byte(`{"id":"AAA","rawId":"BBB","type":"public-key","response":{}}`)
	got, err := canonicalFIDO2CredentialID(assertion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "BBB" {
		t.Fatalf("expected rawId BBB, got %q", got)
	}
}

// TestCanonicalFIDO2CredentialID_FallbackID falls back to the id field when
// rawId is absent.
func TestCanonicalFIDO2CredentialID_FallbackID(t *testing.T) {
	assertion := []byte(`{"id":"CCC","type":"public-key","response":{}}`)
	got, err := canonicalFIDO2CredentialID(assertion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "CCC" {
		t.Fatalf("expected id CCC, got %q", got)
	}
}

// TestCanonicalFIDO2CredentialID_Invalid returns a typed parse error for
// malformed JSON and for an assertion carrying no credential id.
func TestCanonicalFIDO2CredentialID_Invalid(t *testing.T) {
	cases := map[string][]byte{
		"malformed json": []byte(`{not json`),
		"no credential":  []byte(`{"type":"public-key","response":{}}`),
	}
	for name, assertion := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := canonicalFIDO2CredentialID(assertion)
			var want *FIDO2AssertionParseError
			if !errors.As(err, &want) {
				t.Fatalf("expected *FIDO2AssertionParseError, got %T (%v)", err, err)
			}
		})
	}
}

// TestProveCanonicalFIDO2_MissingChallenge fails closed with the typed
// challenge-missing error before any device is touched.
func TestProveCanonicalFIDO2_MissingChallenge(t *testing.T) {
	called := false
	withCanonicalFIDO2AssertFunc(t, func(context.Context, []byte, string, string) ([]byte, error) {
		called = true
		return nil, nil
	})
	begin := &qrdbtransport.BeginLoginResponse{} // no options
	_, _, _, err := proveCanonicalFIDO2(context.Background(), &canonicalAuthLoginOptions{}, begin)
	var want *MissingFIDO2ChallengeError
	if !errors.As(err, &want) {
		t.Fatalf("expected *MissingFIDO2ChallengeError, got %T (%v)", err, err)
	}
	if called {
		t.Fatal("device assert must not be invoked when no challenge is issued")
	}
}

// TestProveCanonicalFIDO2_AssertError surfaces device/timeout errors from the
// authenticator unchanged.
func TestProveCanonicalFIDO2_AssertError(t *testing.T) {
	deviceErr := &FIDO2OperationError{Op: "Assert", Cause: errors.New("user-presence timeout")}
	withCanonicalFIDO2AssertFunc(t, func(context.Context, []byte, string, string) ([]byte, error) {
		return nil, deviceErr
	})
	begin := &qrdbtransport.BeginLoginResponse{
		ChallengeMetadata: map[string]string{canonicalFIDO2OptionsKey: `{"publicKey":{}}`},
	}
	_, _, _, err := proveCanonicalFIDO2(context.Background(), &canonicalAuthLoginOptions{}, begin)
	if !errors.Is(err, deviceErr) {
		t.Fatalf("expected wrapped device error, got %v", err)
	}
}

// TestProveCanonicalFIDO2_Success asserts the full proof assembly: the lookup
// key is the credential id bytes, the signature carries the assertion JSON, and
// the metadata carries the session id + credential id under the EXACT keys the
// server verifier reads. It also confirms the options JSON and device path are
// threaded to the device-drive seam unchanged.
func TestProveCanonicalFIDO2_Success(t *testing.T) {
	const (
		optionsJSON = `{"publicKey":{"challenge":"Y2hhbGxlbmdl","rpId":"quicbaas"}}`
		sessionID   = "rp-session-42"
		credID      = "Y3JlZC1pZA"
	)
	assertionDoc := []byte(`{"id":"` + credID + `","rawId":"` + credID +
		`","type":"public-key","response":{"authenticatorData":"AA","clientDataJSON":"BB","signature":"CC"}}`)

	var gotOptions []byte
	var gotDevice, gotOrigin string
	withCanonicalFIDO2AssertFunc(t, func(_ context.Context, options []byte, device, origin string) ([]byte, error) {
		gotOptions = options
		gotDevice = device
		gotOrigin = origin
		return assertionDoc, nil
	})

	begin := &qrdbtransport.BeginLoginResponse{
		ChallengeMetadata: map[string]string{
			canonicalFIDO2OptionsKey:   optionsJSON,
			canonicalFIDO2SessionIDKey: sessionID,
		},
	}
	opts := &canonicalAuthLoginOptions{device: "/dev/hidraw5", origin: "https://quicbaas"}

	lookupKey, signature, meta, err := proveCanonicalFIDO2(context.Background(), opts, begin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(gotOptions) != optionsJSON {
		t.Errorf("options not threaded: got %s", gotOptions)
	}
	if gotDevice != "/dev/hidraw5" {
		t.Errorf("device path not threaded: got %q", gotDevice)
	}
	if gotOrigin != "https://quicbaas" {
		t.Errorf("origin not threaded: got %q", gotOrigin)
	}
	if string(lookupKey) != credID {
		t.Errorf("lookup key = %q, want credential id %q", lookupKey, credID)
	}
	if string(signature) != string(assertionDoc) {
		t.Errorf("signature must carry the assertion JSON; got %s", signature)
	}
	if meta[canonicalFIDO2SessionIDKey] != sessionID {
		t.Errorf("meta[%q] = %q, want %q", canonicalFIDO2SessionIDKey, meta[canonicalFIDO2SessionIDKey], sessionID)
	}
	if meta[canonicalFIDO2CredentialIDKey] != credID {
		t.Errorf("meta[%q] = %q, want %q", canonicalFIDO2CredentialIDKey, meta[canonicalFIDO2CredentialIDKey], credID)
	}
}

// TestMissingFIDO2ChallengeError_Message documents the operator-facing message
// and that it names the metadata key.
func TestMissingFIDO2ChallengeError_Message(t *testing.T) {
	msg := (&MissingFIDO2ChallengeError{}).Error()
	if msg == "" {
		t.Fatal("empty error message")
	}
	if !errors.As(error(&MissingFIDO2ChallengeError{}), new(*MissingFIDO2ChallengeError)) {
		t.Fatal("type assertion failed")
	}
}

// TestFIDO2AssertionParseError_Unwrap exposes the underlying cause.
func TestFIDO2AssertionParseError_Unwrap(t *testing.T) {
	inner := errors.New("boom")
	e := &FIDO2AssertionParseError{Cause: inner}
	if !errors.Is(e, inner) {
		t.Fatal("expected Unwrap to expose the cause")
	}
}
