//go:build iam

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
	"sync"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Additional federation-specific error types for storage operations.
var (
	// ErrLinkNotFound is returned when a federated identity link is not found.
	ErrLinkNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.link",
		Err:  &IAMMessageError{Message: "federated identity link not found"},
	}

	// ErrLinkAlreadyExists is returned when attempting to create a duplicate link.
	ErrLinkAlreadyExists = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.federation.link",
		Err:  &IAMMessageError{Message: "federated identity link already exists"},
	}
)

// ProviderStorage manages external identity providers.
// This interface provides fine-grained provider operations separate from
// the combined FederationStorage interface.
type ProviderStorage interface {
	// CreateProvider stores a new external provider.
	// Returns ErrProviderAlreadyExists if a provider with the same ID exists.
	CreateProvider(ctx context.Context, provider *ExternalProvider) error

	// GetProvider retrieves a provider by ID.
	// Returns ErrProviderNotFound if the provider does not exist.
	GetProvider(ctx context.Context, tenantID, providerID string) (*ExternalProvider, error)

	// GetProviderByName retrieves a provider by name within a tenant.
	// Returns ErrProviderNotFound if the provider does not exist.
	GetProviderByName(ctx context.Context, tenantID, name string) (*ExternalProvider, error)

	// UpdateProvider updates an existing provider.
	// Returns ErrProviderNotFound if the provider does not exist.
	UpdateProvider(ctx context.Context, provider *ExternalProvider) error

	// DeleteProvider removes a provider.
	// Returns ErrProviderNotFound if the provider does not exist.
	DeleteProvider(ctx context.Context, tenantID, providerID string) error

	// ListProviders retrieves providers for a tenant with pagination.
	ListProviders(ctx context.Context, tenantID string, opts ListOptions) ([]*ExternalProvider, error)
}

// FederationStorage manages federated identity links.
// This interface provides fine-grained identity link operations.
type FederationStorage interface {
	// CreateLink stores a new federated identity link.
	// Returns ErrLinkAlreadyExists if a link with the same ID exists.
	CreateLink(ctx context.Context, link *FederatedIdentity) error

	// GetLink retrieves a link by ID.
	// Returns ErrLinkNotFound if the link does not exist.
	GetLink(ctx context.Context, linkID string) (*FederatedIdentity, error)

	// GetLinkByExternal retrieves a link by provider and external ID.
	// Returns ErrLinkNotFound if the link does not exist.
	GetLinkByExternal(ctx context.Context, providerID, externalID string) (*FederatedIdentity, error)

	// GetLinksByUser retrieves all links for a user.
	GetLinksByUser(ctx context.Context, userID string) ([]*FederatedIdentity, error)

	// UpdateLink updates an existing link.
	// Returns ErrLinkNotFound if the link does not exist.
	UpdateLink(ctx context.Context, link *FederatedIdentity) error

	// DeleteLink removes a link.
	// Returns ErrLinkNotFound if the link does not exist.
	DeleteLink(ctx context.Context, linkID string) error

	// DeleteUserLinks removes all links for a user.
	DeleteUserLinks(ctx context.Context, userID string) error
}

// StateStorage manages OAuth callback state tokens.
type StateStorage interface {
	// SaveState stores a callback state.
	SaveState(ctx context.Context, state *LoginState) error

	// GetState retrieves a state by token.
	// Returns ErrStateNotFound if the state does not exist.
	// Returns ErrStateExpired if the state has expired.
	GetState(ctx context.Context, token string) (*LoginState, error)

	// DeleteState removes a state.
	DeleteState(ctx context.Context, token string) error

	// CleanupExpired removes all expired states and returns the count.
	CleanupExpired(ctx context.Context) (int, error)
}

// MemoryProviderStorage implements ProviderStorage using in-memory maps.
type MemoryProviderStorage struct {
	mu              sync.RWMutex
	providers       map[string]*ExternalProvider // key: tenantID:providerID
	providersByName map[string]*ExternalProvider // key: tenantID:name
}

