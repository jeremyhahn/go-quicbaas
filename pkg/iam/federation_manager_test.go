//go:build iam

package iam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// federationTestAuditLogger is a thread-safe mock audit logger for federation tests.
type federationTestAuditLogger struct {
	mu     sync.Mutex
	events []*AuditEvent
}

func newFederationTestAuditLogger() *federationTestAuditLogger {
	return &federationTestAuditLogger{
		events: make([]*AuditEvent, 0),
	}
}

func (m *federationTestAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *federationTestAuditLogger) LogAsync(event *AuditEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}

func (m *federationTestAuditLogger) Query(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events, len(m.events), nil
}

func (m *federationTestAuditLogger) GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.events {
		if e.ID == eventID && e.TenantID == tenantID {
			return e, nil
		}
	}
	return nil, ErrAuditEventNotFound
}

func (m *federationTestAuditLogger) DeleteOlderThan(ctx context.Context, tenantID string, before time.Time) (int, error) {
	return 0, nil
}

func (m *federationTestAuditLogger) Close() error {
	return nil
}

func (m *federationTestAuditLogger) getEvents() []*AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*AuditEvent{}, m.events...)
}

func (m *federationTestAuditLogger) clearEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = m.events[:0]
}

// errorProviderStorage is a mock that returns errors for testing error paths.
type errorProviderStorage struct {
	createErr      error
	getErr         error
	getByNameErr   error
	updateErr      error
	deleteErr      error
	listErr        error
	provider       *ExternalProvider
	providerByName *ExternalProvider
}

func (e *errorProviderStorage) CreateProvider(ctx context.Context, provider *ExternalProvider) error {
	return e.createErr
}

func (e *errorProviderStorage) GetProvider(ctx context.Context, tenantID, providerID string) (*ExternalProvider, error) {
	if e.getErr != nil {
		return nil, e.getErr
	}
	if e.provider != nil {
		return e.provider, nil
	}
	return nil, ErrProviderNotFound
}

func (e *errorProviderStorage) GetProviderByName(ctx context.Context, tenantID, name string) (*ExternalProvider, error) {
	if e.getByNameErr != nil {
		return nil, e.getByNameErr
	}
	if e.providerByName != nil {
		return e.providerByName, nil
	}
	return nil, ErrProviderNotFound
}

func (e *errorProviderStorage) UpdateProvider(ctx context.Context, provider *ExternalProvider) error {
	return e.updateErr
}

func (e *errorProviderStorage) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	return e.deleteErr
}

func (e *errorProviderStorage) ListProviders(ctx context.Context, tenantID string, opts ListOptions) ([]*ExternalProvider, error) {
	if e.listErr != nil {
		return nil, e.listErr
	}
	return []*ExternalProvider{}, nil
}

// errorFederationStorage is a mock that returns errors for testing error paths.
type errorFederationStorage struct {
	createErr        error
	getErr           error
	getByExternalErr error
	getByUserErr     error
	updateErr        error
	deleteErr        error
	deleteUserErr    error
	link             *FederatedIdentity
	linkByExternal   *FederatedIdentity
	linksByUser      []*FederatedIdentity
}

func (e *errorFederationStorage) CreateLink(ctx context.Context, link *FederatedIdentity) error {
	return e.createErr
}

func (e *errorFederationStorage) GetLink(ctx context.Context, linkID string) (*FederatedIdentity, error) {
	if e.getErr != nil {
		return nil, e.getErr
	}
	if e.link != nil {
		return e.link, nil
	}
	return nil, ErrLinkNotFound
}

func (e *errorFederationStorage) GetLinkByExternal(ctx context.Context, providerID, externalID string) (*FederatedIdentity, error) {
	if e.getByExternalErr != nil {
		return nil, e.getByExternalErr
	}
	if e.linkByExternal != nil {
		return e.linkByExternal, nil
	}
	return nil, ErrLinkNotFound
}

func (e *errorFederationStorage) GetLinksByUser(ctx context.Context, userID string) ([]*FederatedIdentity, error) {
	if e.getByUserErr != nil {
		return nil, e.getByUserErr
	}
	if e.linksByUser != nil {
		return e.linksByUser, nil
	}
	return []*FederatedIdentity{}, nil
}

func (e *errorFederationStorage) UpdateLink(ctx context.Context, link *FederatedIdentity) error {
	return e.updateErr
}

func (e *errorFederationStorage) DeleteLink(ctx context.Context, linkID string) error {
	return e.deleteErr
}

func (e *errorFederationStorage) DeleteUserLinks(ctx context.Context, userID string) error {
	return e.deleteUserErr
}

// errorStateStorage is a mock that returns errors for testing error paths.
type errorStateStorage struct {
	saveErr    error
	getErr     error
	deleteErr  error
	cleanupErr error
	state      *LoginState
}

func (e *errorStateStorage) SaveState(ctx context.Context, state *LoginState) error {
	return e.saveErr
}

func (e *errorStateStorage) GetState(ctx context.Context, token string) (*LoginState, error) {
	if e.getErr != nil {
		return nil, e.getErr
	}
	if e.state != nil {
		return e.state, nil
	}
	return nil, ErrStateNotFound
}

func (e *errorStateStorage) DeleteState(ctx context.Context, token string) error {
	return e.deleteErr
}

func (e *errorStateStorage) CleanupExpired(ctx context.Context) (int, error) {
	return 0, e.cleanupErr
}

// setupTestFederationManager creates a test federation manager with mock dependencies.
func setupTestFederationManager(t *testing.T) (*DefaultFederationManager, *MemoryProviderStorage, *MemoryFederationStorage, *MemoryStateStorage, *IdentityStore, *federationTestAuditLogger) {
	t.Helper()

	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()

	userStorage := newMockUserStorage()
	groupStorage := newMockGroupStorage()
	roleStorage := newMockRoleStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  userStorage,
		GroupStorage: groupStorage,
		RoleStorage:  roleStorage,
	})

	auditLogger := newFederationTestAuditLogger()

	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
		WithFederationAuditLogger(auditLogger),
		WithClientCacheTTL(1*time.Hour),
		WithStateExpiry(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("failed to create federation manager: %v", err)
	}

	return manager, providerStorage, linkStorage, stateStorage, identityStore, auditLogger
}

// createTestProvider creates a valid test provider.
func createTestProvider(tenantID, name string) *ExternalProvider {
	return &ExternalProvider{
		TenantID:      tenantID,
		Name:          name,
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      "test-client-id",
		ClientSecret:  "test-client-secret",
		IssuerURL:     "https://auth.example.com",
		AuthURL:       "https://auth.example.com/authorize",
		TokenURL:      "https://auth.example.com/token",
		UserInfoURL:   "https://auth.example.com/userinfo",
		Scopes:        []string{"openid", "email", "profile"},
		AutoProvision: true,
		AllowLinking:  true,
	}
}

// TestNewFederationManager tests the federation manager constructor.
func TestNewFederationManager(t *testing.T) {
	t.Run("success with all options", func(t *testing.T) {
		providerStorage := NewMemoryProviderStorage()
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		manager, err := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
			WithClientCacheTTL(2*time.Hour),
			WithStateExpiry(10*time.Minute),
		)

		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if manager == nil {
			t.Fatal("expected manager, got nil")
		}
		if manager.cacheTTL != 2*time.Hour {
			t.Errorf("expected cacheTTL 2h, got %v", manager.cacheTTL)
		}
		if manager.stateExpiry != 10*time.Minute {
			t.Errorf("expected stateExpiry 10m, got %v", manager.stateExpiry)
		}
	})

	t.Run("error missing provider storage", func(t *testing.T) {
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		_, err := NewFederationManager(
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
		)

		if err == nil {
			t.Fatal("expected error for missing provider storage")
		}
	})

	t.Run("error missing link storage", func(t *testing.T) {
		providerStorage := NewMemoryProviderStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		_, err := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
		)

		if err == nil {
			t.Fatal("expected error for missing link storage")
		}
	})

	t.Run("error missing state storage", func(t *testing.T) {
		providerStorage := NewMemoryProviderStorage()
		linkStorage := NewMemoryFederationStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		_, err := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithFederationIdentityStore(identityStore),
		)

		if err == nil {
			t.Fatal("expected error for missing state storage")
		}
	})

	t.Run("error missing identity store", func(t *testing.T) {
		providerStorage := NewMemoryProviderStorage()
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()

		_, err := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
		)

		if err == nil {
			t.Fatal("expected error for missing identity store")
		}
	})
}

// TestRegisterProvider tests the RegisterProvider method.
func TestRegisterProvider(t *testing.T) {
	testCases := []struct {
		name        string
		provider    *ExternalProvider
		setup       func(*DefaultFederationManager, *MemoryProviderStorage)
		wantErr     bool
		errContains string
	}{
		{
			name:     "success with valid provider",
			provider: createTestProvider("tenant1", "okta"),
			wantErr:  false,
		},
		{
			name: "success generates ID when empty",
			provider: func() *ExternalProvider {
				p := createTestProvider("tenant1", "google")
				p.ID = ""
				return p
			}(),
			wantErr: false,
		},
		{
			name: "error duplicate provider name",
			provider: func() *ExternalProvider {
				return createTestProvider("tenant1", "okta")
			}(),
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) {
				existing := createTestProvider("tenant1", "okta")
				existing.ID = "existing-id"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, existing)
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "error missing tenant ID",
			provider: func() *ExternalProvider {
				p := createTestProvider("", "provider")
				p.TenantID = ""
				return p
			}(),
			wantErr:     true,
			errContains: "tenant ID",
		},
		{
			name: "error missing provider name",
			provider: func() *ExternalProvider {
				p := createTestProvider("tenant1", "")
				p.Name = ""
				return p
			}(),
			wantErr:     true,
			errContains: "provider name",
		},
		{
			name: "error missing client ID for OIDC",
			provider: func() *ExternalProvider {
				p := createTestProvider("tenant1", "oidc-provider")
				p.ClientID = ""
				return p
			}(),
			wantErr:     true,
			errContains: "client ID",
		},
		{
			name: "error missing issuer URL for OIDC",
			provider: func() *ExternalProvider {
				p := createTestProvider("tenant1", "oidc-provider")
				p.IssuerURL = ""
				return p
			}(),
			wantErr:     true,
			errContains: "issuer URL",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			if tc.setup != nil {
				tc.setup(manager, providerStorage)
			}

			err := manager.RegisterProvider(ctx, tc.provider)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if tc.provider.ID == "" {
					t.Error("expected provider ID to be set")
				}
				if tc.provider.Created.IsZero() {
					t.Error("expected Created timestamp to be set")
				}
				if tc.provider.Updated.IsZero() {
					t.Error("expected Updated timestamp to be set")
				}
			}
		})
	}
}

