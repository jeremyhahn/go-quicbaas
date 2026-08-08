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
	"encoding/json"
	"errors"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// ProjectStore — Interface
// =============================================================================

// ProjectStore defines the persistence contract for Project entities.
type ProjectStore interface {
	// Create persists a new project. Returns ProjectAlreadyExistsError if
	// a project with the same TenantID+Name already exists.
	Create(ctx context.Context, project *Project) error

	// Get retrieves a project by its ID within the given tenant.
	Get(ctx context.Context, tenantID, projectID string) (*Project, error)

	// GetByName retrieves a project by its name within the given tenant.
	GetByName(ctx context.Context, tenantID, name string) (*Project, error)

	// Update replaces the stored project. Returns ProjectNotFoundError if not found.
	Update(ctx context.Context, project *Project) error

	// Delete removes a project by ID. Returns ProjectNotFoundError if not found.
	Delete(ctx context.Context, tenantID, projectID string) error

	// ListByTenant returns all projects owned by the given tenant.
	ListByTenant(ctx context.Context, tenantID string) ([]*Project, error)
}

// =============================================================================
// Key-segment errors
// =============================================================================

// ProjectKeySegmentError reports an identifier the project store cannot use as
// a key segment.
//
// It bridges two contracts that must both hold. Callers of the store match the
// store's validation vocabulary (ErrProjectInvalid / *ProjectInvalidError,
// which pkg/service maps onto a client error), while the key layout in
// store_keys.go states the isolation rule that was broken
// (ErrInvalidKeySegment / *KeySegmentError). Unwrap returns both, so errors.Is
// and errors.As succeed against either contract without the store having to
// duplicate the validation itself.
type ProjectKeySegmentError struct {
	// Invalid restates the violation in the project store's vocabulary.
	Invalid *ProjectInvalidError

	// Segment is the underlying *KeySegmentError from the key layout.
	Segment error
}

// Error implements the error interface.
func (e *ProjectKeySegmentError) Error() string { return e.Invalid.Error() }

// Unwrap exposes both wrapped contracts to errors.Is and errors.As.
func (e *ProjectKeySegmentError) Unwrap() []error { return []error{e.Invalid, e.Segment} }

// projectKeyError converts a key-layout violation into the store's dual-contract
// error. Any other error passes through untouched.
func projectKeyError(err error) error {
	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		return err
	}
	return &ProjectKeySegmentError{
		Invalid: &ProjectInvalidError{Field: segment.Field, Message: segment.Message},
		Segment: err,
	}
}

// =============================================================================
// Key derivation
// =============================================================================

// projectRecordKey returns the key holding one project record:
// orch/project/{tenantID}/{projectID}.
func projectRecordKey(tenantID, projectID string) (string, error) {
	key, err := entityKey(projectKeyspace, tenantID, "id", projectID)
	if err != nil {
		return "", projectKeyError(err)
	}
	return key, nil
}

// projectNameIndexKey returns the key holding one name-index entry:
// orch/projectname/{tenantID}/{name} -> projectID.
//
// The project name is itself a key segment, so it is validated exactly like an
// identifier: a name that is empty or contains the separator is rejected rather
// than encoded. Names are user-visible labels chosen from the same character
// set as identifiers, so an escaping scheme would buy nothing but ambiguity.
func projectNameIndexKey(tenantID, name string) (string, error) {
	key, err := entityKey(projectNameKeyspace, tenantID, "name", name)
	if err != nil {
		return "", projectKeyError(err)
	}
	return key, nil
}

// projectNameScope returns the tenant's name-index prefix. It doubles as the
// lock name that serializes name reservations within a tenant.
func projectNameScope(tenantID string) (string, error) {
	prefix, err := tenantPrefix(projectNameKeyspace, tenantID)
	if err != nil {
		return "", projectKeyError(err)
	}
	return prefix, nil
}

// =============================================================================
// projectStore — the single KV-backed ProjectStore
// =============================================================================

// projectStore is the only ProjectStore implementation. It holds no copy of the
// data it serves: every method reads from, and writes to, the injected
// interfaces.KVStore. Durability, replication and encryption-at-rest are
// therefore properties of the injected store alone — an in-memory KV makes this
// a fast test double, and the server's qrdb-backed KV makes the very same code
// durable and replicated.
type projectStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// recordMu serializes the read-modify-write sequence on one project
	// record. Writes to different projects proceed in parallel.
	recordMu keyedMutex

	// nameMu serializes name-index reservations within one tenant, which is
	// the one resource two different projects contend for. It is always
	// acquired after recordMu and never held while acquiring recordMu, so the
	// two locks cannot deadlock.
	nameMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ ProjectStore = (*projectStore)(nil)

