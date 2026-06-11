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
	"sync"
	"sync/atomic"
	"time"
)

// PEP is the Policy Enforcement Point for local policy evaluation.
// It provides sub-5μs authorization checks with policy caching and synchronization.
type PEP struct {
	// config holds the PEP configuration
	config *PEPConfig

	// evaluator performs policy evaluation
	evaluator *PolicyEvaluator

	// compiler compiles policies
	compiler *PolicyCompiler

	// cache stores compiled policies
	cache *PolicyCache

	// syncer synchronizes policies from the IAM server
	syncer *PolicySyncer

	// metrics collects PEP performance metrics
	metrics *PEPMetrics

	// closed indicates if the PEP is closed
	closed atomic.Bool

	// mu protects concurrent access to PEP state
	mu sync.RWMutex
}

// PEPConfig holds the configuration for a PEP instance.
type PEPConfig struct {
	// TenantID is the tenant this PEP serves
	TenantID string

	// ResourcePrefix is the resource namespace for this PEP (e.g., "orders://")
	ResourcePrefix string

	// CacheTTL is the cache entry time-to-live
	CacheTTL time.Duration

	// SyncInterval is the policy sync interval
	SyncInterval time.Duration

	// MaxCacheSize is the maximum number of cached policies
	MaxCacheSize int

	// Client is the IAM client for fetching policies (optional, for remote sync)
	Client IAMClient

	// EnableSync enables automatic policy synchronization
	EnableSync bool
}

// DefaultPEPConfig returns a PEP configuration with sensible defaults.
func DefaultPEPConfig(tenantID string) *PEPConfig {
	return &PEPConfig{
		TenantID:       tenantID,
		ResourcePrefix: "",
		CacheTTL:       5 * time.Minute,
		SyncInterval:   30 * time.Second,
		MaxCacheSize:   10000,
		EnableSync:     false,
	}
}

// NewPEP creates a new Policy Enforcement Point.
func NewPEP(config *PEPConfig) *PEP {
	if config == nil {
		config = DefaultPEPConfig("")
	}

	// Create compiler and cache
	compiler := NewPolicyCompiler()
	cache := NewPolicyCache(NewCacheConfig(config))

	pep := &PEP{
		config:    config,
		compiler:  compiler,
		evaluator: NewPolicyEvaluator(compiler, cache),
		cache:     cache,
		metrics:   NewPEPMetrics(),
	}

	// Initialize syncer if sync is enabled
	if config.EnableSync && config.Client != nil {
		pep.syncer = NewPolicySyncer(config.Client, pep.cache, config.SyncInterval, config.TenantID)
	}

	return pep
}

// Check performs a single authorization check.
// This is optimized for ~2-5μs latency with cached policies.
func (p *PEP) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	if p.closed.Load() {
		return nil, ErrPEPClosed
	}

	// Validate request
	if err := p.validateCheckRequest(req); err != nil {
		return nil, err
	}

	// Record start time for metrics
	startTime := time.Now()

	// Get applicable policies from cache
	policies := p.cache.GetApplicablePolicies(req.Resource, req.Subject)

	// Record cache metrics
	if len(policies) > 0 {
		p.metrics.RecordCacheHit()
	} else {
		p.metrics.RecordCacheMiss()
	}

	// Evaluate the request
	response, err := p.evaluator.Evaluate(ctx, req)
	if err != nil {
		return nil, NewPolicyEvaluationError(p.config.TenantID, "", err)
	}

	// Record metrics
	p.metrics.RecordCheck(time.Since(startTime), response.Allowed)

	return response, nil
}

// CheckBatch performs batch authorization checks efficiently.
// This is optimized for processing multiple checks with minimal overhead.
func (p *PEP) CheckBatch(ctx context.Context, req *BatchCheckRequest) (*BatchCheckResponse, error) {
	if p.closed.Load() {
		return nil, ErrPEPClosed
	}

	if req == nil || len(req.Checks) == 0 {
		return nil, NewInvalidCheckRequestError("batch request is empty")
	}

	startTime := time.Now()
	responses := make([]*CheckResponse, len(req.Checks))

	// Process each check
	for i, checkReq := range req.Checks {
		resp, err := p.Check(ctx, checkReq)
		if err != nil {
			// Return error response for this check
			responses[i] = &CheckResponse{
				Allowed: false,
				Reason:  err.Error(),
			}
		} else {
			responses[i] = resp
		}
	}

	return &BatchCheckResponse{
		Responses:             responses,
		TotalEvaluationTimeNs: time.Since(startTime).Nanoseconds(),
	}, nil
}

