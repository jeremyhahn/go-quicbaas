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

package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// mockAuthorizationProvider implements interfaces.AuthorizationProvider for testing.
type mockAuthorizationProvider struct {
	authorizeFunc      func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error)
	batchAuthorizeFunc func(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error)
	providerType       string
	closed             bool
	closeErr           error
	mu                 sync.Mutex
}

func newMockProvider() *mockAuthorizationProvider {
	return &mockAuthorizationProvider{
		providerType: "mock",
	}
}

func (m *mockAuthorizationProvider) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	if m.authorizeFunc != nil {
		return m.authorizeFunc(ctx, req)
	}
	return &interfaces.AuthorizationDecision{
		Allowed:       true,
		Reason:        "mock allowed",
		Provider:      m.providerType,
		MatchedPolicy: "mock-policy",
	}, nil
}

func (m *mockAuthorizationProvider) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	if m.batchAuthorizeFunc != nil {
		return m.batchAuthorizeFunc(ctx, reqs)
	}
	decisions := make([]*interfaces.AuthorizationDecision, len(reqs))
	for i := range reqs {
		decisions[i] = &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "mock batch allowed",
			Provider:      m.providerType,
			MatchedPolicy: "mock-batch-policy",
		}
	}
	return decisions, nil
}

func (m *mockAuthorizationProvider) GetProviderType() string {
	return m.providerType
}

func (m *mockAuthorizationProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeErr
}

func (m *mockAuthorizationProvider) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// testLogger creates a logger for testing.
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create test logger: %v", err)
	}
	return log
}

// resetMetrics resets the metrics registry for test isolation.
func resetMetrics() {
	metrics.Reset()
}

func TestNewAuthorizationService(t *testing.T) {
	resetMetrics()

	tests := []struct {
		name        string
		provider    interfaces.AuthorizationProvider
		logger      *logger.Logger
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid inputs",
			provider: newMockProvider(),
			logger:   testLogger(t),
			wantErr:  false,
		},
		{
			name:        "nil provider",
			provider:    nil,
			logger:      testLogger(t),
			wantErr:     true,
			errContains: "authorization provider cannot be nil",
		},
		{
			name:        "nil logger",
			provider:    newMockProvider(),
			logger:      nil,
			wantErr:     true,
			errContains: "logger cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewAuthorizationService(tt.provider, tt.logger)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if svc == nil {
				t.Fatal("expected service but got nil")
			}

			// Cleanup
			if err := svc.Close(); err != nil {
				t.Errorf("failed to close service: %v", err)
			}
		})
	}
}

func TestAuthorizationService_Authorize(t *testing.T) {
	resetMetrics()

	tests := []struct {
		name        string
		setupFunc   func(*mockAuthorizationProvider)
		request     *interfaces.AuthorizationRequest
		wantAllowed bool
		wantErr     bool
		errCode     dragonerrors.ErrorCode
	}{
		{
			name: "successful authorization - allowed",
			setupFunc: func(m *mockAuthorizationProvider) {
				m.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
					return &interfaces.AuthorizationDecision{
						Allowed:       true,
						Reason:        "user has admin role",
						MatchedPolicy: "admin",
					}, nil
				}
			},
			request: &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					ID:            "user-123",
					Username:      "admin",
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: interfaces.ResourceKeys,
				Action:   interfaces.ActionWrite,
			},
			wantAllowed: true,
			wantErr:     false,
		},
		{
			name: "successful authorization - denied",
			setupFunc: func(m *mockAuthorizationProvider) {
				m.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
					return &interfaces.AuthorizationDecision{
						Allowed: false,
						Reason:  "insufficient permissions",
					}, nil
				}
			},
			request: &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					ID:            "user-456",
					Username:      "reader",
					Roles:         []string{"reader"},
					Authenticated: true,
				},
				Resource: interfaces.ResourceKeys,
				Action:   interfaces.ActionDelete,
			},
			wantAllowed: false,
			wantErr:     false,
		},
		{
			name:    "nil request",
			request: nil,
			wantErr: true,
			errCode: dragonerrors.ErrInvalidInput,
		},
		{
			name: "provider returns error",
			setupFunc: func(m *mockAuthorizationProvider) {
				m.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
					return nil, &dragonerrors.DragonError{
						Code: dragonerrors.ErrInternal,
						Op:   "provider.Authorize",
						Err:  errors.New("internal provider error"),
					}
				}
			},
			request: &interfaces.AuthorizationRequest{
				Subject:  interfaces.Subject{ID: "user-1"},
				Resource: interfaces.ResourceKeys,
				Action:   interfaces.ActionRead,
			},
			wantErr: true,
			errCode: dragonerrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetMetrics()

			provider := newMockProvider()
			if tt.setupFunc != nil {
				tt.setupFunc(provider)
			}

			svc, err := NewAuthorizationService(provider, testLogger(t))
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}
			defer svc.Close()

			decision, err := svc.Authorize(context.Background(), tt.request)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				var de *dragonerrors.DragonError
				if errors.As(err, &de) && tt.errCode != 0 {
					if de.Code != tt.errCode {
						t.Errorf("expected error code %v, got %v", tt.errCode, de.Code)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if decision == nil {
				t.Fatal("expected decision but got nil")
			}

			if decision.Allowed != tt.wantAllowed {
				t.Errorf("expected Allowed=%v, got %v", tt.wantAllowed, decision.Allowed)
			}

			// Verify decision has required fields
			if decision.DecisionID == "" {
				t.Error("expected DecisionID to be set")
			}
			if decision.EvaluationTimeNs <= 0 {
				t.Error("expected EvaluationTimeNs to be positive")
			}
			if decision.Provider == "" {
				t.Error("expected Provider to be set")
			}
		})
	}
}

