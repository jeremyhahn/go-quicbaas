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

	// creator and deleter are kv's guarded-write capabilities, asserted once at
	// construction. Nil when the backing store cannot decide a contested write,
	// in which case CreateProject refuses rather than probing and hoping.
	//
	// Both halves are needed. A name reservation is claimed with a guarded
	// create and released with a guarded delete, and an unguarded release can
	// remove a reservation another node legitimately took (go-qrdb#41).
	creator guardedCreator
	deleter guardedDeleter

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
	store := &projectStore{kv: kv, auditor: cfg.auditor}
	// Asserted once: the capabilities are static properties of the injected
	// store, so probing them per call would ask the same question forever.
	if creator, ok := kv.(guardedCreator); ok {
		store.creator = creator
	}
	if deleter, ok := kv.(guardedDeleter); ok {
		store.deleter = deleter
	}
	return store
}

// NewInMemoryProjectStore returns a memory-backed binding of the store returned
// by NewProjectStore — not a second implementation. It is equivalent to
// NewProjectStore(kvstore.NewMemory()) and exists for tests and embedded use
// that need no durability.
func NewInMemoryProjectStore() ProjectStore {
	// go-qrdb's memory KVStore rather than kvstore.NewMemory(): a name
	// reservation is a guarded create, and that adapter wraps a raw storage
	// engine with no conditional write to delegate to.
	return NewProjectStore(NewMemoryTaskKV())
}

// projectTenantContext stamps tenantID onto ctx when it names no tenant.
//
// THE SIXTH INSTANCE OF THE SAME DEFECT, and taskTenantContext's own comment
// predicted it: "doing it in the store rather than at each caller is what stops
// there being a sixth: a reader that forgets to stamp is invisible until it
// destroys something." Projects were counted as fixed in #634 — but that fix
// landed in pkg/service (projectStoreContext), so every caller that reaches the
// store WITHOUT going through the service was still unguarded.
//
// The caller that did: job recovery at daemon start. recoverOrchestrationJobs
// builds its context from context.Background(), resumeJob passes it straight
// down, and launchJob reads the project through loadJobProject THIRTY-FIVE LINES
// before it stamps the tenant on anything. The tenant-routed KV resolved the
// shared store, the project was not there, loadJobProject swallowed the
// not-found at Debug and returned nil, and the resumed SDLC job died on
// requireProject:
//
//	job <id> completed with error: sdlc: invalid project: the job carries no
//	project record; the worker's checkout is described from it
//
// Two jobs on the burn-in node, both resumed after a restart, both charged
// against their task's attempt budget for a fault that was entirely ours.
//
// A context that already names a tenant is left exactly as it is: which tenant a
// caller may act as is an admission decision made above this layer, and this
// helper must never redirect a read or a write into a tenant the caller was not
// admitted to.
func projectTenantContext(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" || TenantFromContext(ctx) != "" {
		return ctx
	}
	return ContextWithTenant(ctx, tenantID)
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
	if project != nil {
		ctx = projectTenantContext(ctx, project.TenantID)
	}
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

	if err := s.guarded(); err != nil {
		return err
	}

	unlockRecord := s.recordMu.lock(recordKey)
	defer unlockRecord()
	unlockName := s.nameMu.lock(nameScope)
	defer unlockName()

	now := time.Now().UTC()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now

	// Marshal before touching the KV so an unencodable project cannot leave
	// half a create behind.
	data, marshalErr := marshalProject(project)
	if marshalErr != nil {
		return marshalErr
	}

	// STEP 1 — THE RECORD FIRST, guarded: two projects must not share an ID.
	//
	// The order of these two writes is the whole correctness argument, so it is
	// stated here rather than left to be re-derived:
	//
	//	A NAME RESERVATION IS PUBLISHED ONLY AFTER ITS PROJECT'S RECORD EXISTS.
	//
	// Every writer upholds it — create by this ordering, rename because the
	// record it re-points already exists — and reserveName depends on it to
	// tell an abandoned reservation from a live one. See reserveName.
	stored, err := s.creator.PutIfAbsent(ctx, recordKey, data)
	if err != nil {
		return &ProjectStoreError{Message: "failed to write project: " + err.Error()}
	}
	if !stored {
		// The ID collided, not the name. Nothing to undo: the record under that
		// key belongs to the project that got there first, and this create
		// never reached the name. Reported with the same typed error the
		// earlier shapes used, so callers matching on it are unaffected.
		return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
	}

	// STEP 2 — the name is the uniqueness decision, and the store makes it.
	reserved, err := s.reserveName(ctx, nameKey, project)
	if err != nil {
		return s.discardRecord(ctx, recordKey, err)
	}
	if !reserved {
		return s.discardRecord(ctx, recordKey,
			&ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name})
	}

	// No confirmation read follows, and none is needed. Winning step 2 is
	// final because the only predicate that can take this reservation away is
	// "the holder has no record", and step 1 already wrote ours.
	return nil
}

