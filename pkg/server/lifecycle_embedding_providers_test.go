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

//go:build vector

package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

// TestRegisterEmbeddingProviders_AllBranches covers the per-provider switch in
// registerEmbeddingProviders for the openai and lmstudio providers (which
// construct without a network round-trip) and the voyage error branch (which
// rejects an unknown model). Provider construction does not dial the backend, so
// the test stays hermetic.
func TestRegisterEmbeddingProviders_AllBranches(t *testing.T) {
	srv := withTempServer(t)

	base := embedding.Config{
		APIURL:         "http://127.0.0.1:1",
		APIKey:         "test-key",
		Model:          "text-embedding-3-small",
		Dimensions:     768,
		Timeout:        1,
		MaxBatchSize:   128,
		CacheSize:      1000,
		CacheTTL:       3600,
		MaxRetries:     1,
		RetryBackoffMS: 100,
	}

	cases := []struct {
		provider string
		wantErr  bool
	}{
		{"openai", false},
		{"lmstudio", false},
		// voyage rejects an unrecognised model at construction time, exercising
		// the provider-create error branch.
		{"voyage", true},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			cfg := base
			cfg.Provider = tc.provider

			mgr, err := embedding.NewManager(cfg)
			require.NoError(t, err, "manager construction must succeed for %s", tc.provider)

			err = srv.registerEmbeddingProviders(mgr, &cfg)
			if tc.wantErr {
				require.Error(t, err, "%s registration must surface the provider-create error", tc.provider)
			} else {
				require.NoError(t, err, "%s provider must register cleanly", tc.provider)
			}
		})
	}
}

// TestRegisterEmbeddingProviders_DuplicateRegistrationErrors covers the
// RegisterProvider-error sub-branches: registering the same provider twice makes
// the manager reject the duplicate, and registerEmbeddingProviders surfaces the
// register-error for the ollama, openai, and lmstudio branches.
func TestRegisterEmbeddingProviders_DuplicateRegistrationErrors(t *testing.T) {
	srv := withTempServer(t)

	base := embedding.Config{
		APIURL:         "http://127.0.0.1:1",
		APIKey:         "test-key",
		Model:          "text-embedding-3-small",
		Dimensions:     768,
		Timeout:        1,
		MaxBatchSize:   128,
		CacheSize:      1000,
		CacheTTL:       3600,
		MaxRetries:     1,
		RetryBackoffMS: 100,
	}

	for _, provider := range []string{"ollama", "openai", "lmstudio"} {
		t.Run(provider, func(t *testing.T) {
			cfg := base
			cfg.Provider = provider

			mgr, err := embedding.NewManager(cfg)
			require.NoError(t, err)

			// First registration succeeds.
			require.NoError(t, srv.registerEmbeddingProviders(mgr, &cfg))
			// Second registration must hit the RegisterProvider duplicate-error
			// branch and surface a non-nil error.
			require.Error(t, srv.registerEmbeddingProviders(mgr, &cfg),
				"re-registering %s must surface the duplicate-registration error", provider)
		})
	}
}
