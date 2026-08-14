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

package iam

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Federation storage key helpers. Provider records carry an explicit tenantID
// on every verb so they route directly and key by (tenantID, providerID). Links
// are keyed by a tenant-encoded link id (by-ID lookups arrive without a tenant);
// per-tenant external and user indexes resolve GetLinkByExternal /
// GetLinksByUser under the request's context tenant (those verbs carry no
// explicit tenantID, but a federation operation always runs inside one tenant's
// request scope). State tokens are keyed under the state's TenantID.
const (
	keyPrefixProviderRec     = KeyPrefixIAM + "fed/provider/"
	keyPrefixProviderNameIdx = KeyPrefixIAM + "fed/provider_name/"
	keyPrefixLinkRec         = KeyPrefixIAM + "fed/link/"
	keyPrefixLinkExternalIdx = KeyPrefixIAM + "fed/link_ext/"
	keyPrefixLinkUserIdx     = KeyPrefixIAM + "fed/link_user/"
	keyPrefixFedState        = KeyPrefixIAM + "fed/state/"
)

func providerRecKey(id string) string       { return keyPrefixProviderRec + id }
func providerNameIdxKey(name string) string { return keyPrefixProviderNameIdx + name }
func linkRecKey(id string) string           { return keyPrefixLinkRec + id }
func linkExternalIdxKey(providerID, externalID string) string {
	return keyPrefixLinkExternalIdx + providerID + "/" + externalID
}
func linkUserIdxKey(userID, id string) string { return keyPrefixLinkUserIdx + userID + "/" + id }
func linkUserIdxPrefix(userID string) string  { return keyPrefixLinkUserIdx + userID + "/" }
func fedStateKey(token string) string         { return keyPrefixFedState + token }

// -----------------------------------------------------------------------------
// BackendProviderStorage — ProviderStorage over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendProviderStorage implements ProviderStorage over the per-tenant routing
// KVStore. Every verb carries an explicit tenantID, so this is the cleanest
// substore: it simply binds the tenant into ctx before each KV call.
type BackendProviderStorage struct {
	kv interfaces.KVStore
}

// NewBackendProviderStorage constructs a persistent, per-tenant provider store.
func NewBackendProviderStorage(kv interfaces.KVStore) *BackendProviderStorage {
	return &BackendProviderStorage{kv: kv}
}

var _ ProviderStorage = (*BackendProviderStorage)(nil)

// CreateProvider routes by the provider's TenantID and writes the record plus
// the per-tenant name index used by GetProviderByName.
func (s *BackendProviderStorage) CreateProvider(ctx context.Context, provider *ExternalProvider) error {
	if err := provider.Validate(); err != nil {
		return err
	}
	ctx = withTenant(ctx, provider.TenantID)
	if _, err := s.kv.Get(ctx, providerRecKey(provider.ID)); err == nil {
		return ErrProviderAlreadyExists
	}
	if _, err := s.kv.Get(ctx, providerNameIdxKey(provider.Name)); err == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.federation.provider.create",
			Err:  &ProviderNameExistsError{Name: provider.Name},
		}
	}
	now := time.Now()
	if provider.Created.IsZero() {
		provider.Created = now
	}
	provider.Updated = now
	data, err := json.Marshal(provider)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.provider.create", Err: err}
	}
	if err := s.kv.Put(ctx, providerRecKey(provider.ID), data); err != nil {
		return err
	}
	return s.kv.Put(ctx, providerNameIdxKey(provider.Name), []byte(provider.ID))
}

// GetProvider routes by tenantID and reads the provider record.
func (s *BackendProviderStorage) GetProvider(ctx context.Context, tenantID, providerID string) (*ExternalProvider, error) {
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, providerRecKey(providerID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrProviderNotFound
		}
		return nil, err
	}
	var p ExternalProvider
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.provider.get", Err: err}
	}
	return &p, nil
}

// GetProviderByName routes by tenantID and resolves the name index.
func (s *BackendProviderStorage) GetProviderByName(ctx context.Context, tenantID, name string) (*ExternalProvider, error) {
	ctx = withTenant(ctx, tenantID)
	idData, err := s.kv.Get(ctx, providerNameIdxKey(name))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrProviderNotFound
		}
		return nil, err
	}
	return s.GetProvider(ctx, tenantID, string(idData))
}

