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

// This file re-exports IAM domain types from pkg/iam for SDK consumers.
// Types are prefixed with IAMDomain to distinguish them from SDK client IAM
// types used for API communication.
package quicbaas

import (
	pkgiam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// IAMDomain type aliases for policy-related structs.
type (
	// IAMDomainPolicy is the Kubernetes-style policy struct with
	// APIVersion/Kind/Metadata/Spec.
	IAMDomainPolicy = pkgiam.Policy

	// IAMDomainPolicyMetadata contains policy identification and description.
	IAMDomainPolicyMetadata = pkgiam.PolicyMetadata

	// IAMDomainPolicySpec contains the policy specification.
	IAMDomainPolicySpec = pkgiam.PolicySpec

	// IAMDomainSubjectSelector defines criteria for matching subjects.
	IAMDomainSubjectSelector = pkgiam.SubjectSelector

	// IAMDomainRule represents a single authorization rule within a policy.
	IAMDomainRule = pkgiam.Rule

	// IAMDomainSubject represents a principal in an authorization check.
	IAMDomainSubject = pkgiam.Subject

	// IAMDomainEffect represents the authorization decision (allow or deny).
	IAMDomainEffect = pkgiam.Effect

	// IAMDomainCondition represents a policy evaluation condition.
	IAMDomainCondition = pkgiam.Condition

	// IAMDomainCheckRequest represents an authorization check request.
	IAMDomainCheckRequest = pkgiam.CheckRequest

	// IAMDomainCheckResponse represents an authorization check response.
	IAMDomainCheckResponse = pkgiam.CheckResponse

	// IAMDomainListOptions provides pagination options for list operations.
	IAMDomainListOptions = pkgiam.ListOptions

	// IAMDomainPolicyEngine is the policy engine for ABAC evaluation.
	IAMDomainPolicyEngine = pkgiam.PolicyEngine

	// IAMDomainPolicyEngineOptions configures the policy engine.
	IAMDomainPolicyEngineOptions = pkgiam.PolicyEngineOptions

	// IAMDomainPolicyStorage defines policy storage operations.
	IAMDomainPolicyStorage = pkgiam.PolicyStorage

	// IAMDomainPEPConfig holds Policy Enforcement Point configuration.
	IAMDomainPEPConfig = pkgiam.PEPConfig

	// IAMDomainCacheConfig holds policy cache configuration.
	IAMDomainCacheConfig = pkgiam.CacheConfig

	// IAMDomainTenantManager defines tenant management operations.
	IAMDomainTenantManager = pkgiam.TenantManager
)

// IAMDomain effect constants.
const (
	// IAMDomainEffectAllow grants access.
	IAMDomainEffectAllow = pkgiam.EffectAllow

	// IAMDomainEffectDeny denies access.
	IAMDomainEffectDeny = pkgiam.EffectDeny
)

// IAMDomain constructor functions.
var (
	// IAMDomainNewPolicyEngine creates a new policy engine with the given options.
	IAMDomainNewPolicyEngine = pkgiam.NewPolicyEngine

	// IAMDomainNewMemoryStorage creates a new in-memory policy storage.
	IAMDomainNewMemoryStorage = pkgiam.NewMemoryStorage

	// IAMDomainNewCacheConfig creates a new CacheConfig from PEPConfig.
	IAMDomainNewCacheConfig = pkgiam.NewCacheConfig

	// IAMDomainNewPolicyCache creates a new PolicyCache with the given config.
	IAMDomainNewPolicyCache = pkgiam.NewPolicyCache
)