// NewProjectStore returns a ProjectStore backed by kv.
//
// The backing store decides the storage semantics: pass kvstore.NewMemory() for
// an in-process store, or the server's qrdb-backed KV for a durable, replicated
// one. There is no second implementation to keep in sync.
func NewProjectStore(kv interfaces.KVStore, opts ...StoreOption) ProjectStore {
	cfg := newStoreConfig(opts...)
	return &projectStore{kv: kv, auditor: cfg.auditor}
}

// NewInMemoryProjectStore returns a memory-backed binding of the store returned
// by NewProjectStore — not a second implementation. It is equivalent to
// NewProjectStore(kvstore.NewMemory()) and exists for tests and embedded use
// that need no durability.
func NewInMemoryProjectStore() ProjectStore {
	return NewProjectStore(kvstore.NewMemory())
}

// ready reports whether the store was constructed with a backing KV.
func (s *projectStore) ready() error {
	if s.kv == nil {
		return &ProjectStoreError{Message: "kv store must not be nil"}
	}
	return nil
}

// =============================================================================
// Create
// =============================================================================

// Create persists a new project and reserves its name within the tenant.
func (s *projectStore) Create(ctx context.Context, project *Project) error {
	var tenantID, projectID string
	if project != nil {
		tenantID, projectID = project.TenantID, project.ID
	}
	err := s.create(ctx, project)
	s.audit(ctx, AuditActionCreate, tenantID, projectID, err)
	return err
}

func (s *projectStore) create(ctx context.Context, project *Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if project == nil {
		return &ProjectInvalidError{Message: "project must not be nil"}
	}

	recordKey, err := projectRecordKey(project.TenantID, project.ID)
	if err != nil {
		return err
	}
	nameKey, err := projectNameIndexKey(project.TenantID, project.Name)
	if err != nil {
		return err
	}
	nameScope, err := projectNameScope(project.TenantID)
	if err != nil {
		return err
	}

	unlockRecord := s.recordMu.lock(recordKey)
	defer unlockRecord()
	unlockName := s.nameMu.lock(nameScope)
	defer unlockName()

	exists, err := s.kv.Exists(ctx, recordKey)
	if err != nil {
		return &ProjectStoreError{Message: "failed to probe project: " + err.Error()}
	}
	if exists {
		return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
	}

	owner, err := s.nameOwner(ctx, nameKey)
	if err != nil {
		return err
	}
	if owner != "" {
		return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
	}

	now := time.Now().UTC()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now

	// Marshal before touching the KV so an unencodable project cannot leave a
	// name reserved.
	data, marshalErr := marshalProject(project)
	if marshalErr != nil {
		return marshalErr
	}

	// Reserve the name before the record so a crash between the two writes
	// leaves an orphaned reservation rather than an unreachable project.
	if err := s.kv.Put(ctx, nameKey, []byte(project.ID)); err != nil {
		return &ProjectStoreError{Message: "failed to reserve project name: " + err.Error()}
	}
	if putErr := s.putRecord(ctx, recordKey, data); putErr != nil {
		return s.rollbackNameReservation(ctx, nameKey, putErr)
	}

	return nil
}

// rollbackNameReservation removes a name reservation whose record write failed
// so the name does not stay claimed by a project that was never stored. The
// original failure is reported either way.
func (s *projectStore) rollbackNameReservation(ctx context.Context, nameKey string, cause *ProjectStoreError) error {
	if err := s.kv.Delete(ctx, nameKey); err != nil {
		return &ProjectStoreError{
			Message: cause.Message + " (name index rollback failed: " + err.Error() + ")",
		}
	}
	return cause
}

// =============================================================================
// Get / GetByName
// =============================================================================

// Get retrieves a project by its ID within the given tenant.
func (s *projectStore) Get(ctx context.Context, tenantID, projectID string) (*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	recordKey, err := projectRecordKey(tenantID, projectID)
	if err != nil {
		return nil, err
	}
	return s.load(ctx, recordKey, tenantID, projectID)
}

// GetByName retrieves a project by its name within the given tenant.
func (s *projectStore) GetByName(ctx context.Context, tenantID, name string) (*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	nameKey, err := projectNameIndexKey(tenantID, name)
	if err != nil {
		return nil, err
	}

	projectID, err := s.nameOwner(ctx, nameKey)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, &ProjectStoreNotFoundError{TenantID: tenantID, Name: name}
	}

	recordKey, err := projectRecordKey(tenantID, projectID)
	if err != nil {
		return nil, err
	}
	project, err := s.load(ctx, recordKey, tenantID, projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			// The index outlived its record. Report the lookup the caller
			// actually made.
			return nil, &ProjectStoreNotFoundError{TenantID: tenantID, ProjectID: projectID, Name: name}
		}
		return nil, err
	}
	return project, nil
}

