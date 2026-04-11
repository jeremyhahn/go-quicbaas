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

// Package providers implements authorization providers for QuicBaaS.
package providers

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Provider type constant for hybrid provider.
const ProviderTypeHybrid = "hybrid"

// HybridProvider implements the AuthorizationProvider interface using a combination
// of ABAC and RBAC authorization. It tries ABAC first (if available), then falls
// back to RBAC when ABAC is not available or returns an error.
//
// Thread-safety: This provider is fully thread-safe. Both underlying providers
// handle their own synchronization, and this provider uses atomic operations
// for state management and decision ID generation.
//
// Decision flow:
//  1. If ABAC provider exists and returns a successful decision, use it
//  2. If ABAC provider returns an error or doesn't exist, fall back to RBAC
//  3. The returned decision indicates which provider made the decision
type HybridProvider struct {
	rbac        interfaces.AuthorizationProvider // Required - always present
	abac        interfaces.AuthorizationProvider // Optional - nil when iam not set
	logger      *logger.Logger
	decisionSeq atomic.Uint64
	closed      atomic.Bool
}

// NewHybridProvider creates a new hybrid authorization provider that combines
// ABAC and RBAC authorization strategies.
//
// Parameters:
//   - rbac: The RBAC provider for role-based authorization. Must not be nil.
//   - abac: The ABAC provider for attribute-based authorization. Can be nil for
//     builds without iam.
//   - log: The logger for diagnostic output. If nil, a default logger is created.
//
// Returns:
//   - *HybridProvider: The configured provider.
//   - error: ErrNilRBACProvider if rbac is nil.
func NewHybridProvider(rbac, abac interfaces.AuthorizationProvider, log *logger.Logger) (*HybridProvider, error) {
	if rbac == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "providers.NewHybridProvider",
			Err:  ErrNilRBACProvider,
		}
	}

	if log == nil {
		var err error
		log, err = logger.New(logger.LogConfig{
			Level:     "info",
			Format:    "json",
			Output:    "stdout",
			Component: "hybrid-provider",
		})
		if err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "providers.NewHybridProvider",
				Err:  err,
			}
		}
	}

	p := &HybridProvider{
		rbac:   rbac,
		abac:   abac,
		logger: log.WithField("provider", ProviderTypeHybrid),
	}

	if abac != nil {
		p.logger.Debug("hybrid provider initialized with ABAC and RBAC")
	} else {
		p.logger.Debug("hybrid provider initialized with RBAC only (ABAC not available)")
	}

	return p, nil
}

// Authorize checks if the request is allowed using the hybrid strategy.
// It first attempts ABAC authorization (if available), then falls back to RBAC.
//
// The decision's Provider field indicates which provider was used:
//   - "hybrid(abac)" - ABAC provider made the decision
//   - "hybrid(rbac)" - RBAC provider made the decision (fallback)
//
// Performance: Target latency < 5us when using ABAC, < 1us with RBAC fallback.
//
// Context is respected for cancellation; if the context is canceled before
// the check completes, ErrCanceled is returned.
func (p *HybridProvider) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	if p.closed.Load() {
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "HybridProvider.Authorize",
			Err:  ErrProviderClosed,
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrCanceled,
			Op:   "HybridProvider.Authorize",
			Err:  err,
		}
	}

	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "HybridProvider.Authorize",
			Err:  ErrNilRequest,
		}
	}

	startTime := time.Now()

	// Try ABAC first if available
	if p.abac != nil {
		decision, err := p.abac.Authorize(ctx, req)
		if err == nil {
			// ABAC returned a successful decision
			decision.Provider = p.formatProvider("abac")
			decision.DecisionID = p.generateDecisionID()
			decision.EvaluationTimeNs = time.Since(startTime).Nanoseconds()

			p.logger.WithFields(map[string]interface{}{
				"subject":        req.Subject.ID,
				"resource":       req.Resource,
				"action":         req.Action,
				"allowed":        decision.Allowed,
				"decided_by":     "abac",
				"matched_policy": decision.MatchedPolicy,
			}).Debug("authorization decision made by ABAC")

			return decision, nil
		}

		// ABAC failed, log and fall back to RBAC
		p.logger.WithFields(map[string]interface{}{
			"subject":  req.Subject.ID,
			"resource": req.Resource,
			"action":   req.Action,
			"error":    err.Error(),
		}).Debug("ABAC authorization failed, falling back to RBAC")
	}

	// Fall back to RBAC
	decision, err := p.rbac.Authorize(ctx, req)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "HybridProvider.Authorize",
			Err:  err,
		}
	}

	decision.Provider = p.formatProvider("rbac")
	decision.DecisionID = p.generateDecisionID()
	decision.EvaluationTimeNs = time.Since(startTime).Nanoseconds()

	p.logger.WithFields(map[string]interface{}{
		"subject":        req.Subject.ID,
		"resource":       req.Resource,
		"action":         req.Action,
		"allowed":        decision.Allowed,
		"decided_by":     "rbac",
		"matched_policy": decision.MatchedPolicy,
		"fallback":       p.abac != nil,
	}).Debug("authorization decision made by RBAC")

	return decision, nil
}