func TestAuthorizationService_Authorize_ContextCanceled(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "user-1"},
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	}

	_, err = svc.Authorize(ctx, req)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}

	var de *dragonerrors.DragonError
	if !errors.As(err, &de) {
		t.Fatalf("expected DragonError, got %T", err)
	}
	if de.Code != dragonerrors.ErrCanceled {
		t.Errorf("expected ErrCanceled, got %v", de.Code)
	}
}

func TestAuthorizationService_Authorize_ServiceClosed(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Close the service
	if err := svc.Close(); err != nil {
		t.Fatalf("failed to close service: %v", err)
	}

	req := &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "user-1"},
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	}

	_, err = svc.Authorize(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for closed service")
	}

	var de *dragonerrors.DragonError
	if !errors.As(err, &de) {
		t.Fatalf("expected DragonError, got %T", err)
	}
	if de.Code != dragonerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable, got %v", de.Code)
	}
}

func TestAuthorizationService_BatchAuthorize(t *testing.T) {
	resetMetrics()

	tests := []struct {
		name      string
		setupFunc func(*mockAuthorizationProvider)
		requests  []*interfaces.AuthorizationRequest
		wantCount int
		wantErr   bool
		errCode   dragonerrors.ErrorCode
	}{
		{
			name: "successful batch authorization",
			requests: []*interfaces.AuthorizationRequest{
				{
					Subject:  interfaces.Subject{ID: "user-1", Username: "user1"},
					Resource: interfaces.ResourceKeys,
					Action:   interfaces.ActionRead,
				},
				{
					Subject:  interfaces.Subject{ID: "user-2", Username: "user2"},
					Resource: interfaces.ResourceSecrets,
					Action:   interfaces.ActionWrite,
				},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "empty batch",
			requests: []*interfaces.AuthorizationRequest{},
			wantErr:  true,
			errCode:  dragonerrors.ErrInvalidInput,
		},
		{
			name: "nil request in batch",
			requests: []*interfaces.AuthorizationRequest{
				{
					Subject:  interfaces.Subject{ID: "user-1"},
					Resource: interfaces.ResourceKeys,
					Action:   interfaces.ActionRead,
				},
				nil,
			},
			wantErr: true,
			errCode: dragonerrors.ErrInvalidInput,
		},
		{
			name: "provider returns error",
			setupFunc: func(m *mockAuthorizationProvider) {
				m.batchAuthorizeFunc = func(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
					return nil, &dragonerrors.DragonError{
						Code: dragonerrors.ErrInternal,
						Op:   "provider.BatchAuthorize",
						Err:  errors.New("batch processing failed"),
					}
				}
			},
			requests: []*interfaces.AuthorizationRequest{
				{
					Subject:  interfaces.Subject{ID: "user-1"},
					Resource: interfaces.ResourceKeys,
					Action:   interfaces.ActionRead,
				},
			},
			wantErr: true,
			errCode: dragonerrors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetMetrics()

			provider := newMockProvider()
			if tt.setupFunc != nil {
				tt.setupFunc(provider)
			}

			svc, err := NewAuthorizationService(provider, testLogger(t))
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}
			defer svc.Close()

			decisions, err := svc.BatchAuthorize(context.Background(), tt.requests)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				var de *dragonerrors.DragonError
				if errors.As(err, &de) && tt.errCode != 0 {
					if de.Code != tt.errCode {
						t.Errorf("expected error code %v, got %v", tt.errCode, de.Code)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(decisions) != tt.wantCount {
				t.Errorf("expected %d decisions, got %d", tt.wantCount, len(decisions))
			}

			// Verify each decision has required fields
			for i, decision := range decisions {
				if decision == nil {
					t.Errorf("decision %d is nil", i)
					continue
				}
				if decision.DecisionID == "" {
					t.Errorf("decision %d: expected DecisionID to be set", i)
				}
				if decision.Provider == "" {
					t.Errorf("decision %d: expected Provider to be set", i)
				}
			}
		})
	}
}

func TestAuthorizationService_BatchAuthorize_ContextCanceled(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reqs := []*interfaces.AuthorizationRequest{
		{
			Subject:  interfaces.Subject{ID: "user-1"},
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		},
	}

	_, err = svc.BatchAuthorize(ctx, reqs)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}

	var de *dragonerrors.DragonError
	if !errors.As(err, &de) {
		t.Fatalf("expected DragonError, got %T", err)
	}
	if de.Code != dragonerrors.ErrCanceled {
		t.Errorf("expected ErrCanceled, got %v", de.Code)
	}
}

