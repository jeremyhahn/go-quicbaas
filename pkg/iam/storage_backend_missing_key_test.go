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

package iam

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// nilOnMissingKVStore answers a missing key with no value and no error, as
// the qrdb in-memory store does.
type nilOnMissingKVStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newNilOnMissingKVStore() *nilOnMissingKVStore {
	return &nilOnMissingKVStore{data: make(map[string][]byte)}
}

func (s *nilOnMissingKVStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = bytes.Clone(value)
	return nil
}

func (s *nilOnMissingKVStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return nil, nil
	}
	return bytes.Clone(v), nil
}

func (s *nilOnMissingKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *nilOnMissingKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	s.mu.Lock()
	snapshot := make(map[string][]byte)
	for k, v := range s.data {
		if strings.HasPrefix(k, prefix) {
			snapshot[k] = bytes.Clone(v)
		}
	}
	s.mu.Unlock()
	for k, v := range snapshot {
		if err := fn(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *nilOnMissingKVStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *nilOnMissingKVStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}

// A missing key reads as not found even when the store answers it with no
// value and no error.
func TestBackendStorage_GetMissingKeyIsNotFound(t *testing.T) {
	storage := NewBackendStorage(newNilOnMissingKVStore())

	_, err := storage.GetTenant(context.Background(), "absent")
	if err == nil {
		t.Fatal("GetTenant on an absent tenant succeeded")
	}
	if !dberrors.IsNotFound(err) {
		t.Fatalf("GetTenant on an absent tenant: got %v, want not found", err)
	}
}

// The first create on an empty store succeeds: its existence check does not
// read a missing key as a record.
func TestBackendStorage_FirstCreateOnEmptyStoreSucceeds(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newNilOnMissingKVStore())

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "t1"}); err != nil {
		t.Fatalf("CreateTenant on an empty store: %v", err)
	}
	got, err := storage.GetTenant(ctx, "t1")
	if err != nil || got.ID != "t1" {
		t.Fatalf("GetTenant after create: %v, %+v", err, got)
	}

	policy := &Policy{APIVersion: "v1", Kind: "Policy", Metadata: PolicyMetadata{Name: "first"}}
	if err := storage.CreatePolicy(ctx, "t1", policy); err != nil {
		t.Fatalf("CreatePolicy on an empty store: %v", err)
	}
	if err := storage.CreatePolicy(ctx, "t1", policy); err == nil {
		t.Fatal("a second CreatePolicy of the same name succeeded")
	}
}

// An empty, non-nil value is a stored value, not a missing key.
func TestBackendStorage_GetEmptyValueIsFound(t *testing.T) {
	ctx := context.Background()
	kv := newNilOnMissingKVStore()
	storage := NewBackendStorage(kv)
	if err := kv.Put(ctx, "k", []byte{}); err != nil {
		t.Fatal(err)
	}
	data, err := storage.get(ctx, "k")
	if err != nil || data == nil {
		t.Fatalf("get of an empty value: %v, %v", data, err)
	}
}
