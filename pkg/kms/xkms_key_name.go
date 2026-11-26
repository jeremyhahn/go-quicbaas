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

	"github.com/jeremyhahn/go-xkms/pkg/validation"
	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// xkmsEncodedKeyNamePrefix starts every go-xkms key name that carries an
// encoded service key ID. The names it reserves are exactly the encoded
// outputs (see xkmsNeedsEncoding): this backend refuses to write a valid
// literal name that is exactly the canonical encoding of a rejected ID
// (refuseReservedKeyName). Any other name that starts with it (an existing
// "qbx.production") is used unchanged.
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
// The mapping is injective over every ID. An ID is encoded, as the prefix
// plus its canonical unpadded base64url form, when go-xkms rejects it or
// when it is itself spelled as an encoded output; every other ID is its own
// go-xkms name. An encoded output therefore always needs encoding as an ID,
// so no literal ID can land on the name of an encoded one: a system key
// named "qbx.<base64url of tenant/acme/k>" is stored under a further encoding
// and is never tenant acme's key "k".
//
// Migration: before this mapping, every go-xkms name was the literal service
// ID. A stored literal keeps working unless it is spelled as an encoded
// output (its payload is the canonical base64url of an ID that needs
// encoding); such a name would now read as the ID it encodes.
// BackendFactory.InitializeFromXKMS therefore refuses to open a key store
// that holds such a name and carries no mapping marker
// (LegacyXKMSKeyNamesError lists the names); the operator re-imports each
// listed key under a name outside that shape. The first clean open writes the
// marker, and later opens skip the scan.
//
// Ownership: a key under an encoded name resolves, or lists, only when
// XKMSBackend holds the ownership record of its full go-xkms key identity
// (backend, type, algorithm, name; xkms_key_owner.go), not of the name alone.
// The node's native go-xkms service writes the same key store under any valid
// literal name, including one spelled as an encoded output, and go-xkms keeps
// distinct keys under one name, so the marker alone cannot keep such a key
// from reading as the ID it encodes; the record proves this backend wrote it.
// TestXKMSKeyName_EncodedOutputsAreReserved pins the shape.
//
// Writes: the backend refuses a literal spelled as an encoded output at
// every entry point (refuseReservedKeyName), so a name this backend writes
// under that shape only ever means the encoded ID.
func xkmsKeyName(keyID string) string {
	if !xkmsNeedsEncoding(keyID) {
		return keyID
	}
	return xkmsEncodedKeyNamePrefix + xkmsEncoding.EncodeToString([]byte(keyID))
}

// ErrXKMSKeyIDTooLong reports a key ID whose go-xkms name go-xkms would
// refuse, in practice because base64 expansion took it past go-xkms's
// 255-character key-name limit, or because the file name the key store writes
// for it would be longer than a filesystem allows.
var ErrXKMSKeyIDTooLong = errors.New("kms: key ID too long for a go-xkms key name")

// checkXKMSKeyName refuses keyID when its go-xkms name fails go-xkms's own
// key ID validation, which Encrypt and Decrypt apply to every key reference.
// Callers check before any key material exists, so no key is stored that
// later crypto operations would reject.
func checkXKMSKeyName(keyID string) error {
	if err := validation.ValidateKeyID(xkmsKeyName(keyID)); err != nil {
		return fmt.Errorf("%w: %w", ErrXKMSKeyIDTooLong, err)
	}
	return nil
}

// xkmsMaxStoredNameComponent is the longest file-name component a
// filesystem accepts. go-xkms file storage names a key's files
// keys/<ID>.key and meta/<ID>.json, and the ownership record's last
// component is the key identity, so each must fit.
const xkmsMaxStoredNameComponent = 255

// xkmsLongestStoredNameSuffix is the longest suffix go-xkms file storage
// adds to KeyAttributes.ID() (".json", meta/<ID>.json).
const xkmsLongestStoredNameSuffix = len(".json")

// checkXKMSKeyStorage refuses the key xattrs describe (CN already the
// go-xkms name, StoreType set) when a file the production file store
// writes for it would have a name component longer than the filesystem
// allows. Callers check before any key material exists.
func (kb *XKMSBackend) checkXKMSKeyStorage(xattrs *kctypes.KeyAttributes) error {
	if n := len(xattrs.ID()) + xkmsLongestStoredNameSuffix; n > xkmsMaxStoredNameComponent {
		return fmt.Errorf("%w: stored key file name is %d bytes, over the %d-byte limit", ErrXKMSKeyIDTooLong, n, xkmsMaxStoredNameComponent)
	}
	if n := len(kb.xkmsKeyIdentity(xattrs)); n > xkmsMaxStoredNameComponent {
		return fmt.Errorf("%w: key ownership record name is %d bytes, over the %d-byte limit", ErrXKMSKeyIDTooLong, n, xkmsMaxStoredNameComponent)
	}
	return nil
}

// xkmsNeedsEncoding reports whether keyID is stored under an encoded name:
// go-xkms rejects it, or it is spelled as an encoded output. An empty ID is
// refused before any go-xkms call (ErrKeyCNEmpty) and is not encoded.
func xkmsNeedsEncoding(keyID string) bool {
	if keyID == "" {
		return false
	}
	if validation.ValidateKeyID(keyID) != nil {
		return true
	}
	_, encoded := decodeXKMSKeyName(keyID)
	return encoded
}

// decodeXKMSKeyName returns the ID name encodes and true when name is exactly
// what xkmsKeyName makes of an ID that needs encoding. The recursion ends:
// each decoded payload is shorter than the name it came from.
func decodeXKMSKeyName(name string) (string, bool) {
	payload, ok := strings.CutPrefix(name, xkmsEncodedKeyNamePrefix)
	if !ok {
		return "", false
	}
	decoded, err := xkmsEncoding.DecodeString(payload)
	if err != nil {
		return "", false
	}
	keyID := string(decoded)
	if !xkmsNeedsEncoding(keyID) {
		// An ID that needs no encoding is never encoded, so this name is a
		// literal.
		return "", false
	}
	return keyID, true
}

// keyIDFromXKMSName returns the service key ID behind a go-xkms key name. It
// decodes a name only when the name is exactly what xkmsKeyName makes of an
// ID that needs encoding; every other name is the key ID itself.
func keyIDFromXKMSName(name string) string {
	if keyID, ok := decodeXKMSKeyName(name); ok {
		return keyID
	}
	return name
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
