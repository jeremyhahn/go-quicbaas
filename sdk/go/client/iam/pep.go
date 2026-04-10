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

package iam

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	pepServiceName = "quicbaas.IAM.PEP"
)

// PEPClient provides Policy Enforcement Point operations.
//
// The PEP client is used to make authorization decisions at runtime.
// It evaluates ABAC policies to determine if a subject is allowed
// to perform an action on a resource.
type PEPClient interface {
	// Check performs a single authorization check.
	Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error)

	// CheckBatch performs multiple authorization checks in a single request.
	CheckBatch(ctx context.Context, req *CheckBatchRequest) (*CheckBatchResponse, error)
}

// CheckRequest represents an authorization check request.
type CheckRequest struct {
	// Subject is the entity making the request.
	Subject CheckSubject `json:"subject"`

	// Action is the operation being performed.
	// Format: "resource_type:operation" (e.g., "data:read", "user:create")
	Action string `json:"action"`

	// Resource is the resource being accessed.
	// Format: "type://path" (e.g., "orders://123", "kv://config/app")
	Resource string `json:"resource"`

	// Context contains additional request context for policy evaluation.
	Context map[string]interface{} `json:"context"`

	// TenantID is the tenant context for the check.
	TenantID string `json:"tenant_id"`

	// CacheKey is an optional key for caching the decision.
	CacheKey string `json:"cache_key"`

	// BypassCache forces a fresh policy evaluation.
	BypassCache bool `json:"bypass_cache"`
}

// CheckSubject represents the subject (user/service) in an authorization check.
type CheckSubject struct {
	// ID is the unique identifier of the subject.
	ID string `json:"id"`

	// TenantID is the tenant the subject belongs to.
	TenantID string `json:"tenant_id"`

	// Type is the subject type (user, service, etc.).
	Type SubjectType `json:"type"`

	// Roles are the roles assigned to the subject.
	Roles []string `json:"roles"`

	// Groups are the groups the subject belongs to.
	Groups []string `json:"groups"`

	// Attributes are additional subject attributes for ABAC evaluation.
	Attributes map[string]interface{} `json:"attributes"`

	// SessionID is the optional session identifier.
	SessionID string `json:"session_id"`

	// ClientIP is the client's IP address.
	ClientIP string `json:"client_ip"`

	// UserAgent is the client's user agent string.
	UserAgent string `json:"user_agent"`
}

// CheckResponse represents the result of an authorization check.
type CheckResponse struct {
	// Allowed indicates if the request is authorized.
	Allowed bool `json:"allowed"`

	// Effect is the authorization effect (allow or deny).
	Effect Effect `json:"effect"`

	// MatchedPolicy is the name of the policy that determined the outcome.
	MatchedPolicy string `json:"matched_policy"`

	// MatchedRule is the name of the rule that determined the outcome.
	MatchedRule string `json:"matched_rule"`

	// EvaluationTimeNs is the time taken to evaluate in nanoseconds.
	EvaluationTimeNs int64 `json:"evaluation_time_ns"`

	// Reason is a human-readable explanation of the decision.
	Reason string `json:"reason"`

	// CacheHit indicates if the result was served from cache.
	CacheHit bool `json:"cache_hit"`

	// Obligations are actions that must be performed if access is granted.
	Obligations []Obligation `json:"obligations"`

	// Timestamp is when the decision was made.
	Timestamp time.Time `json:"timestamp"`
}

// Obligation represents an action that must be performed as a condition of access.
type Obligation struct {
	// ID is the obligation identifier.
	ID string `json:"id"`

	// Type is the obligation type.
	Type ObligationType `json:"type"`

	// Parameters are obligation-specific parameters.
	Parameters map[string]interface{} `json:"parameters"`

	// Description is a human-readable description.
	Description string `json:"description"`
}

// ObligationType represents the type of obligation.
type ObligationType int

const (
	// ObligationTypeLog requires logging the access.
	ObligationTypeLog ObligationType = iota
	// ObligationTypeAudit requires creating an audit record.
	ObligationTypeAudit
	// ObligationTypeNotify requires sending a notification.
	ObligationTypeNotify
	// ObligationTypeEncrypt requires encrypting the response.
	ObligationTypeEncrypt
	// ObligationTypeMask requires masking sensitive data.
	ObligationTypeMask
	// ObligationTypeRateLimit applies rate limiting.
	ObligationTypeRateLimit
	// ObligationTypeCustom is a custom obligation type.
	ObligationTypeCustom
)

// String returns the string representation of the obligation type.
func (t ObligationType) String() string {
	switch t {
	case ObligationTypeLog:
		return "LOG"
	case ObligationTypeAudit:
		return "AUDIT"
	case ObligationTypeNotify:
		return "NOTIFY"
	case ObligationTypeEncrypt:
		return "ENCRYPT"
	case ObligationTypeMask:
		return "MASK"
	case ObligationTypeRateLimit:
		return "RATE_LIMIT"
	case ObligationTypeCustom:
		return "CUSTOM"
	default:
		return "UNKNOWN"
	}
}

