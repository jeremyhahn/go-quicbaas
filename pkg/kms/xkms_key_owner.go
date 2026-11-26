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
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// xkmsKeyOwnerRecordPrefix prefixes the key-store record that proves
// XKMSBackend wrote a go-xkms key under an encoded name. It sits outside
// go-xkms's "keys/" prefix, so go-xkms never lists a record as a key, and no
// native go-xkms RPC writes raw store records. The record names the full key
// identity and, from v3 on, carries a proof bound to the key material (see
// xkmsKeyOwnerProof). A v2 record holds no proof and is never read, so the key
// it named is not owned.
const xkmsKeyOwnerRecordPrefix = "quicbaas.xkms-key-owner.v3/"

// xkmsKeyOwnerCheckPlaintext is the plaintext a symmetric key's ownership
// proof encrypts.
var xkmsKeyOwnerCheckPlaintext = []byte("quicbaas xkms key owner check v3")

// xkmsKeyOwnerProof is the content of an ownership record. It binds the record
// to the key material this backend wrote: a signing key's public-key SHA-256,
// or a check value encrypted under a symmetric key with the record name as
// additional data. Material stored again under the same identity, by any
// writer, does not match it.
type xkmsKeyOwnerProof struct {
	PublicKeySHA256 string `json:"pk,omitempty"`
	Check           []byte `json:"kcv,omitempty"`
	// Parent is the service key ID this key is a rotated version of; empty
	// for a standalone key. A record with a parent vouches for the key only
	// as that parent's version, never as a key of its own.
	Parent string `json:"parent,omitempty"`
}

// isXKMSMaterialAbsent reports whether err from a go-xkms key read confirms
// that no material is stored under the identity. Any other error leaves the
// material's state unknown.
func isXKMSMaterialAbsent(err error) bool {
	return errors.Is(err, kctypes.ErrBackendKeyNotFound) || errors.Is(err, kctypes.ErrStorageNotFound)
}

// xkmsMaterialAbsent reads the material stored under identity and reports
// whether it is confirmed absent. A read error that is not a confirmed absence
// is returned.
func (kb *XKMSBackend) xkmsMaterialAbsent(ctx context.Context, identity string, symmetric bool) (bool, error) {
	var err error
	if symmetric {
		_, err = xkms.GetSymmetricKey(ctx, identity)
	} else {
		_, err = xkms.SignerByID(ctx, identity)
	}
	if err == nil {
		return false, nil
	}
	if isXKMSMaterialAbsent(err) {
		return true, nil
	}
	return false, err
}

// xkmsPublicKeySHA256 returns the hex SHA-256 of signer's PKIX public key.
func xkmsPublicKeySHA256(signer crypto.Signer) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

// xkmsKeyOwnerProofFor computes the proof of the material currently stored
// under identity, for the ownership record named record.
func (kb *XKMSBackend) xkmsKeyOwnerProofFor(ctx context.Context, identity, record string, symmetric bool) (xkmsKeyOwnerProof, error) {
	if !symmetric {
		signer, err := xkms.SignerByID(ctx, identity)
		if err != nil {
			return xkmsKeyOwnerProof{}, err
		}
		fp, err := xkmsPublicKeySHA256(signer)
		if err != nil {
			return xkmsKeyOwnerProof{}, err
		}
		return xkmsKeyOwnerProof{PublicKeySHA256: fp}, nil
	}
	encrypted, err := xkms.Encrypt(ctx, identity, xkmsKeyOwnerCheckPlaintext, &kctypes.EncryptOptions{AdditionalData: []byte(record)})
	if err != nil {
		return xkmsKeyOwnerProof{}, err
	}
	check, err := kb.serializeEncryptedData(encrypted)
	if err != nil {
		return xkmsKeyOwnerProof{}, err
	}
	return xkmsKeyOwnerProof{Check: check}, nil
}

