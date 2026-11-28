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

package orchestration

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const settleTenant = "tenant-settle"

// newSettleProjectStore returns a project store on a guarded memory KV, the
// KV itself, and a stored project.
func newSettleProjectStore(t *testing.T, kv qrdbsdk.AtomicKVStore) (*projectStore, *Project) {
	t.Helper()
	store, ok := NewProjectStore(kv).(*projectStore)
	if !ok {
		t.Fatal("NewProjectStore returned an unexpected type")
	}
	project := newTestProject("proj-settle", settleTenant, "settle-me")
	if err := store.Create(context.Background(), project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, project
}

func guardedMemoryKV(t *testing.T) qrdbsdk.AtomicKVStore {
	t.Helper()
	kv, ok := NewMemoryTaskKV().(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the memory KV takes no guarded writes")
	}
	return kv
}

func rawProjectRecord(t *testing.T, kv qrdbsdk.AtomicKVStore, projectID string) []byte {
	t.Helper()
	key, err := projectRecordKey(settleTenant, projectID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := kv.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestProjectStore_SettleDeleteOfAnAbsentProjectIsGone(t *testing.T) {
	store, _ := newSettleProjectStore(t, guardedMemoryKV(t))
	gone, err := store.SettleDelete(context.Background(), settleTenant, "proj-missing")
	if err != nil || !gone {
		t.Fatalf("SettleDelete = %v, %v; want true, nil", gone, err)
	}
}

// Settling re-stamps a present project, so a delete guarded on the bytes it
// read before the settle can no longer apply.
func TestProjectStore_SettleDeleteMakesAPendingDeleteUnableToApply(t *testing.T) {
	ctx := context.Background()
	kv := guardedMemoryKV(t)
	store, project := newSettleProjectStore(t, kv)
	before := rawProjectRecord(t, kv, project.ID)

	gone, err := store.SettleDelete(ctx, settleTenant, project.ID)
	if err != nil || gone {
		t.Fatalf("SettleDelete = %v, %v; want false, nil", gone, err)
	}
	after := rawProjectRecord(t, kv, project.ID)
	if bytes.Equal(before, after) {
		t.Fatal("SettleDelete left the record bytes unchanged")
	}
	key, err := projectRecordKey(settleTenant, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := kv.CompareAndDelete(ctx, key, before)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("a delete guarded on the pre-settle bytes applied")
	}
	got, err := store.Get(ctx, settleTenant, project.ID)
	if err != nil {
		t.Fatalf("Get after the settle: %v", err)
	}
	if got.Name != project.Name || !got.UpdatedAt.After(project.UpdatedAt) {
		t.Fatalf("project after the settle = %+v, want the same project re-stamped", got)
	}
}

// changeBeforeDeleteKV rewrites a project record just before the first
// guarded delete of it, as a concurrent update would.
type changeBeforeDeleteKV struct {
	qrdbsdk.AtomicKVStore

	mu      sync.Mutex
	changed bool
}

func (k *changeBeforeDeleteKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	k.mu.Lock()
	change := !k.changed && strings.HasPrefix(string(expected), "{")
	k.changed = k.changed || change
	k.mu.Unlock()
	if change {
		rewritten := append(bytes.TrimSuffix(append([]byte(nil), expected...), []byte("}")), []byte(`,"x":1}`)...)
		if err := k.AtomicKVStore.Put(ctx, key, rewritten); err != nil {
			return false, err
		}
	}
	return k.AtomicKVStore.CompareAndDelete(ctx, key, expected)
}

func TestProjectStore_DeleteRefusesARecordChangedAfterItsRead(t *testing.T) {
	ctx := context.Background()
	inner := guardedMemoryKV(t)
	kv := &changeBeforeDeleteKV{AtomicKVStore: inner}
	store, project := newSettleProjectStore(t, kv)

	err := store.Delete(ctx, settleTenant, project.ID)
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("Delete = %v, want a ProjectStoreError", err)
	}
	if rawProjectRecord(t, inner, project.ID) == nil {
		t.Fatal("the changed record was deleted")
	}
	nameKey, err := projectNameIndexKey(settleTenant, project.Name)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := inner.Get(ctx, nameKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(owner) != project.ID {
		t.Fatalf("name reservation = %q, want it still held by %q", owner, project.ID)
	}
}
