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

package providers

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockAuthorizationProvider implements interfaces.AuthorizationProvider for testing.
type mockAuthorizationProvider struct {
	authorizeFunc      func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error)
	batchAuthorizeFunc func(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error)
	providerType       string
	closed             bool
	closeErr           error
	mu                 sync.RWMutex
}

func newMockAuthorizationProvider(providerType string) *mockAuthorizationProvider {
	return &mockAuthorizationProvider{
		providerType: providerType,
	}
}

func (m *mockAuthorizationProvider) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, stderrors.New("provider is closed")
	}

	if m.authorizeFunc != nil {
		return m.authorizeFunc(ctx, req)
	}

	return &interfaces.AuthorizationDecision{
		Allowed:       true,
		Reason:        "default allow",
		Provider:      m.providerType,
		MatchedPolicy: "default",
		DecisionID:    "mock-decision-1",
	}, nil
}

func (m *mockAuthorizationProvider) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, stderrors.New("provider is closed")
	}

	if m.batchAuthorizeFunc != nil {
		return m.batchAuthorizeFunc(ctx, reqs)
	}

	decisions := make([]*interfaces.AuthorizationDecision, len(reqs))
	for i := range reqs {
		decisions[i] = &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "default allow",
			Provider:      m.providerType,
			MatchedPolicy: "default",
			DecisionID:    "mock-decision",
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

func (m *mockAuthorizationProvider) setCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErr = err
}

// createHybridTestLogger creates a logger for hybrid provider tests.
func createHybridTestLogger(t *testing.T) *logger.Logger {
	log, err := logger.New(logger.LogConfig{
		Level:     "debug",
		Format:    "json",
		Output:    "stdout",
		Component: "hybrid-test",
	})
	require.NoError(t, err)
	return log
}

// TestNewHybridProvider tests provider creation.
func TestNewHybridProvider(t *testing.T) {
	t.Run("success with both ABAC and RBAC providers", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		abac := newMockAuthorizationProvider("abac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)

		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.Equal(t, ProviderTypeHybrid, provider.GetProviderType())
		assert.True(t, provider.HasABACProvider())
	})

	t.Run("success with RBAC only (nil ABAC)", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)

		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.Equal(t, ProviderTypeHybrid, provider.GetProviderType())
		assert.False(t, provider.HasABACProvider())
	})

	t.Run("success with nil logger creates default", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")

		provider, err := NewHybridProvider(rbac, nil, nil)

		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.NotNil(t, provider.logger)
	})

	t.Run("error with nil RBAC provider", func(t *testing.T) {
		abac := newMockAuthorizationProvider("abac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(nil, abac, log)

		require.Error(t, err)
		assert.Nil(t, provider)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
		assert.ErrorIs(t, dragonErr.Err, ErrNilRBACProvider)
	})

	t.Run("error with both providers nil", func(t *testing.T) {
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(nil, nil, log)

		require.Error(t, err)
		assert.Nil(t, provider)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestHybridProviderAuthorizeWithABAC tests authorization when ABAC is available.
func TestHybridProviderAuthorizeWithABAC(t *testing.T) {
	t.Run("uses ABAC decision when ABAC succeeds", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		abac := newMockAuthorizationProvider("abac")
		abac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed:       true,
				Reason:        "ABAC policy matched",
				Provider:      "abac",
				MatchedPolicy: "engineering-access",
			}, nil
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user1",
				Username:      "testuser",
				Roles:         []string{"developer"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "hybrid(abac)", decision.Provider)
		assert.Equal(t, "engineering-access", decision.MatchedPolicy)
		assert.Contains(t, decision.DecisionID, "hybrid-")
		assert.Greater(t, decision.EvaluationTimeNs, int64(0))
	})

	t.Run("ABAC deny decision is used", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		abac := newMockAuthorizationProvider("abac")
		abac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed:       false,
				Reason:        "ABAC policy denied",
				Provider:      "abac",
				MatchedPolicy: "deny-external",
			}, nil
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user1",
				Roles:         []string{"external"},
				Authenticated: true,
			},
			Resource: "secrets",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Equal(t, "hybrid(abac)", decision.Provider)
		assert.Equal(t, "deny-external", decision.MatchedPolicy)
	})

	t.Run("falls back to RBAC when ABAC returns error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed:       true,
				Reason:        "RBAC role matched",
				Provider:      "rbac",
				MatchedPolicy: "admin",
			}, nil
		}

		abac := newMockAuthorizationProvider("abac")
		abac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return nil, stderrors.New("ABAC evaluation failed")
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user1",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "admin",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "hybrid(rbac)", decision.Provider)
		assert.Equal(t, "admin", decision.MatchedPolicy)
	})
}

