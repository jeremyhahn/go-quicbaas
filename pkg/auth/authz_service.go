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

// Package auth provides authentication and authorization services for QuicBaaS.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// AuthzMetrics holds Prometheus metrics for authorization.
type AuthzMetrics struct {
	requestsTotal  *prometheus.CounterVec
	latencySeconds *prometheus.HistogramVec
	registeredOnce sync.Once
	registerErr    error
}

// newAuthzMetrics creates new authorization metrics and registers them.
func newAuthzMetrics() *AuthzMetrics {
	m := &AuthzMetrics{}
	m.registeredOnce.Do(func() {
		m.requestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "quicbaas_authz_requests_total",
				Help: "Total number of authorization requests",
			},
			[]string{"provider", "resource", "action", "allowed"},
		)

		m.latencySeconds = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "quicbaas_authz_latency_seconds",
				Help:    "Authorization request latency in seconds",
				Buckets: []float64{.0001, .0005, .001, .005, .01, .025, .05, .1},
			},
			[]string{"provider", "resource"},
		)

		// Register with the global QuicBaaS metrics registry
		if err := metrics.Registry.Register(m.requestsTotal); err != nil {
			// Check if already registered (not an error) using errors.As
			var alreadyRegisteredErr prometheus.AlreadyRegisteredError
			if !stderrors.As(err, &alreadyRegisteredErr) {
				m.registerErr = err
			}
		}
		if err := metrics.Registry.Register(m.latencySeconds); err != nil {
			var alreadyRegisteredErr prometheus.AlreadyRegisteredError
			if !stderrors.As(err, &alreadyRegisteredErr) {
				m.registerErr = err
			}
		}
	})
	return m
}

// recordRequest records a single authorization request metric.
func (m *AuthzMetrics) recordRequest(provider, resource, action string, allowed bool, latency time.Duration) {
	allowedStr := "false"
	if allowed {
		allowedStr = "true"
	}
	m.requestsTotal.WithLabelValues(provider, resource, action, allowedStr).Inc()
	m.latencySeconds.WithLabelValues(provider, resource).Observe(latency.Seconds())
}

// AuthorizationServiceImpl implements the AuthorizationService interface.
// It wraps an AuthorizationProvider with audit logging and metrics collection.
type AuthorizationServiceImpl struct {
	provider interfaces.AuthorizationProvider
	logger   *logger.Logger
	metrics  *AuthzMetrics
	closed   bool
	mu       sync.RWMutex
}

// NewAuthorizationService creates a new AuthorizationService wrapping the given provider.
func NewAuthorizationService(provider interfaces.AuthorizationProvider, log *logger.Logger) (*AuthorizationServiceImpl, error) {
	if provider == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "auth.NewAuthorizationService",
			Err:  ErrNilProvider,
		}
	}

	if log == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "auth.NewAuthorizationService",
			Err:  ErrNilLogger,
		}
	}

	m := newAuthzMetrics()
	if m.registerErr != nil {
		log.WithError(m.registerErr).Warn("Failed to register some authorization metrics")
	}

	log.Info("Authorization service initialized")

	return &AuthorizationServiceImpl{
		provider: provider,
		logger:   log,
		metrics:  m,
	}, nil
}

// Authorize performs a single authorization check with audit logging and metrics.
func (s *AuthorizationServiceImpl) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "auth.Authorize",
			Err:  ErrServiceClosed,
		}
	}
	s.mu.RUnlock()

	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "auth.Authorize",
			Err:  ErrNilRequest,
		}
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, &errors.QBError{
			Code: errors.ErrCanceled,
			Op:   "auth.Authorize",
			Err:  ctx.Err(),
		}
	default:
	}

	startTime := time.Now()

	// Delegate to the underlying provider
	decision, err := s.provider.Authorize(ctx, req)
	if err != nil {
		s.logAuthorizationError(req, err)
		return nil, err
	}

	latency := time.Since(startTime)

	// Generate decision ID if not already set
	if decision.DecisionID == "" {
		decision.DecisionID = generateDecisionID()
	}

	// Set evaluation time
	decision.EvaluationTimeNs = latency.Nanoseconds()

	// Ensure provider type is set
	if decision.Provider == "" {
		decision.Provider = s.provider.GetProviderType()
	}

	// Record metrics
	s.metrics.recordRequest(
		decision.Provider,
		req.Resource,
		req.Action,
		decision.Allowed,
		latency,
	)

	// Log audit trail
	s.logAuthorizationDecision(req, decision)

	return decision, nil
}

