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
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jeremyhahn/go-xkms/pkg/validation"
)

// xkmsEncodedKeyNamePrefix marks a go-xkms key name that carries an encoded
// service key ID.
const xkmsEncodedKeyNamePrefix = "qbx."

// ErrXKMSKeyIDReserved is returned when a new key's ID is the encoded go-xkms
// name of a different service key ID, so the two would share one key.
var ErrXKMSKeyIDReserved = errors.New("key ID is reserved for an encoded key name")

// xkmsKeyName maps a service key ID to the key name go-xkms stores and
// resolves it under. Service IDs may hold characters go-xkms refuses in a key
// reference (a tenant's key is "tenant/<tenantID>/<name>"); go-xkms's own
// validation is not loosened, the name is encoded instead.
//
// Every name go-xkms accepts is used unchanged, as it always was, so a key
// created before the mapping existed keeps its name, whatever its prefix.
// Only an ID go-xkms refuses becomes the prefix plus its unpadded base64url
// form, so the two kinds of name are told apart by the decoded ID: an encoded
// name always decodes to an ID go-xkms refuses (see keyIDFromXKMSName).
func xkmsKeyName(keyID string) string {
	if validation.ValidateKeyID(keyID) == nil {
		return keyID
	}
	return xkmsEncodedKeyNamePrefix + base64.RawURLEncoding.EncodeToString([]byte(keyID))
}

// encodedXKMSKeyID returns the service key ID an encoded go-xkms name
// carries, and false for a name xkmsKeyName stores unchanged.
func encodedXKMSKeyID(name string) (string, bool) {
	encoded, ok := strings.CutPrefix(name, xkmsEncodedKeyNamePrefix)
	if !ok {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return "", false
	}
	id := string(decoded)
	if !utf8.ValidString(id) || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		// A service key ID is text; a legacy name such as "qbx.production"
		// decodes to arbitrary bytes and is stored as is.
		return "", false
	}
	if validation.ValidateKeyID(id) == nil {
		// xkmsKeyName never encodes a valid ID, so this name is stored as is.
		return "", false
	}
	return id, true
}

// keyIDFromXKMSName returns the service key ID behind a go-xkms key name.
func keyIDFromXKMSName(name string) string {
	if id, ok := encodedXKMSKeyID(name); ok {
		return id
	}
	return name
}

// checkNewXKMSKeyID refuses a new key whose ID is itself the encoded name of
// another service key ID: both would resolve to the same go-xkms key. The
// mapping is injective over every ID it admits.
func checkNewXKMSKeyID(keyID string) error {
	if other, ok := encodedXKMSKeyID(keyID); ok {
		return fmt.Errorf("%w: %q is the go-xkms name of key %q", ErrXKMSKeyIDReserved, keyID, other)
	}
	return nil
}
