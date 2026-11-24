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

	"github.com/jeremyhahn/go-xkms/pkg/ssh/authorized"
	"github.com/jeremyhahn/go-xkms/pkg/ssh/keys"
	knownhosts "github.com/jeremyhahn/go-xkms/pkg/ssh/known_hosts"
	sshscope "github.com/jeremyhahn/go-xkms/pkg/ssh/scope"
	"github.com/jeremyhahn/go-xkms/pkg/storage/file"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-xkms/sdk/go/secure"
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
	// Within it, go-xkms's scope layout (pkg/ssh/scope) places a record at
	// ssh/{shared|users/<owner>|services/<owner>}/<kind>/<name>; a record's
	// kind is the segment just before its name.
	recordPrefix = sshscope.Feature + "/"

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
	// are copied into sealed byte for byte.
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
// The legacy files keep the JSON format the old go-xkms file store wrote,
// which the pinned go-xkms SSH stores no longer read, so each record is
// decoded here and written into sealed as a typed record through the go-xkms
// DAO stores, then read back. Each key must then prove that its sealed copy
// holds the same key pair: the public key and fingerprint must match, and a
// signature over a random challenge made by the sealed signer must verify
// against the legacy public key. The legacy tree is destroyed only after
// every record passes. Each file is overwritten with random bytes and synced
// before it is unlinked.
//
// Any failure leaves the legacy store in place and returns a typed error. The
// run is idempotent: a record already present in sealed with the same
// identity (a key's public key and fingerprint, a host pin's fingerprint and
// public key, an authorized-keys entry's line) is accepted, so a boot that
// failed part-way can simply retry. A record present with a different
// identity is a *RecordConflictError, and nothing is overwritten.
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

// decodedKey, decodedPin and decodedAuthorized pair a decoded legacy record
// with the legacy record path it came from.
type decodedKey struct {
	path string
	key  *legacyKey
}

type decodedPin struct {
	path string
	pin  *legacyPin
}

type decodedAuthorized struct {
	path  string
	entry *legacyAuthorized
}

// decodedStore is the legacy store decoded by kind. Records of no known kind
// are kept raw in other.
type decodedStore struct {
	keys       []decodedKey
	pins       []decodedPin
	authorized []decodedAuthorized
	other      []record
}

// wipe clears every private key PEM the decoded store holds.
func (d *decodedStore) wipe() {
	for _, k := range d.keys {
		clear(k.key.PEM)
	}
}

// migrate decodes every legacy record, writes it into the sealed stores and
// verifies it. The legacy store is closed before anything is written, so the
// caller can safely destroy its files once migrate returns.
func migrate(ctx context.Context, legacyDir string, sealed xkmssdk.StorageBackend, provider xkmsssh.BackendProvider, report *Report) error {
	records, err := readLegacy(ctx, legacyDir)
	defer func() {
		for _, r := range records {
			clear(r.value)
		}
	}()
	if err != nil {
		return err
	}

	decoded, err := decodeRecords(records, legacyDir)
	defer decoded.wipe()
	if err != nil {
		return err
	}

	daos, err := xkmssdk.NewBackendDAOFactory(sealed)
	if err != nil {
		return &SealedStoreError{Op: "open", Key: recordPrefix, Err: err}
	}
	stores, err := xkmsssh.NewStores(daos)
	if err != nil {
		return &SealedStoreError{Op: "open", Key: recordPrefix, Err: err}
	}

	for _, k := range decoded.keys {
		if err := copyKey(ctx, stores.Keys, k); err != nil {
			return err
		}
		report.Keys++
	}
	for _, p := range decoded.pins {
		if err := copyPin(ctx, stores.KnownHosts, p); err != nil {
			return err
		}
		report.KnownHosts++
	}
	for _, a := range decoded.authorized {
		if err := copyAuthorized(ctx, stores.Authorized, a); err != nil {
			return err
		}
		report.AuthorizedKeys++
	}
	for _, r := range decoded.other {
		if err := copyRecord(ctx, sealed, r); err != nil {
			return err
		}
		report.Other++
	}

	return verifyKeys(ctx, stores, provider, decoded.keys)
}

