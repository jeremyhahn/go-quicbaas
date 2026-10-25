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

package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// MemoryUserAuthFactors is an in-memory qrdbsdk.UserAuthFactorServicer that
// reproduces go-qrdb's index semantics exactly, so tests exercise the real
// enrollment/lookup contract instead of a simplified stand-in:
//
//   - A factor is indexed under (Type, hex(SHA-256(Credential))) — the same
//     transform go-qrdb pkg/service.lookupKeyFor applies — AND, when present,
//     under (Type, hex(SHA-256(SPKICredential))). Consequently a lookup only
//     succeeds when the login side supplies a credential blob BYTE-IDENTICAL to
//     the one enrollment wrote, which is the property these tests must pin.
//   - A duplicate credential (or duplicate SPKI credential) is rejected with
//     qrdb's FactorAlreadyExistsError semantics via ErrFactorAlreadyExists.
//   - Remove is a soft delete; a revoked factor keeps its index entries and
//     LookupByCredentialKey reports it as revoked, matching qrdb.
//
// The zero value is not usable; construct with NewMemoryUserAuthFactors.
type MemoryUserAuthFactors struct {
	mu sync.Mutex

	// factors holds the canonical records keyed by factor id.
	factors map[string]*qrdbsdk.AuthFactor

	// index maps "type/hex(sha256(credential))" to a factor id.
	index map[string]string

	// nextID generates deterministic factor ids so assertions can be exact.
	nextID int

	// versions holds each user's factor-set version: it moves every time a
	// factor joins or leaves the user's set, as go-qrdb's does.
	versions map[uint64]uint64

	// AddErr, when non-nil, is returned by Add instead of persisting. Tests
	// use it to drive the enrollment-failure path.
	AddErr error
}

// Compile-time interface check.
var (
	_ qrdbsdk.UserAuthFactorServicer = (*MemoryUserAuthFactors)(nil)
	_ qrdbsdk.ActiveFactorChecker    = (*MemoryUserAuthFactors)(nil)
)

// NewMemoryUserAuthFactors constructs an empty in-memory factor store.
func NewMemoryUserAuthFactors() *MemoryUserAuthFactors {
	return &MemoryUserAuthFactors{
		factors:  make(map[string]*qrdbsdk.AuthFactor),
		index:    make(map[string]string),
		versions: make(map[uint64]uint64),
	}
}

// factorIndexKey mirrors go-qrdb's lookupKeyFor: the credential blob is hashed,
// so enrollment and lookup must agree on the raw bytes.
func factorIndexKey(t qrdbsdk.AuthFactorType, credential []byte) string {
	sum := sha256.Sum256(credential)
	return string(t) + "/" + hex.EncodeToString(sum[:])
}

