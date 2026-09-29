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

package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// AccountSecretStore — the ONLY place an AccountCredential is read or
// written.
//
// It is a thin, dedicated wrapper over interfaces.SecretsManager — the same
// encrypted-at-rest, barrier-backed subsystem pkg/service.SecretsService uses
// for every other secret in the platform (see CLAUDE.md "Layer Ownership":
// QuicBaaS composes qrdb/xkms primitives rather than reimplementing them).
// AccountStore (account_store.go) never imports this file's types into its
// own records, so a leak of the metadata KV (tenant-scoped, but otherwise
// plain JSON) can never expose credential material.
//
// Each account's envelope is stored at its own path, encrypted under a
// TENANT-SPECIFIC DEK (StoreOptions.TenantID) — not merely namespaced by a
// tenant-prefixed path. Two tenants' accounts are therefore unreadable to
// each other even in the (extremely unlikely) event the path-prefix
// isolation were ever bypassed: the ciphertext itself is keyed differently.
// =============================================================================

// accountSecretPathPrefix is the SecretsManager path namespace for account
// credential envelopes. Kept out of store_keys.go's KV keyspace list
// deliberately: it addresses SecretsManager, not the plain orchestration KV.
const accountSecretPathPrefix = "orchestration/agent-accounts/"

// AccountSecretDataKey is the single data-map key under which the JSON-
// encoded AccountCredential is stored. SecretsManager.Store takes a
// map[string][]byte; one key keeps the envelope atomic (no partial reads).
//
// It is exported because a SecretsManager written specifically to back an
// AccountSecretStore — the fleet node daemon's read-through materializing store
// (pkg/fleet, go-quicbaas#734) — has to answer with the same key this store
// asks for. Restating the literal there would make a rename silently return
// "envelope present but empty" on the worker host.
const AccountSecretDataKey = "credential"

// AccountSecretPath returns the SecretsManager path an account's credential
// envelope is stored at.
//
// It is exported alongside ParseAccountSecretPath so a SecretsManager written
// to serve this store can invert the addressing rather than hardcode it. The
// pair is the ONE definition of the scheme; TestAccountSecretPath_RoundTrips
// pins them against each other.
func AccountSecretPath(tenantID, accountID string) (string, error) {
	if err := validateKeySegment("tenant_id", tenantID); err != nil {
		return "", accountKeyError(err)
	}
	if err := validateKeySegment("account_id", accountID); err != nil {
		return "", accountKeyError(err)
	}
	return accountSecretPathPrefix + tenantID + keySeparator + accountID, nil
}

// ParseAccountSecretPath inverts AccountSecretPath, reporting false for any
// path that is not one this package addresses.
//
// A path outside the namespace, or one carrying an empty segment, is refused
// rather than being split into whatever it happens to contain: a caller acting
// on a misparsed path would be reading one account's credential under another
// account's name.
func ParseAccountSecretPath(path string) (tenantID, accountID string, ok bool) {
	rest, found := strings.CutPrefix(path, accountSecretPathPrefix)
	if !found {
		return "", "", false
	}
	tenantID, accountID, found = strings.Cut(rest, keySeparator)
	if !found || tenantID == "" || accountID == "" {
		return "", "", false
	}
	// A third segment means the path addresses something else in the
	// namespace; refusing keeps the inverse total rather than lossy.
	if strings.Contains(accountID, keySeparator) {
		return "", "", false
	}
	return tenantID, accountID, true
}

// AccountSecretStore stores and retrieves AccountCredential envelopes.
type AccountSecretStore struct {
	mgr interfaces.SecretsManager

	// lastGeneration is the highest generation stamp this process has issued.
	// It makes Replace's stamps STRICTLY INCREASING regardless of what the
	// wall clock does, so two replacements inside one nanosecond — or one
	// across a clock that stepped backwards — can never mint the same, or a
	// descending, generation. Atomic rather than mutex-guarded: the stamp is a
	// single integer and the contention is a compare-and-swap loop that almost
	// never spins.
	lastGeneration atomic.Int64
}

// nextGeneration mints the stamp for one envelope write: now, or one past the
// last stamp issued, whichever is greater.
func (s *AccountSecretStore) nextGeneration() int64 {
	now := time.Now().UTC().UnixNano()
	for {
		last := s.lastGeneration.Load()
		next := now
		if next <= last {
			next = last + 1
		}
		if s.lastGeneration.CompareAndSwap(last, next) {
			return next
		}
	}
}

// NewAccountSecretStore returns an AccountSecretStore backed by mgr.
func NewAccountSecretStore(mgr interfaces.SecretsManager) *AccountSecretStore {
	return &AccountSecretStore{mgr: mgr}
}

// ready reports whether the store was constructed with a backing manager.
func (s *AccountSecretStore) ready() error {
	if s == nil || s.mgr == nil {
		return &AccountStoreError{Message: "secrets manager must not be nil"}
	}
	return nil
}

