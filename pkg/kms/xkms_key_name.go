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
// it (an existing "qbx.production") is used unchanged.
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
// A name go-xkms accepts is used unchanged, always, so every existing key
// keeps its name. Only an ID go-xkms rejects is encoded, as the prefix plus
// its canonical unpadded base64url form.
//
// go-xkms has one key-name space, and an encoded name must live in it, so
// the mapping cannot be injective over every ID: the literal valid name
// "qbx.<canonical base64url of a rejected ID>" and that rejected ID share a
// go-xkms name. That is the only overlap; keyIDFromXKMSName reads such a
// name as the encoded ID.
func xkmsKeyName(keyID string) string {
	// An empty ID is refused before any go-xkms call (ErrKeyCNEmpty); it is
	// not encoded, so "qbx." alone stays a literal name.
	if keyID == "" || validation.ValidateKeyID(keyID) == nil {
		return keyID
	}
	return xkmsEncodedKeyNamePrefix + xkmsEncoding.EncodeToString([]byte(keyID))
}

// keyIDFromXKMSName returns the service key ID behind a go-xkms key name. It
// decodes a name only when the name is exactly what xkmsKeyName makes of an
// ID go-xkms rejects; every other name is the key ID itself.
func keyIDFromXKMSName(name string) string {
	encoded, ok := strings.CutPrefix(name, xkmsEncodedKeyNamePrefix)
	if !ok {
		return name
	}
	decoded, err := xkmsEncoding.DecodeString(encoded)
	if err != nil {
		return name
	}
	keyID := string(decoded)
	if keyID == "" || validation.ValidateKeyID(keyID) == nil {
		// Neither an empty nor a valid ID is ever encoded, so this name is
		// a literal.
		return name
	}
	return keyID
}