// TestGetProvider tests the GetProvider method.
func TestGetProvider(t *testing.T) {
	testCases := []struct {
		name        string
		tenantID    string
		providerID  string
		setup       func(*DefaultFederationManager, *MemoryProviderStorage) string
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			tenantID:   "tenant1",
			providerID: "", // Will be set by setup
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) string {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
				return provider.ID
			},
			wantErr: false,
		},
		{
			name:        "error empty tenant ID",
			tenantID:    "",
			providerID:  "provider-123",
			wantErr:     true,
			errContains: "tenant ID and provider ID are required",
		},
		{
			name:        "error empty provider ID",
			tenantID:    "tenant1",
			providerID:  "",
			wantErr:     true,
			errContains: "tenant ID and provider ID are required",
		},
		{
			name:       "error provider not found",
			tenantID:   "tenant1",
			providerID: "nonexistent",
			wantErr:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			providerID := tc.providerID
			if tc.setup != nil {
				providerID = tc.setup(manager, providerStorage)
			}

			provider, err := manager.GetProvider(ctx, tc.tenantID, providerID)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if provider == nil {
					t.Fatal("expected provider, got nil")
				}
				if provider.ID != providerID {
					t.Errorf("expected provider ID %q, got %q", providerID, provider.ID)
				}
			}
		})
	}
}

// TestGetProviderByName tests the GetProviderByName method.
func TestGetProviderByName(t *testing.T) {
	testCases := []struct {
		name         string
		tenantID     string
		providerName string
		setup        func(*DefaultFederationManager, *MemoryProviderStorage)
		wantErr      bool
		errContains  string
	}{
		{
			name:         "success",
			tenantID:     "tenant1",
			providerName: "okta",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
			},
			wantErr: false,
		},
		{
			name:         "error empty tenant ID",
			tenantID:     "",
			providerName: "okta",
			wantErr:      true,
			errContains:  "tenant ID and provider name are required",
		},
		{
			name:         "error empty provider name",
			tenantID:     "tenant1",
			providerName: "",
			wantErr:      true,
			errContains:  "tenant ID and provider name are required",
		},
		{
			name:         "error provider not found",
			tenantID:     "tenant1",
			providerName: "nonexistent",
			wantErr:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			if tc.setup != nil {
				tc.setup(manager, providerStorage)
			}

			provider, err := manager.GetProviderByName(ctx, tc.tenantID, tc.providerName)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if provider == nil {
					t.Fatal("expected provider, got nil")
				}
				if provider.Name != tc.providerName {
					t.Errorf("expected provider name %q, got %q", tc.providerName, provider.Name)
				}
			}
		})
	}
}

// TestUpdateProvider tests the UpdateProvider method.
func TestUpdateProvider(t *testing.T) {
	testCases := []struct {
		name        string
		setup       func(*DefaultFederationManager, *MemoryProviderStorage) *ExternalProvider
		update      func(*ExternalProvider)
		wantErr     bool
		errContains string
	}{
		{
			name: "success update enabled status",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) *ExternalProvider {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
				return provider
			},
			update: func(p *ExternalProvider) {
				p.Enabled = false
			},
			wantErr: false,
		},
		{
			name: "success update name",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) *ExternalProvider {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
				return provider
			},
			update: func(p *ExternalProvider) {
				p.Name = "okta-enterprise"
			},
			wantErr: false,
		},
		{
			name: "error provider not found",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) *ExternalProvider {
				return &ExternalProvider{
					ID:        "nonexistent",
					TenantID:  "tenant1",
					Name:      "test",
					Type:      ProviderTypeOIDC,
					ClientID:  "test-client",
					IssuerURL: "https://example.com",
				}
			},
			wantErr: true,
		},
		{
			name: "error name collision",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) *ExternalProvider {
				ctx := context.Background()
				provider1 := createTestProvider("tenant1", "okta")
				provider1.ID = "provider-1"
				_ = ps.CreateProvider(ctx, provider1)

				provider2 := createTestProvider("tenant1", "google")
				provider2.ID = "provider-2"
				_ = ps.CreateProvider(ctx, provider2)

				return provider2
			},
			update: func(p *ExternalProvider) {
				p.Name = "okta" // Collision with existing provider
			},
			wantErr:     true,
			errContains: "already exists",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			provider := tc.setup(manager, providerStorage)
			if tc.update != nil {
				tc.update(provider)
			}

			err := manager.UpdateProvider(ctx, provider)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestDeleteProvider tests the DeleteProvider method.
func TestDeleteProvider(t *testing.T) {
	testCases := []struct {
		name        string
		tenantID    string
		providerID  string
		setup       func(*DefaultFederationManager, *MemoryProviderStorage) string
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			tenantID:   "tenant1",
			providerID: "",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) string {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
				return provider.ID
			},
			wantErr: false,
		},
		{
			name:        "error empty tenant ID",
			tenantID:    "",
			providerID:  "provider-123",
			wantErr:     true,
			errContains: "tenant ID and provider ID are required",
		},
		{
			name:        "error empty provider ID",
			tenantID:    "tenant1",
			providerID:  "",
			wantErr:     true,
			errContains: "tenant ID and provider ID are required",
		},
		{
			name:       "error provider not found",
			tenantID:   "tenant1",
			providerID: "nonexistent",
			wantErr:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			providerID := tc.providerID
			if tc.setup != nil {
				providerID = tc.setup(manager, providerStorage)
			}

			err := manager.DeleteProvider(ctx, tc.tenantID, providerID)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}

				// Verify provider was deleted
				_, err := manager.GetProvider(ctx, tc.tenantID, providerID)
				if err == nil {
					t.Error("expected provider to be deleted")
				}
			}
		})
	}
}

// TestListProviders tests the ListProviders method.
func TestListProviders(t *testing.T) {
	testCases := []struct {
		name          string
		tenantID      string
		opts          ListOptions
		setup         func(*DefaultFederationManager, *MemoryProviderStorage)
		expectedCount int
		wantErr       bool
		errContains   string
	}{
		{
			name:     "success empty list",
			tenantID: "tenant1",
			opts:     ListOptions{Limit: 10},
			wantErr:  false,
		},
		{
			name:     "success with providers",
			tenantID: "tenant1",
			opts:     ListOptions{Limit: 10},
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) {
				ctx := context.Background()
				for i := 0; i < 3; i++ {
					p := createTestProvider("tenant1", fmt.Sprintf("provider-%d", i))
					p.ID = fmt.Sprintf("id-%d", i)
					_ = ps.CreateProvider(ctx, p)
				}
			},
			expectedCount: 3,
			wantErr:       false,
		},
		{
			name:     "success with pagination",
			tenantID: "tenant1",
			opts:     ListOptions{Offset: 1, Limit: 2},
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) {
				ctx := context.Background()
				for i := 0; i < 5; i++ {
					p := createTestProvider("tenant1", fmt.Sprintf("provider-%d", i))
					p.ID = fmt.Sprintf("id-%d", i)
					_ = ps.CreateProvider(ctx, p)
				}
			},
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name:        "error empty tenant ID",
			tenantID:    "",
			opts:        ListOptions{},
			wantErr:     true,
			errContains: "tenant ID is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			if tc.setup != nil {
				tc.setup(manager, providerStorage)
			}

			providers, err := manager.ListProviders(ctx, tc.tenantID, tc.opts)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if len(providers) != tc.expectedCount {
					t.Errorf("expected %d providers, got %d", tc.expectedCount, len(providers))
				}
			}
		})
	}
}

// TestBeginLogin tests the BeginLogin method.
func TestBeginLogin(t *testing.T) {
	testCases := []struct {
		name         string
		tenantID     string
		providerName string
		redirectURI  string
		setup        func(*DefaultFederationManager, *MemoryProviderStorage)
		wantErr      bool
		errContains  string
	}{
		{
			name:         "success",
			tenantID:     "tenant1",
			providerName: "okta",
			redirectURI:  "https://app.example.com/callback",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
			},
			wantErr: false,
		},
		{
			name:         "error empty tenant ID",
			tenantID:     "",
			providerName: "okta",
			redirectURI:  "https://app.example.com/callback",
			wantErr:      true,
			errContains:  "tenant ID and provider name are required",
		},
		{
			name:         "error empty provider name",
			tenantID:     "tenant1",
			providerName: "",
			redirectURI:  "https://app.example.com/callback",
			wantErr:      true,
			errContains:  "tenant ID and provider name are required",
		},
		{
			name:         "error provider not found",
			tenantID:     "tenant1",
			providerName: "nonexistent",
			redirectURI:  "https://app.example.com/callback",
			wantErr:      true,
		},
		{
			name:         "error provider disabled",
			tenantID:     "tenant1",
			providerName: "disabled-provider",
			redirectURI:  "https://app.example.com/callback",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage) {
				provider := createTestProvider("tenant1", "disabled-provider")
				provider.ID = "provider-123"
				provider.Enabled = false
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			if tc.setup != nil {
				tc.setup(manager, providerStorage)
			}

			result, err := manager.BeginLogin(ctx, tc.tenantID, tc.providerName, tc.redirectURI)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if result == nil {
					t.Fatal("expected result, got nil")
				}
				if result.AuthURL == "" {
					t.Error("expected AuthURL to be set")
				}
				if result.State == "" {
					t.Error("expected State to be set")
				}
				if result.ExpiresAt.IsZero() {
					t.Error("expected ExpiresAt to be set")
				}
				// Verify state contains provider authorization URL
				if !strings.Contains(result.AuthURL, "authorize") {
					t.Error("expected AuthURL to contain authorization endpoint")
				}
			}
		})
	}
}