// CheckBatchRequest represents a batch of authorization checks.
type CheckBatchRequest struct {
	// Requests are the individual check requests.
	Requests []CheckRequest `json:"requests"`

	// StopOnDeny stops processing when the first deny is encountered.
	StopOnDeny bool `json:"stop_on_deny"`

	// Parallel enables parallel evaluation of requests.
	Parallel bool `json:"parallel"`

	// MaxConcurrency limits parallel evaluations (0 = unlimited).
	MaxConcurrency int `json:"max_concurrency"`
}

// CheckBatchResponse represents the results of a batch authorization check.
type CheckBatchResponse struct {
	// Results are the individual check results in the same order as requests.
	Results []CheckResponse `json:"results"`

	// AllAllowed indicates if all requests were allowed.
	AllAllowed bool `json:"all_allowed"`

	// AllowedCount is the number of allowed requests.
	AllowedCount int `json:"allowed_count"`

	// DeniedCount is the number of denied requests.
	DeniedCount int `json:"denied_count"`

	// TotalEvaluationTimeNs is the total evaluation time in nanoseconds.
	TotalEvaluationTimeNs int64 `json:"total_evaluation_time_ns"`
}

// Internal request types for transport

type checkRequestInternal struct {
	Request *CheckRequest `json:"request"`
}

type checkBatchRequestInternal struct {
	Request *CheckBatchRequest `json:"request"`
}

// pepClient implements the PEPClient interface.
type pepClient struct {
	transport transport.Transport
}

// NewPEPClient creates a new PEPClient.
//
// Returns nil if the transport is nil.
func NewPEPClient(t transport.Transport) PEPClient {
	if t == nil {
		return nil
	}
	return &pepClient{
		transport: t,
	}
}

// Check performs a single authorization check.
func (c *pepClient) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	if err := validateCheckRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.PEP.Check", "context is required")
	}

	internalReq := &checkRequestInternal{
		Request: req,
	}

	resp, err := c.transport.Request(ctx, pepServiceName, "Check", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.PEP.Check", "nil response from server")
	}

	return parseCheckResponse(resp)
}

// CheckBatch performs multiple authorization checks in a single request.
func (c *pepClient) CheckBatch(ctx context.Context, req *CheckBatchRequest) (*CheckBatchResponse, error) {
	if err := validateCheckBatchRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.PEP.CheckBatch", "context is required")
	}

	internalReq := &checkBatchRequestInternal{
		Request: req,
	}

	resp, err := c.transport.Request(ctx, pepServiceName, "CheckBatch", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.PEP.CheckBatch", "nil response from server")
	}

	return parseCheckBatchResponse(resp)
}

// Validation functions

func validateCheckRequest(req *CheckRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.PEP.Check", "request is required")
	}
	if req.Subject.ID == "" {
		return errors.NewInvalidInput("iam.PEP.Check", "subject ID is required")
	}
	if req.Action == "" {
		return errors.NewInvalidInput("iam.PEP.Check", "action is required")
	}
	if req.Resource == "" {
		return errors.NewInvalidInput("iam.PEP.Check", "resource is required")
	}
	return nil
}

func validateCheckBatchRequest(req *CheckBatchRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.PEP.CheckBatch", "request is required")
	}
	if len(req.Requests) == 0 {
		return errors.NewInvalidInput("iam.PEP.CheckBatch", "at least one request is required")
	}

	for i, r := range req.Requests {
		if r.Subject.ID == "" {
			return errors.NewInvalidInput("iam.PEP.CheckBatch", "subject ID is required").
				WithContext("request_index", i)
		}
		if r.Action == "" {
			return errors.NewInvalidInput("iam.PEP.CheckBatch", "action is required").
				WithContext("request_index", i)
		}
		if r.Resource == "" {
			return errors.NewInvalidInput("iam.PEP.CheckBatch", "resource is required").
				WithContext("request_index", i)
		}
	}
	return nil
}