// load reads and decodes one project record.
func (s *projectStore) load(ctx context.Context, recordKey, tenantID, projectID string) (*Project, error) {
	data, err := s.kv.Get(ctx, recordKey)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &ProjectStoreNotFoundError{TenantID: tenantID, ProjectID: projectID}
		}
		return nil, &ProjectStoreError{Message: "failed to read project: " + err.Error()}
	}
	if data == nil {
		return nil, &ProjectStoreNotFoundError{TenantID: tenantID, ProjectID: projectID}
	}

	var project Project
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, &ProjectStoreError{Message: "failed to unmarshal project: " + err.Error()}
	}

	// Defence in depth. The key already scopes the read to tenantID, so a
	// record whose body names a different tenant means the key layout was
	// corrupted or bypassed. Report it as absent rather than returning another
	// tenant's data to this caller.
	if project.TenantID != tenantID {
		return nil, &ProjectStoreNotFoundError{TenantID: tenantID, ProjectID: projectID}
	}

	return &project, nil
}

// nameOwner returns the project ID holding a name reservation, or "" if the
// name is free.
func (s *projectStore) nameOwner(ctx context.Context, nameKey string) (string, error) {
	data, err := s.kv.Get(ctx, nameKey)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return "", nil
		}
		return "", &ProjectStoreError{Message: "failed to read project name index: " + err.Error()}
	}
	return string(data), nil
}

// =============================================================================
// Update
// =============================================================================

// Update replaces the stored project, re-pointing the name index on rename.
func (s *projectStore) Update(ctx context.Context, project *Project) error {
	var tenantID, projectID string
	if project != nil {
		tenantID, projectID = project.TenantID, project.ID
	}
	err := s.update(ctx, project)
	s.audit(ctx, AuditActionUpdate, tenantID, projectID, err)
	return err
}

func (s *projectStore) update(ctx context.Context, project *Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if project == nil {
		return &ProjectInvalidError{Message: "project must not be nil"}
	}

	recordKey, err := projectRecordKey(project.TenantID, project.ID)
	if err != nil {
		return err
	}

	unlockRecord := s.recordMu.lock(recordKey)
	defer unlockRecord()

	current, err := s.load(ctx, recordKey, project.TenantID, project.ID)
	if err != nil {
		return err
	}

	// An empty name means "leave the name alone". Adopting the stored name
	// keeps the record and its index entry in agreement.
	if project.Name == "" {
		project.Name = current.Name
	}
	if project.CreatedAt.IsZero() {
		project.CreatedAt = current.CreatedAt
	}
	project.UpdatedAt = time.Now().UTC()

	if project.Name == current.Name {
		if err := s.writeRecord(ctx, recordKey, project); err != nil {
			return err
		}
		return nil
	}
	return s.rename(ctx, recordKey, current.Name, project)
}

// rename re-points the tenant's name index at project while writing the new
// record body. The tenant's name scope is held throughout so no other project
// can claim the same name mid-sequence.
func (s *projectStore) rename(ctx context.Context, recordKey, oldName string, project *Project) error {
	newNameKey, err := projectNameIndexKey(project.TenantID, project.Name)
	if err != nil {
		return err
	}
	nameScope, err := projectNameScope(project.TenantID)
	if err != nil {
		return err
	}

	unlockName := s.nameMu.lock(nameScope)
	defer unlockName()

	owner, err := s.nameOwner(ctx, newNameKey)
	if err != nil {
		return err
	}
	if owner != "" && owner != project.ID {
		return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
	}

	if err := s.kv.Put(ctx, newNameKey, []byte(project.ID)); err != nil {
		return &ProjectStoreError{Message: "failed to reserve project name: " + err.Error()}
	}
	if err := s.writeRecord(ctx, recordKey, project); err != nil {
		return s.rollbackNameReservation(ctx, newNameKey, err)
	}

	// The old reservation is only removed once the rename is durable, so a
	// failure above always leaves the project reachable under its old name.
	oldNameKey, err := projectNameIndexKey(project.TenantID, oldName)
	if err != nil {
		// A stored name that is not a valid key segment never had an index
		// entry, so there is nothing to remove.
		return nil
	}
	if err := s.kv.Delete(ctx, oldNameKey); err != nil {
		return &ProjectStoreError{Message: "failed to release previous project name: " + err.Error()}
	}
	return nil
}