// TestHandleCallback tests the HandleCallback method.
func TestHandleCallback(t *testing.T) {
	testCases := []struct {
		name        string
		stateToken  string
		authCode    string
		setup       func(*DefaultFederationManager, *MemoryProviderStorage, *MemoryStateStorage) (*httptest.Server, string)
		cleanup     func(*httptest.Server)
		wantErr     bool
		errContains string
	}{
		{
			name:        "error empty state token",
			stateToken:  "",
			authCode:    "auth-code",
			wantErr:     true,
			errContains: "state token and auth code are required",
		},
		{
			name:        "error empty auth code",
			stateToken:  "state-token",
			authCode:    "",
			wantErr:     true,
			errContains: "state token and auth code are required",
		},
		{
			name:       "error state not found",
			stateToken: "invalid-state",
			authCode:   "auth-code",
			wantErr:    true,
		},
		{
			name:       "error state expired",
			stateToken: "expired-state",
			authCode:   "auth-code",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ss *MemoryStateStorage) (*httptest.Server, string) {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)

				// Create expired state
				state := &FederationCallbackState{
					Nonce:       "expired-state",
					ProviderID:  provider.ID,
					TenantID:    "tenant1",
					RedirectURI: "https://app.example.com/callback",
					ExpiresAt:   time.Now().Add(-1 * time.Hour), // Expired
				}
				_ = ss.SaveState(ctx, state)
				return nil, "expired-state"
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, _, stateStorage, _, _ := setupTestFederationManager(t)
			ctx := context.Background()

			var server *httptest.Server
			stateToken := tc.stateToken
			if tc.setup != nil {
				server, stateToken = tc.setup(manager, providerStorage, stateStorage)
			}
			if tc.cleanup != nil && server != nil {
				defer tc.cleanup(server)
			}

			_, err := manager.HandleCallback(ctx, stateToken, tc.authCode)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestLinkAccount tests the LinkAccount method.
func TestLinkAccount(t *testing.T) {
	testCases := []struct {
		name          string
		userID        string
		tenantID      string
		providerID    string
		externalToken string
		setup         func(*DefaultFederationManager, *MemoryProviderStorage, *MemoryFederationStorage, *IdentityStore) (*httptest.Server, string, string)
		cleanup       func(*httptest.Server)
		wantErr       bool
		errContains   string
	}{
		{
			name:          "error empty user ID",
			userID:        "",
			tenantID:      "tenant1",
			providerID:    "provider-123",
			externalToken: "token",
			wantErr:       true,
			errContains:   "user ID, tenant ID, provider ID, and external token are required",
		},
		{
			name:          "error empty tenant ID",
			userID:        "user-123",
			tenantID:      "",
			providerID:    "provider-123",
			externalToken: "token",
			wantErr:       true,
			errContains:   "user ID, tenant ID, provider ID, and external token are required",
		},
		{
			name:          "error empty provider ID",
			userID:        "user-123",
			tenantID:      "tenant1",
			providerID:    "",
			externalToken: "token",
			wantErr:       true,
			errContains:   "user ID, tenant ID, provider ID, and external token are required",
		},
		{
			name:          "error empty external token",
			userID:        "user-123",
			tenantID:      "tenant1",
			providerID:    "provider-123",
			externalToken: "",
			wantErr:       true,
			errContains:   "user ID, tenant ID, provider ID, and external token are required",
		},
		{
			name:          "error user not found",
			userID:        "nonexistent-user",
			tenantID:      "tenant1",
			providerID:    "provider-123",
			externalToken: "token",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) (*httptest.Server, string, string) {
				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				ctx := context.Background()
				_ = ps.CreateProvider(ctx, provider)
				return nil, "nonexistent-user", "provider-123"
			},
			wantErr: true,
		},
		{
			name:          "error provider not found",
			userID:        "user-123",
			tenantID:      "tenant1",
			providerID:    "nonexistent",
			externalToken: "token",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) (*httptest.Server, string, string) {
				ctx := context.Background()
				user, _ := is.CreateUser(ctx, "tenant1", CreateUserOptions{
					ID:       "user-123",
					Email:    "user@example.com",
					Username: "testuser",
				})
				return nil, user.ID, "nonexistent"
			},
			wantErr: true,
		},
		{
			name:          "error account linking disabled",
			userID:        "user-123",
			tenantID:      "tenant1",
			providerID:    "provider-123",
			externalToken: "token",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) (*httptest.Server, string, string) {
				ctx := context.Background()
				user, _ := is.CreateUser(ctx, "tenant1", CreateUserOptions{
					ID:       "user-123",
					Email:    "user@example.com",
					Username: "testuser",
				})

				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				provider.AllowLinking = false // Disable linking
				_ = ps.CreateProvider(ctx, provider)

				return nil, user.ID, provider.ID
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, linkStorage, _, identityStore, _ := setupTestFederationManager(t)
			ctx := context.Background()

			var server *httptest.Server
			userID := tc.userID
			providerID := tc.providerID
			if tc.setup != nil {
				server, userID, providerID = tc.setup(manager, providerStorage, linkStorage, identityStore)
			}
			if tc.cleanup != nil && server != nil {
				defer tc.cleanup(server)
			}

			err := manager.LinkAccount(ctx, userID, tc.tenantID, providerID, tc.externalToken)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestUnlinkAccount tests the UnlinkAccount method.
func TestUnlinkAccount(t *testing.T) {
	testCases := []struct {
		name        string
		userID      string
		tenantID    string
		providerID  string
		setup       func(*DefaultFederationManager, *MemoryProviderStorage, *MemoryFederationStorage, *IdentityStore) (string, string)
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			userID:     "",
			tenantID:   "tenant1",
			providerID: "",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) (string, string) {
				ctx := context.Background()
				user, _ := is.CreateUser(ctx, "tenant1", CreateUserOptions{
					ID:       "user-123",
					Email:    "user@example.com",
					Username: "testuser",
				})

				provider := createTestProvider("tenant1", "okta")
				provider.ID = "provider-123"
				_ = ps.CreateProvider(ctx, provider)

				link := &FederatedIdentity{
					ID:         "link-123",
					UserID:     user.ID,
					TenantID:   "tenant1",
					ProviderID: provider.ID,
					ExternalID: "external-123",
				}
				_ = ls.CreateLink(ctx, link)

				return user.ID, provider.ID
			},
			wantErr: false,
		},
		{
			name:        "error empty user ID",
			userID:      "",
			tenantID:    "tenant1",
			providerID:  "provider-123",
			wantErr:     true,
			errContains: "user ID, tenant ID, and provider ID are required",
		},
		{
			name:        "error empty tenant ID",
			userID:      "user-123",
			tenantID:    "",
			providerID:  "provider-123",
			wantErr:     true,
			errContains: "user ID, tenant ID, and provider ID are required",
		},
		{
			name:        "error empty provider ID",
			userID:      "user-123",
			tenantID:    "tenant1",
			providerID:  "",
			wantErr:     true,
			errContains: "user ID, tenant ID, and provider ID are required",
		},
		{
			name:       "error user not found",
			userID:     "nonexistent",
			tenantID:   "tenant1",
			providerID: "provider-123",
			wantErr:    true,
		},
		{
			name:       "error link not found",
			userID:     "",
			tenantID:   "tenant1",
			providerID: "other-provider",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) (string, string) {
				ctx := context.Background()
				user, _ := is.CreateUser(ctx, "tenant1", CreateUserOptions{
					ID:       "user-123",
					Email:    "user@example.com",
					Username: "testuser",
				})
				return user.ID, "other-provider"
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, linkStorage, _, identityStore, _ := setupTestFederationManager(t)
			ctx := context.Background()

			userID := tc.userID
			providerID := tc.providerID
			if tc.setup != nil {
				userID, providerID = tc.setup(manager, providerStorage, linkStorage, identityStore)
			}

			err := manager.UnlinkAccount(ctx, userID, tc.tenantID, providerID)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}

				// Verify link was removed
				links, _ := manager.GetUserLinks(ctx, userID, tc.tenantID)
				for _, link := range links {
					if link.ProviderID == providerID {
						t.Error("expected link to be removed")
					}
				}
			}
		})
	}
}

// TestGetUserLinks tests the GetUserLinks method.
func TestGetUserLinks(t *testing.T) {
	testCases := []struct {
		name          string
		userID        string
		tenantID      string
		setup         func(*DefaultFederationManager, *MemoryProviderStorage, *MemoryFederationStorage, *IdentityStore) string
		expectedCount int
		wantErr       bool
		errContains   string
	}{
		{
			name:     "success empty links",
			userID:   "",
			tenantID: "tenant1",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) string {
				ctx := context.Background()
				user, _ := is.CreateUser(ctx, "tenant1", CreateUserOptions{
					ID:       "user-123",
					Email:    "user@example.com",
					Username: "testuser",
				})
				return user.ID
			},
			expectedCount: 0,
			wantErr:       false,
		},
		{
			name:     "success with links",
			userID:   "",
			tenantID: "tenant1",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) string {
				ctx := context.Background()
				user, _ := is.CreateUser(ctx, "tenant1", CreateUserOptions{
					ID:       "user-123",
					Email:    "user@example.com",
					Username: "testuser",
				})

				for i := 0; i < 2; i++ {
					link := &FederatedIdentity{
						ID:         fmt.Sprintf("link-%d", i),
						UserID:     user.ID,
						TenantID:   "tenant1",
						ProviderID: fmt.Sprintf("provider-%d", i),
						ExternalID: fmt.Sprintf("external-%d", i),
					}
					_ = ls.CreateLink(ctx, link)
				}

				return user.ID
			},
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name:     "success filters by tenant",
			userID:   "",
			tenantID: "tenant1",
			setup: func(m *DefaultFederationManager, ps *MemoryProviderStorage, ls *MemoryFederationStorage, is *IdentityStore) string {
				ctx := context.Background()
				user, _ := is.CreateUser(ctx, "tenant1", CreateUserOptions{
					ID:       "user-123",
					Email:    "user@example.com",
					Username: "testuser",
				})

				// Create links for different tenants
				link1 := &FederatedIdentity{
					ID:         "link-1",
					UserID:     user.ID,
					TenantID:   "tenant1",
					ProviderID: "provider-1",
					ExternalID: "external-1",
				}
				_ = ls.CreateLink(ctx, link1)

				link2 := &FederatedIdentity{
					ID:         "link-2",
					UserID:     user.ID,
					TenantID:   "tenant2", // Different tenant
					ProviderID: "provider-2",
					ExternalID: "external-2",
				}
				_ = ls.CreateLink(ctx, link2)

				return user.ID
			},
			expectedCount: 1, // Only tenant1 links
			wantErr:       false,
		},
		{
			name:        "error empty user ID",
			userID:      "",
			tenantID:    "tenant1",
			wantErr:     true,
			errContains: "user ID and tenant ID are required",
		},
		{
			name:        "error empty tenant ID",
			userID:      "user-123",
			tenantID:    "",
			wantErr:     true,
			errContains: "user ID and tenant ID are required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, providerStorage, linkStorage, _, identityStore, _ := setupTestFederationManager(t)
			ctx := context.Background()

			userID := tc.userID
			if tc.setup != nil {
				userID = tc.setup(manager, providerStorage, linkStorage, identityStore)
			}

			links, err := manager.GetUserLinks(ctx, userID, tc.tenantID)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if len(links) != tc.expectedCount {
					t.Errorf("expected %d links, got %d", tc.expectedCount, len(links))
				}
			}
		})
	}
}

