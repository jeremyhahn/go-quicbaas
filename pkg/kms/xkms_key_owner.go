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
	"context"
	"errors"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

// xkmsKeyOwnerRecordPrefix prefixes the key-store record that proves
// XKMSBackend wrote a go-xkms key under an encoded name. It sits outside
// go-xkms's "keys/" prefix, so go-xkms never lists a record as a key, and no
// native go-xkms RPC writes raw store records. The v2 record names the full
// key identity; v1 records named only the go-xkms name.
const xkmsKeyOwnerRecordPrefix = "quicbaas.xkms-key-owner.v2/"

// xkmsKeyOwnerRecord returns the ownership record key for a go-xkms key
// identity. The identity is the full go-xkms key ID,
// <backend>:<type>:<algo>:<name>, so a record vouches for exactly one stored
// key in one backend.
func xkmsKeyOwnerRecord(identity string) string { return xkmsKeyOwnerRecordPrefix + identity }

// xkmsKeyIdentity returns the full go-xkms key ID of the key attrs describe,
// <backend>:<type>:<algo>:<name>. It is the same string GetKeyMetadata
// probes and buildFullKeyID / buildSigningKeyID build, so an ownership record
// keyed by it matches exactly one stored key, in one backend. attrs.CN is
// already the go-xkms name.
func (kb *XKMSBackend) xkmsKeyIdentity(attrs *kctypes.KeyAttributes) string {
	algorithm := kb.attrsToAlgorithmString(attrs)
	if attrs.SymmetricAlgorithm != "" {
		return string(kb.backendType) + ":encryption:" + kb.algorithmToKeyIDFormat(algorithm) + ":" + attrs.CN
	}
	return string(kb.backendType) + ":signing:" + kb.signingAlgorithmToKeyIDFormat(algorithm) + ":" + attrs.CN
}

// XKMSKeyOwnershipError reports a failure to open, read, write or delete the
// ownership record of an encoded go-xkms key name.
type XKMSKeyOwnershipError struct {
	Op   string
	Name string
	Err  error
}

func (e *XKMSKeyOwnershipError) Error() string {
	return "kms: " + e.Op + " ownership record for go-xkms key " + e.Name + ": " + e.Err.Error()
}

func (e *XKMSKeyOwnershipError) Unwrap() error { return e.Err }

// ownsXKMSKey reports whether this backend may read the go-xkms key with the
// given full identity, stored under name, as one of its keys. A literal name
// (one that does not decode to a service key ID) needs no record, so legacy
// and system keys behave as before. A key under an encoded name is this
// backend's only when the ownership record of its full identity exists: the
// node's native go-xkms service writes the same key store under any valid
// literal name, including one shaped like an encoded tenant key name, and
// go-xkms keeps distinct keys under one name by backend, type and algorithm.
// A name-only record would vouch for another writer's key of a different
// type or backend, so only the identity's record proves this backend wrote
// that key.
func (kb *XKMSBackend) ownsXKMSKey(ctx context.Context, name, identity string) (bool, error) {
	if _, ok := decodeXKMSKeyName(name); !ok {
		return true, nil
	}
	ok, err := kb.keyNames.Exists(ctx, xkmsKeyOwnerRecord(identity))
	if err != nil {
		return false, &XKMSKeyOwnershipError{Op: "read", Name: identity, Err: err}
	}
	return ok, nil
}

// claimXKMSKey records that this backend wrote the key attrs describe, so it
// resolves and lists as the service key ID its encoded name decodes to. The
// record binds the full identity (backend, type, algorithm, name), not the
// name alone, because go-xkms keeps distinct keys under one name. A literal
// name needs no record.
func (kb *XKMSBackend) claimXKMSKey(ctx context.Context, attrs *kctypes.KeyAttributes) error {
	if _, ok := decodeXKMSKeyName(attrs.CN); !ok {
		return nil
	}
	identity := kb.xkmsKeyIdentity(attrs)
	if err := kb.keyNames.Put(ctx, xkmsKeyOwnerRecord(identity), []byte("1")); err != nil {
		return &XKMSKeyOwnershipError{Op: "write", Name: identity, Err: err}
	}
	return nil
}

// releaseXKMSKey drops the ownership record of the deleted key attrs
// describe, so a later key the native service stores under the same identity
// never resolves as the deleted service key. A missing record is not an
// error.
func (kb *XKMSBackend) releaseXKMSKey(ctx context.Context, attrs *kctypes.KeyAttributes) error {
	if _, ok := decodeXKMSKeyName(attrs.CN); !ok {
		return nil
	}
	identity := kb.xkmsKeyIdentity(attrs)
	if err := kb.keyNames.Delete(ctx, xkmsKeyOwnerRecord(identity)); err != nil && !errors.Is(err, kctypes.ErrStorageNotFound) {
		return &XKMSKeyOwnershipError{Op: "delete", Name: identity, Err: err}
	}
	return nil
}
