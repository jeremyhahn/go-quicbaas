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

// Package service provides the PEP service interface for QuicBaaS.
package service

import (
	"context"
)

// =============================================================================
// PEP Service - Policy Enforcement Point
// =============================================================================

// PEPService provides high-performance authorization checking with policy caching
// and synchronization. It is designed for sub-5μs latency authorization decisions.
type PEPService interface {
	// Check performs a single authorization check.
	// This is optimized for ~2-5μs latency with cached policies.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Authorization check request with subject, action, and resource
	// Returns:
	//   - CheckResponse: Contains decision, matched policy/rule, and evaluation time
	//   - error: ErrInvalidInput, ErrTimeout, ErrInternal
	Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error)

	// CheckBatch performs batch authorization checks efficiently.
	// This is optimized for processing multiple checks with minimal overhead.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch check request with multiple authorization checks
	// Returns:
	//   - BatchCheckResponse: Contains per-check decisions and total evaluation time
	//   - error: ErrInvalidInput, ErrTimeout, ErrInternal
	CheckBatch(ctx context.Context, req *BatchCheckRequest) (*BatchCheckResponse, error)

	// Subscribe subscribes to policy updates for a tenant.
	// This enables real-time policy synchronization for distributed PEPs.
	// Parameters:
	//   - ctx: Request context with timeout and cancellation
	//   - req: Subscribe request with tenant ID and resource patterns
	// Returns:
	//   - <-chan *PolicyUpdate: Channel of policy updates (snapshot, create, update, delete)
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrTimeout
	Subscribe(ctx context.Context, req *SubscribePolicyUpdatesRequest) (<-chan *PolicyUpdate, error)
}