// TestOAuthClientCache tests the OAuth client caching functionality.
func TestOAuthClientCache(t *testing.T) {
	t.Run("cache hit", func(t *testing.T) {
		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		_ = providerStorage.CreateProvider(ctx, provider)

		// First call should populate cache
		_, err := manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")
		if err != nil {
			t.Fatalf("first BeginLogin failed: %v", err)
		}

		// Verify cache entry exists
		manager.clientCacheMu.RLock()
		_, cached := manager.clientCache[provider.ID]
		manager.clientCacheMu.RUnlock()

		if !cached {
			t.Error("expected OAuth config to be cached")
		}

		// Second call should use cache
		_, err = manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")
		if err != nil {
			t.Fatalf("second BeginLogin failed: %v", err)
		}
	})

	t.Run("cache invalidation on update", func(t *testing.T) {
		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		_ = providerStorage.CreateProvider(ctx, provider)

		// Populate cache
		_, _ = manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")

		// Update provider
		provider.ClientSecret = "new-secret"
		_ = manager.UpdateProvider(ctx, provider)

		// Verify cache was invalidated
		manager.clientCacheMu.RLock()
		_, cached := manager.clientCache[provider.ID]
		manager.clientCacheMu.RUnlock()

		if cached {
			t.Error("expected cache to be invalidated after update")
		}
	})

	t.Run("cache invalidation on delete", func(t *testing.T) {
		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		_ = providerStorage.CreateProvider(ctx, provider)

		// Populate cache
		_, _ = manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")

		// Delete provider
		_ = manager.DeleteProvider(ctx, "tenant1", provider.ID)

		// Verify cache was invalidated
		manager.clientCacheMu.RLock()
		_, cached := manager.clientCache[provider.ID]
		manager.clientCacheMu.RUnlock()

		if cached {
			t.Error("expected cache to be invalidated after delete")
		}
	})
}

// TestOIDCDiscovery tests the OIDC discovery functionality.
func TestOIDCDiscovery(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		discoveryDoc := &OIDCDiscoveryDocument{
			Issuer:                "https://auth.example.com",
			AuthorizationEndpoint: "https://auth.example.com/authorize",
			TokenEndpoint:         "https://auth.example.com/token",
			UserinfoEndpoint:      "https://auth.example.com/userinfo",
			JwksURI:               "https://auth.example.com/.well-known/jwks.json",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/openid-configuration" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(discoveryDoc)
			}
		}))
		defer server.Close()

		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		manager.httpClient = server.Client()
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		provider.IssuerURL = server.URL
		provider.AuthURL = ""  // Clear to trigger discovery
		provider.TokenURL = "" // Clear to trigger discovery
		_ = providerStorage.CreateProvider(ctx, provider)

		// This should trigger OIDC discovery
		result, err := manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}
	})

	t.Run("error non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		manager.httpClient = server.Client()
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		provider.IssuerURL = server.URL
		provider.AuthURL = ""
		provider.TokenURL = ""
		_ = providerStorage.CreateProvider(ctx, provider)

		_, err := manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")
		if err == nil {
			t.Fatal("expected error for non-200 status")
		}
	})

	t.Run("error invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		manager, providerStorage, _, _, _, _ := setupTestFederationManager(t)
		manager.httpClient = server.Client()
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		provider.IssuerURL = server.URL
		provider.AuthURL = ""
		provider.TokenURL = ""
		_ = providerStorage.CreateProvider(ctx, provider)

		_, err := manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

// TestExtractClaim tests the extractClaim helper method.
func TestExtractClaim(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)

	testCases := []struct {
		name       string
		claims     map[string]interface{}
		mappedKey  string
		defaultKey string
		expected   string
	}{
		{
			name:       "string value with mapped key",
			claims:     map[string]interface{}{"custom_sub": "user-123"},
			mappedKey:  "custom_sub",
			defaultKey: "sub",
			expected:   "user-123",
		},
		{
			name:       "string value with default key",
			claims:     map[string]interface{}{"sub": "user-123"},
			mappedKey:  "",
			defaultKey: "sub",
			expected:   "user-123",
		},
		{
			name:       "float64 value",
			claims:     map[string]interface{}{"id": float64(12345)},
			mappedKey:  "id",
			defaultKey: "sub",
			expected:   "12345",
		},
		{
			name:       "missing claim",
			claims:     map[string]interface{}{"other": "value"},
			mappedKey:  "missing",
			defaultKey: "also_missing",
			expected:   "",
		},
		{
			name:       "other type value",
			claims:     map[string]interface{}{"bool_val": true},
			mappedKey:  "bool_val",
			defaultKey: "",
			expected:   "true",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := manager.extractClaim(tc.claims, tc.mappedKey, tc.defaultKey)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestDecodeJWTPayload tests the post-verification payload decoder.
func TestDecodeJWTPayload(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		// Create a valid JWT structure (header.payload.signature)
		claims := map[string]interface{}{
			"sub":   "user-123",
			"email": "user@example.com",
			"name":  "Test User",
		}
		payload, _ := json.Marshal(claims)
		encodedPayload := base64.RawURLEncoding.EncodeToString(payload)

		idToken := fmt.Sprintf("eyJhbGciOiJSUzI1NiJ9.%s.signature", encodedPayload)

		result, err := decodeJWTPayload(idToken)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if result["sub"] != "user-123" {
			t.Errorf("expected sub=user-123, got %v", result["sub"])
		}
	})

	t.Run("error invalid format", func(t *testing.T) {
		_, err := decodeJWTPayload("invalid-token")
		if err == nil {
			t.Fatal("expected error for invalid token format")
		}
	})

	t.Run("error invalid base64", func(t *testing.T) {
		_, err := decodeJWTPayload("header.!!!invalid-base64!!!.signature")
		if err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})

	t.Run("error invalid JSON", func(t *testing.T) {
		invalidPayload := base64.RawURLEncoding.EncodeToString([]byte("not json"))
		_, err := decodeJWTPayload(fmt.Sprintf("header.%s.signature", invalidPayload))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

// TestAuditLogging tests that audit events are logged correctly.
func TestAuditLogging(t *testing.T) {
	t.Run("register provider logs event", func(t *testing.T) {
		manager, _, _, _, _, auditLogger := setupTestFederationManager(t)
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		_ = manager.RegisterProvider(ctx, provider)

		events := auditLogger.getEvents()
		if len(events) == 0 {
			t.Fatal("expected audit event to be logged")
		}

		found := false
		for _, e := range events {
			if e.Action == "register" && e.Resource.Type == "federation_provider" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected register event for federation_provider")
		}
	})

	t.Run("update provider logs event", func(t *testing.T) {
		manager, providerStorage, _, _, _, auditLogger := setupTestFederationManager(t)
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		_ = providerStorage.CreateProvider(ctx, provider)

		// Clear existing events
		auditLogger.clearEvents()

		provider.Enabled = false
		_ = manager.UpdateProvider(ctx, provider)

		events := auditLogger.getEvents()
		found := false
		for _, e := range events {
			if e.Action == "update" && e.Resource.Type == "federation_provider" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected update event for federation_provider")
		}
	})

	t.Run("delete provider logs event", func(t *testing.T) {
		manager, providerStorage, _, _, _, auditLogger := setupTestFederationManager(t)
		ctx := context.Background()

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		_ = providerStorage.CreateProvider(ctx, provider)

		// Clear existing events
		auditLogger.clearEvents()

		_ = manager.DeleteProvider(ctx, "tenant1", provider.ID)

		events := auditLogger.getEvents()
		found := false
		for _, e := range events {
			if e.Action == "delete" && e.Resource.Type == "federation_provider" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected delete event for federation_provider")
		}
	})

	t.Run("unlink account logs event", func(t *testing.T) {
		manager, providerStorage, linkStorage, _, identityStore, auditLogger := setupTestFederationManager(t)
		ctx := context.Background()

		user, _ := identityStore.CreateUser(ctx, "tenant1", CreateUserOptions{
			ID:       "user-123",
			Email:    "user@example.com",
			Username: "testuser",
		})

		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		_ = providerStorage.CreateProvider(ctx, provider)

		link := &FederatedIdentity{
			ID:         "link-123",
			UserID:     user.ID,
			TenantID:   "tenant1",
			ProviderID: provider.ID,
			ExternalID: "external-123",
		}
		_ = linkStorage.CreateLink(ctx, link)

		// Clear existing events
		auditLogger.clearEvents()

		_ = manager.UnlinkAccount(ctx, user.ID, "tenant1", provider.ID)

		events := auditLogger.getEvents()
		found := false
		for _, e := range events {
			if e.Action == "federation_unlink" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected federation_unlink event")
		}
	})
}

// TestGenerateSecureNonce tests the generateSecureNonce function.
func TestGenerateSecureNonce(t *testing.T) {
	t.Run("generates correct length", func(t *testing.T) {
		nonce, err := generateSecureNonce(32)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		// Base64 encoded 32 bytes should be ~43 characters
		if len(nonce) < 40 {
			t.Errorf("expected nonce length >= 40, got %d", len(nonce))
		}
	})

	t.Run("generates unique values", func(t *testing.T) {
		nonces := make(map[string]bool)
		for i := 0; i < 100; i++ {
			nonce, err := generateSecureNonce(32)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if nonces[nonce] {
				t.Fatal("generated duplicate nonce")
			}
			nonces[nonce] = true
		}
	})
}

// TestFederationManagerWithErrorStorage tests error handling with failing storage.
func TestFederationManagerWithErrorStorage(t *testing.T) {
	t.Run("register provider storage error", func(t *testing.T) {
		providerStorage := &errorProviderStorage{
			createErr:    errors.New("storage error"),
			getByNameErr: ErrProviderNotFound,
		}
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		manager, _ := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
		)

		ctx := context.Background()
		provider := createTestProvider("tenant1", "okta")

		err := manager.RegisterProvider(ctx, provider)
		if err == nil {
			t.Fatal("expected error from storage")
		}
	})

	t.Run("get provider storage error", func(t *testing.T) {
		storageErr := &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Err:  errors.New("storage error"),
		}
		providerStorage := &errorProviderStorage{
			getErr: storageErr,
		}
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		manager, _ := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
		)

		ctx := context.Background()
		_, err := manager.GetProvider(ctx, "tenant1", "provider-123")
		if err == nil {
			t.Fatal("expected error from storage")
		}
	})

	t.Run("list providers storage error", func(t *testing.T) {
		providerStorage := &errorProviderStorage{
			listErr: errors.New("storage error"),
		}
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		manager, _ := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
		)

		ctx := context.Background()
		_, err := manager.ListProviders(ctx, "tenant1", ListOptions{})
		if err == nil {
			t.Fatal("expected error from storage")
		}
	})

	t.Run("begin login state storage error", func(t *testing.T) {
		provider := createTestProvider("tenant1", "okta")
		provider.ID = "provider-123"
		providerStorage := &errorProviderStorage{
			providerByName: provider,
		}
		linkStorage := NewMemoryFederationStorage()
		stateStorage := &errorStateStorage{
			saveErr: errors.New("storage error"),
		}
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		manager, _ := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
		)

		ctx := context.Background()
		_, err := manager.BeginLogin(ctx, "tenant1", "okta", "https://app.example.com/callback")
		if err == nil {
			t.Fatal("expected error from state storage")
		}
	})

	t.Run("get user links storage error", func(t *testing.T) {
		providerStorage := NewMemoryProviderStorage()
		linkStorage := &errorFederationStorage{
			getByUserErr: errors.New("storage error"),
		}
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		manager, _ := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
		)

		ctx := context.Background()
		_, err := manager.GetUserLinks(ctx, "user-123", "tenant1")
		if err == nil {
			t.Fatal("expected error from link storage")
		}
	})
}