// UpdateProvider routes by tenantID, reconciles a changed name index, and writes.
func (s *BackendProviderStorage) UpdateProvider(ctx context.Context, provider *ExternalProvider) error {
	if err := provider.Validate(); err != nil {
		return err
	}
	ctx = withTenant(ctx, provider.TenantID)
	existing, err := s.GetProvider(ctx, provider.TenantID, provider.ID)
	if err != nil {
		return err
	}
	if existing.Name != provider.Name {
		if _, err := s.kv.Get(ctx, providerNameIdxKey(provider.Name)); err == nil {
			return &dberrors.QBError{
				Code: dberrors.ErrAlreadyExists,
				Op:   "iam.federation.provider.update",
				Err:  &ProviderNameExistsError{Name: provider.Name},
			}
		}
		_ = s.kv.Delete(ctx, providerNameIdxKey(existing.Name))
	}
	provider.Updated = time.Now()
	data, err := json.Marshal(provider)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.provider.update", Err: err}
	}
	if err := s.kv.Put(ctx, providerRecKey(provider.ID), data); err != nil {
		return err
	}
	return s.kv.Put(ctx, providerNameIdxKey(provider.Name), []byte(provider.ID))
}

// DeleteProvider routes by tenantID and removes the record plus name index.
func (s *BackendProviderStorage) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	ctx = withTenant(ctx, tenantID)
	provider, err := s.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		return err
	}
	_ = s.kv.Delete(ctx, providerNameIdxKey(provider.Name))
	return s.kv.Delete(ctx, providerRecKey(providerID))
}

// ListProviders routes by tenantID and returns every provider in the tenant's
// store with pagination applied in memory.
func (s *BackendProviderStorage) ListProviders(ctx context.Context, tenantID string, opts ListOptions) ([]*ExternalProvider, error) {
	ctx = withTenant(ctx, tenantID)
	var result []*ExternalProvider
	if err := s.kv.Scan(ctx, keyPrefixProviderRec, func(_ string, value []byte) error {
		var p ExternalProvider
		if json.Unmarshal(value, &p) != nil {
			return nil
		}
		result = append(result, &p)
		return nil
	}); err != nil {
		return nil, err
	}
	return paginateProviders(result, opts), nil
}

func paginateProviders(in []*ExternalProvider, opts ListOptions) []*ExternalProvider {
	start := opts.Offset
	if start >= len(in) {
		return []*ExternalProvider{}
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(in) {
		end = len(in)
	}
	return in[start:end]
}

// -----------------------------------------------------------------------------
// BackendFederationStorage — FederationStorage over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendFederationStorage implements FederationStorage over the per-tenant
// routing KVStore. Link ids are tenant-encoded for by-ID routing; the external
// and user indexes live in the request tenant's store and are resolved via the
// context tenant for the verbs that carry no explicit tenantID.
type BackendFederationStorage struct {
	kv interfaces.KVStore
}

// NewBackendFederationStorage constructs a persistent, per-tenant link store.
func NewBackendFederationStorage(kv interfaces.KVStore) *BackendFederationStorage {
	return &BackendFederationStorage{kv: kv}
}

var _ FederationStorage = (*BackendFederationStorage)(nil)

// CreateLink tenant-encodes the link id, routes by the link's TenantID, and
// writes the record plus the per-tenant external and user indexes.
func (s *BackendFederationStorage) CreateLink(ctx context.Context, link *FederatedIdentity) error {
	if err := link.Validate(); err != nil {
		return err
	}
	link.ID = ensureTenantEncoded(link.TenantID, link.ID)
	ctx = withTenant(ctx, link.TenantID)
	if _, err := s.kv.Get(ctx, linkRecKey(link.ID)); err == nil {
		return ErrLinkAlreadyExists
	}
	extKey := linkExternalIdxKey(link.ProviderID, link.ExternalID)
	if _, err := s.kv.Get(ctx, extKey); err == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.federation.link.create",
			Err:  &LinkExternalExistsError{ExternalID: link.ExternalID},
		}
	}
	if link.Created.IsZero() {
		link.Created = time.Now()
	}
	data, err := json.Marshal(link)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.link.create", Err: err}
	}
	if err := s.kv.Put(ctx, linkRecKey(link.ID), data); err != nil {
		return err
	}
	if err := s.kv.Put(ctx, extKey, []byte(link.ID)); err != nil {
		return err
	}
	return s.kv.Put(ctx, linkUserIdxKey(link.UserID, link.ID), []byte(link.ID))
}

