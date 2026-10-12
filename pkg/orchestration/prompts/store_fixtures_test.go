// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package prompts

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const (
	testTenant  = "tenant-a"
	testProject = "proj-1"
	testSet     = SetKind("store_test")
	testCharter = "charter"
	testNote    = "note"
)

// memoryResolver confines each tenant to its own in-memory store and can be
// made to fail the way a sealed barrier does.
type memoryResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
	err    error
}

func newMemoryResolver() *memoryResolver {
	return &memoryResolver{stores: map[string]qrdbsdk.KVStore{}}
}

// Resolve implements qrdbsdk.TenantStoreResolver.
func (r *memoryResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

func (r *memoryResolver) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// recordingAuditor keeps every event it is handed.
type recordingAuditor struct {
	mu     sync.Mutex
	events []AuditEvent
}

// RecordPromptEvent implements Auditor.
func (a *recordingAuditor) RecordPromptEvent(_ context.Context, event AuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

func (a *recordingAuditor) recorded() []AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AuditEvent(nil), a.events...)
}

// storeTestSpec is a two-prompt set: one carrying a marker the engine would
// parse, one with nothing to keep.
func storeTestSpec() SetSpec {
	return SetSpec{
		Kind:        testSet,
		Description: "a set for store tests",
		Prompts: []PromptSpec{
			{
				Name:        testCharter,
				Description: "the worker's charter",
				Default:     "Work the issue. End with STOP: when done.",
				Markers:     []Marker{{Literal: "STOP:", Reason: "the runner parses STOP:"}},
			},
			{
				Name:        testNote,
				Description: "a note to the worker",
				Default:     "Mind ${topic}.",
				Variables:   []Variable{{Name: "topic", Description: "what to mind"}},
			},
		},
	}
}

// storeFixture is a store over memory stores with its own registry.
type storeFixture struct {
	store    *Store
	resolver *memoryResolver
	registry *Registry
	auditor  *recordingAuditor
}

func newStoreFixture(t *testing.T) *storeFixture {
	t.Helper()
	registry := NewRegistry()
	if _, err := registry.Register(storeTestSpec()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	resolver := newMemoryResolver()
	auditor := &recordingAuditor{}
	store, err := NewStore(resolver, WithRegistry(registry), WithAuditor(auditor))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return &storeFixture{store: store, resolver: resolver, registry: registry, auditor: auditor}
}

// plant writes record straight into ref's slot, bypassing every check the
// store makes, to stand for data the store's own writes cannot produce.
func (f *storeFixture) plant(t *testing.T, ref Ref, record *Record) {
	t.Helper()
	kv, err := f.resolver.Resolve(context.Background(), ref.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Put(context.Background(), recordKey(ref), data); err != nil {
		t.Fatalf("plant: %v", err)
	}
}

func systemTenant() string { return qrdbsdk.SystemTenantID }

func charterRef(tenantID, projectID string) Ref {
	return Ref{TenantID: tenantID, ProjectID: projectID, Set: testSet, Name: testCharter}
}

func edit(content string, expected int64) Change {
	return Change{Stamp: Stamp{Author: "alice", Note: "tune", ExpectedVersion: expected}, Content: content}
}