// discardRecord removes the record this create wrote after the create lost the
// name, and returns cause.
//
// UNGUARDED, unlike releaseNameReservation, and for the opposite reason: a
// record key is won by exactly one create — the PutIfAbsent in step 1 decided
// that — so there is no second writer whose value this could remove. Nothing
// references the record either, because the name never named it.
func (s *projectStore) discardRecord(ctx context.Context, recordKey string, cause error) error {
	if err := s.kv.Delete(ctx, recordKey); err != nil {
		return &ProjectStoreError{
			Message: cause.Error() + " (record discard failed: " + err.Error() + ")",
		}
	}
	return cause
}

// guarded reports whether the backing store can decide a contested write.
//
// Checked by every path that claims or releases a name — create, rename and
// delete all do — so a store without the capability refuses up front instead of
// dereferencing a nil capability partway through a two-key sequence.
func (s *projectStore) guarded() error {
	if s.creator == nil || s.deleter == nil {
		return &ProjectStoreError{
			Message: "name uniqueness needs a store that can decide it; this one cannot guard a write",
		}
	}
	return nil
}

// reserveName claims a tenant-scoped project name, healing a reservation whose
// project no longer exists.
//
// HEALING FIRES ON ONE CONDITION ONLY: the reservation names a project whose
// RECORD IS ABSENT. THAT PREDICATE IS DECIDABLE ONLY BECAUSE OF THE WRITE
// ORDER create USES. A reservation is published after its project's record, so
// a live holder — whether it has returned or is still mid-create — always has
// a record. A holder with no record therefore cannot be live, and the single
// writer that produces that state is Delete, between removing the record and
// removing the name. Healing it hands back a name whose project is gone.
//
// THE ORDER IS LOAD-BEARING, NOT INCIDENTAL. Reserving first was the obvious
// shape — it makes a crash leave a healable reservation rather than an
// unreachable project — but it also opens a window in which a creator that has
// merely not written its record yet is INDISTINGUISHABLE from a crashed one,
// and this heal takes its name. Both nodes then write their own, uncontested
// record keys, and two projects answer to one name. No confirmation read
// closes that: a post-write re-read of the owner is not atomic with the healer,
// so the healer can land after it and steal a name a create has already
// returned success on. The window is removed rather than narrowed, and the
// invariant it leaves is exact:
//
//	A CREATOR THAT HAS NOT CRASHED CANNOT HAVE ITS RESERVATION TAKEN.
//
// What that costs, stated because it is real: a crash between the record write
// and the reservation leaves an ORPHAN RECORD — a project reachable by ID and
// by ListByTenant, absent from the name index, whose name some later create is
// free to take. That residue is inert. It refuses nobody a name, it cannot be
// reached by GetByName, and it is visible to an operator, where the orphan
// reservation it replaces was a correctness defect. The create path already
// leaked exactly this record when a crash landed mid-rollback, so this widens
// a residue the store already tolerated rather than introducing a new kind.
// Reaping it belongs with the rename residue below, and for the same reason: a
// create between its two writes has the same key state, so only liveness
// evidence separates them.
//
// IT DELIBERATELY DOES NOT HEAL A NAME-MISMATCH. A reservation N naming a live
// project P whose current name is M != N has exactly two causes, and they are
// INDISTINGUISHABLE: a rename toward N in flight (reservation written, record
// not yet updated) and a rename away from N that crashed before releasing the
// old reservation. In both, N names P, P.Name is M, and M's reservation also
// names P — byte-identical states, so no read sequence separates them. Healing
// on that predicate would kill a live rename about half the time it fired.
//
// The residue is therefore a crashed rename squatting its old name until
// something with independent LIVENESS EVIDENCE clears it. That is the same
// deferral the scheduler's firing mark already makes ("no prune horizon closes
// that without a liveness signal proving no contender is still in flight"), and
// it belongs to WP10's run-is-authority reaper: A RESERVATION WHOSE PROJECT IS
// LIVE UNDER A DIFFERENT NAME, WITH NO CREATE OR RENAME IN FLIGHT, IS AN
// INVARIANT VIOLATION AND IS WP10'S TO REAP. Stated here so WP10's implementer
// finds the case from the code rather than from the plan.
//
// The cost, stated because it is real: one name per crashed rename is refused
// until reaped. Bounded, visible — the caller gets a typed conflict naming the
// squatter — and it errs toward refusing a name rather than handing one
// project's name to another, which is the direction this whole family is chosen
// for. A guarded swap would dissolve it by letting the reservation carry
// pending/committed state (go-qrdb#56).
func (s *projectStore) reserveName(ctx context.Context, nameKey string, project *Project) (bool, error) {
	claimed, err := s.creator.PutIfAbsent(ctx, nameKey, []byte(project.ID))
	if err != nil {
		return false, &ProjectStoreError{Message: "failed to reserve project name: " + err.Error()}
	}
	if claimed {
		return true, nil
	}

	holder, err := s.nameOwner(ctx, nameKey)
	if err != nil {
		return false, err
	}
	if holder == "" || holder == project.ID {
		return holder == project.ID, nil
	}

	abandoned, err := s.reservationAbandoned(ctx, project.TenantID, holder)
	if err != nil {
		return false, err
	}
	if !abandoned {
		return false, nil
	}

	// Both halves guarded: the delete removes only the exact reservation this
	// caller observed, and the re-claim cannot overwrite whatever replaced it.
	removed, err := s.deleter.CompareAndDelete(ctx, nameKey, []byte(holder))
	if err != nil {
		return false, &ProjectStoreError{Message: "failed to clear an abandoned project name: " + err.Error()}
	}
	if !removed {
		return false, nil
	}
	claimed, err = s.creator.PutIfAbsent(ctx, nameKey, []byte(project.ID))
	if err != nil {
		return false, &ProjectStoreError{Message: "failed to reserve project name: " + err.Error()}
	}
	return claimed, nil
}