// parseCheckResponse parses the response from Check.
func parseCheckResponse(resp interface{}) (*CheckResponse, error) {
	// If already a *CheckResponse, return it directly.
	if checkResp, ok := resp.(*CheckResponse); ok {
		return checkResp, nil
	}

	// Handle map response (from gRPC or REST transport).
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.PEP.Check", "invalid response type")
	}

	result := &CheckResponse{}

	if v, ok := respMap["allowed"].(bool); ok {
		result.Allowed = v
	}
	if v, ok := respMap["matched_policy"].(string); ok {
		result.MatchedPolicy = v
	}
	if v, ok := respMap["matched_rule"].(string); ok {
		result.MatchedRule = v
	}
	if v, ok := respMap["evaluation_time_ns"].(float64); ok {
		result.EvaluationTimeNs = int64(v)
	}
	if v, ok := respMap["evaluation_time_ns"].(int64); ok {
		result.EvaluationTimeNs = v
	}
	if v, ok := respMap["reason"].(string); ok {
		result.Reason = v
	}
	if v, ok := respMap["cache_hit"].(bool); ok {
		result.CacheHit = v
	}
	if v, ok := respMap["timestamp"].(time.Time); ok {
		result.Timestamp = v
	}

	// Set effect based on allowed
	if result.Allowed {
		result.Effect = EffectAllow
	} else {
		result.Effect = EffectDeny
	}

	return result, nil
}

// parseCheckBatchResponse parses the response from CheckBatch.
func parseCheckBatchResponse(resp interface{}) (*CheckBatchResponse, error) {
	// If already a *CheckBatchResponse, return it directly.
	if batchResp, ok := resp.(*CheckBatchResponse); ok {
		return batchResp, nil
	}

	// Handle map response (from gRPC or REST transport).
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.PEP.CheckBatch", "invalid response type")
	}

	result := &CheckBatchResponse{
		Results: []CheckResponse{},
	}

	// Parse results array
	if resultsVal, ok := respMap["results"]; ok {
		// Handle map[string]bool format from gRPC handler
		if resultsMap, ok := resultsVal.(map[string]bool); ok {
			for _, allowed := range resultsMap {
				checkResp := CheckResponse{
					Allowed: allowed,
				}
				if allowed {
					checkResp.Effect = EffectAllow
					result.AllowedCount++
				} else {
					checkResp.Effect = EffectDeny
					result.DeniedCount++
				}
				result.Results = append(result.Results, checkResp)
			}
		}
		// Handle []interface{} format
		if results, ok := resultsVal.([]interface{}); ok {
			for _, r := range results {
				if rMap, ok := r.(map[string]interface{}); ok {
					checkResp := CheckResponse{}
					if v, ok := rMap["allowed"].(bool); ok {
						checkResp.Allowed = v
						if v {
							checkResp.Effect = EffectAllow
							result.AllowedCount++
						} else {
							checkResp.Effect = EffectDeny
							result.DeniedCount++
						}
					}
					if v, ok := rMap["matched_policy"].(string); ok {
						checkResp.MatchedPolicy = v
					}
					if v, ok := rMap["matched_rule"].(string); ok {
						checkResp.MatchedRule = v
					}
					result.Results = append(result.Results, checkResp)
				}
			}
		}
	}

	if v, ok := respMap["evaluation_time_ns"].(float64); ok {
		result.TotalEvaluationTimeNs = int64(v)
	}
	if v, ok := respMap["evaluation_time_ns"].(int64); ok {
		result.TotalEvaluationTimeNs = v
	}

	// Compute AllAllowed
	result.AllAllowed = result.DeniedCount == 0 && len(result.Results) > 0

	return result, nil
}

// Helper functions for common authorization patterns

// IsAllowed is a convenience function that returns true if access is allowed.
func (r *CheckResponse) IsAllowed() bool {
	return r != nil && r.Allowed
}

// IsDenied is a convenience function that returns true if access is denied.
func (r *CheckResponse) IsDenied() bool {
	return r == nil || !r.Allowed
}

// HasObligations returns true if the response has obligations.
func (r *CheckResponse) HasObligations() bool {
	return r != nil && len(r.Obligations) > 0
}

// GetObligationsByType returns obligations of a specific type.
func (r *CheckResponse) GetObligationsByType(t ObligationType) []Obligation {
	if r == nil {
		return nil
	}

	var result []Obligation
	for _, o := range r.Obligations {
		if o.Type == t {
			result = append(result, o)
		}
	}
	return result
}

// FirstDenied returns the first denied result in a batch response.
func (r *CheckBatchResponse) FirstDenied() *CheckResponse {
	if r == nil {
		return nil
	}

	for i := range r.Results {
		if !r.Results[i].Allowed {
			return &r.Results[i]
		}
	}
	return nil
}

// GetDeniedIndices returns the indices of denied requests.
func (r *CheckBatchResponse) GetDeniedIndices() []int {
	if r == nil {
		return nil
	}

	var indices []int
	for i, result := range r.Results {
		if !result.Allowed {
			indices = append(indices, i)
		}
	}
	return indices
}

// GetAllowedIndices returns the indices of allowed requests.
func (r *CheckBatchResponse) GetAllowedIndices() []int {
	if r == nil {
		return nil
	}

	var indices []int
	for i, result := range r.Results {
		if result.Allowed {
			indices = append(indices, i)
		}
	}
	return indices
}
