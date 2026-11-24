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
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	stderrors "errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-xkms/pkg/storage"
	"github.com/jeremyhahn/go-xkms/pkg/validation"
	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

// ErrKeyNameTaken is returned when a key would be created under a go-xkms
// name another service key ID already holds.
var ErrKeyNameTaken = stderrors.New("go-xkms key name is held by another key ID")

// errNoKeyNameStore fails every lookup of a backend with no ownership store.
var errNoKeyNameStore = stderrors.New("no go-xkms key name store")

// ErrKeyNameBusy is the sentinel KeyNameBusyError unwraps to. It is
// retryable: another request holds the name only while it creates or deletes.
var ErrKeyNameBusy = stderrors.New("go-xkms key name is being created or deleted by another request; retry")

// KeyNameBusyError reports a go-xkms key name whose lease another request,
// on this node or another, holds. The call never waits for it.
type KeyNameBusyError struct {
	Name string
}

func (e *KeyNameBusyError) Error() string {
	return "go-xkms key name " + e.Name + " is being created or deleted by another request; retry"
}

// Unwrap returns ErrKeyNameBusy.
func (e *KeyNameBusyError) Unwrap() error { return ErrKeyNameBusy }

// ErrKeyNameStoreUnguarded is the sentinel KeyNameStoreUnguardedError
// unwraps to.
var ErrKeyNameStoreUnguarded = stderrors.New("go-xkms key name store has no guarded delete")

// KeyNameStoreUnguardedError reports a key-name store without
// CompareAndDelete. Such a store cannot arbitrate a name across nodes, so
// every claim and release on it fails closed.
type KeyNameStoreUnguardedError struct {
	Backend string
}

func (e *KeyNameStoreUnguardedError) Error() string {
	return "go-xkms key name store of backend " + e.Backend +
		" has no guarded delete (CompareAndDelete), so key names cannot be arbitrated across nodes"
}

// Unwrap returns ErrKeyNameStoreUnguarded.
func (e *KeyNameStoreUnguardedError) Unwrap() error { return ErrKeyNameStoreUnguarded }

// keyNameCompareAndDeleter is the guarded delete the shared key-name store
// must offer; a store without it cannot arbitrate across nodes.
type keyNameCompareAndDeleter interface {
	CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
}

// xkmsNameLeaseTTL bounds how long a name's lease outlives a holder that
// stopped without releasing it; a later request takes an expired lease over.
const xkmsNameLeaseTTL = 2 * time.Minute

// xkmsEncodedKeyNamePrefix starts every go-xkms key name that carries an
// encoded service key ID.
const xkmsEncodedKeyNamePrefix = "qbx."

// xkmsEncoding is strict so that each encoded payload has exactly one
// spelling.
var xkmsEncoding = base64.RawURLEncoding.Strict()

// xkmsNameOwnerPrefix namespaces the ownership records in their store.
const xkmsNameOwnerPrefix = "owners/"

// xkmsKeyName maps a service key ID to the key name go-xkms stores it under.
// A name go-xkms accepts is used unchanged: that is the convention every key
// created before encoding existed was stored under, so an existing key keeps
// its name. Any other ID (a tenant's key is "tenant/<tenantID>/<name>",
// which go-xkms refuses) is encoded as the prefix plus its canonical
// unpadded base64url form; go-xkms's own validation is not loosened.
//
// An encoded name is also a name go-xkms accepts, so an existing key may
// already be stored under it literally. The name alone is therefore never
// trusted: xkmsNameOwners records, persistently, which service key ID owns
// each encoded name, and the backend resolves an encoded name only through
// that record.
func xkmsKeyName(keyID string) string {
	// An empty ID is refused before any go-xkms call (ErrKeyCNEmpty).
	if keyID == "" || validation.ValidateKeyID(keyID) == nil {
		return keyID
	}
	return xkmsEncodedKeyNamePrefix + xkmsEncoding.EncodeToString([]byte(keyID))
}