func TestAuthorizationService_BatchAuthorize_ServiceClosed(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Close the service
	if err := svc.Close(); err != nil {
		t.Fatalf("failed to close service: %v", err)
	}

	reqs := []*interfaces.AuthorizationRequest{
		{
			Subject:  interfaces.Subject{ID: "user-1"},
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		},
	}

	_, err = svc.BatchAuthorize(context.Background(), reqs)
	if err == nil {
		t.Fatal("expected error for closed service")
	}

	var de *dragonerrors.DragonError
	if !errors.As(err, &de) {
		t.Fatalf("expected DragonError, got %T", err)
	}
	if de.Code != dragonerrors.ErrUnavailable {
		t.Errorf("expected ErrUnavailable, got %v", de.Code)
	}
}

func TestAuthorizationService_GetProvider(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	got := svc.GetProvider()
	if got != provider {
		t.Error("GetProvider returned different provider")
	}
}

func TestAuthorizationService_GetProviderType(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	provider.providerType = "rbac"

	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	got := svc.GetProviderType()
	if got != "rbac" {
		t.Errorf("expected provider type 'rbac', got %q", got)
	}
}

func TestAuthorizationService_Close(t *testing.T) {
	resetMetrics()

	t.Run("successful close", func(t *testing.T) {
		provider := newMockProvider()
		svc, err := NewAuthorizationService(provider, testLogger(t))
		if err != nil {
			t.Fatalf("failed to create service: %v", err)
		}

		err = svc.Close()
		if err != nil {
			t.Errorf("unexpected error on close: %v", err)
		}

		// Verify provider was closed
		if !provider.IsClosed() {
			t.Error("expected provider to be closed")
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		provider := newMockProvider()
		svc, err := NewAuthorizationService(provider, testLogger(t))
		if err != nil {
			t.Fatalf("failed to create service: %v", err)
		}

		// Close multiple times
		for i := 0; i < 3; i++ {
			err = svc.Close()
			if err != nil {
				t.Errorf("close %d: unexpected error: %v", i+1, err)
			}
		}
	})

	t.Run("close propagates provider error", func(t *testing.T) {
		provider := newMockProvider()
		provider.closeErr = errors.New("provider close error")

		svc, err := NewAuthorizationService(provider, testLogger(t))
		if err != nil {
			t.Fatalf("failed to create service: %v", err)
		}

		err = svc.Close()
		if err == nil {
			t.Error("expected error from provider close")
		}
	})
}

func TestAuthorizationService_ConcurrentAccess(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	provider.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		// Simulate some work
		time.Sleep(time.Millisecond)
		return &interfaces.AuthorizationDecision{
			Allowed: true,
			Reason:  "allowed",
		}, nil
	}

	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	const numGoroutines = 50
	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			req := &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					ID:       "user-" + string(rune('0'+idx%10)),
					Username: "user",
				},
				Resource: interfaces.ResourceKeys,
				Action:   interfaces.ActionRead,
			}

			decision, err := svc.Authorize(context.Background(), req)
			if err != nil {
				errCh <- err
				return
			}
			if decision == nil {
				errCh <- errors.New("nil decision")
				return
			}
			if !decision.Allowed {
				errCh <- errors.New("expected allowed decision")
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestAuthorizationService_DecisionIDUniqueness(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	req := &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "user-1"},
		Resource: interfaces.ResourceKeys,
		Action:   interfaces.ActionRead,
	}

	const numRequests = 100
	decisionIDs := make(map[string]bool)

	for i := 0; i < numRequests; i++ {
		decision, err := svc.Authorize(context.Background(), req)
		if err != nil {
			t.Fatalf("authorization failed: %v", err)
		}

		if decisionIDs[decision.DecisionID] {
			t.Errorf("duplicate decision ID: %s", decision.DecisionID)
		}
		decisionIDs[decision.DecisionID] = true
	}

	if len(decisionIDs) != numRequests {
		t.Errorf("expected %d unique decision IDs, got %d", numRequests, len(decisionIDs))
	}
}