// Sync synchronizes policies from the IAM server.
// This can be called manually or runs automatically if EnableSync is true.
func (p *PEP) Sync(ctx context.Context) error {
	if p.closed.Load() {
		return ErrPEPClosed
	}

	if p.syncer == nil {
		return NewIAMError(ErrPolicySyncFailed, "iam.pep.sync", "syncer not configured", nil)
	}

	return p.syncer.SyncNow(ctx)
}

// Subscribe subscribes to policy updates from the IAM server.
// Returns a channel that receives policy updates.
func (p *PEP) Subscribe(ctx context.Context, patterns []string) (<-chan PolicyUpdate, error) {
	if p.closed.Load() {
		return nil, ErrPEPClosed
	}

	if p.config.Client == nil {
		return nil, NewIAMError(ErrPolicySyncFailed, "iam.pep.subscribe", "IAM client not configured", nil)
	}

	// Subscribe to policy updates from the IAM server
	updateCh, err := p.config.Client.Subscribe(p.config.TenantID, patterns)
	if err != nil {
		return nil, NewPolicySyncError(p.config.TenantID, err)
	}

	// Create output channel
	outCh := make(chan PolicyUpdate, 100)

	// Process updates in background
	go func() {
		defer close(outCh)
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-updateCh:
				if !ok {
					return
				}
				if update != nil {
					// Apply update to cache
					p.applyPolicyUpdate(update)

					// Forward to subscribers
					select {
					case outCh <- *update:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return outCh, nil
}

// LoadPolicy loads a policy into the PEP cache.
// This can be used to pre-load policies or update them manually.
func (p *PEP) LoadPolicy(ctx context.Context, policy *Policy) error {
	if p.closed.Load() {
		return ErrPEPClosed
	}

	// Compile the policy
	compiled, err := p.compiler.Compile(policy)
	if err != nil {
		return NewPolicyCompilationError(p.config.TenantID, policy.Metadata.Name, err)
	}

	// Store in cache
	p.cache.Set(p.config.TenantID, compiled)

	return nil
}

// RemovePolicy removes a policy from the PEP cache.
func (p *PEP) RemovePolicy(policyName string) {
	if p.closed.Load() {
		return
	}

	p.cache.Invalidate(p.config.TenantID, policyName)
}

// GetStats returns current PEP statistics.
func (p *PEP) GetStats() *PEPStats {
	return p.metrics.GetStats()
}

// Close closes the PEP and stops background sync.
func (p *PEP) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop syncer if running
	if p.syncer != nil {
		p.syncer.Stop()
	}

	// Close client if present
	if p.config.Client != nil {
		if err := p.config.Client.Close(); err != nil {
			return err
		}
	}

	return nil
}

// applyPolicyUpdate applies a policy update to the cache.
func (p *PEP) applyPolicyUpdate(update *PolicyUpdate) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch update.Type {
	case PolicyUpdateSnapshot:
		// Full snapshot - replace all policies
		if update.Policy != nil {
			p.cache.Set(p.config.TenantID, update.Policy)
		}

	case PolicyUpdateCreated, PolicyUpdateUpdated:
		// Add or update policy
		if update.Policy != nil {
			p.cache.Set(p.config.TenantID, update.Policy)
		}

	case PolicyUpdateDeleted:
		// Remove policy
		p.cache.Invalidate(p.config.TenantID, update.PolicyID)
	}

	// Update metrics
	p.metrics.RecordPolicyUpdate()
}

// validateCheckRequest validates a check request.
func (p *PEP) validateCheckRequest(req *CheckRequest) error {
	if req == nil {
		return NewInvalidCheckRequestError("request is nil")
	}

	if req.Subject.ID == "" {
		return NewInvalidCheckRequestError("subject ID is required")
	}

	if req.Subject.TenantID == "" {
		return NewInvalidCheckRequestError("subject tenant ID is required")
	}

	if req.Subject.TenantID != p.config.TenantID {
		return NewInvalidCheckRequestError("subject tenant ID does not match PEP tenant ID")
	}

	if req.Action == "" {
		return NewInvalidCheckRequestError("action is required")
	}

	if req.Resource == "" {
		return NewInvalidCheckRequestError("resource is required")
	}

	return nil
}

// Start starts the PEP and begins background sync if enabled.
func (p *PEP) Start(ctx context.Context) error {
	if p.closed.Load() {
		return ErrPEPClosed
	}

	// Start syncer if enabled
	if p.syncer != nil {
		if err := p.syncer.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}
