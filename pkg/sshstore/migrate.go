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

// Package sshstore moves the legacy plaintext go-xkms SSH file store into the
// barrier-sealed store quicbaasd serves SSH from, proves every record arrived
// intact, and then destroys the plaintext.
//
// quicbaasd used to keep SSH private keys, host pins and authorized-keys
// entries as files under <data-dir>/xkms-ssh. The store now lives in the
// embedded qrdb node's barrier-encrypted keyspace; this package performs the
// one-time move on the first boot that finds the old directory.
package sshstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	gossh "golang.org/x/crypto/ssh"
)

const (
	// LegacyDirName is the directory under the data dir that held the SSH
	// store as plaintext files before it moved behind the barrier.
	LegacyDirName = "xkms-ssh"

	// wipingSuffix marks a legacy tree that has been migrated and verified
	// and is being destroyed. A boot that finds it finishes the wipe.
	wipingSuffix = ".wiping"

	// recordPrefix is the namespace go-xkms keeps every SSH record under.
	recordPrefix = "ssh/"

	keysPrefix           = "ssh/keys/"
	knownHostsPrefix     = "ssh/known_hosts/"
	authorizedKeysPrefix = "ssh/authorized_keys/"

	// challengeSize is the length of the random message each migrated key
	// signs to prove the sealed copy holds the legacy private key.
	challengeSize = 32
)

// Report describes one migration run.
type Report struct {
	// LegacyDir is the legacy store directory that was examined.
	LegacyDir string

	// Found is true when a legacy store or an interrupted wipe was present.
	Found bool

	// ResumedWipe is true when a previous run had migrated and verified the
	// store but was interrupted while destroying it; this run finished that.
	ResumedWipe bool

	// Keys, KnownHosts and AuthorizedKeys count the records migrated by kind.
	// Other counts records under the SSH namespace of no known kind; they
	// are migrated byte for byte like the rest.
	Keys           int
	KnownHosts     int
	AuthorizedKeys int
	Other          int
}

// Records returns the total number of records migrated.
func (r Report) Records() int {
	return r.Keys + r.KnownHosts + r.AuthorizedKeys + r.Other
}

// LegacyDir returns the legacy plaintext store directory under dataDir.
func LegacyDir(dataDir string) string {
	return filepath.Join(dataDir, LegacyDirName)
}

// LegacyPresent reports whether legacyDir, or an interrupted wipe of it,
// still exists on disk.
func LegacyPresent(legacyDir string) (bool, error) {
	for _, dir := range []string{legacyDir, legacyDir + wipingSuffix} {
		exists, err := dirExists(dir)
		if err != nil || exists {
			return exists, err
		}
	}
	return false, nil
}

// MigrateLegacy moves every record of the legacy file store at legacyDir into
// sealed, then destroys the legacy files.
//
// Each record is copied byte for byte and read back. Each key must then prove
// that its sealed copy holds the same key pair: the public key and fingerprint
// must match, and a signature over a random challenge made by the sealed
// signer must verify against the legacy public key. The legacy tree is
// destroyed only after every record passes. Each file is overwritten with
// random bytes and synced before it is unlinked.
//
// Any failure leaves the legacy store in place and returns a typed error. The
// run is idempotent: a record already present in sealed with identical bytes
// is accepted, so a boot that failed part-way can simply retry. A record
// present with different bytes is a *RecordConflictError, and nothing is
// overwritten.
//
// provider is the backend provider the sealed store serves with. It is needed
// only to verify backend-backed keys, and may be nil when there are none.
// sealed stays owned by the caller and is not closed.
//
// When neither legacyDir nor an interrupted wipe of it exists, MigrateLegacy
// returns a Report with Found false and does nothing.
func MigrateLegacy(ctx context.Context, legacyDir string, sealed xkmssdk.StorageBackend, provider xkmsssh.BackendProvider) (Report, error) {
	report := Report{LegacyDir: legacyDir}
	if legacyDir == "" {
		return report, &NilInputError{Field: "legacyDir"}
	}
	if sealed == nil {
		return report, &NilInputError{Field: "sealed"}
	}

	tombstone := legacyDir + wipingSuffix
	interrupted, err := dirExists(tombstone)
	if err != nil {
		return report, err
	}
	if interrupted {
		report.Found = true
		report.ResumedWipe = true
		if err := wipeTree(tombstone); err != nil {
			return report, err
		}
	}

	present, err := dirExists(legacyDir)
	if err != nil || !present {
		return report, err
	}
	report.Found = true

	if err := migrate(ctx, legacyDir, sealed, provider, &report); err != nil {
		return report, err
	}

	// Rename first so an interrupted wipe is recognisable as already
	// migrated and verified, and is never read as a store to migrate.
	if err := os.Rename(legacyDir, tombstone); err != nil {
		return report, &LegacyStoreError{Op: "rename", Path: legacyDir, Err: err}
	}
	return report, wipeTree(tombstone)
}

// record is one raw key/value pair of the legacy store.
type record struct {
	key   string
	value []byte
}

