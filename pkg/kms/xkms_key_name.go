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
	"fmt"
	"strings"

	"github.com/jeremyhahn/go-xkms/pkg/validation"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// xkmsEncodedKeyNamePrefix starts every go-xkms key name that carries an
// encoded service key ID. The prefix is reserved for encoded names: a valid
// literal name that is exactly the canonical encoding of a rejected ID is
// refused (refuseReservedKeyName). Every other "qbx." name, such as an
// existing "qbx.production", is still used unchanged.
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
// the encoded name of a rejected ID and the literal valid name
// "qbx.<canonical base64url of that ID>" share a go-xkms name. The backend
// refuses that literal at every entry point (refuseReservedKeyName), so the
// shared name only ever means the encoded ID, and keyIDFromXKMSName reads it
// that way.
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

// refuseReservedKeyName refuses a key ID that go-xkms accepts literally but
// that keyIDFromXKMSName would read as a different, encoded ID: such a
// literal names the same go-xkms key as that encoded ID (a tenant's key), so
// accepting it would cross the tenant boundary. It returns an invalid-input
// error wrapping ErrReservedKeyName, and nil for every other ID.
func refuseReservedKeyName(op, keyID string) error {
	if keyID == "" || validation.ValidateKeyID(keyID) != nil || keyIDFromXKMSName(keyID) == keyID {
		return nil
	}
	return &qberrors.QBError{
		Code: qberrors.ErrInvalidInput,
		Op:   op,
		Err:  fmt.Errorf("%w: %q", ErrReservedKeyName, keyID),
	}
}