// writeRecord marshals and stores a project record. It returns the concrete
// error type so callers can hand the failure straight to
// rollbackNameReservation; callers must nil-check before returning it as an
// error interface.
func (s *projectStore) writeRecord(ctx context.Context, recordKey string, project *Project) *ProjectStoreError {
	data, err := marshalProject(project)
	if err != nil {
		return err
	}
	return s.putRecord(ctx, recordKey, data)
}

// marshalProject encodes a project record.
func marshalProject(project *Project) ([]byte, *ProjectStoreError) {
	data, err := json.Marshal(project)
	if err != nil {
		return nil, &ProjectStoreError{Message: "failed to marshal project: " + err.Error()}
	}
	return data, nil
}

// putRecord stores an encoded project record.
func (s *projectStore) putRecord(ctx context.Context, recordKey string, data []byte) *ProjectStoreError {
	if err := s.kv.Put(ctx, recordKey, data); err != nil {
		return &ProjectStoreError{Message: "failed to write project: " + err.Error()}
	}
	return nil
}

// =============================================================================
// Delete
// =============================================================================

// Delete removes a project and releases its name reservation.
func (s *projectStore) Delete(ctx context.Context, tenantID, projectID string) error {
	err := s.remove(ctx, tenantID, projectID)
	s.audit(ctx, AuditActionDelete, tenantID, projectID, err)
	return err
}

func (s *projectStore) remove(ctx context.Context, tenantID, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}

	recordKey, err := projectRecordKey(tenantID, projectID)
	if err != nil {
		return err
	}

	unlockRecord := s.recordMu.lock(recordKey)
	defer unlockRecord()

	current, err := s.load(ctx, recordKey, tenantID, projectID)
	if err != nil {
		return err
	}

	if err := s.kv.Delete(ctx, recordKey); err != nil {
		return &ProjectStoreError{Message: "failed to delete project: " + err.Error()}
	}

	// A leftover reservation would resolve to a missing record, which reads as
	// "not found"; a leftover record with no reservation would be invisible to
	// GetByName. The record is therefore removed first.
	nameKey, err := projectNameIndexKey(tenantID, current.Name)
	if err != nil {
		// A stored name that is not a valid key segment never had an index
		// entry, so there is nothing to remove.
		return nil
	}

	nameScope, err := projectNameScope(tenantID)
	if err != nil {
		return err
	}
	unlockName := s.nameMu.lock(nameScope)
	defer unlockName()

	if err := s.kv.Delete(ctx, nameKey); err != nil {
		return &ProjectStoreError{Message: "failed to release project name: " + err.Error()}
	}
	return nil
}

// =============================================================================
// ListByTenant
// =============================================================================

// ListByTenant returns every project owned by tenantID, in key order.
func (s *projectStore) ListByTenant(ctx context.Context, tenantID string) ([]*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}

	// The scan is bounded to this tenant's subtree, so no other tenant's
	// records are reachable from here.
	prefix, err := tenantPrefix(projectKeyspace, tenantID)
	if err != nil {
		return nil, projectKeyError(err)
	}

	var (
		projects  []*Project
		decodeErr error
	)
	scanErr := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		if err := ctx.Err(); err != nil {
			decodeErr = err
			return err
		}
		var project Project
		if err := json.Unmarshal(value, &project); err != nil {
			decodeErr = &ProjectStoreError{
				Message: "failed to unmarshal project " + idFromKey(key, prefix) + ": " + err.Error(),
			}
			return decodeErr
		}
		if project.TenantID != tenantID {
			// Defence in depth against a corrupted key: a record filed under
			// this tenant that claims another owner is not this tenant's to
			// see.
			return nil
		}
		projects = append(projects, &project)
		return nil
	})
	// A callback failure is already a typed store error; anything else came
	// from the KV itself.
	if decodeErr != nil {
		return nil, decodeErr
	}
	if scanErr != nil {
		return nil, &ProjectStoreError{Message: "failed to list projects: " + scanErr.Error()}
	}

	return projects, nil
}

// =============================================================================
// Audit
// =============================================================================

// audit emits exactly one store event per mutating call. A denied or failed
// attempt is recorded with Success false, which is the signal an auditor needs
// to show that a change was attempted and rejected.
func (s *projectStore) audit(ctx context.Context, action, tenantID, projectID string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceProject,
		ResourceID: projectID,
		Action:     action,
		Success:    err == nil,
	})
}
