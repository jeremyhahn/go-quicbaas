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
	"errors"
	"strconv"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Store — tenant-owned prompt records, tenant-resolved
// =============================================================================
//
// TENANCY IS THE RESOLVER'S, NOT THE KEY'S. Every operation names its tenant
// and routes through qrdbsdk.TenantStoreResolver, which hands back a store
// already confined to that tenant; nothing is cached against a tenant id, so
// a tenant whose barrier is sealed fails here. A record is addressed by a
// digest of (project scope, set, name), so a read is one key; the record's
// own fields are compared on every read and write, so a digest collision is
// refused rather than served as somebody else's prompt.
//
// EVERY WRITE IS A NEW REVISION guarded by the version the caller read:
// edits, rollbacks, resets and reconciles alike. History is appended to and
// bounded, never rewritten.
//
// THE STORE DECIDES THE GUARD, NOT THE READ BEFORE IT. A create is a
// PutIfAbsent and an edit is a CompareAndSwap against the exact bytes read,
// both from qrdbsdk.AtomicKVStore, so two writers holding one version cannot
// both win on any node. A store without those verbs is refused, never
// emulated with a read and an unguarded put.

// recordKeyPrefix is the key namespace of prompt records within a tenant's
// store; a record's key appends its slot id.
const recordKeyPrefix = "prompt_record/"

// Audit vocabulary for prompt records. Events carry identifiers only, never
// a prompt's content, so the audit trail cannot become a second copy of it.
const (
	// AuditResource identifies a prompt record on the audit trail.
	AuditResource = "prompt"

	AuditActionCreate   = "create"
	AuditActionUpdate   = "update"
	AuditActionRollback = "rollback"
	AuditActionReset    = "reset"
	AuditActionDelete   = "delete"
)

// AuditEvent describes one write to a prompt record.
type AuditEvent struct {
	TenantID   string
	Resource   string
	ResourceID string
	Action     string
	Success    bool
}

// Auditor receives prompt-record writes. The server binds it to the platform
// audit logger. Implementations must not block the caller.
type Auditor interface {
	RecordPromptEvent(ctx context.Context, event AuditEvent)
}

// Stamp is who makes a write, why, and against which version.
type Stamp struct {
	// Author names who made the write: a user, or "build <commit>" for the
	// boot reconciler.
	Author string

	// Note says why.
	Note string

	// ExpectedVersion is the version the caller read; 0 creates the record.
	ExpectedVersion int64
}

// Change is a new body for a prompt.
type Change struct {
	Stamp

	// Content is the new body. It is validated against the prompt's
	// specification before anything is written.
	Content string

	// FollowParent marks the record as an unedited copy of the scope above.
	// Only seeding sets it; a person's edit leaves it false.
	FollowParent bool
}

// StoreOption configures a Store.
type StoreOption func(*Store)

// WithAuditor routes every write to auditor.
func WithAuditor(auditor Auditor) StoreOption {
	return func(s *Store) { s.auditor = auditor }
}

// WithRegistry validates against registry instead of the process registry.
func WithRegistry(registry *Registry) StoreOption {
	return func(s *Store) { s.registry = registry }
}

// Store reads and writes prompt records.
type Store struct {
	resolver qrdbsdk.TenantStoreResolver
	registry *Registry
	auditor  Auditor
	now      func() time.Time
}