// BatchAuthorize performs multiple authorization checks with audit logging.
func (s *AuthorizationServiceImpl) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "auth.BatchAuthorize",
			Err:  ErrServiceClosed,
		}
	}
	s.mu.RUnlock()

	if len(reqs) == 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "auth.BatchAuthorize",
			Err:  ErrEmptyBatch,
		}
	}

	// Validate all requests before processing
	for i, req := range reqs {
		if req == nil {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "auth.BatchAuthorize",
				Err:  ErrNilRequestInBatch,
				Context: map[string]interface{}{
					"index": i,
				},
			}
		}
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, &errors.QBError{
			Code: errors.ErrCanceled,
			Op:   "auth.BatchAuthorize",
			Err:  ctx.Err(),
		}
	default:
	}

	startTime := time.Now()

	// Delegate to the underlying provider
	decisions, err := s.provider.BatchAuthorize(ctx, reqs)
	if err != nil {
		s.logger.WithError(err).Error("Batch authorization failed")
		return nil, err
	}

	batchLatency := time.Since(startTime)
	avgLatency := batchLatency / time.Duration(len(reqs))

	// Process each decision
	for i, decision := range decisions {
		if decision == nil {
			continue
		}

		// Generate decision ID if not already set
		if decision.DecisionID == "" {
			decision.DecisionID = generateDecisionID()
		}

		// Set evaluation time (approximate per-request)
		if decision.EvaluationTimeNs == 0 {
			decision.EvaluationTimeNs = avgLatency.Nanoseconds()
		}

		// Ensure provider type is set
		if decision.Provider == "" {
			decision.Provider = s.provider.GetProviderType()
		}

		// Record metrics for each request
		s.metrics.recordRequest(
			decision.Provider,
			reqs[i].Resource,
			reqs[i].Action,
			decision.Allowed,
			avgLatency,
		)

		// Log audit trail for each decision
		s.logAuthorizationDecision(reqs[i], decision)
	}

	return decisions, nil
}

// GetProvider returns the underlying authorization provider.
func (s *AuthorizationServiceImpl) GetProvider() interfaces.AuthorizationProvider {
	return s.provider
}

// GetProviderType returns the type of the underlying provider.
func (s *AuthorizationServiceImpl) GetProviderType() string {
	return s.provider.GetProviderType()
}

// Close releases resources held by the service and provider.
func (s *AuthorizationServiceImpl) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	s.logger.Info("Authorization service closing")

	return s.provider.Close()
}

// logAuthorizationDecision logs an authorization decision for audit purposes.
func (s *AuthorizationServiceImpl) logAuthorizationDecision(req *interfaces.AuthorizationRequest, decision *interfaces.AuthorizationDecision) {
	logEntry := s.logger.WithFields(map[string]interface{}{
		"decision_id":    decision.DecisionID,
		"subject_id":     req.Subject.ID,
		"subject_user":   req.Subject.Username,
		"tenant_id":      req.Subject.TenantID,
		"resource":       req.Resource,
		"action":         req.Action,
		"allowed":        decision.Allowed,
		"reason":         decision.Reason,
		"provider":       decision.Provider,
		"matched_policy": decision.MatchedPolicy,
		"evaluation_ns":  decision.EvaluationTimeNs,
		"authenticated":  req.Subject.Authenticated,
	})

	if decision.Allowed {
		logEntry.Debug("Authorization granted")
	} else {
		logEntry.Info("Authorization denied")
	}
}

// logAuthorizationError logs an authorization error.
func (s *AuthorizationServiceImpl) logAuthorizationError(req *interfaces.AuthorizationRequest, err error) {
	s.logger.WithError(err).WithFields(map[string]interface{}{
		"subject_id":   req.Subject.ID,
		"subject_user": req.Subject.Username,
		"resource":     req.Resource,
		"action":       req.Action,
	}).Error("Authorization check failed")
}

// generateDecisionID generates a unique decision ID using cryptographic random bytes.
func generateDecisionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto random fails
		return "dec-" + hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))[:32]
	}
	return "dec-" + hex.EncodeToString(b)
}
