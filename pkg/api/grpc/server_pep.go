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

package grpc

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// defaultPollInterval is the interval for polling policy changes.
const defaultPollInterval = 5 * time.Second

// PEPServer implements the Policy Enforcement Point gRPC service.
type PEPServer struct {
	iampb.UnimplementedPEPServiceServer
	pepService PEPService
	logger     *logger.Logger
}

// PEPService defines the interface for policy enforcement operations.
// This will be implemented by the service layer.
type PEPService interface {
	// Check performs a single authorization check.
	Check(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error)

	// CheckBatch performs batch authorization checks.
	CheckBatch(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error)

	// Subscribe streams policy updates to clients.
	Subscribe(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error
}

// PolicyUpdate represents a policy change notification.
type PolicyUpdate struct {
	Type       PolicyUpdateType
	PolicyData []byte
	PolicyID   string
	Version    int64
}

// PolicyUpdateType represents the type of policy update.
type PolicyUpdateType int

const (
	// PolicyUpdateSnapshot represents a full policy snapshot.
	PolicyUpdateSnapshot PolicyUpdateType = iota
	// PolicyUpdateCreated represents a new policy creation.
	PolicyUpdateCreated
	// PolicyUpdateUpdated represents a policy update.
	PolicyUpdateUpdated
	// PolicyUpdateDeleted represents a policy deletion.
	PolicyUpdateDeleted
)

// NewPEPServer creates a new PEP gRPC server.
func NewPEPServer(pepService PEPService, log *logger.Logger) *PEPServer {
	return &PEPServer{
		pepService: pepService,
		logger:     log.WithField("component", "pep-grpc-server"),
	}
}

// Check performs a single authorization check.
func (s *PEPServer) Check(ctx context.Context, req *iampb.CheckRequest) (*iampb.CheckResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"subject_id": req.Subject.GetId(),
		"action":     req.Action,
		"resource":   req.Resource,
	}).Debug("Processing PEP Check request")

	// Validate request
	if req.Subject == nil {
		return nil, status.Error(codes.InvalidArgument, "subject is required")
	}
	if req.Action == "" {
		return nil, status.Error(codes.InvalidArgument, "action is required")
	}
	if req.Resource == "" {
		return nil, status.Error(codes.InvalidArgument, "resource is required")
	}

	// Convert proto request to domain request
	checkReq := &iam.CheckRequest{
		Subject:  *convertProtoToSubject(req.Subject),
		Action:   req.Action,
		Resource: req.Resource,
		Context:  make(map[string]interface{}),
	}

	// Convert context
	for k, v := range req.Context {
		checkReq.Context[k] = v
	}

	// Perform authorization check
	result, err := s.pepService.Check(ctx, checkReq)
	if err != nil {
		s.logger.WithError(err).Error("Authorization check failed")
		return nil, convertPEPError(err)
	}

	// Convert result to proto response
	return &iampb.CheckResponse{
		Allowed:          result.Allowed,
		MatchedPolicy:    result.MatchedPolicy,
		MatchedRule:      result.MatchedRule,
		EvaluationTimeNs: result.EvaluationTimeNs,
	}, nil
}

// CheckBatch performs batch authorization checks for multiple resources.
func (s *PEPServer) CheckBatch(ctx context.Context, req *iampb.CheckBatchRequest) (*iampb.CheckBatchResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"subject_id":     req.Subject.GetId(),
		"action":         req.Action,
		"resource_count": len(req.Resources),
	}).Debug("Processing PEP CheckBatch request")

	// Validate request
	if req.Subject == nil {
		return nil, status.Error(codes.InvalidArgument, "subject is required")
	}
	if req.Action == "" {
		return nil, status.Error(codes.InvalidArgument, "action is required")
	}
	if len(req.Resources) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one resource is required")
	}

	// Convert proto subject to domain subject
	subject := convertProtoToSubject(req.Subject)

	// Convert context
	reqContext := make(map[string]interface{})
	for k, v := range req.Context {
		reqContext[k] = v
	}

	// Perform batch authorization check
	results, evaluationTime, err := s.pepService.CheckBatch(ctx, subject, req.Action, req.Resources, reqContext)
	if err != nil {
		s.logger.WithError(err).Error("Batch authorization check failed")
		return nil, convertPEPError(err)
	}

	// Convert results to proto response
	return &iampb.CheckBatchResponse{
		Results:          results,
		EvaluationTimeNs: evaluationTime,
	}, nil
}