// NewMemoryProviderStorage creates a new in-memory provider storage.
func NewMemoryProviderStorage() *MemoryProviderStorage {
	return &MemoryProviderStorage{
		providers:       make(map[string]*ExternalProvider),
		providersByName: make(map[string]*ExternalProvider),
	}
}

// CreateProvider stores a new external provider.
func (m *MemoryProviderStorage) CreateProvider(ctx context.Context, provider *ExternalProvider) error {
	if err := provider.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(provider.TenantID, provider.ID)
	if _, exists := m.providers[key]; exists {
		return ErrProviderAlreadyExists
	}

	nameKey := makeKey(provider.TenantID, provider.Name)
	if _, exists := m.providersByName[nameKey]; exists {
		return &dberrors.DragonError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.federation.provider.create",
			Err:  &ProviderNameExistsError{Name: provider.Name},
		}
	}

	// Create a copy to avoid external modifications
	p := *provider
	now := time.Now()
	if p.Created.IsZero() {
		p.Created = now
	}
	p.Updated = now

	m.providers[key] = &p
	m.providersByName[nameKey] = &p

	return nil
}

// GetProvider retrieves a provider by ID.
func (m *MemoryProviderStorage) GetProvider(ctx context.Context, tenantID, providerID string) (*ExternalProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := makeKey(tenantID, providerID)
	provider, exists := m.providers[key]
	if !exists {
		return nil, ErrProviderNotFound
	}

	// Return a copy
	p := *provider
	return &p, nil
}

// GetProviderByName retrieves a provider by name within a tenant.
func (m *MemoryProviderStorage) GetProviderByName(ctx context.Context, tenantID, name string) (*ExternalProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nameKey := makeKey(tenantID, name)
	provider, exists := m.providersByName[nameKey]
	if !exists {
		return nil, ErrProviderNotFound
	}

	// Return a copy
	p := *provider
	return &p, nil
}

// UpdateProvider updates an existing provider.
func (m *MemoryProviderStorage) UpdateProvider(ctx context.Context, provider *ExternalProvider) error {
	if err := provider.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(provider.TenantID, provider.ID)
	oldProvider, exists := m.providers[key]
	if !exists {
		return ErrProviderNotFound
	}

	// Handle name change
	if oldProvider.Name != provider.Name {
		oldNameKey := makeKey(provider.TenantID, oldProvider.Name)
		newNameKey := makeKey(provider.TenantID, provider.Name)

		// Check if new name conflicts
		if _, exists := m.providersByName[newNameKey]; exists {
			return &dberrors.DragonError{
				Code: dberrors.ErrAlreadyExists,
				Op:   "iam.federation.provider.update",
				Err:  &ProviderNameExistsError{Name: provider.Name},
			}
		}

		delete(m.providersByName, oldNameKey)
	}

	// Create a copy
	p := *provider
	p.Updated = time.Now()

	m.providers[key] = &p
	nameKey := makeKey(provider.TenantID, provider.Name)
	m.providersByName[nameKey] = &p

	return nil
}

// DeleteProvider removes a provider.
func (m *MemoryProviderStorage) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(tenantID, providerID)
	provider, exists := m.providers[key]
	if !exists {
		return ErrProviderNotFound
	}

	nameKey := makeKey(tenantID, provider.Name)
	delete(m.providers, key)
	delete(m.providersByName, nameKey)

	return nil
}

