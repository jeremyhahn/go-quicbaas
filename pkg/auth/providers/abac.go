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

// Package providers implements authorization providers for QuicBaaS.
package providers

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ABACProviderError represents an error from the ABAC provider.
type ABACProviderError struct {
	Operation string
	Message   string
	Cause     error
}

// Error returns the error message.
func (e *ABACProviderError) Error() string {
	if e.Cause != nil {
		return "abac provider: " + e.Operation + ": " + e.Message + ": " + e.Cause.Error()
	}
	return "abac provider: " + e.Operation + ": " + e.Message
}

// Unwrap returns the underlying error.
func (e *ABACProviderError) Unwrap() error {
	return e.Cause
}

// providerType is the type identifier for this provider.
const providerType = "abac"

// ABACProvider implements interfaces.AuthorizationProvider using the IAM PolicyEvaluator.
// It provides attribute-based access control (ABAC) authorization decisions.
//
// Thread-safety: This implementation is thread-safe. All operations are designed
// for concurrent access without external synchronization.
//
// Performance target: < 5us per check with compiled policy cache.
type ABACProvider struct {
	evaluator *iam.PolicyEvaluator
	tenantID  string
	logger    *logger.Logger
	closed    atomic.Bool
}

// NewABACProvider creates a new ABAC authorization provider.
//
// Parameters:
//   - evaluator: The IAM policy evaluator for evaluating authorization requests.
//   - tenantID: The tenant identifier for multi-tenant deployments.
//   - log: Logger instance for logging authorization events.
//
// Returns nil error on success.
func NewABACProvider(evaluator *iam.PolicyEvaluator, tenantID string, log *logger.Logger) (*ABACProvider, error) {
	if evaluator == nil {
		return nil, &ABACProviderError{
			Operation: "NewABACProvider",
			Message:   "evaluator cannot be nil",
		}
	}

	if log == nil {
		return nil, &ABACProviderError{
			Operation: "NewABACProvider",
			Message:   "logger cannot be nil",
		}
	}

	return &ABACProvider{
		evaluator: evaluator,
		tenantID:  tenantID,
		logger:    log.WithField("provider", providerType),
	}, nil
}

// Authorize checks if the request is allowed based on ABAC policies.
//
// This method converts the interfaces.AuthorizationRequest to an iam.CheckRequest,
// evaluates it against the configured policies, and returns an AuthorizationDecision.
//
// Context is used for cancellation and deadline propagation.
//
// Returns an error if the provider is closed or if evaluation fails.
func (p *ABACProvider) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	startTime := time.Now()

	// Check if provider is closed
	if p.closed.Load() {
		return nil, &ABACProviderError{
			Operation: "Authorize",
			Message:   "provider is closed",
		}
	}

	// Validate request
	if req == nil {
		return &interfaces.AuthorizationDecision{
			Allowed:          false,
			Reason:           "request cannot be nil",
			Provider:         providerType,
			DecisionID:       generateDecisionID(),
			EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
		}, nil
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, &ABACProviderError{
			Operation: "Authorize",
			Message:   "context cancelled",
			Cause:     ctx.Err(),
		}
	default:
	}

	// Convert to IAM CheckRequest
	checkReq := convertToCheckRequest(ctx, req, p.tenantID)

	// Evaluate using the policy evaluator
	checkResp, err := p.evaluator.Evaluate(ctx, checkReq)
	if err != nil {
		p.logger.WithError(err).Error("policy evaluation failed")
		return nil, &ABACProviderError{
			Operation: "Authorize",
			Message:   "policy evaluation failed",
			Cause:     err,
		}
	}

	// Convert response to AuthorizationDecision
	decision := convertToAuthorizationDecision(checkResp, startTime)

	p.logger.WithFields(map[string]interface{}{
		"subject":        req.Subject.ID,
		"resource":       req.Resource,
		"action":         req.Action,
		"allowed":        decision.Allowed,
		"matched_policy": decision.MatchedPolicy,
		"latency_ns":     decision.EvaluationTimeNs,
	}).Debug("authorization decision made")

	return decision, nil
}