// Subscribe streams policy updates to the client.
func (s *PEPServer) Subscribe(req *iampb.SubscribeRequest, stream iampb.PEPService_SubscribeServer) error {
	s.logger.WithFields(map[string]interface{}{
		"service_id":    req.ServiceId,
		"pattern_count": len(req.ResourcePatterns),
	}).Info("Processing PEP Subscribe request")

	// Validate request
	if req.ServiceId == "" {
		return status.Error(codes.InvalidArgument, "service_id is required")
	}

	// Create channel for policy updates
	updatesChan := make(chan *PolicyUpdate, 100)
	defer close(updatesChan)

	// Start subscription in background goroutine
	errChan := make(chan error, 1)
	go func() {
		err := s.pepService.Subscribe(stream.Context(), req.ServiceId, req.ResourcePatterns, updatesChan)
		if err != nil && !stderrors.Is(err, io.EOF) {
			s.logger.WithError(err).Error("Policy subscription failed")
			errChan <- err
		}
		close(errChan)
	}()

	// Stream updates to client
	for {
		select {
		case <-stream.Context().Done():
			s.logger.Debug("Client disconnected from policy subscription")
			return nil

		case err := <-errChan:
			if err != nil {
				return convertPEPError(err)
			}
			return nil

		case update, ok := <-updatesChan:
			if !ok {
				// Channel closed, subscription ended
				s.logger.Debug("Policy subscription channel closed")
				return nil
			}

			// Convert update to proto message
			protoUpdate := &iampb.PolicyUpdate{
				Type:       convertPolicyUpdateType(update.Type),
				PolicyData: update.PolicyData,
				PolicyId:   update.PolicyID,
				Version:    update.Version,
			}

			// Send update to client
			if err := stream.Send(protoUpdate); err != nil {
				s.logger.WithError(err).Error("Failed to send policy update to client")
				return convertPEPError(&dberrors.QBError{
					Code: dberrors.ErrInternal,
					Op:   "pep.Subscribe",
					Err:  err,
				})
			}

			s.logger.WithFields(map[string]interface{}{
				"update_type": update.Type,
				"policy_id":   update.PolicyID,
				"version":     update.Version,
			}).Debug("Sent policy update to client")
		}
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// convertPolicyUpdateType converts domain PolicyUpdateType to proto PolicyUpdate_Type.
func convertPolicyUpdateType(t PolicyUpdateType) iampb.PolicyUpdate_Type {
	switch t {
	case PolicyUpdateSnapshot:
		return iampb.PolicyUpdate_SNAPSHOT
	case PolicyUpdateCreated:
		return iampb.PolicyUpdate_CREATED
	case PolicyUpdateUpdated:
		return iampb.PolicyUpdate_UPDATED
	case PolicyUpdateDeleted:
		return iampb.PolicyUpdate_DELETED
	default:
		return iampb.PolicyUpdate_SNAPSHOT
	}
}

// =============================================================================
// Error Types
// =============================================================================

// PolicySerializationError is returned when policy data cannot be serialized.
type PolicySerializationError struct {
	PolicyID string
	Cause    error
}

// Error implements the error interface.
func (e *PolicySerializationError) Error() string {
	return "failed to serialize policy " + e.PolicyID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *PolicySerializationError) Unwrap() error {
	return e.Cause
}

// PolicyListError is returned when listing policies fails during subscription polling.
type PolicyListError struct {
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *PolicyListError) Error() string {
	return "failed to list policies for tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *PolicyListError) Unwrap() error {
	return e.Cause
}

// =============================================================================
// Error Conversion
// =============================================================================

// convertPEPError converts PEP domain errors to gRPC status errors.
func convertPEPError(err error) error {
	if err == nil {
		return nil
	}

	var dErr *dberrors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	switch dErr.Code {
	case dberrors.ErrNotFound:
		return status.Error(codes.NotFound, dErr.Error())
	case dberrors.ErrAlreadyExists:
		return status.Error(codes.AlreadyExists, dErr.Error())
	case dberrors.ErrInvalidInput, dberrors.ErrValidation:
		return status.Error(codes.InvalidArgument, dErr.Error())
	case dberrors.ErrUnauthorized:
		return status.Error(codes.Unauthenticated, dErr.Error())
	case dberrors.ErrForbidden:
		return status.Error(codes.PermissionDenied, dErr.Error())
	case dberrors.ErrTimeout, dberrors.ErrDeadlineExceeded:
		return status.Error(codes.DeadlineExceeded, dErr.Error())
	case dberrors.ErrResourceExhausted:
		return status.Error(codes.ResourceExhausted, dErr.Error())
	case dberrors.ErrUnavailable:
		return status.Error(codes.Unavailable, dErr.Error())
	case dberrors.ErrConflict:
		return status.Error(codes.FailedPrecondition, dErr.Error())
	case dberrors.ErrUnimplemented, dberrors.ErrNotImplemented:
		return status.Error(codes.Unimplemented, dErr.Error())
	case dberrors.ErrCanceled:
		return status.Error(codes.Canceled, dErr.Error())
	default:
		return status.Error(codes.Internal, dErr.Error())
	}
}

// PEPServiceAdapter adapts iam.PolicyEngine to the PEPService interface.
type PEPServiceAdapter struct {
	policyEngine *iam.PolicyEngine
	pollInterval time.Duration
}

// PEPServiceAdapterOption is a functional option for configuring PEPServiceAdapter.
type PEPServiceAdapterOption func(*PEPServiceAdapter)

// WithPollInterval sets the polling interval for the subscription.
func WithPollInterval(interval time.Duration) PEPServiceAdapterOption {
	return func(a *PEPServiceAdapter) {
		if interval > 0 {
			a.pollInterval = interval
		}
	}
}

// NewPEPServiceAdapter creates a new PEP service adapter.
func NewPEPServiceAdapter(policyEngine *iam.PolicyEngine, opts ...PEPServiceAdapterOption) *PEPServiceAdapter {
	adapter := &PEPServiceAdapter{
		policyEngine: policyEngine,
		pollInterval: defaultPollInterval,
	}
	for _, opt := range opts {
		opt(adapter)
	}
	return adapter
}

// Check performs a single authorization check.
func (a *PEPServiceAdapter) Check(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	return a.policyEngine.Evaluate(ctx, req)
}

// CheckBatch performs batch authorization checks.
func (a *PEPServiceAdapter) CheckBatch(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
	results := make([]bool, len(resources))
	var totalTime int64

	for i, resource := range resources {
		req := &iam.CheckRequest{
			Subject:  *subject,
			Action:   action,
			Resource: resource,
			Context:  context,
		}
		resp, err := a.policyEngine.Evaluate(ctx, req)
		if err != nil {
			return nil, 0, err
		}
		results[i] = resp.Allowed
		totalTime += resp.EvaluationTimeNs
	}

	return results, totalTime, nil
}

// Subscribe streams policy updates to clients using a polling-based approach.
// It sends an initial snapshot of all matching policies, then polls for changes
// at the configured interval. Detected additions, updates, and deletions are
// pushed to the updates channel as individual PolicyUpdate events.
func (a *PEPServiceAdapter) Subscribe(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error {
	if a.policyEngine == nil {
		// No policy engine available - block until context is cancelled
		<-ctx.Done()
		return ctx.Err()
	}

	tenantID := iam.TenantFromContext(ctx)

	// Send initial snapshot of all matching policies
	knownPolicies, err := a.sendSnapshot(ctx, tenantID, resourcePatterns, updates)
	if err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "pep.Subscribe.snapshot",
			Err:  err,
		}
	}

	// Poll for changes using a ticker
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			knownPolicies, err = a.pollChanges(ctx, tenantID, resourcePatterns, knownPolicies, updates)
			if err != nil {
				return &dberrors.QBError{
					Code: dberrors.ErrInternal,
					Op:   "pep.Subscribe.poll",
					Err:  err,
				}
			}
		}
	}
}