// Add persists the factor and its credential indexes.
func (m *MemoryUserAuthFactors) Add(_ context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	if m.AddErr != nil {
		return m.AddErr
	}
	if factor == nil || len(factor.Credential) == 0 || userID == 0 {
		return qrdbsdk.ErrInvalidFactor
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked(userID, factor)
}

// addLocked persists factor for userID and moves the user's factor-set
// version. The caller holds m.mu and has validated factor.
func (m *MemoryUserAuthFactors) addLocked(userID uint64, factor *qrdbsdk.AuthFactor) error {
	keys := m.indexKeysFor(factor)
	for _, key := range keys {
		if existingID, ok := m.index[key]; ok {
			return &FactorAlreadyExistsError{Type: factor.Type, FactorID: existingID}
		}
	}

	m.nextID++
	stored := *factor
	stored.UserID = userID
	if stored.ID == "" {
		stored.ID = "factor-" + strconv.Itoa(m.nextID)
	}
	if stored.RegisteredAt.IsZero() {
		stored.RegisteredAt = time.Now().UTC()
	}
	factor.ID = stored.ID
	factor.RegisteredAt = stored.RegisteredAt

	m.factors[stored.ID] = &stored
	for _, key := range keys {
		m.index[key] = stored.ID
	}
	m.versions[userID]++
	return nil
}

// AddConditional enrolls factor only when every condition set in cond holds
// against the user's factor set, as go-qrdb's guarded add does, and returns
// the factor-set version the add committed. A condition that does not hold
// returns *qrdbsdk.FactorConditionFailedError and enrolls nothing.
func (m *MemoryUserAuthFactors) AddConditional(_ context.Context, userID uint64, factor *qrdbsdk.AuthFactor, cond qrdbsdk.FactorAddCondition) (uint64, error) {
	if err := cond.Validate(); err != nil {
		return 0, err
	}
	if m.AddErr != nil {
		return 0, m.AddErr
	}
	if factor == nil || len(factor.Credential) == 0 || userID == 0 {
		return 0, qrdbsdk.ErrInvalidFactor
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.versions[userID]
	if cond.ExpectedVersion != nil && *cond.ExpectedVersion != current {
		return 0, &qrdbsdk.FactorConditionFailedError{
			UserID:          userID,
			Type:            factor.Type,
			Condition:       qrdbsdk.FactorConditionExpectedVersion,
			ExpectedVersion: *cond.ExpectedVersion,
			CurrentVersion:  current,
		}
	}
	if cond.NoActiveOfType {
		if existingID := m.activeOfTypeLocked(userID, factor.Type); existingID != "" {
			return 0, &qrdbsdk.FactorConditionFailedError{
				UserID:           userID,
				Type:             factor.Type,
				Condition:        qrdbsdk.FactorConditionNoActiveOfType,
				CurrentVersion:   current,
				ExistingFactorID: existingID,
			}
		}
	}
	if err := m.addLocked(userID, factor); err != nil {
		return 0, err
	}
	return m.versions[userID], nil
}

// FactorSetVersion returns the user's factor-set version: 0 until the first
// membership change.
func (m *MemoryUserAuthFactors) FactorSetVersion(_ context.Context, userID uint64) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.versions[userID], nil
}

// activeOfTypeLocked returns the ID of an active factor of type t that
// userID holds, or "" when there is none. The caller holds m.mu.
func (m *MemoryUserAuthFactors) activeOfTypeLocked(userID uint64, t qrdbsdk.AuthFactorType) string {
	for id, f := range m.factors {
		if f.UserID == userID && f.Type == t && f.RevokedAt == nil {
			return id
		}
	}
	return ""
}

// indexKeysFor returns every index key a factor occupies.
// AddAttested records what a ceremony proved the way the production service
// does: the factor is added and the reserved attest_* keys are written by the
// store, never by the caller. It makes the fake usable as a
// qrdbsdk.AttestationRecorder in tests of the fido2 write-through.
func (m *MemoryUserAuthFactors) AddAttested(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor, attestation qrdbsdk.VerifiedAttestation) error {
	if !attestation.Level.Valid() {
		return &qrdbsdk.InvalidAttestationError{Level: attestation.Level}
	}
	if factor != nil {
		factor.Metadata = stampAttestation(factor.Metadata, attestation)
	}
	return m.Add(ctx, userID, factor)
}

// RotateAttested is AddAttested's rotation counterpart.
func (m *MemoryUserAuthFactors) RotateAttested(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor, attestation qrdbsdk.VerifiedAttestation) error {
	if !attestation.Level.Valid() {
		return &qrdbsdk.InvalidAttestationError{Level: attestation.Level}
	}
	if newFactor != nil {
		newFactor.Metadata = stampAttestation(newFactor.Metadata, attestation)
	}
	return m.Rotate(ctx, oldID, newFactor)
}

// stampAttestation writes the reserved attestation keys the server owns.
func stampAttestation(meta map[string]string, attestation qrdbsdk.VerifiedAttestation) map[string]string {
	out := make(map[string]string, len(meta)+2)
	for k, v := range meta {
		out[k] = v
	}
	out[qrdbsdk.MetadataAttestLevel] = string(attestation.Level)
	if attestation.AAGUID != "" {
		out[qrdbsdk.MetadataAttestAAGUID] = attestation.AAGUID
	}
	return out
}

func (m *MemoryUserAuthFactors) indexKeysFor(factor *qrdbsdk.AuthFactor) []string {
	keys := []string{factorIndexKey(factor.Type, factor.Credential)}
	if len(factor.SPKICredential) > 0 {
		keys = append(keys, factorIndexKey(factor.Type, factor.SPKICredential))
	}
	return keys
}

// List returns every factor belonging to the user.
func (m *MemoryUserAuthFactors) List(_ context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*qrdbsdk.AuthFactor, 0, len(m.factors))
	for _, f := range m.factors {
		if f.UserID == userID {
			clone := *f
			out = append(out, &clone)
		}
	}
	return out, nil
}

// HasActiveFactor reports whether userID owns a non-revoked factor. It
// answers from the canonical records, as qrdb's ActiveFactorChecker requires.
func (m *MemoryUserAuthFactors) HasActiveFactor(_ context.Context, userID uint64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, f := range m.factors {
		if f.UserID == userID && f.IsActive() {
			return true, nil
		}
	}
	return false, nil
}