// reservationAbandoned reports whether a reservation names a project with no
// record.
//
// Under create's record-before-name ordering that is decidable evidence, not a
// guess: every live holder has a record by the time its reservation exists, so
// a holder without one is a project that has been deleted. See reserveName.
func (s *projectStore) reservationAbandoned(ctx context.Context, tenantID, holderID string) (bool, error) {
	holderKey, keyErr := projectRecordKey(tenantID, holderID)
	if keyErr != nil {
		// The reservation holds a value that is not even a legal key segment,
		// so nothing it names can be loaded.
		return true, nil
	}
	exists, err := s.kv.Exists(ctx, holderKey)
	if err != nil {
		return false, &ProjectStoreError{Message: "failed to probe a project name holder: " + err.Error()}
	}
	return !exists, nil
}

// releaseNameReservation gives back a reservation this caller made, and returns
// cause.
//
// GUARDED, because a read followed by a delete is half a primitive. An
// unguarded release can remove a reservation another node legitimately took in
// the interim, handing that node's name away while it believes it holds one
// (go-qrdb#41). CompareAndDelete releases only while the name still names this
// project; a release that arrives late does nothing, which is exactly when it
// must do nothing.
func (s *projectStore) releaseNameReservation(ctx context.Context, nameKey, projectID string, cause error) error {
	if _, err := s.deleter.CompareAndDelete(ctx, nameKey, []byte(projectID)); err != nil {
		return &ProjectStoreError{
			Message: cause.Error() + " (name reservation release failed: " + err.Error() + ")",
		}
	}
	return cause
}

