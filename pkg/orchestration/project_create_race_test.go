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
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Node-backed project store (go-quicbaas#825)
// =============================================================================
//
// These tests run the project store over a REAL qrdb node, because the property
// under test is that the store, not this process, decides who owns a name. The
// memory store answers PutIfAbsent under a mutex, which is the arbitration the
// implementation already assumes; only the node's Raft-applied PutIfAbsent can
// show that the assumption holds where it ships.

// nodeProjectKV presents the shared node's KV servicer as the
// interfaces.KVStore the project store takes. It only translates shapes: every
// guarded verb goes straight to the servicer, which evaluates the condition in
// the state machine.
type nodeProjectKV struct {
	svc qrdbsdk.KVServicer
}

func newNodeProjectKV(t *testing.T) *nodeProjectKV {
	t.Helper()
	return &nodeProjectKV{svc: claimNode(t).Node.KVService()}
}

func (k *nodeProjectKV) Put(ctx context.Context, key string, value []byte) error {
	return k.svc.Put(ctx, key, value)
}

// Get reports a missing key as the not-found error the store matches on.
func (k *nodeProjectKV) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := k.svc.Get(ctx, key)
	if err != nil {
		var missing *qrdbsdk.KeyNotFoundError
		if errors.As(err, &missing) {
			return nil, &qerrors.QBError{Code: qerrors.ErrNotFound, Op: "kv.get"}
		}
		return nil, err
	}
	return data, nil
}

func (k *nodeProjectKV) Delete(ctx context.Context, key string) error {
	return k.svc.Delete(ctx, key)
}

// Scan visits matches in key order, which the servicer's map does not keep.
func (k *nodeProjectKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	pairs, err := k.svc.Scan(ctx, prefix)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := fn(key, pairs[key]); err != nil {
			return err
		}
	}
	return nil
}

func (k *nodeProjectKV) List(ctx context.Context, prefix string) ([]string, error) {
	return k.svc.List(ctx, prefix)
}

func (k *nodeProjectKV) Exists(ctx context.Context, key string) (bool, error) {
	_, err := k.Get(ctx, key)
	if qerrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (k *nodeProjectKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return k.svc.PutIfAbsent(ctx, key, value)
}

func (k *nodeProjectKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return k.svc.CompareAndDelete(ctx, key, expected)
}

// uniqueProjectTenant hands each test its own tenant, so tests sharing the node
// cannot contend for one another's names.
func uniqueProjectTenant() string {
	return uniqueTaskID("tenant-825")
}

// TestProjectStore_Create_NodeAdmitsOneWinnerPerName is the go-quicbaas#825
// regression: nine stores — one per simulated node, so no in-process lock is
// shared — are released together on one channel and each creates the same
// name under a distinct ID. Exactly one may win; every other must get the
// typed conflict and leave no record behind.
func TestProjectStore_Create_NodeAdmitsOneWinnerPerName(t *testing.T) {
	const nodes = 9

	ctx := context.Background()
	kv := newNodeProjectKV(t)
	tenantID := uniqueProjectTenant()

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make([]error, nodes)
	)
	for i := 0; i < nodes; i++ {
		wg.Add(1)
		go func(node int) {
			defer wg.Done()
			store := NewProjectStore(kv)
			<-start
			errs[node] = store.Create(ctx, newTestProject("id-"+strconv.Itoa(node), tenantID, "contested"))
		}(i)
	}
	close(start)
	wg.Wait()

	winner := -1
	for node, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("nodes %d and %d both created %q", winner, node, "contested")
			}
			winner = node
			continue
		}
		if !errors.Is(err, ErrProjectAlreadyExists) {
			t.Fatalf("node %d lost but got %T: %v, want ErrProjectAlreadyExists", node, err, err)
		}
	}
	if winner < 0 {
		t.Fatalf("no node created %q; errors: %v", "contested", errs)
	}

	got, err := NewProjectStore(kv).GetByName(ctx, tenantID, "contested")
	if err != nil {
		t.Fatalf("GetByName() error = %v", err)
	}
	if want := "id-" + strconv.Itoa(winner); got.ID != want {
		t.Fatalf("name resolves to %q, but %q won", got.ID, want)
	}

	// A loser's record must be withdrawn, or it is a project reachable by ID
	// that no name leads to.
	for node := range errs {
		if node == winner {
			continue
		}
		recordKey, keyErr := projectRecordKey(tenantID, "id-"+strconv.Itoa(node))
		if keyErr != nil {
			t.Fatalf("projectRecordKey() error = %v", keyErr)
		}
		exists, existsErr := kv.Exists(ctx, recordKey)
		if existsErr != nil {
			t.Fatalf("Exists(%s) error = %v", recordKey, existsErr)
		}
		if exists {
			t.Errorf("losing node %d left its record behind", node)
		}
	}
}

// TestProjectStore_Update_NodeReleasesTheNameWhenTheRecordWriteFails pins the
// rollback on the one path that reserves a name before writing a record: a
// rename. When the record write fails the new name must be released on the
// node, or it stays reserved for a project that never took it and every later
// create of that name is refused.
func TestProjectStore_Update_NodeReleasesTheNameWhenTheRecordWriteFails(t *testing.T) {
	ctx := context.Background()
	kv := newNodeProjectKV(t)
	tenantID := uniqueProjectTenant()

	if err := NewProjectStore(kv).Create(ctx, newTestProject("id-1", tenantID, "seed")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	recordKey, err := projectRecordKey(tenantID, "id-1")
	if err != nil {
		t.Fatalf("projectRecordKey() error = %v", err)
	}

	failing := newProjectFailingKV(kv, projectVerbPut, recordKey)
	err = NewProjectStore(failing).Update(ctx, newTestProject("id-1", tenantID, "renamed"))
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("Update() = %T: %v, want *ProjectStoreError", err, err)
	}

	nameKey, err := projectNameIndexKey(tenantID, "renamed")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	held, err := kv.Exists(ctx, nameKey)
	if err != nil {
		t.Fatalf("Exists(nameKey) error = %v", err)
	}
	if held {
		t.Fatal("the reservation outlived the failed record write")
	}

	// The released name is free to another project.
	if err := NewProjectStore(kv).Create(ctx, newTestProject("id-2", tenantID, "renamed")); err != nil {
		t.Fatalf("Create() of the released name error = %v", err)
	}
	seed, err := NewProjectStore(kv).GetByName(ctx, tenantID, "seed")
	if err != nil {
		t.Fatalf("original name must still resolve: %v", err)
	}
	if seed.ID != "id-1" {
		t.Errorf("seed resolves to %q, want id-1", seed.ID)
	}
}

// TestNodeProjectKV_GetReportsAMissingKeyAsNotFound guards the one translation
// the adapter makes: without it the store reads an absent name as a backend
// failure and refuses every first create.
func TestNodeProjectKV_GetReportsAMissingKeyAsNotFound(t *testing.T) {
	kv := newNodeProjectKV(t)
	_, err := kv.Get(context.Background(), "projects-825/"+uniqueProjectTenant()+"/missing")
	if !qerrors.IsNotFound(err) {
		t.Fatalf("Get(missing) = %T: %v, want a not-found error", err, err)
	}
}