// BatchAuthorize performs multiple authorization checks efficiently.
//
// This method processes multiple requests and returns corresponding decisions.
// Processing continues even if individual checks fail; failed checks will have
// appropriate error information in their decision.
//
// Returns an error only if the provider is closed or context is cancelled.
func (p *ABACProvider) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	startTime := time.Now()

	// Check if provider is closed
	if p.closed.Load() {
		return nil, &ABACProviderError{
			Operation: "BatchAuthorize",
			Message:   "provider is closed",
		}
	}

	// Handle nil or empty requests
	if len(reqs) == 0 {
		return []*interfaces.AuthorizationDecision{}, nil
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, &ABACProviderError{
			Operation: "BatchAuthorize",
			Message:   "context cancelled",
			Cause:     ctx.Err(),
		}
	default:
	}

	decisions := make([]*interfaces.AuthorizationDecision, len(reqs))

	for i, req := range reqs {
		// Check context between iterations for long batches
		select {
		case <-ctx.Done():
			// Fill remaining decisions with cancelled status
			for j := i; j < len(reqs); j++ {
				decisions[j] = &interfaces.AuthorizationDecision{
					Allowed:          false,
					Reason:           "batch authorization cancelled",
					Provider:         providerType,
					DecisionID:       generateDecisionID(),
					EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
				}
			}
			return decisions, nil
		default:
		}

		decision, err := p.Authorize(ctx, req)
		if err != nil {
			// Individual check failed - create a deny decision with error info
			decisions[i] = &interfaces.AuthorizationDecision{
				Allowed:          false,
				Reason:           "authorization check failed: " + err.Error(),
				Provider:         providerType,
				DecisionID:       generateDecisionID(),
				EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
			}
		} else {
			decisions[i] = decision
		}
	}

	p.logger.WithFields(map[string]interface{}{
		"batch_size": len(reqs),
		"latency_ns": time.Since(startTime).Nanoseconds(),
	}).Debug("batch authorization completed")

	return decisions, nil
}

// GetProviderType returns the provider type identifier.
func (p *ABACProvider) GetProviderType() string {
	return providerType
}

// Close releases any resources held by the provider.
// After Close is called, all Authorize calls will return an error.
func (p *ABACProvider) Close() error {
	if p.closed.Swap(true) {
		// Already closed
		return &ABACProviderError{
			Operation: "Close",
			Message:   "provider already closed",
		}
	}
	p.logger.Info("ABAC provider closed")
	return nil
}

// convertToCheckRequest converts an interfaces.AuthorizationRequest to an iam.CheckRequest.
// It extracts vector context from the Go context and adds it to the request context.
func convertToCheckRequest(ctx context.Context, req *interfaces.AuthorizationRequest, defaultTenantID string) *iam.CheckRequest {
	// Determine tenant ID - use subject's tenant if set, otherwise use provider's default
	tenantID := req.Subject.TenantID
	if tenantID == "" {
		tenantID = defaultTenantID
	}

	// Build IAM Subject
	subject := iam.Subject{
		ID:            req.Subject.ID,
		Name:          req.Subject.Username,
		TenantID:      tenantID,
		Tenant:        tenantID, // Set both for compatibility
		Roles:         copyStringSlice(req.Subject.Roles),
		Groups:        copyStringSlice(req.Subject.Groups),
		Attributes:    copyMapInterface(req.Subject.Attributes),
		Authenticated: req.Subject.Authenticated,
	}

	// Build context with any existing values
	requestContext := copyMapInterface(req.Context)
	if requestContext == nil {
		requestContext = make(map[string]interface{})
	}

	// Extract and add vector context if present
	extractVectorContext(ctx, requestContext)

	// Extract and add parameter context if present
	extractParameterContext(ctx, requestContext)

	// Extract and add tenant context if present
	if tenantFromCtx := iam.TenantFromContext(ctx); tenantFromCtx != "" {
		requestContext["tenant_id"] = tenantFromCtx
	}

	// Extract and add user context if present
	if userFromCtx := iam.UserFromContext(ctx); userFromCtx != "" {
		requestContext["user_id"] = userFromCtx
	}

	return &iam.CheckRequest{
		Subject:  subject,
		Action:   req.Action,
		Resource: req.Resource,
		Context:  requestContext,
	}
}