// GetLink decodes the tenant from the link id and reads the record.
func (s *BackendFederationStorage) GetLink(ctx context.Context, linkID string) (*FederatedIdentity, error) {
	tenantID, fullID := decodeTenantID(linkID)
	ctx = withTenant(ctx, tenantID)
	return s.getRouted(ctx, fullID)
}

func (s *BackendFederationStorage) getRouted(ctx context.Context, fullID string) (*FederatedIdentity, error) {
	data, err := s.kv.Get(ctx, linkRecKey(fullID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrLinkNotFound
		}
		return nil, err
	}
	var l FederatedIdentity
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.link.get", Err: err}
	}
	return &l, nil
}

// GetLinkByExternal routes by the request's context tenant (federation runs in
// one tenant's request scope) and resolves the external index.
func (s *BackendFederationStorage) GetLinkByExternal(ctx context.Context, providerID, externalID string) (*FederatedIdentity, error) {
	idData, err := s.kv.Get(ctx, linkExternalIdxKey(providerID, externalID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrLinkNotFound
		}
		return nil, err
	}
	_, fullID := decodeTenantID(string(idData))
	return s.getRouted(ctx, fullID)
}

// GetLinksByUser routes by the request's context tenant and returns every link
// in the user index.
func (s *BackendFederationStorage) GetLinksByUser(ctx context.Context, userID string) ([]*FederatedIdentity, error) {
	var ids []string
	if err := s.kv.Scan(ctx, linkUserIdxPrefix(userID), func(_ string, value []byte) error {
		ids = append(ids, string(value))
		return nil
	}); err != nil {
		return nil, err
	}
	links := make([]*FederatedIdentity, 0, len(ids))
	for _, id := range ids {
		_, fullID := decodeTenantID(id)
		l, err := s.getRouted(ctx, fullID)
		if err != nil {
			if errors.Is(err, ErrLinkNotFound) {
				continue
			}
			return nil, err
		}
		links = append(links, l)
	}
	return links, nil
}

// UpdateLink decodes the tenant, reconciles changed external index, and writes.
func (s *BackendFederationStorage) UpdateLink(ctx context.Context, link *FederatedIdentity) error {
	if err := link.Validate(); err != nil {
		return err
	}
	link.ID = ensureTenantEncoded(link.TenantID, link.ID)
	tenantID, fullID := decodeTenantID(link.ID)
	ctx = withTenant(ctx, tenantID)
	existing, err := s.getRouted(ctx, fullID)
	if err != nil {
		return err
	}
	if existing.ExternalID != link.ExternalID || existing.ProviderID != link.ProviderID {
		newExtKey := linkExternalIdxKey(link.ProviderID, link.ExternalID)
		if _, err := s.kv.Get(ctx, newExtKey); err == nil {
			return &dberrors.QBError{
				Code: dberrors.ErrAlreadyExists,
				Op:   "iam.federation.link.update",
				Err:  &LinkExternalExistsError{ExternalID: link.ExternalID},
			}
		}
		_ = s.kv.Delete(ctx, linkExternalIdxKey(existing.ProviderID, existing.ExternalID))
		if err := s.kv.Put(ctx, newExtKey, []byte(link.ID)); err != nil {
			return err
		}
	}
	if existing.UserID != link.UserID {
		_ = s.kv.Delete(ctx, linkUserIdxKey(existing.UserID, link.ID))
		if err := s.kv.Put(ctx, linkUserIdxKey(link.UserID, link.ID), []byte(link.ID)); err != nil {
			return err
		}
	}
	data, err := json.Marshal(link)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.link.update", Err: err}
	}
	return s.kv.Put(ctx, linkRecKey(fullID), data)
}

