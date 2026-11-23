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
	"encoding/base64"
	stderrors "errors"
	"sync"

	"github.com/jeremyhahn/go-xkms/pkg/storage"
	"github.com/jeremyhahn/go-xkms/pkg/validation"
	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

// ErrKeyNameTaken is returned when a key would be created under a go-xkms
// name another service key ID already holds.
var ErrKeyNameTaken = stderrors.New("go-xkms key name is held by another key ID")

// errNoKeyNameStore fails every lookup of a backend with no ownership store.
var errNoKeyNameStore = stderrors.New("no go-xkms key name store")

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
// one step: reservation, creation and the rollback of a failed creation all
// hold the same lock, so a failed request can never drop a record another
// request's successful creation relies on.
//
// exists reports, authoritatively, whether go-xkms already holds a key under
// a name; its error fails the claim, as an inconclusive lookup is never taken
// for absence. An encoded name go-xkms holds without a record is an existing
// literal key, and claiming it would make keyID an alias of that key, so
// claim refuses it with ErrKeyNameTaken. The record is written before create
// runs, and is dropped again only when this call wrote it and create failed.
func (o *xkmsNameOwners) claim(ctx context.Context, keyID string, exists func(name string) (bool, error), create func(name string) error) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	name := xkmsKeyName(keyID)
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
	if err := o.store.Put(ctx, o.recordKey(name), []byte(keyID)); err != nil {
		return err
	}
	if err := create(name); err != nil {
		_ = o.dropLocked(ctx, keyID, name)
		return err
	}
	return nil
}

// release deletes keyID's key with del and then drops the ownership record of
// its encoded name. Both run under the lock claim holds, so a concurrent
// create of the same key ID either finishes before the delete starts or
// starts after the record is gone: the delete never drops the record a
// recreated key relies on.
func (o *xkmsNameOwners) release(ctx context.Context, keyID string, del func() error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := del(); err != nil {
		return err
	}
	name := xkmsKeyName(keyID)
	if name == keyID {
		return nil
	}
	return o.dropLocked(ctx, keyID, name)
}

// dropLocked deletes name's record if keyID owns it; o.mu is held.
func (o *xkmsNameOwners) dropLocked(ctx context.Context, keyID, name string) error {
	owner, found, err := o.owner(ctx, name)
	if err != nil || !found || owner != keyID {
		return err
	}
	err = o.store.Delete(ctx, o.recordKey(name))
	if stderrors.Is(err, storage.ErrNotFound) {
		return nil
	}
	return err
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
