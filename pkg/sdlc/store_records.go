package sdlc

import (
	"context"
	"encoding/json"
	"strconv"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// The store's version-guarded records — bindings, deliveries, train entries
// and train ledgers — are read and written through the tenant's KVStore
// directly rather than through a qrdb DAO.
//
// The DAO's conditional save evaluates its condition with a read and then
// writes in a separate step, so two writers that read the same version both
// pass the check and the later write silently replaces the earlier one. The
// guard is only a guard when the store decides it: a create lands only in an
// empty slot (PutIfAbsent), and an edit only over the exact bytes the writer
// read (CompareAndSwap).
//
// Records keep the layout the DAO wrote them in — JSON under
// "<entityType>/<id>" — so records written before the move read back
// unchanged, and MigrateBindingAgents still finds legacy bindings.

// recordKey is where the record of entityType with the given id lives.
func recordKey(entityType string, id uint64) string {
	return entityType + "/" + strconv.FormatUint(id, 10)
}

// tenantKV resolves the tenant's store.
func (s *Store) tenantKV(ctx context.Context, op, tenantID string) (qrdbsdk.KVStore, error) {
	store, err := s.resolver.Resolve(ctx, tenantID)
	if err != nil {
		return nil, backendError(op+".resolve", err)
	}
	return store, nil
}

// guardedKV resolves the tenant's store and refuses one that cannot decide a
// contested write, rather than emulating the guard with a read and a put.
func (s *Store) guardedKV(ctx context.Context, op, tenantID string) (qrdbsdk.AtomicKVStore, error) {
	store, err := s.tenantKV(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	atomic, ok := store.(qrdbsdk.AtomicKVStore)
	if !ok {
		return nil, backendError(op+".resolve", ErrGuardedWriteUnsupported)
	}
	return atomic, nil
}

// loadRecord reads the record under key, returning it with the bytes it was
// decoded from. An absent record is (nil, nil, nil).
func loadRecord[E any](ctx context.Context, op string, store qrdbsdk.KVStore, key string) (*E, []byte, error) {
	raw, err := store.Get(ctx, key)
	if err != nil {
		return nil, nil, backendError(op, err)
	}
	if raw == nil {
		return nil, nil, nil
	}
	record := new(E)
	if err := json.Unmarshal(raw, record); err != nil {
		return nil, nil, backendError(op+".decode", err)
	}
	return record, raw, nil
}

// storeRecord writes record under key only if the slot still holds held: a
// nil held creates the record only where none exists, and otherwise the
// record replaces exactly the bytes held. It reports whether the write was
// applied; false means another writer moved the slot first.
func storeRecord(ctx context.Context, op string, store qrdbsdk.AtomicKVStore, key string, held []byte, record any) (bool, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return false, backendError(op+".encode", err)
	}
	var applied bool
	if held == nil {
		applied, err = store.PutIfAbsent(ctx, key, data)
	} else {
		applied, err = store.CompareAndSwap(ctx, key, held, data)
	}
	if err != nil {
		return false, backendError(op, err)
	}
	return applied, nil
}

// replaceRecord writes the record build returns over whatever the slot holds.
// When another writer moves the slot first, it reads the slot again and
// rebuilds, so a value derived from the held record — its version, its
// creation time — is never derived from a record already replaced. Every
// lost round means another writer's write landed, so the loop ends.
func replaceRecord[E any](ctx context.Context, op string, store qrdbsdk.AtomicKVStore, key string, build func(held *E) any) error {
	for {
		held, raw, err := loadRecord[E](ctx, op, store, key)
		if err != nil {
			return err
		}
		applied, err := storeRecord(ctx, op, store, key, raw, build(held))
		if err != nil {
			return err
		}
		if applied {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return backendError(op, err)
		}
	}
}

// removeRecord deletes the record at key only while the store still holds
// the exact bytes read and accepted by owns, so a write landing between the
// read and the delete is never erased. A record that is absent, or that owns
// rejects (a digest collision), is left alone. It returns the record it read
// and whether that record changed underneath the delete; a record another
// writer removed in the meantime counts as deleted.
func removeRecord[E any](ctx context.Context, op string, store qrdbsdk.AtomicKVStore, key string, owns func(*E) bool) (*E, bool, error) {
	held, raw, err := loadRecord[E](ctx, op, store, key)
	if err != nil {
		return nil, false, err
	}
	if held == nil || !owns(held) {
		return nil, false, nil
	}
	deleted, err := store.CompareAndDelete(ctx, key, raw)
	if err != nil {
		return nil, false, backendError(op, err)
	}
	if deleted {
		return held, false, nil
	}
	now, _, err := loadRecord[E](ctx, op, store, key)
	if err != nil {
		return nil, false, err
	}
	return held, now != nil && owns(now), nil
}

// scanRecords decodes every record of entityType.
func scanRecords[E any](ctx context.Context, op string, store qrdbsdk.KVStore, entityType string) ([]*E, error) {
	var out []*E
	scanErr := store.Scan(ctx, entityType+"/", func(_ string, raw []byte) error {
		record := new(E)
		if err := json.Unmarshal(raw, record); err != nil {
			return err
		}
		out = append(out, record)
		return nil
	})
	if scanErr != nil {
		return nil, backendError(op, scanErr)
	}
	return out, nil
}