// =============================================================================
// Get / GetByName
// =============================================================================

// Get retrieves a project by its ID within the given tenant.
func (s *projectStore) Get(ctx context.Context, tenantID, projectID string) (*Project, error) {
	ctx = projectTenantContext(ctx, tenantID)
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
	ctx = projectTenantContext(ctx, tenantID)
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
	if project != nil {
		ctx = projectTenantContext(ctx, project.TenantID)
	}
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

	if err := s.guarded(); err != nil {
		return err
	}

	unlockName := s.nameMu.lock(nameScope)
	defer unlockName()

	reserved, reserveErr := s.reserveName(ctx, newNameKey, project)
	if reserveErr != nil {
		return reserveErr
	}
	if !reserved {
		return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
	}
	if err := s.writeRecord(ctx, recordKey, project); err != nil {
		return s.releaseNameReservation(ctx, newNameKey, project.ID, err)
	}

	// The old reservation is only removed once the rename is durable, so a
	// failure above always leaves the project reachable under its old name.
	oldNameKey, err := projectNameIndexKey(project.TenantID, oldName)
	if err != nil {
		// A stored name that is not a valid key segment never had an index
		// entry, so there is nothing to remove.
		return nil
	}
	// GUARDED, for the reason releaseNameReservation gives: an unguarded delete
	// here releases whatever holds the old name, which after a slow rename may
	// be a DIFFERENT project that legitimately claimed it. CompareAndDelete
	// releases only this project's own hold.
	//
	// A crash between the record write above and this release leaves the old
	// name reserved for a project that no longer answers to it. That residue is
	// deliberately NOT auto-healed — see reserveName for why it cannot be, and
	// whose job it is.
	if _, err := s.deleter.CompareAndDelete(ctx, oldNameKey, []byte(project.ID)); err != nil {
		return &ProjectStoreError{Message: "failed to release previous project name: " + err.Error()}
	}
	return nil
}

// writeRecord marshals and stores a project record. It returns the concrete
// error type so callers can hand the failure straight to
// releaseNameReservation; callers must nil-check before returning it as an
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
	ctx = projectTenantContext(ctx, tenantID)
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

	// The name release below is guarded, so a store that cannot guard a write
	// is refused before the record is touched rather than halfway through.
	if err := s.guarded(); err != nil {
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

	// GUARDED, for the reason releaseNameReservation gives. Once the record is
	// gone a concurrent create correctly heals the reservation naming this
	// project and claims the name for its own; an unguarded delete here would
	// then remove that LIVE project's reservation (go-qrdb#41). A false result
	// means the name already belongs to someone else, which is a no-op success:
	// this project's record is gone either way.
	if _, err := s.deleter.CompareAndDelete(ctx, nameKey, []byte(projectID)); err != nil {
		return &ProjectStoreError{Message: "failed to release project name: " + err.Error()}
	}
	return nil
}

// =============================================================================
// ListByTenant
// =============================================================================

// ListByTenant returns every project owned by tenantID, in key order.
func (s *projectStore) ListByTenant(ctx context.Context, tenantID string) ([]*Project, error) {
	ctx = projectTenantContext(ctx, tenantID)
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