// ListProviders retrieves providers for a tenant with pagination.
func (m *MemoryProviderStorage) ListProviders(ctx context.Context, tenantID string, opts ListOptions) ([]*ExternalProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*ExternalProvider
	prefix := tenantID + ":"
	for key, provider := range m.providers {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			p := *provider
			result = append(result, &p)
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*ExternalProvider{}, nil
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// MemoryFederationStorage implements FederationStorage using in-memory maps.
type MemoryFederationStorage struct {
	mu              sync.RWMutex
	links           map[string]*FederatedIdentity   // key: linkID
	linksByExternal map[string]*FederatedIdentity   // key: providerID:externalID
	linksByUser     map[string][]*FederatedIdentity // key: userID
}

// NewMemoryFederationStorage creates a new in-memory federation storage.
func NewMemoryFederationStorage() *MemoryFederationStorage {
	return &MemoryFederationStorage{
		links:           make(map[string]*FederatedIdentity),
		linksByExternal: make(map[string]*FederatedIdentity),
		linksByUser:     make(map[string][]*FederatedIdentity),
	}
}

// CreateLink stores a new federated identity link.
func (m *MemoryFederationStorage) CreateLink(ctx context.Context, link *FederatedIdentity) error {
	if err := link.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.links[link.ID]; exists {
		return ErrLinkAlreadyExists
	}

	externalKey := makeKey(link.ProviderID, link.ExternalID)
	if _, exists := m.linksByExternal[externalKey]; exists {
		return &dberrors.DragonError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.federation.link.create",
			Err:  &LinkExternalExistsError{ExternalID: link.ExternalID},
		}
	}

	// Create a copy
	l := *link
	now := time.Now()
	if l.Created.IsZero() {
		l.Created = now
	}

	m.links[link.ID] = &l
	m.linksByExternal[externalKey] = &l
	m.linksByUser[link.UserID] = append(m.linksByUser[link.UserID], &l)

	return nil
}

// GetLink retrieves a link by ID.
func (m *MemoryFederationStorage) GetLink(ctx context.Context, linkID string) (*FederatedIdentity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	link, exists := m.links[linkID]
	if !exists {
		return nil, ErrLinkNotFound
	}

	// Return a copy
	l := *link
	return &l, nil
}

// GetLinkByExternal retrieves a link by provider and external ID.
func (m *MemoryFederationStorage) GetLinkByExternal(ctx context.Context, providerID, externalID string) (*FederatedIdentity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	externalKey := makeKey(providerID, externalID)
	link, exists := m.linksByExternal[externalKey]
	if !exists {
		return nil, ErrLinkNotFound
	}

	// Return a copy
	l := *link
	return &l, nil
}

// GetLinksByUser retrieves all links for a user.
func (m *MemoryFederationStorage) GetLinksByUser(ctx context.Context, userID string) ([]*FederatedIdentity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	links := m.linksByUser[userID]
	if len(links) == 0 {
		return []*FederatedIdentity{}, nil
	}

	// Return copies
	result := make([]*FederatedIdentity, len(links))
	for i, link := range links {
		l := *link
		result[i] = &l
	}

	return result, nil
}

// UpdateLink updates an existing link.
func (m *MemoryFederationStorage) UpdateLink(ctx context.Context, link *FederatedIdentity) error {
	if err := link.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldLink, exists := m.links[link.ID]
	if !exists {
		return ErrLinkNotFound
	}

	// Handle external ID change
	if oldLink.ExternalID != link.ExternalID || oldLink.ProviderID != link.ProviderID {
		oldExternalKey := makeKey(oldLink.ProviderID, oldLink.ExternalID)
		newExternalKey := makeKey(link.ProviderID, link.ExternalID)

		if _, exists := m.linksByExternal[newExternalKey]; exists && oldExternalKey != newExternalKey {
			return &dberrors.DragonError{
				Code: dberrors.ErrAlreadyExists,
				Op:   "iam.federation.link.update",
				Err:  &LinkExternalExistsError{ExternalID: link.ExternalID},
			}
		}

		delete(m.linksByExternal, oldExternalKey)
		m.linksByExternal[newExternalKey] = link
	}

	// Handle user change
	if oldLink.UserID != link.UserID {
		// Remove from old user's list
		m.removeFromUserLinks(oldLink.UserID, link.ID)
		// Add to new user's list
		m.linksByUser[link.UserID] = append(m.linksByUser[link.UserID], link)
	}

	// Create a copy
	l := *link
	m.links[link.ID] = &l

	// Update in user links slice
	for i, ul := range m.linksByUser[link.UserID] {
		if ul.ID == link.ID {
			m.linksByUser[link.UserID][i] = &l
			break
		}
	}

	// Update in external map
	externalKey := makeKey(link.ProviderID, link.ExternalID)
	m.linksByExternal[externalKey] = &l

	return nil
}

// DeleteLink removes a link.
func (m *MemoryFederationStorage) DeleteLink(ctx context.Context, linkID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	link, exists := m.links[linkID]
	if !exists {
		return ErrLinkNotFound
	}

	externalKey := makeKey(link.ProviderID, link.ExternalID)
	delete(m.links, linkID)
	delete(m.linksByExternal, externalKey)
	m.removeFromUserLinks(link.UserID, linkID)

	return nil
}

// DeleteUserLinks removes all links for a user.
func (m *MemoryFederationStorage) DeleteUserLinks(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	links := m.linksByUser[userID]
	for _, link := range links {
		externalKey := makeKey(link.ProviderID, link.ExternalID)
		delete(m.links, link.ID)
		delete(m.linksByExternal, externalKey)
	}
	delete(m.linksByUser, userID)

	return nil
}

// removeFromUserLinks removes a link from the user's links slice.
// Must be called with the mutex held.
func (m *MemoryFederationStorage) removeFromUserLinks(userID, linkID string) {
	links := m.linksByUser[userID]
	for i, link := range links {
		if link.ID == linkID {
			// Remove by swapping with last element and truncating
			m.linksByUser[userID] = append(links[:i], links[i+1:]...)
			break
		}
	}
	if len(m.linksByUser[userID]) == 0 {
		delete(m.linksByUser, userID)
	}
}

// MemoryStateStorage implements StateStorage using in-memory maps.
type MemoryStateStorage struct {
	mu     sync.RWMutex
	states map[string]*LoginState // key: token
}

// NewMemoryStateStorage creates a new in-memory state storage.
func NewMemoryStateStorage() *MemoryStateStorage {
	return &MemoryStateStorage{
		states: make(map[string]*LoginState),
	}
}

// SaveState stores a callback state.
func (m *MemoryStateStorage) SaveState(ctx context.Context, state *LoginState) error {
	if state.Nonce == "" {
		return ErrStateInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a copy
	s := *state
	m.states[state.Nonce] = &s

	return nil
}

// GetState retrieves a state by token.
func (m *MemoryStateStorage) GetState(ctx context.Context, token string) (*LoginState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.states[token]
	if !exists {
		return nil, ErrStateNotFound
	}

	if state.IsExpired() {
		return nil, ErrStateExpired
	}

	// Return a copy
	s := *state
	return &s, nil
}

// DeleteState removes a state.
func (m *MemoryStateStorage) DeleteState(ctx context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.states, token)
	return nil
}

// CleanupExpired removes all expired states and returns the count.
func (m *MemoryStateStorage) CleanupExpired(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	now := time.Now()
	for token, state := range m.states {
		if now.After(state.ExpiresAt) {
			delete(m.states, token)
			count++
		}
	}

	return count, nil
}

// NewLinkNotFoundError creates a link not found error with context.
func NewLinkNotFoundError(linkID string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.link",
		Err:  &FederatedLinkNotFoundError{LinkID: linkID},
		Context: map[string]interface{}{
			"link_id": linkID,
		},
	}
}

// NewStateExpiredError creates a state expired error with context.
func NewStateExpiredError(token string) error {
	return &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.state",
		Err:  &StateExpiredDetailError{Token: token},
		Context: map[string]interface{}{
			"token": token,
		},
	}
}

// Compile-time interface satisfaction checks.
var (
	_ ProviderStorage   = (*MemoryProviderStorage)(nil)
	_ FederationStorage = (*MemoryFederationStorage)(nil)
	_ StateStorage      = (*MemoryStateStorage)(nil)
)
