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
// XKMSBackend wrote an encoded go-xkms key name. It sits outside go-xkms's
// "keys/" prefix, so go-xkms never lists a record as a key, and no native
// go-xkms RPC writes raw store records.
const xkmsKeyOwnerRecordPrefix = "quicbaas.xkms-key-owner.v1/"

// xkmsKeyOwnerRecord returns the ownership record key for a go-xkms name.
func xkmsKeyOwnerRecord(name string) string { return xkmsKeyOwnerRecordPrefix + name }

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

// ownsXKMSName reports whether this backend may read name as one of its keys.
// A literal name (one that does not decode to a service key ID) needs no
// record, so legacy and system keys behave as before. An encoded name is
// this backend's only when its ownership record exists: the node's native
// go-xkms service writes the same key store under any valid literal name,
// including one shaped like an encoded tenant key name, so only the record
// proves that this backend wrote it.
func (kb *XKMSBackend) ownsXKMSName(ctx context.Context, name string) (bool, error) {
	if _, ok := decodeXKMSKeyName(name); !ok {
		return true, nil
	}
	ok, err := kb.keyNames.Exists(ctx, xkmsKeyOwnerRecord(name))
	if err != nil {
		return false, &XKMSKeyOwnershipError{Op: "read", Name: name, Err: err}
	}
	return ok, nil
}

// claimXKMSName records that this backend wrote the encoded name, so it
// resolves and lists as the service key ID it decodes to. A literal name
// needs no record.
func (kb *XKMSBackend) claimXKMSName(ctx context.Context, name string) error {
	if _, ok := decodeXKMSKeyName(name); !ok {
		return nil
	}
	if err := kb.keyNames.Put(ctx, xkmsKeyOwnerRecord(name), []byte("1")); err != nil {
		return &XKMSKeyOwnershipError{Op: "write", Name: name, Err: err}
	}
	return nil
}

// releaseXKMSName drops the ownership record of a deleted encoded name, so a
// later key the native service stores under the same name never resolves as
// the deleted service key. A missing record is not an error.
func (kb *XKMSBackend) releaseXKMSName(ctx context.Context, name string) error {
	if _, ok := decodeXKMSKeyName(name); !ok {
		return nil
	}
	if err := kb.keyNames.Delete(ctx, xkmsKeyOwnerRecord(name)); err != nil && !errors.Is(err, kctypes.ErrStorageNotFound) {
		return &XKMSKeyOwnershipError{Op: "delete", Name: name, Err: err}
	}
	return nil
}
