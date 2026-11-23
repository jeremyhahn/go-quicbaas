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
// encoded service key ID. A name with the prefix whose payload is not such an
// encoding (an existing "qbx.production") is an ordinary literal name.
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
// The mapping is injective: two service IDs never share a go-xkms name, so
// one ID can never reach another's key material. A valid ID is used
// unchanged unless it has the encoded shape (decodeXKMSKeyName); every other
// ID, rejected or of that shape, is the prefix plus its canonical unpadded
// base64url form. A literal output therefore never has the encoded shape,
// and an encoded output always has it.
func xkmsKeyName(keyID string) string {
	// An empty ID is refused before any go-xkms call (ErrKeyCNEmpty); it is
	// not encoded.
	if keyID == "" {
		return keyID
	}
	if validation.ValidateKeyID(keyID) == nil {
		if _, encoded := decodeXKMSKeyName(keyID); !encoded {
			return keyID
		}
	}
	return xkmsEncodedKeyNamePrefix + xkmsEncoding.EncodeToString([]byte(keyID))
}

// decodeXKMSKeyName reports whether name has the encoded shape, and the
// service ID it encodes: the prefix plus the canonical encoding of a
// non-empty ID that xkmsKeyName encodes, either one go-xkms rejects or a
// valid ID that itself has this shape. A decoded ID is shorter than its
// name, so the recursion ends.
func decodeXKMSKeyName(name string) (string, bool) {
	payload, ok := strings.CutPrefix(name, xkmsEncodedKeyNamePrefix)
	if !ok {
		return "", false
	}
	decoded, err := xkmsEncoding.DecodeString(payload)
	if err != nil || len(decoded) == 0 {
		return "", false
	}
	keyID := string(decoded)
	if validation.ValidateKeyID(keyID) != nil {
		return keyID, true
	}
	if _, nested := decodeXKMSKeyName(keyID); nested {
		return keyID, true
	}
	return "", false
}

// keyIDFromXKMSName returns the service key ID behind a go-xkms key name:
// the inverse of xkmsKeyName. A name of the encoded shape is decoded; every
// other name is the key ID itself.
func keyIDFromXKMSName(name string) string {
	if keyID, ok := decodeXKMSKeyName(name); ok {
		return keyID
	}
	return name
}