func (s *AccountSecretStore) path(tenantID, accountID string) (string, error) {
	return AccountSecretPath(tenantID, accountID)
}

// Put stores or overwrites cred for tenantID/accountID, encrypted under a
// tenant-specific DEK. It is Replace for callers with no use for the
// generation stamp.
func (s *AccountSecretStore) Put(ctx context.Context, tenantID, accountID string, cred *AccountCredential) error {
	_, err := s.Replace(ctx, tenantID, accountID, cred)
	return err
}

// Replace stores or overwrites cred for tenantID/accountID and reports the
// GENERATION it stamped onto the stored envelope.
//
// EVERY WRITE IS A NEW GENERATION, and the stamp goes into the same encrypted
// body as the material it names. A caller therefore cannot publish material
// without publishing its identity, and cannot lose the identity while keeping
// the material: the two are one Put. That is what makes the generation the
// AUTHORITY on "which copy is this?", and everything else — the mirror on the
// metadata record, the generation recorded on a session — a derivative of it
// that can be reconciled back from here (go-quicbaas#997).
//
// cred itself is NOT mutated: the stamp is applied to the copy that is
// serialized, so a caller's envelope never silently acquires a generation it
// did not ask for.
func (s *AccountSecretStore) Replace(ctx context.Context, tenantID, accountID string, cred *AccountCredential) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	if cred == nil {
		return 0, &AccountInvalidError{Field: "credential", Message: "must not be nil"}
	}
	path, err := s.path(tenantID, accountID)
	if err != nil {
		return 0, err
	}
	stamped := *cred
	stamped.Generation = s.nextGeneration()
	body, err := json.Marshal(&stamped)
	if err != nil {
		return 0, &AccountStoreError{Message: "failed to marshal account credential: " + err.Error()}
	}
	// Metadata carries non-secret bookkeeping only — never any field from
	// cred itself — so the SecretsManager's own audit/versioning surface
	// (which may log metadata) can never leak credential content.
	metadata := map[string]string{"kind": "agent_account_credential"}
	storeErr := s.mgr.StoreWithOptions(ctx, path, map[string][]byte{AccountSecretDataKey: body}, metadata,
		&interfaces.StoreOptions{TenantID: tenantID})
	if storeErr != nil {
		return 0, &AccountStoreError{Message: "failed to store account credential: " + storeErr.Error(), Cause: storeErr}
	}
	return stamped.Generation, nil
}

// Get retrieves the credential envelope for tenantID/accountID.
func (s *AccountSecretStore) Get(ctx context.Context, tenantID, accountID string) (*AccountCredential, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	path, err := s.path(tenantID, accountID)
	if err != nil {
		return nil, err
	}
	data, _, getErr := s.mgr.Get(ctx, path)
	if getErr != nil {
		// A MISS AND AN OUTAGE ARE DIFFERENT ANSWERS, and flattening them
		// into "not found" was safe only while nothing branched on it.
		// go-quicbaas#235 made the SDLC module branch on it: a missing credential
		// is a configuration fault that PARKS the work item and refunds its
		// attempt, because no operator action short of re-importing the
		// credential will help. A secrets backend that timed out or failed is
		// the opposite — it fixes itself, and parking on it takes an item out
		// of the queue for a condition nobody has to act on.
		//
		// So the sentinel now means what its name says. Everything else is a
		// store error, which no fault class claims.
		if qerrors.IsNotFound(getErr) {
			return nil, &AccountCredentialNotFoundError{TenantID: tenantID, AccountID: accountID}
		}
		return nil, &AccountStoreError{
			Message: "failed to read account credential: " + getErr.Error(),
			Cause:   getErr,
		}
	}
	// An envelope that exists with no body in it is a genuine miss: the
	// metadata says the account has a credential and there is nothing there.
	body, ok := data[AccountSecretDataKey]
	if !ok || len(body) == 0 {
		return nil, &AccountCredentialNotFoundError{TenantID: tenantID, AccountID: accountID}
	}
	var cred AccountCredential
	if err := json.Unmarshal(body, &cred); err != nil {
		return nil, &AccountStoreError{Message: "failed to unmarshal account credential: " + err.Error()}
	}
	return &cred, nil
}

// Delete permanently removes the credential envelope for tenantID/accountID.
// Idempotent: deleting an already-absent envelope is not an error, matching
// the deletion semantics AccountStore.Delete expects to compose with.
func (s *AccountSecretStore) Delete(ctx context.Context, tenantID, accountID string) error {
	if err := s.ready(); err != nil {
		return err
	}
	path, err := s.path(tenantID, accountID)
	if err != nil {
		return err
	}
	if destroyErr := s.mgr.Destroy(ctx, path); destroyErr != nil {
		if qerrors.IsNotFound(destroyErr) {
			return nil
		}
		return &AccountStoreError{Message: "failed to destroy account credential: " + destroyErr.Error(), Cause: destroyErr}
	}
	return nil
}