// DeleteLink decodes the tenant and removes the record plus both indexes.
func (s *BackendFederationStorage) DeleteLink(ctx context.Context, linkID string) error {
	tenantID, fullID := decodeTenantID(linkID)
	ctx = withTenant(ctx, tenantID)
	link, err := s.getRouted(ctx, fullID)
	if err != nil {
		return err
	}
	_ = s.kv.Delete(ctx, linkExternalIdxKey(link.ProviderID, link.ExternalID))
	_ = s.kv.Delete(ctx, linkUserIdxKey(link.UserID, fullID))
	return s.kv.Delete(ctx, linkRecKey(fullID))
}

// DeleteUserLinks routes by the request's context tenant and removes every link
// in the user index.
func (s *BackendFederationStorage) DeleteUserLinks(ctx context.Context, userID string) error {
	links, err := s.GetLinksByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, link := range links {
		_, fullID := decodeTenantID(link.ID)
		_ = s.kv.Delete(ctx, linkExternalIdxKey(link.ProviderID, link.ExternalID))
		_ = s.kv.Delete(ctx, linkUserIdxKey(userID, fullID))
		if err := s.kv.Delete(ctx, linkRecKey(fullID)); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// BackendStateStorage — StateStorage over a routing KVStore.
// -----------------------------------------------------------------------------

// BackendStateStorage implements StateStorage over the per-tenant routing
// KVStore. State tokens are tenant-encoded (minted at BeginLogin where the
// tenant is known) so GetState / DeleteState route to the owning tenant.
type BackendStateStorage struct {
	kv      interfaces.KVStore
	tenants tenantLister
}

// NewBackendStateStorage constructs a persistent, per-tenant state store. The
// tenants lister is optional and used only by CleanupExpired.
func NewBackendStateStorage(kv interfaces.KVStore, tenants tenantLister) *BackendStateStorage {
	return &BackendStateStorage{kv: kv, tenants: tenants}
}

var _ StateStorage = (*BackendStateStorage)(nil)

// SaveState tenant-encodes the state nonce (mutating it so the caller embeds the
// routable token in the redirect), routes by the state's TenantID, and writes.
func (s *BackendStateStorage) SaveState(ctx context.Context, state *LoginState) error {
	if state.Nonce == "" {
		return ErrStateInvalid
	}
	state.Nonce = ensureTenantEncoded(state.TenantID, state.Nonce)
	ctx = withTenant(ctx, state.TenantID)
	data, err := json.Marshal(state)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.state.save", Err: err}
	}
	return s.kv.Put(ctx, fedStateKey(state.Nonce), data)
}

// GetState decodes the tenant from the token, routes, and returns the state if
// not expired.
func (s *BackendStateStorage) GetState(ctx context.Context, token string) (*LoginState, error) {
	tenantID, fullToken := decodeTenantID(token)
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, fedStateKey(fullToken))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrStateNotFound
		}
		return nil, err
	}
	var state LoginState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.federation.state.get", Err: err}
	}
	if state.IsExpired() {
		return nil, ErrStateExpired
	}
	return &state, nil
}

// DeleteState decodes the tenant and removes the state.
func (s *BackendStateStorage) DeleteState(ctx context.Context, token string) error {
	tenantID, fullToken := decodeTenantID(token)
	ctx = withTenant(ctx, tenantID)
	return s.kv.Delete(ctx, fedStateKey(fullToken))
}

// CleanupExpired sweeps expired states per tenant. Without a tenantLister it
// sweeps only the context tenant.
func (s *BackendStateStorage) CleanupExpired(ctx context.Context) (int, error) {
	tenantIDs, err := sweepTenantIDs(ctx, s.tenants)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, tid := range tenantIDs {
		n, err := s.cleanupTenant(withTenant(ctx, tid))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (s *BackendStateStorage) cleanupTenant(ctx context.Context) (int, error) {
	now := time.Now()
	var stale []string
	if err := s.kv.Scan(ctx, keyPrefixFedState, func(_ string, value []byte) error {
		var state LoginState
		if json.Unmarshal(value, &state) != nil {
			return nil
		}
		if now.After(state.ExpiresAt) {
			stale = append(stale, state.Nonce)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	count := 0
	for _, nonce := range stale {
		if err := s.kv.Delete(ctx, fedStateKey(nonce)); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