// policySnapshot holds tracked state for a single policy.
type policySnapshot struct {
	version int64
	data    []byte
}

// sendSnapshot lists all policies matching the resource patterns and sends them
// as a snapshot update. Returns a map of policy name to snapshot for change tracking.
func (a *PEPServiceAdapter) sendSnapshot(
	ctx context.Context,
	tenantID string,
	resourcePatterns []string,
	updates chan<- *PolicyUpdate,
) (map[string]policySnapshot, error) {

	policies, err := a.policyEngine.ListPolicies(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return nil, &PolicyListError{TenantID: tenantID, Cause: err}
	}

	known := make(map[string]policySnapshot, len(policies))

	for _, policy := range policies {
		if !policyMatchesPatterns(policy, resourcePatterns) {
			continue
		}

		data, serErr := json.Marshal(policy)
		if serErr != nil {
			return nil, &PolicySerializationError{PolicyID: policy.Metadata.Name, Cause: serErr}
		}

		known[policy.Metadata.Name] = policySnapshot{
			version: policy.Metadata.Updated.UnixNano(),
			data:    data,
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case updates <- &PolicyUpdate{
			Type:       PolicyUpdateSnapshot,
			PolicyData: data,
			PolicyID:   policy.Metadata.Name,
			Version:    policy.Metadata.Updated.UnixNano(),
		}:
		}
	}

	return known, nil
}