// TestFederationManagerWithNilAuditLogger tests that operations work without audit logger.
func TestFederationManagerWithNilAuditLogger(t *testing.T) {
	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
	})

	manager, _ := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
		// No audit logger
	)

	ctx := context.Background()
	provider := createTestProvider("tenant1", "okta")

	// Should not panic when logging without audit logger
	err := manager.RegisterProvider(ctx, provider)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	provider.Enabled = false
	err = manager.UpdateProvider(ctx, provider)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	err = manager.DeleteProvider(ctx, "tenant1", provider.ID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestFederationManagerOptions tests individual option functions.
func TestFederationManagerOptions(t *testing.T) {
	t.Run("WithFederationHTTPClient", func(t *testing.T) {
		client := &http.Client{Timeout: 30 * time.Second}
		providerStorage := NewMemoryProviderStorage()
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		manager, _ := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
			WithFederationHTTPClient(client),
		)

		if manager.httpClient != client {
			t.Error("expected custom HTTP client to be set")
		}
	})

	t.Run("WithFederationOIDCProvider", func(t *testing.T) {
		providerStorage := NewMemoryProviderStorage()
		linkStorage := NewMemoryFederationStorage()
		stateStorage := NewMemoryStateStorage()
		identityStore := NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})

		oidcProvider := &OIDCProvider{} // Mock OIDC provider

		manager, _ := NewFederationManager(
			WithProviderStorage(providerStorage),
			WithLinkStorage(linkStorage),
			WithStateStorage(stateStorage),
			WithFederationIdentityStore(identityStore),
			WithFederationOIDCProvider(oidcProvider),
		)

		if manager.oidcProvider != oidcProvider {
			t.Error("expected OIDC provider to be set")
		}
	})
}

// TestExternalProviderValidation tests ExternalProvider.Validate method.
func TestExternalProviderValidation(t *testing.T) {
	testCases := []struct {
		name        string
		provider    *ExternalProvider
		wantErr     bool
		errContains string
	}{
		{
			name: "valid OIDC provider",
			provider: &ExternalProvider{
				ID:        "provider-123",
				TenantID:  "tenant1",
				Name:      "okta",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-id",
				IssuerURL: "https://auth.example.com",
			},
			wantErr: false,
		},
		{
			name: "valid OAuth2 provider without issuer",
			provider: &ExternalProvider{
				ID:       "provider-123",
				TenantID: "tenant1",
				Name:     "custom-oauth",
				Type:     ProviderTypeOAuth2,
				ClientID: "client-id",
				AuthURL:  "https://auth.example.com/authorize",
				TokenURL: "https://auth.example.com/token",
			},
			wantErr: false,
		},
		{
			name: "error missing ID",
			provider: &ExternalProvider{
				TenantID: "tenant1",
				Name:     "okta",
				Type:     ProviderTypeOIDC,
			},
			wantErr:     true,
			errContains: "provider ID",
		},
		{
			name: "error missing tenant ID",
			provider: &ExternalProvider{
				ID:   "provider-123",
				Name: "okta",
				Type: ProviderTypeOIDC,
			},
			wantErr:     true,
			errContains: "tenant ID",
		},
		{
			name: "error missing name",
			provider: &ExternalProvider{
				ID:       "provider-123",
				TenantID: "tenant1",
				Type:     ProviderTypeOIDC,
			},
			wantErr:     true,
			errContains: "provider name",
		},
		{
			name: "error missing type",
			provider: &ExternalProvider{
				ID:       "provider-123",
				TenantID: "tenant1",
				Name:     "okta",
			},
			wantErr:     true,
			errContains: "provider type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.provider.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestExternalProviderApplyDefaults tests ExternalProvider.ApplyDefaults method.
func TestExternalProviderApplyDefaults(t *testing.T) {
	t.Run("applies defaults to empty provider", func(t *testing.T) {
		provider := &ExternalProvider{}
		provider.ApplyDefaults()

		if provider.Type != ProviderTypeOIDC {
			t.Errorf("expected type OIDC, got %v", provider.Type)
		}
		if len(provider.Scopes) != 3 {
			t.Errorf("expected 3 scopes, got %d", len(provider.Scopes))
		}
		if provider.ClaimMappings == nil {
			t.Error("expected ClaimMappings to be initialized")
		}
		if provider.ClaimMappings.Subject != DefaultSubjectClaim {
			t.Errorf("expected Subject=%s, got %s", DefaultSubjectClaim, provider.ClaimMappings.Subject)
		}
	})

	t.Run("preserves existing values", func(t *testing.T) {
		provider := &ExternalProvider{
			Type:   ProviderTypeSAML,
			Scopes: []string{"custom"},
			ClaimMappings: &ClaimMappings{
				Subject: "custom_sub",
			},
		}
		provider.ApplyDefaults()

		if provider.Type != ProviderTypeSAML {
			t.Error("expected type to be preserved")
		}
		if len(provider.Scopes) != 1 || provider.Scopes[0] != "custom" {
			t.Error("expected scopes to be preserved")
		}
		if provider.ClaimMappings.Subject != "custom_sub" {
			t.Error("expected subject claim to be preserved")
		}
	})
}

// TestFederatedIdentityValidation tests FederatedIdentity.Validate method.
func TestFederatedIdentityValidation(t *testing.T) {
	testCases := []struct {
		name        string
		identity    *FederatedIdentity
		wantErr     bool
		errContains string
	}{
		{
			name: "valid identity",
			identity: &FederatedIdentity{
				ID:         "link-123",
				UserID:     "user-123",
				TenantID:   "tenant1",
				ProviderID: "provider-123",
				ExternalID: "external-123",
			},
			wantErr: false,
		},
		{
			name: "error missing ID",
			identity: &FederatedIdentity{
				UserID:     "user-123",
				TenantID:   "tenant1",
				ProviderID: "provider-123",
				ExternalID: "external-123",
			},
			wantErr:     true,
			errContains: "identity ID",
		},
		{
			name: "error missing user ID",
			identity: &FederatedIdentity{
				ID:         "link-123",
				TenantID:   "tenant1",
				ProviderID: "provider-123",
				ExternalID: "external-123",
			},
			wantErr:     true,
			errContains: "user ID",
		},
		{
			name: "error missing tenant ID",
			identity: &FederatedIdentity{
				ID:         "link-123",
				UserID:     "user-123",
				ProviderID: "provider-123",
				ExternalID: "external-123",
			},
			wantErr:     true,
			errContains: "tenant ID",
		},
		{
			name: "error missing provider ID",
			identity: &FederatedIdentity{
				ID:         "link-123",
				UserID:     "user-123",
				TenantID:   "tenant1",
				ExternalID: "external-123",
			},
			wantErr:     true,
			errContains: "provider ID",
		},
		{
			name: "error missing external ID",
			identity: &FederatedIdentity{
				ID:         "link-123",
				UserID:     "user-123",
				TenantID:   "tenant1",
				ProviderID: "provider-123",
			},
			wantErr:     true,
			errContains: "external ID",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.identity.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestFederationCallbackStateValidation tests FederationCallbackState.Validate method.
func TestFederationCallbackStateValidation(t *testing.T) {
	testCases := []struct {
		name        string
		state       *FederationCallbackState
		wantErr     bool
		errContains string
	}{
		{
			name: "valid state",
			state: &FederationCallbackState{
				Nonce:       "nonce-123",
				ProviderID:  "provider-123",
				TenantID:    "tenant1",
				RedirectURI: "https://app.example.com/callback",
			},
			wantErr: false,
		},
		{
			name: "error missing nonce",
			state: &FederationCallbackState{
				ProviderID:  "provider-123",
				TenantID:    "tenant1",
				RedirectURI: "https://app.example.com/callback",
			},
			wantErr:     true,
			errContains: "nonce",
		},
		{
			name: "error missing provider ID",
			state: &FederationCallbackState{
				Nonce:       "nonce-123",
				TenantID:    "tenant1",
				RedirectURI: "https://app.example.com/callback",
			},
			wantErr:     true,
			errContains: "provider ID",
		},
		{
			name: "error missing tenant ID",
			state: &FederationCallbackState{
				Nonce:       "nonce-123",
				ProviderID:  "provider-123",
				RedirectURI: "https://app.example.com/callback",
			},
			wantErr:     true,
			errContains: "tenant ID",
		},
		{
			name: "error missing redirect URI",
			state: &FederationCallbackState{
				Nonce:      "nonce-123",
				ProviderID: "provider-123",
				TenantID:   "tenant1",
			},
			wantErr:     true,
			errContains: "redirect URI",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.state.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestFederationCallbackStateIsExpired tests FederationCallbackState.IsExpired method.
func TestFederationCallbackStateIsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		state := &FederationCallbackState{
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}
		if state.IsExpired() {
			t.Error("expected state to not be expired")
		}
	})

	t.Run("expired", func(t *testing.T) {
		state := &FederationCallbackState{
			ExpiresAt: time.Now().Add(-1 * time.Minute),
		}
		if !state.IsExpired() {
			t.Error("expected state to be expired")
		}
	})
}

// TestClaimMappingsApplyDefaults tests ClaimMappings.ApplyDefaults method.
func TestClaimMappingsApplyDefaults(t *testing.T) {
	t.Run("applies defaults to empty mappings", func(t *testing.T) {
		mappings := &ClaimMappings{}
		mappings.ApplyDefaults()

		if mappings.Subject != DefaultSubjectClaim {
			t.Errorf("expected Subject=%s, got %s", DefaultSubjectClaim, mappings.Subject)
		}
		if mappings.Email != DefaultEmailClaim {
			t.Errorf("expected Email=%s, got %s", DefaultEmailClaim, mappings.Email)
		}
		if mappings.Name != DefaultNameClaim {
			t.Errorf("expected Name=%s, got %s", DefaultNameClaim, mappings.Name)
		}
	})

	t.Run("preserves existing values", func(t *testing.T) {
		mappings := &ClaimMappings{
			Subject: "custom_sub",
			Email:   "custom_email",
			Name:    "custom_name",
		}
		mappings.ApplyDefaults()

		if mappings.Subject != "custom_sub" {
			t.Error("expected subject to be preserved")
		}
		if mappings.Email != "custom_email" {
			t.Error("expected email to be preserved")
		}
		if mappings.Name != "custom_name" {
			t.Error("expected name to be preserved")
		}
	})
}

// =============================================================================
// Federation: extractClaims tests
// =============================================================================

// makeIDToken builds a fake JWT (header.payload.signature) with the given claims.
func makeIDToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	payloadBytes, err := json.Marshal(claims)
	require.NoError(t, err)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return header + "." + payload + "." + signature
}

// makeOAuth2Token creates an oauth2.Token with the given access token and optional id_token extra.
func makeOAuth2Token(accessToken, idToken string) *oauth2.Token {
	token := &oauth2.Token{AccessToken: accessToken}
	if idToken != "" {
		token = token.WithExtra(map[string]interface{}{
			"id_token": idToken,
		})
	}
	return token
}

func TestExtractClaims_UnsignedIDTokenRejected(t *testing.T) {
	// Regression: the legacy parseIDToken base64-decoded the ID-token payload
	// WITHOUT verifying the signature. extractClaims must now fail closed when
	// the ID token cannot be verified against the provider JWKS — there is no
	// fall-through to an unverified payload.
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"
	// No JWKS server is reachable, so any id_token must be rejected.
	provider.JWKSURL = "http://127.0.0.1:0/jwks"

	claims := map[string]interface{}{
		"sub":   "user-123",
		"email": "user@example.com",
		"name":  "Test User",
	}
	idToken := makeIDToken(t, claims)
	token := makeOAuth2Token("test-access-token", idToken)

	_, err := manager.extractClaims(ctx, provider, token, nil)
	require.Error(t, err, "unverified ID token must be rejected (fail closed)")
}

func TestExtractClaims_NoIDToken_FetchesUserInfo(t *testing.T) {
	userInfoClaims := map[string]interface{}{
		"sub":   "ext-user-456",
		"email": "ext@example.com",
		"name":  "External User",
	}
	userInfoJSON, err := json.Marshal(userInfoClaims)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(userInfoJSON)
		assert.NoError(t, writeErr)
	}))
	defer server.Close()

	manager, _, _, _, _, _ := setupTestFederationManager(t)
	manager.httpClient = server.Client()
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"
	provider.UserInfoURL = server.URL

	token := makeOAuth2Token("test-access-token", "")

	result, err := manager.extractClaims(ctx, provider, token, nil)
	require.NoError(t, err)
	assert.Equal(t, "ext-user-456", result["sub"])
	assert.Equal(t, "ext@example.com", result["email"])
}

func TestExtractClaims_NoUserInfoURL_Error(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-no-userinfo"
	provider.UserInfoURL = ""

	token := makeOAuth2Token("test-access-token", "")

	_, err := manager.extractClaims(ctx, provider, token, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no userinfo endpoint available")
}

func TestExtractClaims_UserInfoBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	manager, _, _, _, _, _ := setupTestFederationManager(t)
	manager.httpClient = server.Client()
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"
	provider.UserInfoURL = server.URL

	token := makeOAuth2Token("test-access-token", "")

	_, err := manager.extractClaims(ctx, provider, token, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "userinfo returned status")
}

func TestExtractClaims_UserInfoInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write([]byte("not json"))
		assert.NoError(t, writeErr)
	}))
	defer server.Close()

	manager, _, _, _, _, _ := setupTestFederationManager(t)
	manager.httpClient = server.Client()
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"
	provider.UserInfoURL = server.URL

	token := makeOAuth2Token("test-access-token", "")

	_, err := manager.extractClaims(ctx, provider, token, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse userinfo response")
}

