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

package service

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ParameterStoreService defines operations for hierarchical parameter management.
// Parameters are tenant-scoped and support encryption, TTL, and tagging.
//
// Thread-safety: All implementations must be safe for concurrent use.
//
// Performance targets:
//   - Get operations: < 1ms p99
//   - Set operations: < 5ms p99
//   - List operations: < 10ms p99 (up to 100 results)
//   - Batch operations: < 50ms p99 (up to 100 items)
type ParameterStoreService interface {
	// GetParameter retrieves a single parameter by path.
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing the parameter path and decryption options.
	//
	// Returns:
	//   - GetParameterResponse containing the parameter value and metadata.
	//   - ErrParameterNotFound if the parameter doesn't exist.
	//   - ErrParameterExpired if the parameter has expired.
	//   - ErrUnauthorized if access is denied.
	GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error)

	// SetParameter creates or updates a parameter.
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing path, value, and optional metadata.
	//
	// Returns:
	//   - SetParameterResponse confirming creation/update.
	//   - ErrParameterExists if path exists and Overwrite is false.
	//   - ErrInvalidPath if the path format is invalid.
	//   - ErrUnauthorized if access is denied.
	SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error)

	// DeleteParameter removes a parameter by path.
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing the parameter path.
	//
	// Returns:
	//   - DeleteParameterResponse confirming deletion.
	//   - ErrParameterNotFound if the parameter doesn't exist.
	//   - ErrUnauthorized if access is denied.
	DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error)

	// GetParameters retrieves multiple parameters by paths (batch get).
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing parameter paths.
	//
	// Returns:
	//   - GetParametersResponse with successfully retrieved parameters.
	//   - Per-path errors for missing/expired parameters.
	GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error)

	// SetParameters creates or updates multiple parameters (batch set).
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing parameter data.
	//
	// Returns:
	//   - SetParametersResponse with per-path results.
	//   - Atomic: all succeed or all fail.
	SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error)

	// DeleteParameters removes multiple parameters (batch delete).
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing parameter paths.
	//
	// Returns:
	//   - DeleteParametersResponse with per-path results.
	DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error)

	// ListParameters returns parameters matching a path prefix.
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing prefix and pagination options.
	//
	// Returns:
	//   - ListParametersResponse with matching parameters.
	//   - Results are sorted by path.
	ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error)

	// SearchByTags finds parameters matching tag key-value pairs.
	//
	// Parameters:
	//   - ctx: Context for cancellation and tenant extraction.
	//   - req: Request containing required tags.
	//
	// Returns:
	//   - SearchByTagsResponse with matching parameters.
	//   - Multiple tags are ANDed together.
	SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error)
}
