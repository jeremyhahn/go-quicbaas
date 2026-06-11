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

package iam

import (
	"errors"
	"testing"
)

// knownAAGUID is a real FIDO2 AAGUID used in tests (YubiKey 5 NFC).
const knownAAGUID = "2fc0579f-8113-47ea-b116-bb5a8db9202a"

// hexAAGUID is knownAAGUID without hyphens, lowercase.
const hexAAGUID = "2fc0579f811347eab116bb5a8db9202a"

// unknownAAGUID is a AAGUID that is NOT in the allowlist.
const unknownAAGUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func hexToBytes(t *testing.T, hex string) []byte {
	t.Helper()
	b := make([]byte, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		var v byte
		for j, c := range hex[i : i+2] {
			var nibble byte
			switch {
			case c >= '0' && c <= '9':
				nibble = byte(c - '0')
			case c >= 'a' && c <= 'f':
				nibble = byte(c-'a') + 10
			case c >= 'A' && c <= 'F':
				nibble = byte(c-'A') + 10
			}
			if j == 0 {
				v = nibble << 4
			} else {
				v |= nibble
			}
		}
		b[i/2] = v
	}
	return b
}

// TestAAGUIDAllowlist_LogOnly_DoesNotRejectUnknown verifies that in permissive mode,
// an unknown AAGUID does not cause an error.
func TestAAGUIDAllowlist_LogOnly_DoesNotRejectUnknown(t *testing.T) {
	allowlist := NewAAGUIDAllowlist([]string{knownAAGUID}, AAGUIDPolicyPermissive)

	unknownBytes := hexToBytes(t, "aaaaaaaabbbbccccddddeeeeeeeeeeee")
	if err := allowlist.Check(unknownBytes); err != nil {
		t.Fatalf("permissive mode must not reject unknown AAGUID: %v", err)
	}
}

// TestAAGUIDAllowlist_Enforce_RejectsUnknown verifies that in strict mode,
// an unknown AAGUID is rejected with the typed error.
func TestAAGUIDAllowlist_Enforce_RejectsUnknown(t *testing.T) {
	allowlist := NewAAGUIDAllowlist([]string{knownAAGUID}, AAGUIDPolicyStrict)

	unknownBytes := hexToBytes(t, "aaaaaaaabbbbccccddddeeeeeeeeeeee")
	err := allowlist.Check(unknownBytes)
	if err == nil {
		t.Fatal("strict mode must reject unknown AAGUID")
	}
	var notAllowed *ErrAAGUIDNotAllowedError
	if !errors.As(err, &notAllowed) {
		t.Fatalf("expected ErrAAGUIDNotAllowedError, got %T: %v", err, err)
	}
	if notAllowed.AAGUID == "" {
		t.Error("expected non-empty AAGUID in error")
	}
}

// TestAAGUIDAllowlist_Enforce_AllowsKnown verifies that in strict mode,
// a known AAGUID is accepted.
func TestAAGUIDAllowlist_Enforce_AllowsKnown(t *testing.T) {
	allowlist := NewAAGUIDAllowlist([]string{knownAAGUID}, AAGUIDPolicyStrict)

	knownBytes := hexToBytes(t, hexAAGUID)
	if err := allowlist.Check(knownBytes); err != nil {
		t.Fatalf("strict mode must allow known AAGUID: %v", err)
	}
}

// TestAAGUIDAllowlist_EmptyListPermissive verifies that an empty allowlist in
// permissive mode allows all AAGUIDs.
func TestAAGUIDAllowlist_EmptyListPermissive(t *testing.T) {
	allowlist := NewAAGUIDAllowlist(nil, AAGUIDPolicyPermissive)

	anyBytes := hexToBytes(t, "aaaaaaaabbbbccccddddeeeeeeeeeeee")
	if err := allowlist.Check(anyBytes); err != nil {
		t.Fatalf("empty permissive allowlist must allow any AAGUID: %v", err)
	}
}

// TestAAGUIDAllowlist_EmptyListStrict verifies that an empty allowlist in
// strict mode rejects all AAGUIDs (no authenticators are permitted).
func TestAAGUIDAllowlist_EmptyListStrict(t *testing.T) {
	allowlist := NewAAGUIDAllowlist(nil, AAGUIDPolicyStrict)

	anyBytes := hexToBytes(t, "aaaaaaaabbbbccccddddeeeeeeeeeeee")
	err := allowlist.Check(anyBytes)
	if err == nil {
		t.Fatal("empty strict allowlist must reject all AAGUIDs")
	}
}

// TestAAGUIDAllowlist_UUIDFormatNormalization verifies that hyphenated UUID-format
// AAGUIDs are accepted and normalized correctly.
func TestAAGUIDAllowlist_UUIDFormatNormalization(t *testing.T) {
	allowlist := NewAAGUIDAllowlist([]string{knownAAGUID}, AAGUIDPolicyStrict)

	knownBytes := hexToBytes(t, hexAAGUID)
	if err := allowlist.Check(knownBytes); err != nil {
		t.Fatalf("UUID-format AAGUID must be normalized and accepted: %v", err)
	}
	if allowlist.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", allowlist.Len())
	}
}

// TestAAGUIDAllowlist_Contains verifies the Contains helper.
func TestAAGUIDAllowlist_Contains(t *testing.T) {
	allowlist := NewAAGUIDAllowlist([]string{knownAAGUID}, AAGUIDPolicyStrict)

	knownBytes := hexToBytes(t, hexAAGUID)
	if !allowlist.Contains(knownBytes) {
		t.Error("Contains must return true for a known AAGUID")
	}

	unknownBytes := hexToBytes(t, "aaaaaaaabbbbccccddddeeeeeeeeeeee")
	if allowlist.Contains(unknownBytes) {
		t.Error("Contains must return false for an unknown AAGUID")
	}
}

// TestAAGUIDAllowlist_Policy verifies the Policy accessor.
func TestAAGUIDAllowlist_Policy(t *testing.T) {
	permissive := NewAAGUIDAllowlist(nil, AAGUIDPolicyPermissive)
	if permissive.Policy() != AAGUIDPolicyPermissive {
		t.Errorf("expected AAGUIDPolicyPermissive, got %v", permissive.Policy())
	}

	strict := NewAAGUIDAllowlist(nil, AAGUIDPolicyStrict)
	if strict.Policy() != AAGUIDPolicyStrict {
		t.Errorf("expected AAGUIDPolicyStrict, got %v", strict.Policy())
	}
}