// xkmsDecrypt decrypts data under kid through the backend's decrypt seam,
// xkms.Decrypt when none is set.
func (kb *XKMSBackend) xkmsDecrypt(ctx context.Context, kid string, data *kctypes.EncryptedData, opts *kctypes.DecryptOptions) ([]byte, error) {
	if kb.decrypt != nil {
		return kb.decrypt(ctx, kid, data, opts)
	}
	return xkms.Decrypt(ctx, kid, data, opts)
}

// xkmsGetSymmetricKey reads the symmetric key kid through the test seam, or
// through go-xkms when none is set.
func (kb *XKMSBackend) xkmsGetSymmetricKey(ctx context.Context, kid string) (kctypes.SymmetricKey, error) {
	if kb.getSymmetricKey != nil {
		return kb.getSymmetricKey(ctx, kid)
	}
	return xkms.GetSymmetricKey(ctx, kid)
}

// xkmsDeleteKey deletes the key under attrs through the test seam when set,
// and through xkms.DeleteKey otherwise.
func (kb *XKMSBackend) xkmsDeleteKey(ctx context.Context, attrs *kctypes.KeyAttributes) error {
	if kb.deleteKey != nil {
		return kb.deleteKey(ctx, attrs)
	}
	return xkms.DeleteKey(ctx, attrs)
}

// xkmsAEADOpenErrors are the errors an AEAD returns when a ciphertext does
// not authenticate under its key and additional data. Go and x/crypto keep
// them unexported, so they are captured once from a failing Open.
// NewGCMWithRandomNonce is used because it also works under fips140=only.
var xkmsAEADOpenErrors = captureXKMSAEADOpenErrors()

func captureXKMSAEADOpenErrors() []error {
	var openErrs []error
	if block, err := aes.NewCipher(make([]byte, 16)); err == nil {
		if gcm, err := cipher.NewGCMWithRandomNonce(block); err == nil {
			for _, n := range []int{gcm.Overhead(), gcm.Overhead() - 1} {
				if _, err := gcm.Open(nil, nil, make([]byte, n), nil); err != nil {
					openErrs = append(openErrs, err)
				}
			}
		}
	}
	if c, err := chacha20poly1305.New(make([]byte, chacha20poly1305.KeySize)); err == nil {
		if _, err := c.Open(nil, make([]byte, c.NonceSize()), make([]byte, c.Overhead()), nil); err != nil {
			openErrs = append(openErrs, err)
		}
	}
	return openErrs
}

// isXKMSProofMismatch reports whether err from decrypting an ownership
// proof is an authentication failure: the material under the identity is
// not the material the proof was made with. Any other error (a throttled,
// denied or unreachable backend) says nothing about ownership.
func isXKMSProofMismatch(err error) bool {
	if errors.Is(err, kctypes.AESGCMErrDecryptionFailed) {
		return true
	}
	for _, openErr := range xkmsAEADOpenErrors {
		if errors.Is(err, openErr) {
			return true
		}
	}
	return false
}

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
		// The same kid buildFullKeyID builds, under the type the key is
		// stored under: the legacy type's segment only for a legacy key.
		segment := symmetricKeyTypeSegment
		if attrs.KeyType == legacySymmetricKeyType {
			segment = legacySymmetricKeyTypeSegment
		}
		return string(kb.backendType) + ":" + segment + ":" + kb.algorithmToKeyIDFormat(algorithm) + ":" + attrs.CN
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
// given full identity, stored under name, as one of its keys. symmetric names
// the identity's key type. A literal name (one that does not decode to a
// service key ID) needs no record, so legacy and system keys behave as before.
// A key under an encoded name is this backend's only when the ownership record
// of its full identity exists and its proof verifies against the material
// stored under that identity now: the node's native go-xkms service writes the
// same key store under any valid literal name, including one shaped like an
// encoded tenant key name, and go-xkms keeps distinct keys under one name by
// backend, type and algorithm. The record proves the material this backend
// wrote, not just the identity, so a key that native go-xkms deletes and
// recreates under the same identity is not owned. A missing, unreadable or
// non-matching proof means not owned; a material read error that is not a
// confirmed absence is returned. A symmetric proof that fails to decrypt
// means not owned only when the failure is an AEAD authentication failure;
// any other decrypt error returns an *XKMSKeyOwnershipError with Op
// "verify", since a throttled or denied decrypt says nothing about
// ownership. ownsXKMSKey asks whether the key is owned
// as a standalone key; a rotated version is owned only through
// ownsXKMSKeyAs with its parent.
func (kb *XKMSBackend) ownsXKMSKey(ctx context.Context, name, identity string, symmetric bool) (bool, error) {
	return kb.ownsXKMSKeyAs(ctx, name, identity, symmetric, "")
}

