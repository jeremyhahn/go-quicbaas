// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// placementKeyspace holds one record per spec name:
// orch/placement/{tenant}/{name}. The record is a storedPlacement: a live spec,
// or the tombstone a withdraw leaves, so a name's generations never repeat.
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
	// that is no longer stored, returns *PlacementSpecStaleError. An edit of
	// a withdrawn name returns *PlacementNotFoundError. A create of a
	// withdrawn name continues that name's generations, so a stale client or
	// a consent grant of the withdrawn spec never matches the recreated one.
	DeclarePlacement(ctx context.Context, tenantID string, spec *PlacementSpec) (*PlacementSpec, error)

	// GetPlacement returns one spec, or *PlacementNotFoundError.
	GetPlacement(ctx context.Context, tenantID, name string) (*PlacementSpec, error)

	// ListPlacements returns the tenant's specs ordered by name.
	ListPlacements(ctx context.Context, tenantID string) ([]*PlacementSpec, error)

	// WithdrawPlacement withdraws a spec, or returns *PlacementNotFoundError.
	// It leaves a tombstone at the next generation rather than deleting the
	// record, so a name's generations never repeat: grants and stale clients
	// of a withdrawn spec never match a spec later created under its name.
	WithdrawPlacement(ctx context.Context, tenantID, name string) error
}

type placementStore struct {
	kv      interfaces.KVStore
	creator guardedCreator
	swapper guardedSwapper
}

// storedPlacement is the record kept under a spec's key. The embedded spec
// keeps the JSON flat, so records written before tombstones existed decode as
// live specs. Withdrawn marks the tombstone a withdraw leaves: its Generation
// is the name's last, and the next create continues from it.
type storedPlacement struct {
	PlacementSpec
	Withdrawn bool `json:"withdrawn,omitempty"`
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
	if _, ok := kv.(guardedDeleter); !ok {
		return nil, &PlacementStoreUnguardedError{Missing: "CompareAndDelete"}
	}
	return &placementStore{kv: kv, creator: creator, swapper: swapper}, nil
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
	encode := func(generation uint64) (*PlacementSpec, []byte, error) {
		next := spec.clone()
		next.TenantID = tenantID
		next.Generation = generation
		data, err := json.Marshal(next)
		if err != nil {
			return nil, nil, fmt.Errorf("placement spec %q: encode: %w", spec.Name, err)
		}
		return next, data, nil
	}

	if spec.Generation == 0 {
		return s.create(ctx, tenantID, spec.Name, key, encode)
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
	next, data, err := encode(spec.Generation + 1)
	if err != nil {
		return nil, err
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

// create stores a new spec under a name. A name never declared starts at
// generation 1; a withdrawn name continues past its tombstone's generation,
// so nothing that named the withdrawn spec matches the new one. A live record
// under the name, or a lost race for the tombstone, is
// *PlacementSpecStaleError.
func (s *placementStore) create(ctx context.Context, tenantID, name, key string, encode func(uint64) (*PlacementSpec, []byte, error)) (*PlacementSpec, error) {
	next, data, err := encode(1)
	if err != nil {
		return nil, err
	}
	stored, err := s.creator.PutIfAbsent(ctx, key, data)
	if err != nil {
		return nil, fmt.Errorf("placement spec %q: create: %w", name, err)
	}
	if stored {
		return next, nil
	}
	raw, record, err := s.readRecord(ctx, tenantID, name, key)
	if err != nil {
		if errors.Is(err, ErrPlacementNotFound) {
			// Deleted between the create and the read: report the race.
			return nil, &PlacementSpecStaleError{TenantID: tenantID, Name: name}
		}
		return nil, err
	}
	if !record.Withdrawn {
		return nil, &PlacementSpecStaleError{TenantID: tenantID, Name: name, Current: record.Generation}
	}
	next, data, err = encode(record.Generation + 1)
	if err != nil {
		return nil, err
	}
	swapped, err := s.swapper.CompareAndSwap(ctx, key, raw, data)
	if err != nil {
		return nil, fmt.Errorf("placement spec %q: create: %w", name, err)
	}
	if !swapped {
		return nil, s.stale(ctx, tenantID, name, key, 0)
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
	raw, record, err := s.readRecord(ctx, tenantID, name, key)
	if err != nil {
		return nil, nil, err
	}
	if record.Withdrawn {
		return nil, nil, &PlacementNotFoundError{TenantID: tenantID, Name: name}
	}
	return raw, &record.PlacementSpec, nil
}

// readRecord returns the stored bytes and the decoded record under key,
// a withdraw's tombstone included, or *PlacementNotFoundError when the key
// holds nothing.
func (s *placementStore) readRecord(ctx context.Context, tenantID, name, key string) ([]byte, *storedPlacement, error) {
	raw, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, nil, &PlacementNotFoundError{TenantID: tenantID, Name: name}
		}
		return nil, nil, fmt.Errorf("placement spec %q: read: %w", name, err)
	}
	if raw == nil {
		return nil, nil, &PlacementNotFoundError{TenantID: tenantID, Name: name}
	}
	var record storedPlacement
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, nil, fmt.Errorf("placement spec %q: decode: %w", name, err)
	}
	return raw, &record, nil
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
		var record storedPlacement
		if err := json.Unmarshal(value, &record); err != nil {
			return fmt.Errorf("placement spec at %q: decode: %w", key, err)
		}
		if record.Withdrawn {
			return nil
		}
		specs = append(specs, &record.PlacementSpec)
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
	raw, current, err := s.read(ctx, tenantID, name, key)
	if err != nil {
		return err
	}
	tombstone, err := json.Marshal(storedPlacement{
		PlacementSpec: PlacementSpec{TenantID: tenantID, Name: name, Generation: current.Generation + 1},
		Withdrawn:     true,
	})
	if err != nil {
		return fmt.Errorf("placement spec %q: encode tombstone: %w", name, err)
	}
	swapped, err := s.swapper.CompareAndSwap(ctx, key, raw, tombstone)
	if err != nil {
		return fmt.Errorf("placement spec %q: withdraw: %w", name, err)
	}
	if !swapped {
		// Another writer edited or withdrew it between the read and the
		// swap. Withdrawing what was then stored would withdraw an edit this
		// caller never saw; report the race instead.
		refusal := &PlacementSpecStaleError{TenantID: tenantID, Name: name}
		if _, latest, readErr := s.read(ctx, tenantID, name, key); readErr == nil {
			refusal.Current = latest.Generation
		} else if errors.Is(readErr, ErrPlacementNotFound) {
			return readErr
		}
		return refusal
	}
	return nil
}
