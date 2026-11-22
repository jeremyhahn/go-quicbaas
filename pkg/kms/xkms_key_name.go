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

package kms

import (
	"encoding/base64"
	"strings"

	"github.com/jeremyhahn/go-xkms/pkg/validation"
)

// xkmsEncodedKeyNamePrefix starts every go-xkms key name that carries an
// encoded service key ID. It is NOT reserved: a valid name that starts with
// it but is not in the encoded form (an existing "qbx.production") is used
// unchanged.
const xkmsEncodedKeyNamePrefix = "qbx."

// xkmsEncoding is strict so that each encoded payload has exactly one
// spelling: a name with non-canonical trailing bits is never an encoded
// output and reads back literally.
var xkmsEncoding = base64.RawURLEncoding.Strict()

// xkmsKeyName maps a service key ID to the key name go-xkms stores and
// resolves it under. Service IDs may hold characters go-xkms refuses in a key
// reference (a tenant's key is "tenant/<tenantID>/<name>"); go-xkms's own
// validation is not loosened, the name is encoded instead.
//
// The mapping is injective, so two service IDs never share a go-xkms key:
//   - a name go-xkms accepts that is not in the encoded form (see
//     isEncodedXKMSName) is used unchanged, so every ordinary key keeps its
//     name;
//   - every other ID is encoded as the prefix plus its canonical unpadded
//     base64url form. That covers an ID go-xkms rejects and also a valid
//     literal ID that is itself in the encoded form, so the literal
//     "qbx.dGVuYW50L2FjbWUvaw" and the tenant ID "tenant/acme/k" (whose
//     encoding it spells) get different go-xkms names.
//
// Literal outputs are never in the encoded form and encoded outputs always
// are, so the two sets are disjoint, and base64 is injective within each.
func xkmsKeyName(keyID string) string {
	// An empty ID is refused before any go-xkms call (ErrKeyCNEmpty); it is
	// not encoded, so "qbx." alone stays a literal name.
	if keyID == "" || (validation.ValidateKeyID(keyID) == nil && !isEncodedXKMSName(keyID)) {
		return keyID
	}
	return xkmsEncodedKeyNamePrefix + xkmsEncoding.EncodeToString([]byte(keyID))
}

// isEncodedXKMSName reports whether name is exactly what xkmsKeyName makes of
// an ID it encodes: the prefix, then the canonical base64url of a non-empty ID
// that go-xkms rejects or that is itself in the encoded form. The recursion
// ends because each decoded payload is shorter than the name holding it.
func isEncodedXKMSName(name string) bool {
	encoded, ok := strings.CutPrefix(name, xkmsEncodedKeyNamePrefix)
	if !ok {
		return false
	}
	decoded, err := xkmsEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return false
	}
	keyID := string(decoded)
	return validation.ValidateKeyID(keyID) != nil || isEncodedXKMSName(keyID)
}

// keyIDFromXKMSName returns the service key ID behind a go-xkms key name, the
// inverse of xkmsKeyName. It decodes a name only when the name is in the
// encoded form; every other name is the key ID itself.
func keyIDFromXKMSName(name string) string {
	if !isEncodedXKMSName(name) {
		return name
	}
	decoded, err := xkmsEncoding.DecodeString(strings.TrimPrefix(name, xkmsEncodedKeyNamePrefix))
	if err != nil {
		return name
	}
	return string(decoded)
}