// Get returns the factor by id.
func (m *MemoryUserAuthFactors) Get(_ context.Context, factorID string) (*qrdbsdk.AuthFactor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.factors[factorID]
	if !ok {
		return nil, &FactorNotFoundError{FactorID: factorID}
	}
	clone := *f
	return &clone, nil
}

// LookupByCredentialKey resolves the active factor indexed under (t, key).
func (m *MemoryUserAuthFactors) LookupByCredentialKey(_ context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, ok := m.index[factorIndexKey(t, key)]
	if !ok {
		return nil, &FactorNotFoundError{}
	}
	f, ok := m.factors[id]
	if !ok {
		return nil, &FactorNotFoundError{FactorID: id}
	}
	if f.RevokedAt != nil {
		return nil, &qrdbsdk.FactorRevokedError{FactorID: f.ID}
	}
	clone := *f
	return &clone, nil
}

// Remove soft-deletes the factor, keeping its index entries as qrdb does.
func (m *MemoryUserAuthFactors) Remove(_ context.Context, factorID string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.factors[factorID]
	if !ok {
		return &FactorNotFoundError{FactorID: factorID}
	}
	now := time.Now().UTC()
	f.RevokedAt = &now
	m.versions[f.UserID]++
	return nil
}

// Delete hard-deletes an active factor and its index entries so the same
// credential can be bound again. A revoked factor is refused with
// *qrdbsdk.FactorRevokedError and a missing one with *FactorNotFoundError.
// Like Remove, the double does not enforce the last-factor guard.
func (m *MemoryUserAuthFactors) Delete(_ context.Context, factorID string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.factors[factorID]
	if !ok {
		return &FactorNotFoundError{FactorID: factorID}
	}
	if f.RevokedAt != nil {
		return &qrdbsdk.FactorRevokedError{FactorID: factorID}
	}
	for _, key := range m.indexKeysFor(f) {
		delete(m.index, key)
	}
	delete(m.factors, factorID)
	m.versions[f.UserID]++
	return nil
}

// Rotate adds the new factor and revokes the old one.
func (m *MemoryUserAuthFactors) Rotate(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error {
	old, err := m.Get(ctx, oldID)
	if err != nil {
		return err
	}
	if newFactor != nil && newFactor.UserID == 0 {
		newFactor.UserID = old.UserID
	}
	if err := m.Add(ctx, newFactor.UserID, newFactor); err != nil {
		return err
	}
	return m.Remove(ctx, oldID, true)
}

// Relabel updates the factor's display name.
func (m *MemoryUserAuthFactors) Relabel(_ context.Context, factorID, label string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.factors[factorID]
	if !ok {
		return &FactorNotFoundError{FactorID: factorID}
	}
	f.Label = label
	return nil
}

// MarkUsed records the last successful use of the factor.
func (m *MemoryUserAuthFactors) MarkUsed(_ context.Context, factorID string, ts time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.factors[factorID]
	if !ok {
		return &FactorNotFoundError{FactorID: factorID}
	}
	used := ts
	f.LastUsedAt = &used
	return nil
}

// FactorAlreadyExistsError mirrors go-qrdb's duplicate-credential error: it
// carries the colliding factor's identity AND unwraps to the SDK sentinel, so
// production code that branches with errors.Is(err, qrdbsdk.ErrFactorAlreadyExists)
// behaves against this store exactly as it does against a live node. (go-qrdb's
// concrete error type is not re-exported by its SDK, so it is mirrored here
// rather than reached for across the SDK boundary.)
type FactorAlreadyExistsError struct {
	Type     qrdbsdk.AuthFactorType
	FactorID string
}

// Error implements the error interface.
func (e *FactorAlreadyExistsError) Error() string {
	return "testutil: factor credential already enrolled: " + e.FactorID
}

// Unwrap returns the SDK sentinel so errors.Is succeeds.
func (e *FactorAlreadyExistsError) Unwrap() error { return qrdbsdk.ErrFactorAlreadyExists }

// FactorNotFoundError mirrors go-qrdb's missing-factor error and unwraps to the
// SDK sentinel.
type FactorNotFoundError struct {
	FactorID string
}

// Error implements the error interface.
func (e *FactorNotFoundError) Error() string {
	if e == nil || e.FactorID == "" {
		return "testutil: factor not found"
	}
	return "testutil: factor not found: " + e.FactorID
}

// Unwrap returns the SDK sentinel so errors.Is succeeds.
func (e *FactorNotFoundError) Unwrap() error { return qrdbsdk.ErrFactorNotFound }