// extractVectorContext extracts vector-specific context from the Go context
// and adds it to the request context map for ABAC policy evaluation.
// This enables policies to use conditions like:
//   - context.namespace == "embeddings-prod"
//   - context.batch_size <= 1000
//   - context.operation == "search"
func extractVectorContext(ctx context.Context, requestContext map[string]interface{}) {
	vectorCtx := iam.VectorContextFromContext(ctx)
	if vectorCtx == nil {
		return
	}

	// Add namespace if set
	if vectorCtx.Namespace != "" {
		requestContext["namespace"] = vectorCtx.Namespace
	}

	// Add vector ID if set
	if vectorCtx.VectorID != "" {
		requestContext["vector_id"] = vectorCtx.VectorID
	}

	// Add operation if set
	if vectorCtx.Operation != "" {
		requestContext["operation"] = vectorCtx.Operation
	}

	// Add batch size if set (0 is valid, so always include if > 0)
	if vectorCtx.BatchSize > 0 {
		requestContext["batch_size"] = vectorCtx.BatchSize
	}

	// Add metadata presence flag (useful for policies that restrict metadata writes)
	requestContext["has_metadata"] = len(vectorCtx.VectorMetadata) > 0

	// Add metadata count
	requestContext["metadata_count"] = len(vectorCtx.VectorMetadata)
}

// extractParameterContext extracts parameter-specific context from the Go context
// and adds it to the request context map for ABAC policy evaluation.
// This enables policies to use conditions like:
//   - context.parameter_path startswith "/app/database/"
//   - context.parameter_encrypted == true
//   - context.parameter_tags.env == "production"
func extractParameterContext(ctx context.Context, requestContext map[string]interface{}) {
	paramCtx := iam.ParameterContextFromContext(ctx)
	if paramCtx == nil {
		return
	}

	// Add parameter path if set
	if paramCtx.Path != "" {
		requestContext["parameter_path"] = paramCtx.Path
	}

	// Add parameter type if set
	if paramCtx.Type != "" {
		requestContext["parameter_type"] = paramCtx.Type
	}

	// Add encryption status
	requestContext["parameter_encrypted"] = paramCtx.IsEncrypted

	// Add tags if present
	if len(paramCtx.Tags) > 0 {
		requestContext["parameter_tags"] = paramCtx.Tags
	}

	// Add owner if set
	if paramCtx.Owner != "" {
		requestContext["parameter_owner"] = paramCtx.Owner
	}

	// Add tenant if set
	if paramCtx.TenantID != "" {
		requestContext["parameter_tenant"] = paramCtx.TenantID
	}

	// Add operation if set
	if paramCtx.Operation != "" {
		requestContext["parameter_operation"] = paramCtx.Operation
	}

	// Add batch size if set (for batch operations)
	if paramCtx.BatchSize > 0 {
		requestContext["parameter_batch_size"] = paramCtx.BatchSize
	}

	// Add audit enabled flag
	requestContext["parameter_audit_enabled"] = paramCtx.AuditEnabled

	// Add description presence flag
	requestContext["parameter_has_description"] = paramCtx.Description != ""
}

// convertToAuthorizationDecision converts an iam.CheckResponse to an interfaces.AuthorizationDecision.
func convertToAuthorizationDecision(resp *iam.CheckResponse, startTime time.Time) *interfaces.AuthorizationDecision {
	matchedPolicy := resp.MatchedPolicy
	if resp.MatchedRule != "" && matchedPolicy != "" {
		matchedPolicy = matchedPolicy + "/" + resp.MatchedRule
	}

	return &interfaces.AuthorizationDecision{
		Allowed:          resp.Allowed,
		Reason:           resp.Reason,
		Provider:         providerType,
		MatchedPolicy:    matchedPolicy,
		DecisionID:       generateDecisionID(),
		EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
	}
}

// generateDecisionID generates a unique decision identifier for audit purposes.
func generateDecisionID() string {
	return uuid.New().String()
}

// copyStringSlice creates a copy of a string slice.
func copyStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	result := make([]string, len(s))
	copy(result, s)
	return result
}

// copyMapInterface creates a shallow copy of a map[string]interface{}.
func copyMapInterface(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