// readLegacy opens the legacy file store, reads every record under the SSH
// namespace and closes it again.
func readLegacy(ctx context.Context, legacyDir string) (records []record, err error) {
	backend, err := file.New(legacyDir)
	if err != nil {
		return nil, &LegacyStoreError{Op: "open", Path: legacyDir, Err: err}
	}
	defer func() {
		if cerr := backend.Close(); cerr != nil {
			err = errors.Join(err, &LegacyStoreError{Op: "close", Path: legacyDir, Err: cerr})
		}
	}()
	return readRecords(ctx, backend, legacyDir)
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

// recordKind classifies a legacy record path by the segment just before the
// record name: ssh/<scope keyspace>/<kind>/<name>. A path of no known kind
// returns the empty Kind.
func recordKind(path string) sshscope.Kind {
	parts := strings.Split(path, "/")
	if len(parts) < 3 {
		return ""
	}
	switch kind := sshscope.Kind(parts[len(parts)-2]); kind {
	case sshscope.KindKeys, sshscope.KindKnownHosts, sshscope.KindAuthorizedKeys:
		return kind
	default:
		return ""
	}
}

// decodeRecords decodes every legacy record by kind. Every key record must
// decode to a key, because a key that does not decode cannot be verified: the
// count is reported as an *UnparsedKeysError. A host pin or authorized-keys
// record that does not decode is a *LegacyStoreError with Op "decode".
func decodeRecords(records []record, legacyDir string) (*decodedStore, error) {
	decoded := &decodedStore{}
	keyRecords := 0
	for _, r := range records {
		switch recordKind(r.key) {
		case sshscope.KindKeys:
			keyRecords++
			key, err := decodeLegacyKey(r.value)
			if err != nil {
				continue
			}
			decoded.keys = append(decoded.keys, decodedKey{path: r.key, key: key})
		case sshscope.KindKnownHosts:
			pin, err := decodeLegacyPin(r.value)
			if err != nil {
				return decoded, &LegacyStoreError{Op: "decode", Path: filepath.Join(legacyDir, filepath.FromSlash(r.key)), Err: err}
			}
			decoded.pins = append(decoded.pins, decodedPin{path: r.key, pin: pin})
		case sshscope.KindAuthorizedKeys:
			entry, err := decodeLegacyAuthorized(r.value)
			if err != nil {
				return decoded, &LegacyStoreError{Op: "decode", Path: filepath.Join(legacyDir, filepath.FromSlash(r.key)), Err: err}
			}
			decoded.authorized = append(decoded.authorized, decodedAuthorized{path: r.key, entry: entry})
		default:
			decoded.other = append(decoded.other, r)
		}
	}
	if len(decoded.keys) != keyRecords {
		return decoded, &UnparsedKeysError{Records: keyRecords, Parsed: len(decoded.keys)}
	}
	return decoded, nil
}

// copyKey writes a legacy key into the sealed key store and reads it back. A
// key already present with the same public key and fingerprint is accepted,
// so a partial run can be retried; one present with a different key is a
// *RecordConflictError and is not overwritten.
func copyKey(ctx context.Context, store *keys.DAOStore, d decodedKey) error {
	lk := d.key
	existing, err := store.Get(ctx, lk.Scope, lk.Owner, lk.Name)
	if err == nil {
		defer existing.Destroy()
		if !sameKey(existing, lk) {
			return &RecordConflictError{Key: d.path}
		}
		return nil
	}
	var notFound *keys.KeyNotFoundError
	if !errors.As(err, &notFound) {
		return &SealedStoreError{Op: "exists", Key: d.path, Err: err}
	}

	key := &keys.Key{
		PublicKey:    lk.PublicKey,
		Algorithm:    keys.Algorithm(lk.Algorithm),
		Fingerprint:  lk.Fingerprint,
		Scope:        lk.Scope,
		Owner:        lk.Owner,
		Name:         lk.Name,
		CreatedAt:    lk.CreatedAt,
		Comment:      lk.Comment,
		BackendID:    lk.BackendID,
		BackendKeyID: lk.BackendKeyID,
	}
	defer key.Destroy()
	if lk.BackendID == "" {
		// NewSecret wipes the PEM it seals.
		material, err := secure.NewSecret(ctx, lk.PEM)
		if err != nil {
			return &SealedStoreError{Op: "seal", Key: d.path, Err: err}
		}
		key.Material = material
	}
	if err := store.Put(ctx, key); err != nil {
		var owned *keys.OwnerConflictError
		if errors.As(err, &owned) {
			return &RecordConflictError{Key: d.path}
		}
		return &SealedStoreError{Op: "put", Key: d.path, Err: err}
	}

	stored, err := store.Get(ctx, lk.Scope, lk.Owner, lk.Name)
	if err != nil {
		return &SealedStoreError{Op: "get", Key: d.path, Err: err}
	}
	defer stored.Destroy()
	if !sameKey(stored, lk) {
		return &RecordMismatchError{Key: d.path}
	}
	return nil
}

// sameKey reports whether a sealed key has the legacy key's identity.
func sameKey(got *keys.Key, lk *legacyKey) bool {
	return got.PublicKey != nil &&
		bytes.Equal(got.PublicKey.Marshal(), lk.PublicKey.Marshal()) &&
		got.Fingerprint == lk.Fingerprint
}

// copyPin writes a legacy host pin into the sealed known-hosts store and
// reads it back, with copyKey's retry and conflict rules.
func copyPin(ctx context.Context, store *knownhosts.DAOStore, d decodedPin) error {
	lp := d.pin
	existing, err := store.Get(ctx, lp.Scope, lp.Owner, lp.Host)
	if err == nil {
		if !samePin(existing, lp) {
			return &RecordConflictError{Key: d.path}
		}
		return nil
	}
	var notFound *knownhosts.NotFoundError
	if !errors.As(err, &notFound) {
		return &SealedStoreError{Op: "exists", Key: d.path, Err: err}
	}

	pin := &knownhosts.Pin{
		Host:        lp.Host,
		Fingerprint: lp.Fingerprint,
		Algorithm:   lp.Algorithm,
		Scope:       lp.Scope,
		Owner:       lp.Owner,
		AddedAt:     lp.AddedAt,
		Source:      lp.Source,
		PublicKey:   bytes.Clone(lp.PublicKey),
	}
	if err := store.Add(ctx, pin); err != nil {
		var dup *knownhosts.DuplicateError
		if errors.As(err, &dup) {
			return &RecordConflictError{Key: d.path}
		}
		return &SealedStoreError{Op: "put", Key: d.path, Err: err}
	}

	stored, err := store.Get(ctx, lp.Scope, lp.Owner, lp.Host)
	if err != nil {
		return &SealedStoreError{Op: "get", Key: d.path, Err: err}
	}
	if !samePin(stored, lp) {
		return &RecordMismatchError{Key: d.path}
	}
	return nil
}

// samePin reports whether a sealed pin has the legacy pin's identity.
func samePin(got *knownhosts.Pin, lp *legacyPin) bool {
	return got.Fingerprint == lp.Fingerprint && bytes.Equal(got.PublicKey, lp.PublicKey)
}

// copyAuthorized writes a legacy authorized-keys entry into the sealed
// authorized-keys store and reads it back, with copyKey's retry and conflict
// rules. Entries are compared by their authorized_keys line.
func copyAuthorized(ctx context.Context, store *authorized.DAOStore, d decodedAuthorized) error {
	la := d.entry
	want, err := authorized.ParseLine(la.Line)
	if err != nil {
		return &LegacyStoreError{Op: "decode", Path: d.path, Err: err}
	}
	wantLine, err := authorized.Emit(want)
	if err != nil {
		return &LegacyStoreError{Op: "decode", Path: d.path, Err: err}
	}

	existing, err := store.Get(ctx, la.Scope, la.Owner, la.Name)
	if err == nil {
		if emittedLine(existing) != wantLine {
			return &RecordConflictError{Key: d.path}
		}
		return nil
	}
	var notFound *authorized.NotFoundError
	if !errors.As(err, &notFound) {
		return &SealedStoreError{Op: "exists", Key: d.path, Err: err}
	}

	if err := store.Add(ctx, la.Scope, la.Owner, la.Name, want); err != nil {
		var dup *authorized.DuplicateError
		if errors.As(err, &dup) {
			return &RecordConflictError{Key: d.path}
		}
		return &SealedStoreError{Op: "put", Key: d.path, Err: err}
	}

	stored, err := store.Get(ctx, la.Scope, la.Owner, la.Name)
	if err != nil {
		return &SealedStoreError{Op: "get", Key: d.path, Err: err}
	}
	if emittedLine(stored) != wantLine {
		return &RecordMismatchError{Key: d.path}
	}
	return nil
}

// emittedLine is the authorized_keys line of a stored record, or "" when it
// has none.
func emittedLine(rec *authorized.Record) string {
	if rec == nil || rec.Entry == nil {
		return ""
	}
	line, err := authorized.Emit(rec.Entry)
	if err != nil {
		return ""
	}
	return line
}

// copyRecord writes a record of no known kind into sealed byte for byte and
// reads it back. A record already present with identical bytes is accepted,
// so a partial run can be retried.
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

// verifyKeys proves every legacy key is usable from the sealed stores.
func verifyKeys(ctx context.Context, stores *xkmsssh.Stores, provider xkmsssh.BackendProvider, legacyKeys []decodedKey) (err error) {
	if len(legacyKeys) == 0 {
		return nil
	}
	// Closing the verifier leaves the stores, and sealed, open.
	verifier, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Stores: stores, BackendProvider: provider})
	if err != nil {
		return &KeyVerificationError{Key: string(sshscope.KindKeys), Reason: "open sealed store", Err: err}
	}
	defer func() {
		if cerr := verifier.Close(); cerr != nil {
			err = errors.Join(err, &KeyVerificationError{Key: string(sshscope.KindKeys), Reason: "close sealed store", Err: cerr})
		}
	}()
	for _, d := range legacyKeys {
		legacyKey := &xkmsssh.Key{
			Scope:       string(d.key.Scope),
			Owner:       d.key.Owner,
			Name:        d.key.Name,
			PublicKey:   authorizedKeyLine(d.key.PublicKey),
			Fingerprint: d.key.Fingerprint,
		}
		if err := verifyKey(ctx, verifier.Keys(), legacyKey); err != nil {
			return err
		}
	}
	return nil
}

// authorizedKeyLine is pub in authorized_keys form, without a newline.
func authorizedKeyLine(pub gossh.PublicKey) string {
	return strings.TrimRight(string(gossh.MarshalAuthorizedKey(pub)), "\n")
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
