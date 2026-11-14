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

// xkmsEncodedKeyNamePrefix marks a go-xkms key name that carries an encoded
// service key ID. No name passed through unchanged starts with it.
const xkmsEncodedKeyNamePrefix = "qbx."

// xkmsKeyName maps a service key ID to the key name go-xkms stores and
// resolves it under. Service IDs may hold characters go-xkms refuses in a key
// reference (a tenant's key is "tenant/<tenantID>/<name>"); go-xkms's own
// validation is not loosened, the name is encoded instead.
//
// A name go-xkms accepts is used unchanged, so existing keys keep their
// names. Any other ID, or one that starts with the reserved prefix, becomes
// the prefix plus its unpadded base64url form. The mapping is injective, and
// keyIDFromXKMSName inverts it.
func xkmsKeyName(keyID string) string {
	if !strings.HasPrefix(keyID, xkmsEncodedKeyNamePrefix) && validation.ValidateKeyID(keyID) == nil {
		return keyID
	}
	return xkmsEncodedKeyNamePrefix + base64.RawURLEncoding.EncodeToString([]byte(keyID))
}

// keyIDFromXKMSName returns the service key ID behind a go-xkms key name.
func keyIDFromXKMSName(name string) string {
	encoded, ok := strings.CutPrefix(name, xkmsEncodedKeyNamePrefix)
	if !ok {
		return name
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return name
	}
	return string(decoded)
}