func TestAuthorizationService_ProviderTypeInDecision(t *testing.T) {
	resetMetrics()

	testCases := []struct {
		providerType string
	}{
		{"rbac"},
		{"abac"},
		{"hybrid"},
	}

	for _, tc := range testCases {
		t.Run(tc.providerType, func(t *testing.T) {
			resetMetrics()

			provider := newMockProvider()
			provider.providerType = tc.providerType
			provider.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
				return &interfaces.AuthorizationDecision{
					Allowed: true,
					Reason:  "allowed",
					// Provider not set - service should fill it in
				}, nil
			}

			svc, err := NewAuthorizationService(provider, testLogger(t))
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}
			defer svc.Close()

			req := &interfaces.AuthorizationRequest{
				Subject:  interfaces.Subject{ID: "user-1"},
				Resource: interfaces.ResourceKeys,
				Action:   interfaces.ActionRead,
			}

			decision, err := svc.Authorize(context.Background(), req)
			if err != nil {
				t.Fatalf("authorization failed: %v", err)
			}

			if decision.Provider != tc.providerType {
				t.Errorf("expected provider %q, got %q", tc.providerType, decision.Provider)
			}
		})
	}
}

func TestAuthorizationService_BatchWithNilDecisions(t *testing.T) {
	resetMetrics()

	provider := newMockProvider()
	provider.batchAuthorizeFunc = func(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
		// Return decisions with some nil entries
		decisions := make([]*interfaces.AuthorizationDecision, len(reqs))
		decisions[0] = &interfaces.AuthorizationDecision{
			Allowed: true,
			Reason:  "allowed",
		}
		// decisions[1] is nil - should be handled gracefully
		return decisions, nil
	}

	svc, err := NewAuthorizationService(provider, testLogger(t))
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	reqs := []*interfaces.AuthorizationRequest{
		{
			Subject:  interfaces.Subject{ID: "user-1"},
			Resource: interfaces.ResourceKeys,
			Action:   interfaces.ActionRead,
		},
		{
			Subject:  interfaces.Subject{ID: "user-2"},
			Resource: interfaces.ResourceSecrets,
			Action:   interfaces.ActionWrite,
		},
	}

	decisions, err := svc.BatchAuthorize(context.Background(), reqs)
	if err != nil {
		t.Fatalf("batch authorize failed: %v", err)
	}

	if len(decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(decisions))
	}

	// First decision should be valid
	if decisions[0] == nil {
		t.Error("first decision should not be nil")
	} else if decisions[0].DecisionID == "" {
		t.Error("first decision should have DecisionID set")
	}

	// Second decision is nil from provider - should remain nil
	if decisions[1] != nil {
		t.Error("second decision should be nil as returned by provider")
	}
}

func TestGenerateDecisionID(t *testing.T) {
	// Test that generateDecisionID produces valid IDs
	seen := make(map[string]bool)
	const iterations = 1000

	for i := 0; i < iterations; i++ {
		id := generateDecisionID()

		// Check format
		if len(id) < 10 {
			t.Errorf("decision ID too short: %s", id)
		}
		if id[:4] != "dec-" {
			t.Errorf("decision ID should start with 'dec-': %s", id)
		}

		// Check uniqueness
		if seen[id] {
			t.Errorf("duplicate decision ID: %s", id)
		}
		seen[id] = true
	}
}

func TestAuthzMetrics_RecordRequest(t *testing.T) {
	resetMetrics()

	m := newAuthzMetrics()

	// Record some requests
	m.recordRequest("rbac", "keys", "read", true, 100*time.Microsecond)
	m.recordRequest("rbac", "keys", "write", false, 200*time.Microsecond)
	m.recordRequest("abac", "secrets", "read", true, 150*time.Microsecond)

	// Just verify no panic - actual metric values would require Prometheus registry inspection
}

// containsString checks if a string contains a substring.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