// ownsXKMSKeyAs is ownsXKMSKey for a key owned as parent's rotated version;
// an empty parent means a standalone key. A record whose parent differs does
// not vouch for the key, so a version never resolves as a key of its own and
// a standalone key never serves as another key's version.
func (kb *XKMSBackend) ownsXKMSKeyAs(ctx context.Context, name, identity string, symmetric bool, parent string) (bool, error) {
	if _, ok := decodeXKMSKeyName(name); !ok {
		return true, nil
	}
	record := xkmsKeyOwnerRecord(identity)
	ok, err := kb.keyNames.Exists(ctx, record)
	if err != nil {
		return false, &XKMSKeyOwnershipError{Op: "read", Name: identity, Err: err}
	}
	if !ok {
		return false, nil
	}
	raw, err := kb.keyNames.Get(ctx, record)
	if err != nil {
		if errors.Is(err, kctypes.ErrStorageNotFound) {
			return false, nil
		}
		return false, &XKMSKeyOwnershipError{Op: "read", Name: identity, Err: err}
	}
	if len(raw) == 0 {
		return false, nil
	}
	var proof xkmsKeyOwnerProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return false, nil
	}
	if proof.Parent != parent {
		return false, nil
	}
	if !symmetric {
		if proof.PublicKeySHA256 == "" {
			return false, nil
		}
		signer, err := xkms.SignerByID(ctx, identity)
		if err != nil {
			if isXKMSMaterialAbsent(err) {
				return false, nil
			}
			return false, &XKMSKeyOwnershipError{Op: "verify", Name: identity, Err: err}
		}
		fp, err := xkmsPublicKeySHA256(signer)
		if err != nil {
			return false, nil
		}
		return fp == proof.PublicKeySHA256, nil
	}
	if len(proof.Check) == 0 {
		return false, nil
	}
	if _, err := xkms.GetSymmetricKey(ctx, identity); err != nil {
		if isXKMSMaterialAbsent(err) {
			return false, nil
		}
		return false, &XKMSKeyOwnershipError{Op: "verify", Name: identity, Err: err}
	}
	encrypted, err := kb.deserializeEncryptedData(proof.Check)
	if err != nil {
		return false, nil
	}
	plaintext, err := kb.xkmsDecrypt(ctx, identity, encrypted, &kctypes.DecryptOptions{AdditionalData: []byte(record)})
	if err != nil {
		// The material is present (GetSymmetricKey succeeded). Only an
		// authentication failure proves it is not the material this backend
		// wrote; any other error leaves ownership unknown.
		if isXKMSProofMismatch(err) {
			return false, nil
		}
		return false, &XKMSKeyOwnershipError{Op: "verify", Name: identity, Err: err}
	}
	return bytes.Equal(plaintext, xkmsKeyOwnerCheckPlaintext), nil
}

// claimXKMSKey records that this backend wrote the key attrs describe, so it
// resolves and lists as the service key ID its encoded name decodes to. The
// record binds the full identity (backend, type, algorithm, name), not the
// name alone, because go-xkms keeps distinct keys under one name, and it holds
// a proof of the material just written, so only that material verifies as
// owned (see ownsXKMSKey). A literal name needs no record. claimXKMSKey
// claims a standalone key; a rotated version is claimed with its parent
// through claimXKMSKeyAs.
func (kb *XKMSBackend) claimXKMSKey(ctx context.Context, attrs *kctypes.KeyAttributes) error {
	return kb.claimXKMSKeyAs(ctx, attrs, "")
}