// pollChanges lists current policies, compares against the known set, and sends
// created, updated, or deleted events as appropriate. Returns the updated known map.
func (a *PEPServiceAdapter) pollChanges(
	ctx context.Context,
	tenantID string,
	resourcePatterns []string,
	known map[string]policySnapshot,
	updates chan<- *PolicyUpdate,
) (map[string]policySnapshot, error) {

	policies, err := a.policyEngine.ListPolicies(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return known, &PolicyListError{TenantID: tenantID, Cause: err}
	}

	current := make(map[string]policySnapshot, len(policies))

	for _, policy := range policies {
		if !policyMatchesPatterns(policy, resourcePatterns) {
			continue
		}

		version := policy.Metadata.Updated.UnixNano()
		data, serErr := json.Marshal(policy)
		if serErr != nil {
			return known, &PolicySerializationError{PolicyID: policy.Metadata.Name, Cause: serErr}
		}

		current[policy.Metadata.Name] = policySnapshot{
			version: version,
			data:    data,
		}

		prev, existed := known[policy.Metadata.Name]

		// Determine update type
		var updateType PolicyUpdateType
		switch {
		case !existed:
			updateType = PolicyUpdateCreated
		case prev.version != version:
			updateType = PolicyUpdateUpdated
		default:
			// No change
			continue
		}

		select {
		case <-ctx.Done():
			return known, ctx.Err()
		case updates <- &PolicyUpdate{
			Type:       updateType,
			PolicyData: data,
			PolicyID:   policy.Metadata.Name,
			Version:    version,
		}:
		}
	}

	// Detect deleted policies
	for name, snap := range known {
		if _, exists := current[name]; !exists {
			select {
			case <-ctx.Done():
				return known, ctx.Err()
			case updates <- &PolicyUpdate{
				Type:     PolicyUpdateDeleted,
				PolicyID: name,
				Version:  snap.version,
			}:
			}
		}
	}

	return current, nil
}

// policyMatchesPatterns checks whether a policy's rules reference any resources
// that match the given resource patterns. An empty pattern list matches all policies.
func policyMatchesPatterns(policy *iam.Policy, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}

	for _, rule := range policy.Spec.Rules {
		for _, resource := range rule.Resources {
			for _, pattern := range patterns {
				if resourceMatchesPattern(resource, pattern) {
					return true
				}
			}
		}
	}
	return false
}

// resourceMatchesPattern performs a simple prefix or wildcard match between a
// resource identifier and a pattern. Patterns ending in "/*" or "/**" match
// any resource sharing the prefix before the wildcard. An exact string match
// is also accepted.
func resourceMatchesPattern(resource, pattern string) bool {
	if pattern == "*" || pattern == "**" {
		return true
	}

	// Strip trailing wildcard for prefix match
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(resource, prefix)
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(resource, prefix)
	}

	return resource == pattern
}

// Compile-time check that PEPServiceAdapter implements PEPService.
var _ PEPService = (*PEPServiceAdapter)(nil)
