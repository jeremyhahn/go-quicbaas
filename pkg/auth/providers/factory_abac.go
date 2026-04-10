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

package providers

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Default cache configuration values for the ABAC provider.
const (
	defaultCacheTTL          = 5 * time.Minute
	defaultCacheMaxSize      = 10000
	defaultBloomFilterSize   = 1000000 // 1M bits ~= 125KB
	defaultBloomFilterHashes = 7       // Optimal for ~1% false positive rate
)

// createABACProvider creates an ABAC authorization provider using the IAM PolicyEvaluator.
// This function is only available when the iam build tag is set.
func createABACProvider(cfg *FactoryConfig) (interfaces.AuthorizationProvider, error) {
	// Create a default logger if not provided
	log := cfg.Logger
	if log == nil {
		var err error
		log, err = logger.New(logger.LogConfig{
			Level:     "info",
			Format:    "json",
			Output:    "stdout",
			Component: "abac-provider-factory",
		})
		if err != nil {
			return nil, &errors.DragonError{
				Code: errors.ErrInternal,
				Op:   "providers.createABACProvider",
				Err:  err,
			}
		}
	}

	// Create policy compiler and cache for the evaluator
	compiler := iam.NewPolicyCompiler()

	// Create cache configuration with sensible defaults
	cacheConfig := &iam.CacheConfig{
		TTL:               defaultCacheTTL,
		MaxSize:           defaultCacheMaxSize,
		BloomFilterSize:   defaultBloomFilterSize,
		BloomFilterHashes: defaultBloomFilterHashes,
	}
	cache := iam.NewPolicyCache(cacheConfig)

	// Create the policy evaluator
	evaluator := iam.NewPolicyEvaluator(compiler, cache)

	// Create and return the ABAC provider
	return NewABACProvider(evaluator, cfg.TenantID, log)
}

// IsABACAvailable returns true if ABAC is available in this build.
// This function returns true when the iam build tag is set.
func IsABACAvailable() bool {
	return true
}