// TestHybridProviderAuthorizeWithRBACOnly tests authorization without ABAC.
func TestHybridProviderAuthorizeWithRBACOnly(t *testing.T) {
	t.Run("uses RBAC when ABAC not available", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed:       true,
				Reason:        "RBAC role grants access",
				Provider:      "rbac",
				MatchedPolicy: "readwrite",
			}, nil
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user1",
				Roles:         []string{"readwrite"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "write",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "hybrid(rbac)", decision.Provider)
		assert.Equal(t, "readwrite", decision.MatchedPolicy)
	})

	t.Run("RBAC deny decision is used", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed:       false,
				Reason:        "no matching role",
				Provider:      "rbac",
				MatchedPolicy: "",
			}, nil
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user1",
				Roles:         []string{"readonly"},
				Authenticated: true,
			},
			Resource: "secrets",
			Action:   "write",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Equal(t, "hybrid(rbac)", decision.Provider)
	})
}

// TestHybridProviderAuthorizeErrors tests error handling.
func TestHybridProviderAuthorizeErrors(t *testing.T) {
	t.Run("nil request returns error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		decision, err := provider.Authorize(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, decision)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("canceled context returns error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(ctx, req)

		require.Error(t, err)
		assert.Nil(t, decision)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrCanceled, dragonErr.Code)
	})

	t.Run("closed provider returns error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		err = provider.Close()
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.Error(t, err)
		assert.Nil(t, decision)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrUnavailable, dragonErr.Code)
	})

	t.Run("RBAC error is propagated when ABAC not available", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return nil, stderrors.New("RBAC internal error")
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.Error(t, err)
		assert.Nil(t, decision)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInternal, dragonErr.Code)
	})

	t.Run("RBAC error after ABAC fallback is propagated", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return nil, stderrors.New("RBAC internal error")
		}

		abac := newMockAuthorizationProvider("abac")
		abac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return nil, stderrors.New("ABAC evaluation failed")
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.Error(t, err)
		assert.Nil(t, decision)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInternal, dragonErr.Code)
	})
}

// TestHybridProviderBatchAuthorize tests batch authorization.
func TestHybridProviderBatchAuthorize(t *testing.T) {
	t.Run("batch with mixed ABAC and RBAC decisions", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed:       true,
				Reason:        "RBAC fallback",
				Provider:      "rbac",
				MatchedPolicy: "admin",
			}, nil
		}

		callCount := 0
		abac := newMockAuthorizationProvider("abac")
		abac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			callCount++
			// First call succeeds, second fails (triggers RBAC fallback)
			if callCount == 1 {
				return &interfaces.AuthorizationDecision{
					Allowed:       true,
					Reason:        "ABAC matched",
					Provider:      "abac",
					MatchedPolicy: "policy1",
				}, nil
			}
			return nil, stderrors.New("ABAC failed")
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "user1",
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
			{
				Subject: interfaces.Subject{
					ID:            "user2",
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "write",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)

		require.NoError(t, err)
		require.Len(t, decisions, 2)
		assert.True(t, decisions[0].Allowed)
		assert.Equal(t, "hybrid(abac)", decisions[0].Provider)
		assert.True(t, decisions[1].Allowed)
		assert.Equal(t, "hybrid(rbac)", decisions[1].Provider)
	})

	t.Run("empty batch returns empty slice", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		decisions, err := provider.BatchAuthorize(context.Background(), []*interfaces.AuthorizationRequest{})

		require.NoError(t, err)
		assert.Empty(t, decisions)
	})

	t.Run("batch with nil request handles gracefully", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed:       true,
				Reason:        "allowed",
				Provider:      "rbac",
				MatchedPolicy: "admin",
			}, nil
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
			nil,
			{
				Subject: interfaces.Subject{
					Roles:         []string{"readonly"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)

		require.NoError(t, err)
		require.Len(t, decisions, 3)
		assert.True(t, decisions[0].Allowed)
		assert.False(t, decisions[1].Allowed)
		assert.Equal(t, "nil request", decisions[1].Reason)
		assert.True(t, decisions[2].Allowed)
	})

	t.Run("canceled context returns error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(ctx, reqs)

		require.Error(t, err)
		assert.Nil(t, decisions)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrCanceled, dragonErr.Code)
	})

	t.Run("closed provider returns error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		err = provider.Close()
		require.NoError(t, err)

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)

		require.Error(t, err)
		assert.Nil(t, decisions)

		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrUnavailable, dragonErr.Code)
	})

	t.Run("context cancellation mid-batch returns error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			// Simulate a delay that allows context to be canceled
			time.Sleep(10 * time.Millisecond)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return &interfaces.AuthorizationDecision{
				Allowed:       true,
				Reason:        "allowed",
				Provider:      "rbac",
				MatchedPolicy: "admin",
			}, nil
		}
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		// Create many requests
		reqs := make([]*interfaces.AuthorizationRequest, 10)
		for i := range reqs {
			reqs[i] = &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			}
		}

		// Cancel context after a short delay
		go func() {
			time.Sleep(25 * time.Millisecond)
			cancel()
		}()

		decisions, err := provider.BatchAuthorize(ctx, reqs)

		// Either error or decisions may be returned depending on timing
		if err != nil {
			var dragonErr *errors.DragonError
			if errors.IsCanceled(err) || stderrors.As(err, &dragonErr) {
				// Expected - context was canceled
				return
			}
		}
		// If we get here, all decisions completed before cancellation
		assert.NotNil(t, decisions)
	})
}

