// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// placementKeyspace holds one record per spec: orch/placement/{tenant}/{name}.
const placementKeyspace = "orch/placement/"

// PlacementStore keeps the tenant-scoped placement specs.
//
// Every write is guarded by the stored bytes it was decided against, so the
// store is correct across control-plane replicas sharing one KV, not only
// within one process.
type PlacementStore interface {
	// DeclarePlacement creates (spec.Generation zero) or edits (the generation
	// the caller read) a spec, and returns what was stored, at the new
	// generation. A create of an existing name, or an edit of a generation
	// that is no longer stored, returns *PlacementSpecStaleError.
	DeclarePlacement(ctx context.Context, tenantID string, spec *PlacementSpec) (*PlacementSpec, error)

	// GetPlacement returns one spec, or *PlacementNotFoundError.
	GetPlacement(ctx context.Context, tenantID, name string) (*PlacementSpec, error)

	// ListPlacements returns the tenant's specs ordered by name.
	ListPlacements(ctx context.Context, tenantID string) ([]*PlacementSpec, error)

	// WithdrawPlacement deletes a spec, or returns *PlacementNotFoundError.
	WithdrawPlacement(ctx context.Context, tenantID, name string) error
}

type placementStore struct {
	kv      interfaces.KVStore
	creator guardedCreator
	swapper guardedSwapper
	deleter guardedDeleter
}

// NewPlacementStore returns a PlacementStore over kv. It refuses a kv that
// cannot decide a guarded create, edit and delete
// (*PlacementStoreUnguardedError): over a plain Put, two racing edits would
// both "succeed" and one would be lost.
func NewPlacementStore(kv interfaces.KVStore) (PlacementStore, error) {
	if kv == nil {
		return nil, &PlacementStoreUnguardedError{Missing: "a store"}
	}
	creator, ok := kv.(guardedCreator)
	if !ok {
		return nil, &PlacementStoreUnguardedError{Missing: "PutIfAbsent"}
	}
	swapper, ok := kv.(guardedSwapper)
	if !ok {
		return nil, &PlacementStoreUnguardedError{Missing: "CompareAndSwap"}
	}
	deleter, ok := kv.(guardedDeleter)
	if !ok {
		return nil, &PlacementStoreUnguardedError{Missing: "CompareAndDelete"}
	}
	return &placementStore{kv: kv, creator: creator, swapper: swapper, deleter: deleter}, nil
}

func placementKey(tenantID, name string) (string, error) {
	return entityKey(placementKeyspace, tenantID, "name", name)
}

func (s *placementStore) DeclarePlacement(ctx context.Context, tenantID string, spec *PlacementSpec) (*PlacementSpec, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.TenantID != "" && spec.TenantID != tenantID {
		return nil, &PlacementSpecInvalidError{
			Name: spec.Name, Field: "tenant_id",
			Reason: fmt.Sprintf("names tenant %q but is written in tenant %q", spec.TenantID, tenantID),
		}
	}
	key, err := placementKey(tenantID, spec.Name)
	if err != nil {
		return nil, err
	}
	next := spec.clone()
	next.TenantID = tenantID
	next.Generation = spec.Generation + 1
	data, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("placement spec %q: encode: %w", spec.Name, err)
	}

	if spec.Generation == 0 {
		stored, err := s.creator.PutIfAbsent(ctx, key, data)
		if err != nil {
			return nil, fmt.Errorf("placement spec %q: create: %w", spec.Name, err)
		}
		if !stored {
			return nil, s.stale(ctx, tenantID, spec.Name, key, 0)
		}
		return next, nil
	}

	raw, current, err := s.read(ctx, tenantID, spec.Name, key)
	if err != nil {
		return nil, err
	}
	if current.Generation != spec.Generation {
		return nil, &PlacementSpecStaleError{
			TenantID: tenantID, Name: spec.Name, Read: spec.Generation, Current: current.Generation,
		}
	}
	swapped, err := s.swapper.CompareAndSwap(ctx, key, raw, data)
	if err != nil {
		return nil, fmt.Errorf("placement spec %q: edit: %w", spec.Name, err)
	}
	if !swapped {
		return nil, s.stale(ctx, tenantID, spec.Name, key, spec.Generation)
	}
	return next, nil
}

// stale builds the refusal for a lost guarded write, naming the generation
// that won when it can still be read.
func (s *placementStore) stale(ctx context.Context, tenantID, name, key string, read uint64) error {
	refusal := &PlacementSpecStaleError{TenantID: tenantID, Name: name, Read: read}
	if _, current, err := s.read(ctx, tenantID, name, key); err == nil {
		refusal.Current = current.Generation
	}
	return refusal
}

// read returns the stored bytes, for a guarded write against them, and the
// decoded spec.
func (s *placementStore) read(ctx context.Context, tenantID, name, key string) ([]byte, *PlacementSpec, error) {
	raw, err := s.kv.Get(ctx, key)
	if err != nil {
		if isKVNotFound(err) {
			return nil, nil, &PlacementNotFoundError{TenantID: tenantID, Name: name}
		}
		return nil, nil, fmt.Errorf("placement spec %q: read: %w", name, err)
	}
	if raw == nil {
		return nil, nil, &PlacementNotFoundError{TenantID: tenantID, Name: name}
	}
	var spec PlacementSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, nil, fmt.Errorf("placement spec %q: decode: %w", name, err)
	}
	return raw, &spec, nil
}

func (s *placementStore) GetPlacement(ctx context.Context, tenantID, name string) (*PlacementSpec, error) {
	key, err := placementKey(tenantID, name)
	if err != nil {
		return nil, err
	}
	_, spec, err := s.read(ctx, tenantID, name, key)
	return spec, err
}

func (s *placementStore) ListPlacements(ctx context.Context, tenantID string) ([]*PlacementSpec, error) {
	prefix, err := tenantPrefix(placementKeyspace, tenantID)
	if err != nil {
		return nil, err
	}
	var specs []*PlacementSpec
	err = s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var spec PlacementSpec
		if err := json.Unmarshal(value, &spec); err != nil {
			return fmt.Errorf("placement spec at %q: decode: %w", key, err)
		}
		specs = append(specs, &spec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs, nil
}

func (s *placementStore) WithdrawPlacement(ctx context.Context, tenantID, name string) error {
	key, err := placementKey(tenantID, name)
	if err != nil {
		return err
	}
	raw, _, err := s.read(ctx, tenantID, name, key)
	if err != nil {
		return err
	}
	deleted, err := s.deleter.CompareAndDelete(ctx, key, raw)
	if err != nil {
		return fmt.Errorf("placement spec %q: withdraw: %w", name, err)
	}
	if !deleted {
		// Another writer edited or withdrew it between the read and the
		// delete. Withdrawing what was then stored would delete an edit this
		// caller never saw; report the race instead.
		refusal := &PlacementSpecStaleError{TenantID: tenantID, Name: name}
		if _, current, readErr := s.read(ctx, tenantID, name, key); readErr == nil {
			refusal.Current = current.Generation
		} else if errors.Is(readErr, ErrPlacementNotFound) {
			return readErr
		}
		return refusal
	}
	return nil
}