// xkmsNameOwners is the persisted mapping from each encoded go-xkms key name
// to the service key ID that owns it. Only encoded names have a record; a
// name without one is a literal key ID, whatever it looks like, so a key
// stored before the mapping existed needs no migration: it has no record and
// stays its own ID.
//
// Records are per go-xkms backend: each backend has its own key namespace, so
// a name one backend's tenant key owns may be an existing literal key in
// another.
//
// The store is shared by every node that shares the KMS backend (production
// keeps it in the platform shard's replicated keyspace), so the per-instance
// lock alone cannot serialize a name: claim and release also hold a lease on
// the name in the store, and every record write and delete is guarded
// (PutIfAbsent, CompareAndDelete). A store without a guarded delete fails
// closed.
type xkmsNameOwners struct {
	mu      sync.Mutex
	backend string
	store   kctypes.StorageBackend
}

// recordKey is the store key of name's ownership record.
func (o *xkmsNameOwners) recordKey(name string) string {
	return xkmsNameOwnerPrefix + o.backend + "/" + name
}

// owner returns the service key ID recorded for name.
func (o *xkmsNameOwners) owner(ctx context.Context, name string) (string, bool, error) {
	if o.store == nil {
		return "", false, errNoKeyNameStore
	}
	b, err := o.store.Get(ctx, o.recordKey(name))
	if stderrors.Is(err, storage.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

// resolve returns the go-xkms name of an existing key ID. It fails with
// ErrKeyNotFound when the name belongs to another ID: an encoded name with no
// record for keyID (it may be an existing literal key), or a literal ID
// spelled like an encoded name that a tenant key owns.
func (o *xkmsNameOwners) resolve(ctx context.Context, keyID string) (string, error) {
	name := xkmsKeyName(keyID)
	owner, found, err := o.owner(ctx, name)
	if err != nil {
		return "", err
	}
	if name == keyID {
		if found {
			return "", ErrKeyNotFound
		}
		return name, nil
	}
	if !found || owner != keyID {
		return "", ErrKeyNotFound
	}
	return name, nil
}

// claim reserves the go-xkms name of keyID and creates its key under it, as
// one step. Two layers serialize it: the per-instance lock, and a lease on
// the name in the shared store that every node sharing the KMS backend takes
// before it reads the record. Reservation, creation and the rollback of a
// failed creation all run under both, so a failed request on one node can
// never drop a record another node's successful creation relies on. A name
// another request holds the lease of fails fast with *KeyNameBusyError.
//
// exists reports, authoritatively, whether go-xkms already holds a key under
// a name; its error fails the claim, as an inconclusive lookup is never taken
// for absence. An encoded name go-xkms holds without a record is an existing
// literal key, and claiming it would make keyID an alias of that key, so
// claim refuses it with ErrKeyNameTaken. The record is written with
// PutIfAbsent before create runs, and is dropped again, by a delete guarded
// on keyID, only when this call wrote it and create failed.
func (o *xkmsNameOwners) claim(ctx context.Context, keyID string, exists func(name string) (bool, error), create func(name string) error) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	name := xkmsKeyName(keyID)
	unlock, err := o.lease(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()

	owner, found, err := o.owner(ctx, name)
	if err != nil {
		return err
	}
	if name == keyID {
		if found {
			return ErrKeyNameTaken
		}
		return create(name)
	}
	if found {
		if owner != keyID {
			return ErrKeyNameTaken
		}
		return create(name)
	}
	held, err := exists(name)
	if err != nil {
		return err
	}
	if held {
		return ErrKeyNameTaken
	}
	stored, err := o.store.PutIfAbsent(ctx, o.recordKey(name), []byte(keyID))
	if err != nil {
		return err
	}
	if !stored {
		// The lease is held, so a record that appeared since the read above was
		// written outside the protocol; it is never overwritten.
		return ErrKeyNameTaken
	}
	if err := create(name); err != nil {
		_ = o.dropLocked(ctx, keyID, name)
		return err
	}
	return nil
}

// release deletes keyID's key with del and then drops the ownership record of
// its encoded name. Both run under the same lock and shared lease claim
// takes, so a create of the same key ID on any node either finishes before
// the delete starts or starts after the record is gone: the delete never
// drops the record a recreated key relies on.
func (o *xkmsNameOwners) release(ctx context.Context, keyID string, del func() error) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	name := xkmsKeyName(keyID)
	unlock, err := o.lease(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()

	if err := del(); err != nil {
		return err
	}
	if name == keyID {
		return nil
	}
	return o.dropLocked(ctx, keyID, name)
}

// dropLocked deletes name's record only while it still names keyID: the
// delete is guarded on the stored value, so it never removes a record that
// names another owner. o.mu and name's lease are held.
func (o *xkmsNameOwners) dropLocked(ctx context.Context, keyID, name string) error {
	guarded, ok := o.store.(keyNameCompareAndDeleter)
	if !ok {
		return &KeyNameStoreUnguardedError{Backend: o.backend}
	}
	_, err := guarded.CompareAndDelete(ctx, o.recordKey(name), []byte(keyID))
	return err
}

// leaseKey is the store key of name's cross-node lease.
func (o *xkmsNameOwners) leaseKey(name string) string {
	return xkmsNameOwnerPrefix + "lease/" + o.backend + "/" + name
}

// lease takes name's lease in the shared store and returns the function that
// releases it. The lease is written with PutIfAbsent as "<token>|<deadline>"
// and released by a delete guarded on that exact value, so a holder never
// frees a lease it no longer holds. A live lease held by another request
// fails with *KeyNameBusyError at once; an expired one is taken over by a
// guarded delete of the observed value and one more PutIfAbsent, and losing
// either step is busy too.
func (o *xkmsNameOwners) lease(ctx context.Context, name string) (func(), error) {
	if o.store == nil {
		return nil, errNoKeyNameStore
	}
	guarded, ok := o.store.(keyNameCompareAndDeleter)
	if !ok {
		return nil, &KeyNameStoreUnguardedError{Backend: o.backend}
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	key := o.leaseKey(name)
	value := []byte(hex.EncodeToString(token) + "|" +
		strconv.FormatInt(time.Now().Add(xkmsNameLeaseTTL).UnixNano(), 10))

	taken, err := o.store.PutIfAbsent(ctx, key, value)
	if err != nil {
		return nil, err
	}
	if !taken {
		held, err := o.store.Get(ctx, key)
		switch {
		case stderrors.Is(err, storage.ErrNotFound):
			// Released between the two calls: one more attempt below.
		case err != nil:
			return nil, err
		case !xkmsNameLeaseExpired(held, time.Now()):
			return nil, &KeyNameBusyError{Name: name}
		default:
			removed, err := guarded.CompareAndDelete(ctx, key, held)
			if err != nil {
				return nil, err
			}
			if !removed {
				return nil, &KeyNameBusyError{Name: name}
			}
		}
		taken, err = o.store.PutIfAbsent(ctx, key, value)
		if err != nil {
			return nil, err
		}
		if !taken {
			return nil, &KeyNameBusyError{Name: name}
		}
	}
	return func() {
		// Best effort: a lease this fails to free expires after
		// xkmsNameLeaseTTL. A cancelled request still frees its lease.
		_, _ = guarded.CompareAndDelete(context.WithoutCancel(ctx), key, value)
	}, nil
}

// xkmsNameLeaseExpired reports whether a stored lease's deadline has passed.
// A value this protocol did not write has no deadline it can honour, and is
// treated as expired so it cannot block the name forever.
func xkmsNameLeaseExpired(value []byte, now time.Time) bool {
	_, deadline, ok := strings.Cut(string(value), "|")
	if !ok {
		return true
	}
	nanos, err := strconv.ParseInt(deadline, 10, 64)
	if err != nil {
		return true
	}
	return !now.Before(time.Unix(0, nanos))
}

// keyID returns the service key ID behind a go-xkms key name: the recorded
// owner of an encoded name, and the name itself for every other name.
func (o *xkmsNameOwners) keyID(ctx context.Context, name string) (string, error) {
	owner, found, err := o.owner(ctx, name)
	if err != nil {
		return "", err
	}
	if found {
		return owner, nil
	}
	return name, nil
}