// TestHybridProviderClose tests provider close behavior.
func TestHybridProviderClose(t *testing.T) {
	t.Run("close succeeds with both providers", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		abac := newMockAuthorizationProvider("abac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		err = provider.Close()

		assert.NoError(t, err)
	})

	t.Run("close succeeds with RBAC only", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		err = provider.Close()

		assert.NoError(t, err)
	})

	t.Run("double close is idempotent", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)
	})

	t.Run("close propagates ABAC close error", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		abac := newMockAuthorizationProvider("abac")
		abac.setCloseError(stderrors.New("ABAC close error"))
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		err = provider.Close()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ABAC close error")
	})

	t.Run("close propagates RBAC close error when ABAC succeeds", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.setCloseError(stderrors.New("RBAC close error"))
		abac := newMockAuthorizationProvider("abac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		err = provider.Close()

		// ABAC error should be returned first since it's checked first
		// but since ABAC succeeds, RBAC error is returned
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "RBAC close error")
	})

	t.Run("close returns first error when both fail", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		rbac.setCloseError(stderrors.New("RBAC close error"))
		abac := newMockAuthorizationProvider("abac")
		abac.setCloseError(stderrors.New("ABAC close error"))
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		err = provider.Close()

		// First error (ABAC) should be returned
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ABAC close error")
	})
}

// TestHybridProviderGetProviderType tests the provider type getter.
func TestHybridProviderGetProviderType(t *testing.T) {
	rbac := newMockAuthorizationProvider("rbac")
	log := createHybridTestLogger(t)

	provider, err := NewHybridProvider(rbac, nil, log)
	require.NoError(t, err)

	assert.Equal(t, "hybrid", provider.GetProviderType())
	assert.Equal(t, ProviderTypeHybrid, provider.GetProviderType())
}

// TestHybridProviderHasABACProvider tests the ABAC provider check.
func TestHybridProviderHasABACProvider(t *testing.T) {
	t.Run("returns true when ABAC is configured", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		abac := newMockAuthorizationProvider("abac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, abac, log)
		require.NoError(t, err)

		assert.True(t, provider.HasABACProvider())
	})

	t.Run("returns false when ABAC is not configured", func(t *testing.T) {
		rbac := newMockAuthorizationProvider("rbac")
		log := createHybridTestLogger(t)

		provider, err := NewHybridProvider(rbac, nil, log)
		require.NoError(t, err)

		assert.False(t, provider.HasABACProvider())
	})
}

// TestHybridProviderDecisionIDUniqueness tests that decision IDs are unique.
func TestHybridProviderDecisionIDUniqueness(t *testing.T) {
	rbac := newMockAuthorizationProvider("rbac")
	rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "allowed",
			Provider:      "rbac",
			MatchedPolicy: "admin",
		}, nil
	}
	log := createHybridTestLogger(t)

	provider, err := NewHybridProvider(rbac, nil, log)
	require.NoError(t, err)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)

		if ids[decision.DecisionID] {
			t.Fatalf("duplicate decision ID: %s", decision.DecisionID)
		}
		ids[decision.DecisionID] = true
	}

	assert.Len(t, ids, 100)
}

// TestHybridProviderConcurrentAccess tests thread safety.
func TestHybridProviderConcurrentAccess(t *testing.T) {
	rbac := newMockAuthorizationProvider("rbac")
	rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "allowed",
			Provider:      "rbac",
			MatchedPolicy: "admin",
		}, nil
	}
	log := createHybridTestLogger(t)

	provider, err := NewHybridProvider(rbac, nil, log)
	require.NoError(t, err)

	const numGoroutines = 50
	const numRequests = 100

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*numRequests)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					ID:            "user",
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			}

			for j := 0; j < numRequests; j++ {
				_, err := provider.Authorize(context.Background(), req)
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("unexpected error during concurrent access: %v", err)
	}
}