// claimXKMSKeyAs is claimXKMSKey for a key written as parent's rotated
// version; an empty parent claims a standalone key. The record names the
// parent, so the version is owned only as that key's version (see
// ownsXKMSKeyAs).
func (kb *XKMSBackend) claimXKMSKeyAs(ctx context.Context, attrs *kctypes.KeyAttributes, parent string) error {
	if _, ok := decodeXKMSKeyName(attrs.CN); !ok {
		return nil
	}
	identity := kb.xkmsKeyIdentity(attrs)
	record := xkmsKeyOwnerRecord(identity)
	proof, err := kb.xkmsKeyOwnerProofFor(ctx, identity, record, attrs.SymmetricAlgorithm != "")
	if err != nil {
		return &XKMSKeyOwnershipError{Op: "write", Name: identity, Err: err}
	}
	proof.Parent = parent
	value, err := json.Marshal(proof)
	if err != nil {
		return &XKMSKeyOwnershipError{Op: "write", Name: identity, Err: err}
	}
	if err := kb.keyNames.Put(ctx, record, value); err != nil {
		return &XKMSKeyOwnershipError{Op: "write", Name: identity, Err: err}
	}
	return nil
}

// xkmsRollbackTimeout bounds the delete that rolls back an unclaimed key.
const xkmsRollbackTimeout = 30 * time.Second

// rollbackUnclaimedXKMSKey deletes the key material just created under attrs
// after its ownership record could not be written. Without the rollback the
// key would stay unowned: metadata lookups refuse it, and a retry fails because
// the key already exists. The claim error is returned unchanged when the key is
// gone; when the delete also fails, both errors are returned and the key stays
// unowned and invisible. The delete runs under its own bounded context, so a
// request that ended mid-claim still rolls back.
func (kb *XKMSBackend) rollbackUnclaimedXKMSKey(ctx context.Context, attrs *kctypes.KeyAttributes, claimErr error) error {
	// The claim can fail because the request's context ended; the delete must
	// still run, so it keeps the request's values but not its cancellation or
	// deadline, and is bounded on its own.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), xkmsRollbackTimeout)
	defer cancel()
	// go-xkms resolves the backend from attrs.Backend and falls back to its
	// default backend, not to StoreType; delete from the backend that created
	// the key.
	cleanup := *attrs
	cleanup.Backend = string(kb.backendType)
	if delErr := kb.xkmsDeleteKey(cleanupCtx, &cleanup); delErr != nil && !errors.Is(delErr, xkms.ErrKeyNotFound) {
		return errors.Join(claimErr, &XKMSKeyOwnershipError{Op: "roll back", Name: attrs.CN, Err: delErr})
	}
	return claimErr
}

// releaseXKMSKey drops the ownership record of the deleted key attrs
// describe, so a later key the native service stores under the same identity
// never resolves as the deleted service key. It reports whether a record was
// there to drop; a missing record is not an error, and a literal name has
// none.
func (kb *XKMSBackend) releaseXKMSKey(ctx context.Context, attrs *kctypes.KeyAttributes) (bool, error) {
	return kb.releaseXKMSKeyIdentity(ctx, attrs.CN, kb.xkmsKeyIdentity(attrs))
}

// releaseXKMSKeyIdentity drops the ownership record of identity, the full
// go-xkms key ID of a key stored under name, and reports whether one was
// there. A literal name has no record.
func (kb *XKMSBackend) releaseXKMSKeyIdentity(ctx context.Context, name, identity string) (bool, error) {
	if _, ok := decodeXKMSKeyName(name); !ok {
		return false, nil
	}
	record := xkmsKeyOwnerRecord(identity)
	ok, err := kb.keyNames.Exists(ctx, record)
	if err != nil {
		return false, &XKMSKeyOwnershipError{Op: "read", Name: identity, Err: err}
	}
	if !ok {
		return false, nil
	}
	if err := kb.keyNames.Delete(ctx, record); err != nil {
		if errors.Is(err, kctypes.ErrStorageNotFound) {
			return false, nil
		}
		return false, &XKMSKeyOwnershipError{Op: "delete", Name: identity, Err: err}
	}
	return true, nil
}