// NewStore builds a store over the tenant resolver.
func NewStore(resolver qrdbsdk.TenantStoreResolver, opts ...StoreOption) (*Store, error) {
	if resolver == nil {
		return nil, &InvalidInputError{
			Field:   "resolver",
			Message: "the store confines every record to its tenant through the resolver and cannot run without one",
		}
	}
	s := &Store{resolver: resolver, registry: registry, now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Registry returns the registry the store validates against.
func (s *Store) Registry() *Registry { return s.registry }

// spec returns the specification a ref's body is held to.
func (s *Store) spec(ref Ref) (PromptSpec, error) {
	set, known := s.registry.Lookup(ref.Set)
	if !known {
		return PromptSpec{}, &UnknownSetError{Set: ref.Set}
	}
	spec, known := set.Spec().Prompt(ref.Name)
	if !known {
		return PromptSpec{}, &UnknownPromptError{Set: ref.Set, Prompt: ref.Name}
	}
	return spec, nil
}

// kv resolves the tenant's store.
func (s *Store) kv(ctx context.Context, op, tenantID string) (qrdbsdk.KVStore, error) {
	store, err := s.resolver.Resolve(ctx, tenantID)
	if err != nil {
		return nil, backendError(op+".resolve", err)
	}
	return store, nil
}

// guarded resolves the tenant's store and asserts it can decide a contested
// write, failing closed when it cannot.
func (s *Store) guarded(ctx context.Context, op, tenantID string) (qrdbsdk.AtomicKVStore, error) {
	store, err := s.kv(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	atomic, ok := store.(qrdbsdk.AtomicKVStore)
	if !ok {
		return nil, backendError(op+".resolve", ErrGuardedWriteUnsupported)
	}
	return atomic, nil
}

// recordKey is the key of ref's slot.
func recordKey(ref Ref) string {
	return recordKeyPrefix + strconv.FormatUint(ref.entityID(), 10)
}

// decodeRecord decodes a stored record. The slot id is derived, never
// stored, so the record read back carries it again.
func decodeRecord(raw []byte) (*Record, error) {
	record := &Record{}
	if err := json.Unmarshal(raw, record); err != nil {
		return nil, err
	}
	record.ID = record.Ref().entityID()
	return record, nil
}

// held reads the record in ref's slot and the exact bytes it was read from,
// which a guarded write compares against: nil when the slot is empty, a
// CollisionError when it holds another prompt.
func held(ctx context.Context, op string, store qrdbsdk.KVStore, ref Ref) (*Record, []byte, error) {
	raw, err := store.Get(ctx, recordKey(ref))
	if err != nil {
		return nil, nil, backendError(op, err)
	}
	if raw == nil {
		return nil, nil, nil
	}
	record, err := decodeRecord(raw)
	if err != nil {
		return nil, nil, backendError(op+".decode", err)
	}
	if !record.identifies(ref) {
		return nil, nil, &CollisionError{Ref: ref, Held: Ref{
			TenantID: ref.TenantID, ProjectID: record.ProjectID, Set: record.Set, Name: record.Name,
		}}
	}
	return record, raw, nil
}

// Get returns the record for ref.
func (s *Store) Get(ctx context.Context, ref Ref) (*Record, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.spec(ref); err != nil {
		return nil, err
	}
	store, err := s.kv(ctx, "Get", ref.TenantID)
	if err != nil {
		return nil, err
	}
	record, _, err := held(ctx, "Get", store, ref)
	if err != nil {
		var collision *CollisionError
		if errors.As(err, &collision) {
			// The slot holds another prompt; this one was never written.
			return nil, &NotFoundError{Ref: ref}
		}
		return nil, err
	}
	if record == nil {
		return nil, &NotFoundError{Ref: ref}
	}
	return record, nil
}

// List returns every record of set at one scope of a tenant: the tenant-wide
// records when projectID is empty, one project's otherwise. An empty set
// lists every set.
func (s *Store) List(ctx context.Context, tenantID, projectID string, set SetKind) ([]*Record, error) {
	if err := validateScope(tenantID, projectID); err != nil {
		return nil, err
	}
	store, err := s.kv(ctx, "List", tenantID)
	if err != nil {
		return nil, err
	}
	var out []*Record
	scanErr := store.Scan(ctx, recordKeyPrefix, func(_ string, raw []byte) error {
		record, err := decodeRecord(raw)
		if err != nil {
			return err
		}
		if record.ProjectID == projectID && (set == "" || record.Set == set) {
			out = append(out, record)
		}
		return nil
	})
	if scanErr != nil {
		return nil, backendError("List", scanErr)
	}
	return out, nil
}

// History returns a record's revisions, oldest first.
func (s *Store) History(ctx context.Context, ref Ref) ([]Revision, error) {
	record, err := s.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return append([]Revision(nil), record.Revisions...), nil
}

// Save writes a new body as a new revision. ExpectedVersion 0 creates the
// record; any other value must be the version the record carries now.
func (s *Store) Save(ctx context.Context, ref Ref, change Change) (*Record, error) {
	action := AuditActionUpdate
	if change.ExpectedVersion == 0 {
		action = AuditActionCreate
	}
	return s.write(ctx, "Save", action, ref, change.Stamp, func(spec PromptSpec, _ *Record) (Revision, error) {
		if err := Validate(ref.Set, spec, change.Content); err != nil {
			return Revision{}, err
		}
		return Revision{Content: change.Content, FollowParent: change.FollowParent}, nil
	})
}

// Rollback writes an earlier revision's body as a new revision. The result
// is an edit like any other: it no longer follows its parent.
func (s *Store) Rollback(ctx context.Context, ref Ref, toVersion int64, stamp Stamp) (*Record, error) {
	return s.write(ctx, "Rollback", AuditActionRollback, ref, stamp, func(spec PromptSpec, record *Record) (Revision, error) {
		if record == nil {
			return Revision{}, &NotFoundError{Ref: ref}
		}
		earlier, known := record.Revision(toVersion)
		if !known {
			return Revision{}, &UnknownRevisionError{Ref: ref, Version: toVersion}
		}
		// The specification may have changed since that revision was written.
		if err := Validate(ref.Set, spec, earlier.Content); err != nil {
			return Revision{}, err
		}
		return Revision{Content: earlier.Content}, nil
	})
}

// Reset returns a record to following its parent, as a new revision that
// keeps the current body. Resolution skips it from then on.
func (s *Store) Reset(ctx context.Context, ref Ref, stamp Stamp) (*Record, error) {
	return s.write(ctx, "Reset", AuditActionReset, ref, stamp, func(_ PromptSpec, record *Record) (Revision, error) {
		if record == nil {
			return Revision{}, &NotFoundError{Ref: ref}
		}
		return Revision{Content: record.Content, FollowParent: true}, nil
	})
}

// Delete removes the record for ref. It exists for the seeder to undo its
// own writes when tenant creation rolls back; history is otherwise never
// discarded. Deleting an absent record is not an error.
func (s *Store) Delete(ctx context.Context, ref Ref) (err error) {
	if err := ref.Validate(); err != nil {
		return err
	}
	defer func() { s.audit(ctx, ref, AuditActionDelete, err) }()
	store, err := s.kv(ctx, "Delete", ref.TenantID)
	if err != nil {
		return err
	}
	record, _, err := held(ctx, "Delete", store, ref)
	if err != nil {
		var collision *CollisionError
		if errors.As(err, &collision) {
			return nil
		}
		return err
	}
	if record == nil {
		return nil
	}
	if err := store.Delete(ctx, recordKey(ref)); err != nil {
		return backendError("Delete", err)
	}
	return nil
}

// revise builds the next revision from the spec and the record as read (nil
// when there is none).
type revise func(spec PromptSpec, record *Record) (Revision, error)

// write is every guarded write: read the slot, refuse a stale version, build
// the revision, append it, and save on the version that was read.
func (s *Store) write(ctx context.Context, op, action string, ref Ref, stamp Stamp, next revise) (record *Record, err error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if stamp.Author == "" {
		return nil, &InvalidInputError{Field: "author", Message: "every revision names who wrote it"}
	}
	if stamp.ExpectedVersion < 0 {
		return nil, &InvalidInputError{Field: "expected_version", Message: "must not be negative"}
	}
	spec, err := s.spec(ref)
	if err != nil {
		return nil, err
	}
	defer func() { s.audit(ctx, ref, action, err) }()
	store, err := s.guarded(ctx, op, ref.TenantID)
	if err != nil {
		return nil, err
	}
	current, raw, err := held(ctx, op, store, ref)
	if err != nil {
		return nil, err
	}
	switch {
	case current == nil && stamp.ExpectedVersion != 0 && op != "Save":
		// Nothing to roll back or reset.
		return nil, &NotFoundError{Ref: ref}
	case current == nil && stamp.ExpectedVersion != 0,
		current != nil && current.Version != stamp.ExpectedVersion:
		return nil, &VersionConflictError{Ref: ref, Expected: stamp.ExpectedVersion}
	}
	revision, err := next(spec, current)
	if err != nil {
		return nil, err
	}

	now := s.now()
	updated := &Record{
		ID: ref.entityID(), TenantID: ref.TenantID, ProjectID: ref.ProjectID, Set: ref.Set, Name: ref.Name,
		CreatedAt: now,
	}
	if current != nil {
		updated.CreatedAt = current.CreatedAt
		updated.Revisions = append(updated.Revisions, current.Revisions...)
	}
	revision.Version = stamp.ExpectedVersion + 1
	revision.ContentHash = Hash(revision.Content)
	revision.Author = stamp.Author
	revision.Note = stamp.Note
	revision.CreatedAt = now
	updated.Revisions = append(updated.Revisions, revision)
	if excess := len(updated.Revisions) - MaxRevisions; excess > 0 {
		updated.Revisions = append([]Revision(nil), updated.Revisions[excess:]...)
	}
	updated.Content = revision.Content
	updated.ContentHash = revision.ContentHash
	updated.FollowParent = revision.FollowParent
	updated.Version = revision.Version
	updated.UpdatedAt = now

	data, err := json.Marshal(updated)
	if err != nil {
		return nil, backendError(op+".encode", err)
	}
	// The store decides the guard: a create lands only in an empty slot, and
	// an edit only over the exact bytes read above, so of two writers that
	// read the same version exactly one is applied.
	var applied bool
	if current == nil {
		applied, err = store.PutIfAbsent(ctx, recordKey(ref), data)
	} else {
		applied, err = store.CompareAndSwap(ctx, recordKey(ref), raw, data)
	}
	if err != nil {
		return nil, backendError(op, err)
	}
	if !applied {
		return nil, &VersionConflictError{Ref: ref, Expected: stamp.ExpectedVersion}
	}
	return updated, nil
}

// audit records one write attempt, identifiers only.
func (s *Store) audit(ctx context.Context, ref Ref, action string, err error) {
	if s.auditor == nil {
		return
	}
	s.auditor.RecordPromptEvent(ctx, AuditEvent{
		TenantID:   ref.TenantID,
		Resource:   AuditResource,
		ResourceID: ref.scopeSegment() + "/" + string(ref.Set) + "/" + ref.Name,
		Action:     action,
		Success:    err == nil,
	})
}