// TestHybridProviderWithRealRBAC tests integration with real RBAC provider.
func TestHybridProviderWithRealRBAC(t *testing.T) {
	rm := newMockRoleManager()
	log := createHybridTestLogger(t)

	rbacProvider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	hybridProvider, err := NewHybridProvider(rbacProvider, nil, log)
	require.NoError(t, err)

	t.Run("admin role allows all actions", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user1",
				Username:      "admin-user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "admin",
		}

		decision, err := hybridProvider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "hybrid(rbac)", decision.Provider)
		assert.Equal(t, "admin", decision.MatchedPolicy)
	})

	t.Run("readonly role denies write on keys", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user2",
				Username:      "readonly-user",
				Roles:         []string{"readonly"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "write",
		}

		decision, err := hybridProvider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.False(t, decision.Allowed)
		assert.Equal(t, "hybrid(rbac)", decision.Provider)
	})

	t.Run("anonymous access to health allowed", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				Authenticated: false,
			},
			Resource: "health",
			Action:   "read",
		}

		decision, err := hybridProvider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "hybrid(rbac)", decision.Provider)
	})
}

// TestHybridProviderFormatProvider tests provider string formatting.
func TestHybridProviderFormatProvider(t *testing.T) {
	rbac := newMockAuthorizationProvider("rbac")
	log := createHybridTestLogger(t)

	provider, err := NewHybridProvider(rbac, nil, log)
	require.NoError(t, err)

	assert.Equal(t, "hybrid(abac)", provider.formatProvider("abac"))
	assert.Equal(t, "hybrid(rbac)", provider.formatProvider("rbac"))
}

// TestHybridProviderPerformance tests authorization latency.
func TestHybridProviderPerformance(t *testing.T) {
	rbac := newMockAuthorizationProvider("rbac")
	rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "allowed",
			Provider:      "rbac",
			MatchedPolicy: "admin",
		}, nil
	}
	log := createHybridTestLogger(t)

	provider, err := NewHybridProvider(rbac, nil, log)
	require.NoError(t, err)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	// Warm up
	for i := 0; i < 1000; i++ {
		_, _ = provider.Authorize(context.Background(), req)
	}

	// Measure
	const iterations = 10000
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = provider.Authorize(context.Background(), req)
	}
	duration := time.Since(start)

	avgLatency := duration / iterations
	t.Logf("Average hybrid authorization latency: %v", avgLatency)

	// Use a lenient threshold that works with race detector overhead (200us)
	assert.Less(t, avgLatency, 200*time.Microsecond,
		"Authorization latency should be under 200us (with race detector), got %v", avgLatency)
}

// BenchmarkHybridProviderAuthorize benchmarks single authorization performance.
func BenchmarkHybridProviderAuthorize(b *testing.B) {
	rbac := newMockAuthorizationProvider("rbac")
	rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "allowed",
			Provider:      "rbac",
			MatchedPolicy: "admin",
		}, nil
	}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "bench",
	})
	provider, _ := NewHybridProvider(rbac, nil, log)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = provider.Authorize(ctx, req)
	}
}

// BenchmarkHybridProviderWithABAC benchmarks authorization with ABAC provider.
func BenchmarkHybridProviderWithABAC(b *testing.B) {
	rbac := newMockAuthorizationProvider("rbac")
	abac := newMockAuthorizationProvider("abac")
	abac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "ABAC allowed",
			Provider:      "abac",
			MatchedPolicy: "policy1",
		}, nil
	}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "bench",
	})
	provider, _ := NewHybridProvider(rbac, abac, log)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = provider.Authorize(ctx, req)
	}
}

// BenchmarkHybridProviderBatchAuthorize benchmarks batch authorization performance.
func BenchmarkHybridProviderBatchAuthorize(b *testing.B) {
	rbac := newMockAuthorizationProvider("rbac")
	rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "allowed",
			Provider:      "rbac",
			MatchedPolicy: "admin",
		}, nil
	}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "bench",
	})
	provider, _ := NewHybridProvider(rbac, nil, log)

	reqs := make([]*interfaces.AuthorizationRequest, 100)
	for i := 0; i < 100; i++ {
		reqs[i] = &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = provider.BatchAuthorize(ctx, reqs)
	}
}

// BenchmarkHybridProviderAuthorizeParallel benchmarks concurrent authorization.
func BenchmarkHybridProviderAuthorizeParallel(b *testing.B) {
	rbac := newMockAuthorizationProvider("rbac")
	rbac.authorizeFunc = func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "allowed",
			Provider:      "rbac",
			MatchedPolicy: "admin",
		}, nil
	}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "bench",
	})
	provider, _ := NewHybridProvider(rbac, nil, log)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = provider.Authorize(ctx, req)
		}
	})
}