// =============================================================================
// Federation: processAuthentication tests
// =============================================================================

func TestProcessAuthentication_ExistingLink(t *testing.T) {
	manager, _, linkStorage, _, identityStore, _ := setupTestFederationManager(t)
	ctx := context.Background()

	user, err := identityStore.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "existing@example.com",
		Username: "existing-user",
		Name:     "Existing User",
		Enabled:  true,
	})
	require.NoError(t, err)

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"

	err = linkStorage.CreateLink(ctx, &FederatedIdentity{
		ID:         "link-1",
		UserID:     user.ID,
		TenantID:   "tenant1",
		ProviderID: provider.ID,
		ExternalID: "ext-user-1",
		Created:    time.Now(),
		Updated:    time.Now(),
	})
	require.NoError(t, err)

	claims := map[string]interface{}{
		"sub":            "ext-user-1",
		"email":          "existing@example.com",
		"email_verified": true,
		"name":           "Existing User",
	}

	state := &FederationCallbackState{
		Nonce:      "test-nonce",
		ProviderID: provider.ID,
		TenantID:   "tenant1",
	}

	result, err := manager.processAuthentication(ctx, provider, claims, state)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, user.ID, result.User.ID)
	assert.False(t, result.Provisioned)
}

func TestProcessAuthentication_LinkByEmail(t *testing.T) {
	manager, _, _, _, identityStore, _ := setupTestFederationManager(t)
	ctx := context.Background()

	user, err := identityStore.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "linkable@example.com",
		Username: "linkable-user",
		Name:     "Linkable User",
		Enabled:  true,
	})
	require.NoError(t, err)

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-link"
	provider.AllowLinking = true

	claims := map[string]interface{}{
		"sub":            "new-ext-id",
		"email":          "linkable@example.com",
		"email_verified": true,
		"name":           "Linkable User",
	}

	state := &FederationCallbackState{
		Nonce:      "test-nonce",
		ProviderID: provider.ID,
		TenantID:   "tenant1",
	}

	result, err := manager.processAuthentication(ctx, provider, claims, state)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, user.ID, result.User.ID)
	assert.False(t, result.Provisioned)
}

func TestProcessAuthentication_JITProvision(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-jit"
	provider.AutoProvision = true
	provider.AllowLinking = false

	claims := map[string]interface{}{
		"sub":            "brand-new-user",
		"email":          "newuser@example.com",
		"email_verified": true,
		"name":           "New User",
	}

	state := &FederationCallbackState{
		Nonce:      "test-nonce",
		ProviderID: provider.ID,
		TenantID:   "tenant1",
	}

	result, err := manager.processAuthentication(ctx, provider, claims, state)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Provisioned)
	assert.NotEmpty(t, result.User.ID)
}

func TestProcessAuthentication_NoExternalID(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"

	claims := map[string]interface{}{
		"email": "test@example.com",
	}

	state := &FederationCallbackState{
		Nonce:      "test-nonce",
		ProviderID: provider.ID,
		TenantID:   "tenant1",
	}

	_, err := manager.processAuthentication(ctx, provider, claims, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "external ID not found")
}

func TestProcessAuthentication_JITDisabled(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-no-jit"
	provider.AutoProvision = false
	provider.AllowLinking = false

	claims := map[string]interface{}{
		"sub":   "unknown-user",
		"email": "unknown@example.com",
	}

	state := &FederationCallbackState{
		Nonce:      "test-nonce",
		ProviderID: provider.ID,
		TenantID:   "tenant1",
	}

	_, err := manager.processAuthentication(ctx, provider, claims, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JIT provisioning is disabled")
}

// =============================================================================
// Federation: provisionUser tests
// =============================================================================

func TestProvisionUser_Success(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"

	user, err := manager.provisionUser(ctx, provider, "tenant1", "ext-1", "provisioned@example.com", "Provisioned User", nil)
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.NotEmpty(t, user.ID)
	assert.Equal(t, "Provisioned User", user.Name)
	assert.Equal(t, "provisioned@example.com", user.Email)
}

func TestProvisionUser_NoEmail_Error(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"

	// CreateUser requires a non-empty email, so provisionUser with empty email must fail
	user, err := manager.provisionUser(ctx, provider, "tenant1", "ext-no-email", "", "No Email User", nil)
	require.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "failed to create user")
}

func TestProvisionUser_LongUsername_Truncated(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"

	longEmail := ""
	for i := 0; i < 80; i++ {
		longEmail += "a"
	}
	longEmail += "@example.com"

	user, err := manager.provisionUser(ctx, provider, "tenant1", "ext-long", longEmail, "Long Username User", nil)
	require.NoError(t, err)
	require.NotNil(t, user)
	// The user was created successfully despite the long username being truncated internally
	assert.NotEmpty(t, user.ID)
}