// migrate copies and verifies every legacy record. The legacy store is closed
// before it returns, so the caller can safely destroy its files.
func migrate(ctx context.Context, legacyDir string, sealed xkmssdk.StorageBackend, provider xkmsssh.BackendProvider, report *Report) (err error) {
	legacyBackend, err := xkmsssh.NewFileStorage(legacyDir)
	if err != nil {
		return &LegacyStoreError{Op: "open", Path: legacyDir, Err: err}
	}
	legacy, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: legacyBackend})
	if err != nil {
		return &LegacyStoreError{Op: "open", Path: legacyDir, Err: errors.Join(err, legacyBackend.Close())}
	}
	defer func() {
		if cerr := legacy.Close(); cerr != nil {
			err = errors.Join(err, &LegacyStoreError{Op: "close", Path: legacyDir, Err: cerr})
		}
	}()

	records, err := readRecords(ctx, legacyBackend, legacyDir)
	defer func() {
		for _, r := range records {
			clear(r.value)
		}
	}()
	if err != nil {
		return err
	}

	for _, r := range records {
		if err := copyRecord(ctx, sealed, r); err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(r.key, keysPrefix):
			report.Keys++
		case strings.HasPrefix(r.key, knownHostsPrefix):
			report.KnownHosts++
		case strings.HasPrefix(r.key, authorizedKeysPrefix):
			report.AuthorizedKeys++
		default:
			report.Other++
		}
	}

	return verifyKeys(ctx, legacy, sealed, provider, report.Keys)
}

// readRecords returns every record under the SSH namespace, sorted by key.
func readRecords(ctx context.Context, backend xkmssdk.StorageBackend, legacyDir string) ([]record, error) {
	var records []record
	err := backend.Scan(ctx, recordPrefix, func(key string, value []byte) error {
		records = append(records, record{key: key, value: bytes.Clone(value)})
		return nil
	})
	if err != nil {
		return records, &LegacyStoreError{Op: "scan", Path: legacyDir, Err: err}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].key < records[j].key })
	return records, nil
}

// copyRecord writes r into sealed and reads it back. A record already present
// with identical bytes is accepted, so a partial run can be retried.
func copyRecord(ctx context.Context, sealed xkmssdk.StorageBackend, r record) error {
	exists, err := sealed.Exists(ctx, r.key)
	if err != nil {
		return &SealedStoreError{Op: "exists", Key: r.key, Err: err}
	}
	if !exists {
		if err := sealed.Put(ctx, r.key, r.value); err != nil {
			return &SealedStoreError{Op: "put", Key: r.key, Err: err}
		}
	}

	stored, err := sealed.Get(ctx, r.key)
	if err != nil {
		return &SealedStoreError{Op: "get", Key: r.key, Err: err}
	}
	defer clear(stored)
	if bytes.Equal(stored, r.value) {
		return nil
	}
	if exists {
		return &RecordConflictError{Key: r.key}
	}
	return &RecordMismatchError{Key: r.key}
}

// verifyKeys proves every legacy key is usable from sealed. keyRecords is the
// number of raw key records copied; every one must parse as a key, because a
// record that does not parse cannot be verified.
func verifyKeys(ctx context.Context, legacy xkmsssh.Client, sealed xkmssdk.StorageBackend, provider xkmsssh.BackendProvider, keyRecords int) error {
	keys, err := legacy.Keys().List(ctx, xkmsssh.ListKeysRequest{})
	if err != nil {
		return &KeyVerificationError{Key: keysPrefix, Reason: "list legacy keys", Err: err}
	}
	if len(keys) != keyRecords {
		return &UnparsedKeysError{Records: keyRecords, Parsed: len(keys)}
	}

	// The verifying client is not closed: closing it would close sealed,
	// which the caller owns.
	verifier, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: sealed, BackendProvider: provider})
	if err != nil {
		return &KeyVerificationError{Key: keysPrefix, Reason: "open sealed store", Err: err}
	}
	for _, k := range keys {
		if err := verifyKey(ctx, verifier.Keys(), k); err != nil {
			return err
		}
	}
	return nil
}

// verifyKey proves the sealed copy of legacyKey holds the same key pair.
func verifyKey(ctx context.Context, sealed xkmsssh.KeysClient, legacyKey *xkmsssh.Key) error {
	id := legacyKey.Scope + "/" + legacyKey.Owner + "/" + legacyKey.Name
	req := xkmsssh.GetKeyRequest{Scope: legacyKey.Scope, Owner: legacyKey.Owner, Name: legacyKey.Name}

	got, err := sealed.Get(ctx, req)
	if err != nil {
		return &KeyVerificationError{Key: id, Reason: "read sealed key", Err: err}
	}
	if got.PublicKey != legacyKey.PublicKey || got.Fingerprint != legacyKey.Fingerprint {
		return &KeyVerificationError{Key: id, Reason: "sealed public key differs from the legacy public key"}
	}

	legacyPub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(legacyKey.PublicKey))
	if err != nil {
		return &KeyVerificationError{Key: id, Reason: "parse legacy public key", Err: err}
	}
	signer, err := sealed.Signer(ctx, req)
	if err != nil {
		return &KeyVerificationError{Key: id, Reason: "open sealed signer", Err: err}
	}
	challenge := make([]byte, challengeSize)
	if _, err := rand.Read(challenge); err != nil {
		return &KeyVerificationError{Key: id, Reason: "generate challenge", Err: err}
	}
	sig, err := signer.Sign(rand.Reader, challenge)
	if err != nil {
		return &KeyVerificationError{Key: id, Reason: "sign challenge with the sealed key", Err: err}
	}
	if err := legacyPub.Verify(challenge, sig); err != nil {
		return &KeyVerificationError{Key: id, Reason: "sealed signature does not verify against the legacy public key", Err: err}
	}
	return nil
}

// dirExists reports whether path is an existing directory. A path that exists
// but is not a directory is an *UnexpectedEntryError.
func dirExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, &LegacyStoreError{Op: "stat", Path: path, Err: err}
	}
	if !info.IsDir() {
		return false, &UnexpectedEntryError{Path: path, Mode: info.Mode().Type()}
	}
	return true, nil
}
