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

// accountSecretDataKey is the single data-map key under which the JSON-
// encoded AccountCredential is stored. SecretsManager.Store takes a
// map[string][]byte; one key keeps the envelope atomic (no partial reads).
const accountSecretDataKey = "credential"

// AccountSecretStore stores and retrieves AccountCredential envelopes.
type AccountSecretStore struct {
	mgr interfaces.SecretsManager
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
	if err := validateKeySegment("tenant_id", tenantID); err != nil {
		return "", accountKeyError(err)
	}
	if err := validateKeySegment("account_id", accountID); err != nil {
		return "", accountKeyError(err)
	}
	return accountSecretPathPrefix + tenantID + keySeparator + accountID, nil
}

// Put stores or overwrites cred for tenantID/accountID, encrypted under a
// tenant-specific DEK.
func (s *AccountSecretStore) Put(ctx context.Context, tenantID, accountID string, cred *AccountCredential) error {
	if err := s.ready(); err != nil {
		return err
	}
	if cred == nil {
		return &AccountInvalidError{Field: "credential", Message: "must not be nil"}
	}
	path, err := s.path(tenantID, accountID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(cred)
	if err != nil {
		return &AccountStoreError{Message: "failed to marshal account credential: " + err.Error()}
	}
	// Metadata carries non-secret bookkeeping only — never any field from
	// cred itself — so the SecretsManager's own audit/versioning surface
	// (which may log metadata) can never leak credential content.
	metadata := map[string]string{"kind": "agent_account_credential"}
	storeErr := s.mgr.StoreWithOptions(ctx, path, map[string][]byte{accountSecretDataKey: body}, metadata,
		&interfaces.StoreOptions{TenantID: tenantID})
	if storeErr != nil {
		return &AccountStoreError{Message: "failed to store account credential: " + storeErr.Error()}
	}
	return nil
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
		return nil, &AccountCredentialNotFoundError{TenantID: tenantID, AccountID: accountID}
	}
	body, ok := data[accountSecretDataKey]
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
		return &AccountStoreError{Message: "failed to destroy account credential: " + destroyErr.Error()}
	}
	return nil
}