func TestProvisionUser_WithDefaultRolesAndGroups(t *testing.T) {
	manager, _, _, _, identityStore, _ := setupTestFederationManager(t)
	ctx := context.Background()

	role, err := identityStore.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name:        "default-role",
		Description: "A default role",
	})
	require.NoError(t, err)

	group, err := identityStore.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name:        "default-group",
		Description: "A default group",
	})
	require.NoError(t, err)

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-with-defaults"
	provider.DefaultRoles = []string{role.ID}
	provider.DefaultGroups = []string{group.ID}

	user, err := manager.provisionUser(ctx, provider, "tenant1", "ext-with-defaults", "defaults@example.com", "Defaults User", nil)
	require.NoError(t, err)
	require.NotNil(t, user)
}

// =============================================================================
// Federation: generateTokenResult tests
// =============================================================================

func TestGenerateTokenResult_WithoutOIDCProvider(t *testing.T) {
	manager, _, _, _, identityStore, _ := setupTestFederationManager(t)
	ctx := context.Background()

	user, err := identityStore.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "tokenuser@example.com",
		Username: "tokenuser",
		Name:     "Token User",
		Enabled:  true,
	})
	require.NoError(t, err)

	identity := &FederatedIdentity{
		ID:         "identity-1",
		UserID:     user.ID,
		TenantID:   "tenant1",
		ProviderID: "provider-1",
		ExternalID: "ext-1",
	}

	result, err := manager.generateTokenResult(ctx, user, identity, false, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, user.ID, result.User.ID)
	assert.Empty(t, result.AccessToken) // No OIDC provider set
	assert.False(t, result.Provisioned)
}

func TestGenerateTokenResult_Provisioned(t *testing.T) {
	manager, _, _, _, identityStore, _ := setupTestFederationManager(t)
	ctx := context.Background()

	user, err := identityStore.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "provuser@example.com",
		Username: "provuser",
		Name:     "Provisioned User",
		Enabled:  true,
	})
	require.NoError(t, err)

	identity := &FederatedIdentity{
		ID:         "identity-2",
		UserID:     user.ID,
		TenantID:   "tenant1",
		ProviderID: "provider-1",
		ExternalID: "ext-2",
	}

	result, err := manager.generateTokenResult(ctx, user, identity, true, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Provisioned)
}

// =============================================================================
// Federation: logAuthEvent tests
// =============================================================================

func TestLogAuthEvent_Success(t *testing.T) {
	manager, _, _, _, _, auditLogger := setupTestFederationManager(t)
	ctx := context.Background()

	user := &User{ID: "user-1", Name: "Test User", Email: "test@example.com"}
	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"

	auditLogger.clearEvents()
	manager.logAuthEvent(ctx, "tenant1", user, provider, "login", true, nil)

	events := auditLogger.getEvents()
	require.Len(t, events, 1)
	assert.Equal(t, AuditEventAuthentication, events[0].EventType)
	assert.Equal(t, "federation_login", events[0].Action)
	assert.Equal(t, AuditOutcomeSuccess, events[0].Outcome)
}

func TestLogAuthEvent_Failure_WithDetails(t *testing.T) {
	manager, _, _, _, _, auditLogger := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-1"

	details := map[string]interface{}{"error": "token exchange failed"}
	auditLogger.clearEvents()
	manager.logAuthEvent(ctx, "tenant1", nil, provider, "callback", false, details)

	events := auditLogger.getEvents()
	require.Len(t, events, 1)
	assert.Equal(t, AuditOutcomeFailure, events[0].Outcome)
	assert.Nil(t, events[0].Actor) // nil user means nil actor
	assert.Equal(t, "provider-1", events[0].Details["provider_id"])
}

func TestLogAuthEvent_NilAuditLogger(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	manager.auditLogger = nil
	ctx := context.Background()

	// Should not panic
	manager.logAuthEvent(ctx, "tenant1", nil, nil, "test", true, nil)
}

// =============================================================================
// GDPR: markExportFailed tests
// =============================================================================

func TestMarkExportFailed_Success(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant-mark-export"
	userID := "user-mark-export"

	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	exportReq, err := manager.RequestExport(ctx, tenantID, userID, userID, "json", nil, nil)
	require.NoError(t, err)

	manager.markExportFailed(exportReq.ID, "test failure reason")

	updated, err := manager.GetExportStatus(ctx, exportReq.ID)
	require.NoError(t, err)
	assert.Equal(t, DataExportStatusFailed, updated.Status)
	assert.Equal(t, "test failure reason", updated.ErrorMessage)
	assert.NotNil(t, updated.CompletedAt)
}

func TestMarkExportFailed_NonexistentExport(t *testing.T) {
	manager, _ := createTestGDPRManager(t)

	// Should not panic on a nonexistent export
	manager.markExportFailed("nonexistent-export-id", "should not crash")
}

// =============================================================================
// GDPR: markDeletionFailed tests
// =============================================================================

func TestMarkDeletionFailed_Success(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	tenantID := "tenant-mark-deletion"
	userID := "user-mark-deletion"

	createTestUser(t, manager.deps.UserStorage, tenantID, userID)

	delReq, err := manager.RequestDeletion(ctx, tenantID, userID, userID, "test reason", nil, nil, 30)
	require.NoError(t, err)

	manager.markDeletionFailed(delReq.ID, "test deletion failure")

	updated, err := manager.deps.DeletionStorage.GetDeletionRequest(ctx, delReq.ID)
	require.NoError(t, err)
	assert.Equal(t, DeletionStatusFailed, updated.Status)
	assert.Equal(t, "test deletion failure", updated.ErrorMessage)
	assert.NotNil(t, updated.CompletedAt)
}

func TestMarkDeletionFailed_NonexistentDeletion(t *testing.T) {
	manager, _ := createTestGDPRManager(t)

	// Should not panic on a nonexistent deletion
	manager.markDeletionFailed("nonexistent-deletion-id", "should not crash")
}

// =============================================================================
// Enhanced PEP Sync: SyncIncremental tests
// =============================================================================

func TestEnhancedSyncer_SyncIncremental_Success(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.EnableIncrementalSync = true
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	// Append a baseline event at version 100 so minVersion=100
	baseEvent := &EnhancedPolicyChangeEvent{
		EventID:    "event-0",
		Type:       PolicyUpdateCreated,
		TenantID:   "tenant-1",
		PolicyName: "base-policy",
		Version:    100,
		Timestamp:  time.Now(),
		NodeID:     "other-node",
	}
	err = syncer.deltaLog.Append(baseEvent)
	require.NoError(t, err)

	// Append the event we want to sync at version 101
	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-1",
		Type:       PolicyUpdateCreated,
		TenantID:   "tenant-1",
		PolicyName: "test-policy",
		Version:    101,
		Timestamp:  time.Now(),
		NodeID:     "other-node",
		Policy: &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "tenant-1"},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		},
	}
	err = syncer.deltaLog.Append(event)
	require.NoError(t, err)

	// sinceVersion=100 >= minVersion=100, so GetSince returns events with Version > 100 (i.e., event at 101)
	err = syncer.SyncIncremental(ctx, "tenant-1", 100)
	require.NoError(t, err)

	// Verify the version tracker was updated
	version := syncer.GetNodeVersion("tenant-1")
	assert.Equal(t, int64(101), version)
}

func TestEnhancedSyncer_SyncIncremental_Disabled(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.EnableIncrementalSync = false
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()
	err = syncer.SyncIncremental(ctx, "tenant-1", 99)
	require.Error(t, err)
	assert.IsType(t, &IncrementalSyncDisabledError{}, err)
}

func TestEnhancedSyncer_SyncIncremental_Closed(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	syncer.Close()

	ctx := context.Background()
	err = syncer.SyncIncremental(ctx, "tenant-1", 99)
	require.Error(t, err)
}

func TestEnhancedSyncer_SyncIncremental_NoChanges(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.EnableIncrementalSync = true
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()
	err = syncer.SyncIncremental(ctx, "tenant-1", 99)
	require.NoError(t, err)
}

// =============================================================================
// Enhanced PEP Sync: applyChange tests
// =============================================================================

func TestEnhancedSyncer_ApplyChange_NilEvent(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()
	err = syncer.applyChange(ctx, nil)
	require.Error(t, err)
	assert.IsType(t, &NilEventError{}, err)
}

func TestEnhancedSyncer_ApplyChange_Created(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-1",
		Type:       PolicyUpdateCreated,
		TenantID:   "tenant-1",
		PolicyName: "test-policy",
		Version:    100,
		Timestamp:  time.Now(),
		Policy: &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "tenant-1"},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		},
	}

	err = syncer.applyChange(ctx, event)
	require.NoError(t, err)

	version := syncer.versionTracker.GetPolicyVersion("tenant-1", "test-policy")
	assert.Equal(t, int64(100), version)
}

func TestEnhancedSyncer_ApplyChange_Deleted(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-del-1",
		Type:       PolicyUpdateDeleted,
		TenantID:   "tenant-1",
		PolicyName: "deleted-policy",
		Version:    200,
		Timestamp:  time.Now(),
	}

	err = syncer.applyChange(ctx, event)
	require.NoError(t, err)
}

func TestEnhancedSyncer_ApplyChange_VersionConflict_Reject(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache,
		WithConflictResolution(ConflictResolutionReject),
	)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	syncer.versionTracker.SetPolicyVersion("tenant-1", "conflict-policy", 200)

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-old",
		Type:       PolicyUpdateUpdated,
		TenantID:   "tenant-1",
		PolicyName: "conflict-policy",
		Version:    100,
		Timestamp:  time.Now(),
		Policy: &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: "conflict-policy", Tenant: "tenant-1"},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		},
	}

	err = syncer.applyChange(ctx, event)
	require.Error(t, err)
	assert.IsType(t, &VersionConflictError{}, err)
}

func TestEnhancedSyncer_ApplyChange_VersionConflict_HighestVersion(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache,
		WithConflictResolution(ConflictResolutionHighestVersion),
	)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	syncer.versionTracker.SetPolicyVersion("tenant-1", "hv-policy", 200)

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-old",
		Type:       PolicyUpdateUpdated,
		TenantID:   "tenant-1",
		PolicyName: "hv-policy",
		Version:    100,
		Timestamp:  time.Now(),
		Policy: &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: "hv-policy", Tenant: "tenant-1"},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		},
	}

	err = syncer.applyChange(ctx, event)
	require.NoError(t, err)

	version := syncer.versionTracker.GetPolicyVersion("tenant-1", "hv-policy")
	assert.Equal(t, int64(200), version)
}

