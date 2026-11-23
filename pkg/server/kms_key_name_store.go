package server

import (
	"context"
	"errors"
	"sync"

	xkmsstorage "github.com/jeremyhahn/go-xkms/pkg/storage"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// kmsKeyNameRecordPrefix namespaces the go-xkms key-name ownership records in
// the platform shard's replicated keyspace.
const kmsKeyNameRecordPrefix = "kms-key-names/"

// kmsKeyNameRouter is the store of go-xkms key-name ownership records the KMS
// backends share. KMS starts before the embedded datastore, so the router
// begins on a node-local store and, once the service has the datastore,
// routes every record to the platform shard's replicated keyspace
// (docs/SPEC.md: one embedded datastore, system records in the system
// tenant). A tenant key created on one node then resolves on every node that
// shares its KMS backend, which a node-local record cannot give.
//
// Records an earlier version wrote to the node-local store stay readable: a
// lookup that misses the replicated store falls back to the local one and
// copies the record it finds into the replicated store, so an upgraded
// cluster keeps every key it had.
type kmsKeyNameRouter struct {
	mu         sync.RWMutex
	local      xkmssdk.StorageBackend
	replicated xkmssdk.StorageBackend
}

var _ xkmssdk.StorageBackend = (*kmsKeyNameRouter)(nil)

func newKMSKeyNameRouter(local xkmssdk.StorageBackend) *kmsKeyNameRouter {
	return &kmsKeyNameRouter{local: local}
}

// setReplicated routes every record to replicated from now on.
func (r *kmsKeyNameRouter) setReplicated(replicated xkmssdk.StorageBackend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replicated = replicated
}

// stores returns the store records go to and, once that is the replicated
// one, the node-local store legacy records are read from.
func (r *kmsKeyNameRouter) stores() (primary, legacy xkmssdk.StorageBackend) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.replicated == nil {
		return r.local, nil
	}
	return r.replicated, r.local
}

func isKeyNameNotFound(err error) bool {
	return errors.Is(err, xkmsstorage.ErrNotFound) || xkmssdk.IsNotFound(err)
}

// adopt copies a legacy record into the replicated store unless that already
// holds one, which then wins.
func adoptKeyNameRecord(ctx context.Context, primary xkmssdk.StorageBackend, key string, value []byte) ([]byte, error) {
	stored, err := primary.PutIfAbsent(ctx, key, value)
	if err != nil {
		return nil, err
	}
	if stored {
		return value, nil
	}
	return primary.Get(ctx, key)
}

func (r *kmsKeyNameRouter) Get(ctx context.Context, key string) ([]byte, error) {
	primary, legacy := r.stores()
	value, err := primary.Get(ctx, key)
	if legacy == nil || !isKeyNameNotFound(err) {
		return value, err
	}
	old, lerr := legacy.Get(ctx, key)
	if lerr != nil {
		if isKeyNameNotFound(lerr) {
			return nil, err
		}
		return nil, lerr
	}
	return adoptKeyNameRecord(ctx, primary, key, old)
}

func (r *kmsKeyNameRouter) Put(ctx context.Context, key string, value []byte) error {
	primary, _ := r.stores()
	return primary.Put(ctx, key, value)
}

func (r *kmsKeyNameRouter) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	primary, legacy := r.stores()
	if legacy != nil {
		old, err := legacy.Get(ctx, key)
		switch {
		case err == nil:
			if _, err := adoptKeyNameRecord(ctx, primary, key, old); err != nil {
				return false, err
			}
			return false, nil
		case !isKeyNameNotFound(err):
			return false, err
		}
	}
	return primary.PutIfAbsent(ctx, key, value)
}

// Delete removes the record from the replicated store and any legacy copy,
// so a deleted record never resurfaces through the fallback.
func (r *kmsKeyNameRouter) Delete(ctx context.Context, key string) error {
	primary, legacy := r.stores()
	err := primary.Delete(ctx, key)
	if legacy == nil {
		return err
	}
	lerr := legacy.Delete(ctx, key)
	switch {
	case err != nil && !isKeyNameNotFound(err):
		return err
	case lerr != nil && !isKeyNameNotFound(lerr):
		return lerr
	case err == nil || lerr == nil:
		return nil
	}
	return err
}

func (r *kmsKeyNameRouter) List(ctx context.Context, prefix string) ([]string, error) {
	primary, legacy := r.stores()
	keys, err := primary.List(ctx, prefix)
	if err != nil || legacy == nil {
		return keys, err
	}
	old, err := legacy.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		seen[k] = struct{}{}
	}
	for _, k := range old {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (r *kmsKeyNameRouter) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	primary, legacy := r.stores()
	seen := map[string]struct{}{}
	if err := primary.Scan(ctx, prefix, func(key string, value []byte) error {
		seen[key] = struct{}{}
		return fn(key, value)
	}); err != nil || legacy == nil {
		return err
	}
	return legacy.Scan(ctx, prefix, func(key string, value []byte) error {
		if _, ok := seen[key]; ok {
			return nil
		}
		return fn(key, value)
	})
}

func (r *kmsKeyNameRouter) Exists(ctx context.Context, key string) (bool, error) {
	primary, legacy := r.stores()
	ok, err := primary.Exists(ctx, key)
	if err != nil || ok || legacy == nil {
		return ok, err
	}
	return legacy.Exists(ctx, key)
}

// Close closes the node-local store only: the replicated store is the
// datastore's, which the server closes itself.
func (r *kmsKeyNameRouter) Close() error {
	return r.local.Close()
}

// useReplicatedKMSKeyNames routes the KMS key-name ownership records to the
// platform shard's replicated keyspace once the service has its datastore.
func (s *Server) useReplicatedKMSKeyNames() error {
	if s.kmsKeyNames == nil || s.qrdbClient == nil {
		return nil
	}
	shardClient, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil {
		return err
	}
	replicated, err := xkmssdk.NewStoragePrefixBackend(xkmssdk.NewQRDBStorageBackend(shardClient.KV()), kmsKeyNameRecordPrefix)
	if err != nil {
		return err
	}
	s.kmsKeyNames.setReplicated(replicated)
	return nil
}