// BatchAuthorize performs multiple authorization checks efficiently.
// Each request is evaluated through the same ABAC-first, RBAC-fallback logic.
//
// Returns a slice of decisions corresponding to each request.
// If the context is canceled, processing stops and ErrCanceled is returned.
func (p *HybridProvider) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	if p.closed.Load() {
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "HybridProvider.BatchAuthorize",
			Err:  ErrProviderClosed,
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrCanceled,
			Op:   "HybridProvider.BatchAuthorize",
			Err:  err,
		}
	}

	if len(reqs) == 0 {
		return []*interfaces.AuthorizationDecision{}, nil
	}

	decisions := make([]*interfaces.AuthorizationDecision, len(reqs))

	for i, req := range reqs {
		// Check context cancellation between requests
		if err := ctx.Err(); err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrCanceled,
				Op:   "HybridProvider.BatchAuthorize",
				Err:  err,
			}
		}

		if req == nil {
			decisions[i] = &interfaces.AuthorizationDecision{
				Allowed:       false,
				Reason:        "nil request",
				Provider:      p.formatProvider("rbac"),
				MatchedPolicy: "",
				DecisionID:    p.generateDecisionID(),
			}
			continue
		}

		// Use the same Authorize logic for each request
		decision, err := p.Authorize(ctx, req)
		if err != nil {
			// If authorization itself failed (not just denied), create an error decision
			decisions[i] = &interfaces.AuthorizationDecision{
				Allowed:       false,
				Reason:        fmt.Sprintf("authorization error: %s", err.Error()),
				Provider:      ProviderTypeHybrid,
				MatchedPolicy: "",
				DecisionID:    p.generateDecisionID(),
			}
			continue
		}

		decisions[i] = decision
	}

	p.logger.WithFields(map[string]interface{}{
		"batch_size": len(reqs),
	}).Debug("batch authorization completed")

	return decisions, nil
}

// GetProviderType returns the provider type identifier.
func (p *HybridProvider) GetProviderType() string {
	return ProviderTypeHybrid
}

// Close releases any resources held by the provider and its underlying providers.
// After Close is called, all authorization requests will return ErrProviderClosed.
func (p *HybridProvider) Close() error {
	if p.closed.Swap(true) {
		// Already closed
		return nil
	}

	var closeErr error

	// Close ABAC provider if present
	if p.abac != nil {
		if err := p.abac.Close(); err != nil {
			p.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Warn("error closing ABAC provider")
			closeErr = err
		}
	}

	// Close RBAC provider
	if err := p.rbac.Close(); err != nil {
		p.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Warn("error closing RBAC provider")
		if closeErr == nil {
			closeErr = err
		}
	}

	p.logger.Debug("hybrid provider closed")
	return closeErr
}

// HasABACProvider returns true if the hybrid provider has an ABAC provider configured.
func (p *HybridProvider) HasABACProvider() bool {
	return p.abac != nil
}

// formatProvider formats the provider string to indicate which underlying provider
// made the decision.
func (p *HybridProvider) formatProvider(decided string) string {
	return fmt.Sprintf("hybrid(%s)", decided)
}

// generateDecisionID creates a unique identifier for the decision.
func (p *HybridProvider) generateDecisionID() string {
	seq := p.decisionSeq.Add(1)
	return fmt.Sprintf("hybrid-%d-%d", time.Now().UnixNano(), seq)
}