func TestEnhancedSyncer_ApplyChange_LastWrite(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache,
		WithConflictResolution(ConflictResolutionLastWrite),
	)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	syncer.versionTracker.SetPolicyVersion("tenant-1", "lw-policy", 200)

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-lw",
		Type:       PolicyUpdateUpdated,
		TenantID:   "tenant-1",
		PolicyName: "lw-policy",
		Version:    200,
		Timestamp:  time.Now(),
		Policy: &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: "lw-policy", Tenant: "tenant-1"},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		},
	}

	err = syncer.applyChange(ctx, event)
	require.NoError(t, err)
}

func TestEnhancedSyncer_ApplyChange_MissingPolicyData(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-missing",
		Type:       PolicyUpdateCreated,
		TenantID:   "tenant-1",
		PolicyName: "missing-policy",
		Version:    100,
		Timestamp:  time.Now(),
		Policy:     nil,
	}

	err = syncer.applyChange(ctx, event)
	require.Error(t, err)
	assert.IsType(t, &MissingPolicyDataError{}, err)
}

func TestEnhancedSyncer_ApplyChange_Snapshot(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-snapshot",
		Type:       PolicyUpdateSnapshot,
		TenantID:   "tenant-1",
		PolicyName: "snapshot-policy",
		Version:    300,
		Timestamp:  time.Now(),
	}

	err = syncer.applyChange(ctx, event)
	require.NoError(t, err)
}

func TestEnhancedSyncer_ApplyChange_Batch(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	event := &EnhancedPolicyChangeEvent{
		EventID:    "event-batch",
		Type:       PolicyUpdateBatch,
		TenantID:   "tenant-1",
		PolicyName: "batch-policy",
		Version:    400,
		Timestamp:  time.Now(),
	}

	err = syncer.applyChange(ctx, event)
	require.NoError(t, err)
}

func TestEnhancedSyncer_ApplyChange_WithCompiledPolicy(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "compiled-policy", Tenant: "tenant-1"},
		Spec: PolicySpec{
			Rules: []Rule{
				{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
			},
		},
	}
	compiled, err := syncer.compiler.Compile(policy)
	require.NoError(t, err)

	event := &EnhancedPolicyChangeEvent{
		EventID:        "event-compiled",
		Type:           PolicyUpdateUpdated,
		TenantID:       "tenant-1",
		PolicyName:     "compiled-policy",
		Version:        500,
		Timestamp:      time.Now(),
		CompiledPolicy: compiled,
	}

	err = syncer.applyChange(ctx, event)
	require.NoError(t, err)
}

// =============================================================================
// Enhanced PEP Sync: GetNodeVersion tests
// =============================================================================

func TestEnhancedSyncer_GetNodeVersion(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	version := syncer.GetNodeVersion("tenant-1")
	assert.Equal(t, int64(0), version)

	syncer.versionTracker.SetPolicyVersion("tenant-1", "test-policy", 42)
	version = syncer.GetNodeVersion("tenant-1")
	assert.Equal(t, int64(42), version)
}

// =============================================================================
// Enhanced PEP Sync: WithEnhancedVersionTracker tests
// =============================================================================

func TestEnhancedSyncer_WithEnhancedVersionTracker(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	customTracker := NewEnhancedCacheVersionTracker()
	customTracker.SetPolicyVersion("tenant-1", "pre-existing", 99)

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache,
		WithEnhancedVersionTracker(customTracker),
	)
	require.NoError(t, err)
	defer syncer.Close()

	version := syncer.versionTracker.GetPolicyVersion("tenant-1", "pre-existing")
	assert.Equal(t, int64(99), version)
}

// =============================================================================
// Enhanced PEP Sync: convertToEnhancedType tests
// =============================================================================

func TestEnhancedSyncer_ConvertToEnhancedType(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	assert.Equal(t, PolicyUpdateCreated, syncer.convertToEnhancedType(PolicyChangeCreated))
	assert.Equal(t, PolicyUpdateUpdated, syncer.convertToEnhancedType(PolicyChangeUpdated))
	assert.Equal(t, PolicyUpdateDeleted, syncer.convertToEnhancedType(PolicyChangeDeleted))
	assert.Equal(t, PolicyUpdateSnapshot, syncer.convertToEnhancedType(PolicyChangeSnapshot))
	assert.Equal(t, PolicyUpdateUpdated, syncer.convertToEnhancedType(PolicyChangeType(99)))
}

// =============================================================================
// Distributed PEP: handlePolicyChange tests
// =============================================================================

func TestDistributedPEP_HandlePolicyChange_SkipOwnNode(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	event := PolicyChangeEvent{
		Type:     PolicyChangeCreated,
		PolicyID: "test-policy",
		TenantID: "tenant-1",
		Version:  1,
		NodeID:   config.SyncConfig.NodeID,
	}

	err = dpep.handlePolicyChange(ctx, event)
	require.NoError(t, err)
}

func TestDistributedPEP_HandlePolicyChange_Created(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "remote-policy", Tenant: "tenant-1"},
		Spec: PolicySpec{
			Rules: []Rule{
				{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
			},
		},
	}
	policyData, err := json.Marshal(policy)
	require.NoError(t, err)

	event := PolicyChangeEvent{
		Type:     PolicyChangeCreated,
		PolicyID: "remote-policy",
		TenantID: "tenant-1",
		Version:  1,
		Data:     policyData,
		NodeID:   "other-node",
	}

	err = dpep.handlePolicyChange(ctx, event)
	require.NoError(t, err)
}

func TestDistributedPEP_HandlePolicyChange_Deleted(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	event := PolicyChangeEvent{
		Type:     PolicyChangeDeleted,
		PolicyID: "deleted-policy",
		TenantID: "tenant-1",
		Version:  2,
		NodeID:   "other-node",
	}

	err = dpep.handlePolicyChange(ctx, event)
	require.NoError(t, err)
}

func TestDistributedPEP_HandlePolicyChange_MissingData(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	event := PolicyChangeEvent{
		Type:     PolicyChangeCreated,
		PolicyID: "no-data-policy",
		TenantID: "tenant-1",
		Version:  3,
		Data:     nil,
		NodeID:   "other-node",
	}

	err = dpep.handlePolicyChange(ctx, event)
	require.Error(t, err)
	assert.IsType(t, &MissingPolicyDataError{}, err)
}

func TestDistributedPEP_HandlePolicyChange_InvalidJSON(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	event := PolicyChangeEvent{
		Type:     PolicyChangeUpdated,
		PolicyID: "bad-json-policy",
		TenantID: "tenant-1",
		Version:  4,
		Data:     []byte("not valid json"),
		NodeID:   "other-node",
	}

	err = dpep.handlePolicyChange(ctx, event)
	require.Error(t, err)
	assert.IsType(t, &PolicyUnmarshalError{}, err)
}

func TestDistributedPEP_HandlePolicyChange_Snapshot(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	event := PolicyChangeEvent{
		Type:     PolicyChangeSnapshot,
		PolicyID: "snapshot",
		TenantID: "tenant-1",
		Version:  5,
		NodeID:   "other-node",
	}

	err = dpep.handlePolicyChange(ctx, event)
	require.NoError(t, err)
}

// =============================================================================
// Distributed PEP: convertToBaseType tests
// =============================================================================

func TestDistributedPEP_ConvertToBaseType(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	assert.Equal(t, PolicyChangeCreated, dpep.convertToBaseType(PolicyUpdateCreated))
	assert.Equal(t, PolicyChangeUpdated, dpep.convertToBaseType(PolicyUpdateUpdated))
	assert.Equal(t, PolicyChangeDeleted, dpep.convertToBaseType(PolicyUpdateDeleted))
	assert.Equal(t, PolicyChangeSnapshot, dpep.convertToBaseType(PolicyUpdateSnapshot))
	assert.Equal(t, PolicyChangeUpdated, dpep.convertToBaseType(PolicyUpdateType(99)))
}

// =============================================================================
// Distributed PEP: Check and CheckBatch tests
// =============================================================================

func TestDistributedPEP_Check_WithPolicy(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "check-policy", Tenant: "tenant-1"},
		Spec: PolicySpec{
			Rules: []Rule{
				{
					Name:      "allow-read",
					Effect:    EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"*"},
				},
			},
		},
	}
	err = dpep.LoadPolicy(ctx, policy)
	require.NoError(t, err)

	req := &CheckRequest{
		Subject: Subject{
			ID:       "user-1",
			TenantID: "tenant-1",
		},
		Action:   "read",
		Resource: "documents/doc1",
	}

	resp, err := dpep.Check(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestDistributedPEP_CheckBatch_WithChecks(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	ctx := context.Background()

	req := &BatchCheckRequest{
		Checks: []*CheckRequest{
			{
				Subject:  Subject{ID: "user-1", TenantID: "tenant-1"},
				Action:   "read",
				Resource: "documents/doc1",
			},
		},
	}

	resp, err := dpep.CheckBatch(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Enhanced PEP Sync: SyncFromLeader tests
// =============================================================================

func TestEnhancedSyncer_SyncFromLeader_Error(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	cache := newDistPEPMockCacheSyncInterface()
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 1

	syncer, err := NewEnhancedRaftCacheSyncer(config, raft, cache)
	require.NoError(t, err)
	defer syncer.Close()

	ctx := context.Background()

	// Mock raft returns nil for Read, so SyncFromLeader fails with type assertion error
	err = syncer.SyncFromLeader(ctx)
	require.Error(t, err)
	assert.IsType(t, &SyncFailedError{}, err)
}

// =============================================================================
// Distributed PEP: GetStats with error tests
// =============================================================================

func TestDistributedPEP_GetStats_WithSyncError(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	dpep.lastSyncError.Store(&SyncFailedError{Operation: "test", Cause: ErrSyncerClosed})

	stats := dpep.GetStats()
	require.NotNil(t, stats)
	assert.Contains(t, stats.LastSyncError, "sync failed during test")
}

func TestDistributedPEP_GetStats_NoError(t *testing.T) {
	raft := newDistPEPMockRaftProposer()
	config := DefaultDistributedPEPConfig("node-1", "tenant-1")

	dpep, err := NewDistributedPEP(config, raft)
	require.NoError(t, err)
	defer dpep.Close()

	stats := dpep.GetStats()
	require.NotNil(t, stats)
	assert.Empty(t, stats.LastSyncError)
	assert.False(t, stats.SyncInProgress)
}
