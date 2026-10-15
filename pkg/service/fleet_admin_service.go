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
	stderrors "errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// FleetAdminService — Interface
// =============================================================================

// FleetAdminService manages global fleet node lifecycle operations such as
// disabling and re-enabling fleet nodes across all projects.
type FleetAdminService interface {
	// DisableFleetNode administratively disables a fleet node globally. All active
	// work claims for this fleet node should be cancelled by the caller. The
	// fleet node's OAuth2 client and mTLS cert pin are revoked via the IAM layer.
	// Returns FleetNodeAlreadyDisabledError if already disabled.
	DisableFleetNode(ctx context.Context, req *DisableFleetNodeRequest) (*DisableFleetNodeResponse, error)

	// EnableFleetNode re-activates a previously disabled fleet node.
	// Returns FleetNodeNotDisabledError if the fleet node is not currently disabled.
	EnableFleetNode(ctx context.Context, req *EnableFleetNodeRequest) (*EnableFleetNodeResponse, error)

	// GetFleetNodeRegistration retrieves a single fleet node registration.
	GetFleetNodeRegistration(ctx context.Context, req *GetFleetNodeRegistrationRequest) (*GetFleetNodeRegistrationResponse, error)

	// ListFleetNodeRegistrations returns all registered fleet nodes for a tenant.
	ListFleetNodeRegistrations(ctx context.Context, req *ListFleetNodeRegistrationsRequest) (*ListFleetNodeRegistrationsResponse, error)
}

// =============================================================================
// Request / Response Types
// =============================================================================

// DisableFleetNodeRequest contains the parameters for disabling a fleet node.
type DisableFleetNodeRequest struct {
	TenantID string
	NodeID   string
}

// DisableFleetNodeResponse is returned after a fleet node has been disabled.
type DisableFleetNodeResponse struct {
	NodeID     string    `json:"node_id"`
	Disabled   bool      `json:"disabled"`
	DisabledAt time.Time `json:"disabled_at"`
	Timestamp  time.Time `json:"timestamp"`
}

// EnableFleetNodeRequest contains the parameters for re-enabling a fleet node.
type EnableFleetNodeRequest struct {
	TenantID string
	NodeID   string
}

// EnableFleetNodeResponse is returned after a fleet node has been re-enabled.
type EnableFleetNodeResponse struct {
	NodeID    string    `json:"node_id"`
	Enabled   bool      `json:"enabled"`
	Timestamp time.Time `json:"timestamp"`
}

// GetFleetNodeRegistrationRequest identifies a fleet node by tenant and fleet node ID.
type GetFleetNodeRegistrationRequest struct {
	TenantID string
	NodeID   string
}

// GetFleetNodeRegistrationResponse wraps the retrieved fleet node registration.
type GetFleetNodeRegistrationResponse struct {
	Agent     *orchestration.FleetNode
	Timestamp time.Time
}

// ListFleetNodeRegistrationsRequest specifies which tenant's fleet nodes to list.
type ListFleetNodeRegistrationsRequest struct {
	TenantID string
	Filter   *orchestration.FleetNodeFilter
}

// ListFleetNodeRegistrationsResponse contains the list of fleet node registrations.
type ListFleetNodeRegistrationsResponse struct {
	Agents    []*orchestration.FleetNode
	Total     int
	Timestamp time.Time
}

// =============================================================================
// Typed Errors
// =============================================================================

// FleetAdminNotFoundError is returned when the target fleet node does not exist.
type FleetAdminNotFoundError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetAdminNotFoundError) Error() string {
	return "agent not found: tenant=" + e.TenantID + " agent=" + e.NodeID
}

// FleetNodeAlreadyDisabledError is returned when the fleet node is already disabled.
type FleetNodeAlreadyDisabledError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetNodeAlreadyDisabledError) Error() string {
	return "agent already disabled: tenant=" + e.TenantID + " agent=" + e.NodeID
}

// FleetNodeNotDisabledError is returned when the fleet node is not disabled.
type FleetNodeNotDisabledError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetNodeNotDisabledError) Error() string {
	return "agent not disabled: tenant=" + e.TenantID + " agent=" + e.NodeID
}

// FleetNodeDisabledError is returned when a disabled fleet node attempts an operation.
type FleetNodeDisabledError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetNodeDisabledError) Error() string {
	return "agent is disabled: tenant=" + e.TenantID + " agent=" + e.NodeID
}

// =============================================================================
// fleetAdminService — concrete implementation
// =============================================================================

// AppClientRevoker is the minimal interface required to revoke an OAuth2 application.
// It is satisfied by *iam.AppManager. Using a narrow interface keeps the
// fleet admin service free of a hard dependency on the full IAM package.
type AppClientRevoker interface {
	DeleteApp(ctx context.Context, clientID string) error
}

// FleetStreamCloser is the minimal interface for cancelling active gRPC streams
// on behalf of a specific fleet node. Implementations hold a per-agent context cancel
// registry keyed by nodeID.
//
// TODO(gap2-stream-close): A control plane stream-cancel registry does not yet exist.
// When the gRPC fleet node event stream server (pkg/api/grpc) is extended to track
// per-agent cancels, wire an implementation here via SetStreamCloser.
// Until then, streams time out naturally once the fleet node's OAuth2 client is
// revoked and token refresh fails.
type FleetStreamCloser interface {
	// CloseFleetNodeStreams cancels all active server-side gRPC streams for nodeID.
	CloseFleetNodeStreams(ctx context.Context, nodeID string) error
}

// FleetStreamCloseNotImplementedError is returned when no stream-close registry
// has been wired. Callers should treat this as a non-fatal advisory.
type FleetStreamCloseNotImplementedError struct {
	NodeID string
}

// Error implements the error interface.
func (e *FleetStreamCloseNotImplementedError) Error() string {
	return "agent stream close not implemented: no stream registry wired for agent=" + e.NodeID
}

// fleetAdminService implements FleetAdminService backed by a FleetRegistry.
type fleetAdminService struct {
	registry     orchestration.FleetRegistry
	appRevoker   AppClientRevoker  // optional: revokes OAuth2 client on disable
	streamCloser FleetStreamCloser // optional: cancels active gRPC streams on disable
	auditLogger  iam.AuditLogger   // optional: emits audit events
}

// NewFleetAdminService creates a new FleetAdminService backed by the given registry.
func NewFleetAdminService(registry orchestration.FleetRegistry) FleetAdminService {
	return &fleetAdminService{registry: registry}
}

// SetAppClientRevoker wires the OAuth2 app revoker used by DisableFleetNode to
// invalidate the fleet node's client credentials at the IAM layer.
func (s *fleetAdminService) SetAppClientRevoker(r AppClientRevoker) {
	s.appRevoker = r
}

// SetStreamCloser wires the stream-cancel registry used by DisableFleetNode to
// force-close active gRPC connections for the disabled fleet node.
func (s *fleetAdminService) SetStreamCloser(sc FleetStreamCloser) {
	s.streamCloser = sc
}

// SetFleetRegistry rebinds the fleet registry this service operates on.
//
// The facade constructs the FleetAdminService at Build() time with a private
// in-memory registry as a safe default for standalone callers. The server's
// composition root, however, owns the single shared fleet registry that the
// orchestrator AgentService (RegisterFleetNode / ListFleetNodes) writes to. Disable
// and Enable must resolve fleet nodes against that SAME registry, otherwise a
// fleet-registered fleet node is invisible to FleetAdmin and disable returns 404.
// The server calls this once, after the shared registry is established, to
// point FleetAdmin at it.
func (s *fleetAdminService) SetFleetRegistry(registry orchestration.FleetRegistry) {
	s.registry = registry
}

// SetAuditLogger wires the audit logger used to emit fleet node.disabled /
// fleet node.enabled events for compliance and observability.
func (s *fleetAdminService) SetAuditLogger(l iam.AuditLogger) {
	s.auditLogger = l
}

// DisableFleetNode marks the fleet node as disabled in the registry.
func (s *fleetAdminService) DisableFleetNode(ctx context.Context, req *DisableFleetNodeRequest) (*DisableFleetNodeResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.NodeID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	agent, err := s.registry.GetFleetNode(ctx, req.TenantID, req.NodeID)
	if err != nil {
		return nil, s.convertRegistryError(err, req.TenantID, req.NodeID)
	}

	if agent.Status == orchestration.FleetNodeStatusDisabled {
		return nil, &FleetNodeAlreadyDisabledError{TenantID: req.TenantID, NodeID: req.NodeID}
	}

	now := time.Now().UTC()
	agent.Status = orchestration.FleetNodeStatusDisabled
	agent.DisabledAt = now.UnixNano()
	// Do not pre-increment Version: UpdateFleetNode uses CAS and handles version bumping.

	if err := s.registry.UpdateFleetNode(ctx, agent); err != nil {
		return nil, err
	}

	// Revoke the fleet node's OAuth2 client so subsequent token requests are rejected.
	// The client_id is stored in the registration metadata under "oauth2_client_id".
	// If the key is absent (legacy registration), the app is looked up by fleet node ID
	// convention (nodeID == OAuth2 app name).
	if s.appRevoker != nil {
		clientID := req.NodeID
		if v, ok := agent.Metadata["oauth2_client_id"]; ok && v != "" {
			clientID = v
		}
		if revokeErr := s.appRevoker.DeleteApp(ctx, clientID); revokeErr != nil {
			// Non-fatal: the fleet node is already disabled in the registry.
			// Log via audit if available, but don't fail the operation.
			_ = s.emitAuditEvent(ctx, "agent.disable.oauth2_revoke_failed", req.TenantID, req.NodeID, false)
		} else {
			_ = s.emitAuditEvent(ctx, "agent.disable.oauth2_revoked", req.TenantID, req.NodeID, true)
		}

		// mTLS thumbprint pin: also revoke the secondary self_signed_tls_client_auth
		// app registered for the fleet node's peer certificate. Stored under "mtls_client_id".
		if mtlsID, ok := agent.Metadata["mtls_client_id"]; ok && mtlsID != "" {
			_ = s.appRevoker.DeleteApp(ctx, mtlsID) // best-effort
		}
	}

	// Force-close active gRPC streams for this fleet node.
	if s.streamCloser != nil {
		if closeErr := s.streamCloser.CloseFleetNodeStreams(ctx, req.NodeID); closeErr != nil {
			_ = s.emitAuditEvent(ctx, "agent.disable.stream_close_failed", req.TenantID, req.NodeID, false)
		}
	}
	// NOTE: If no stream-closer is wired, streams will time out naturally once
	// the fleet node's token refresh fails after OAuth2 client revocation.
	// See FleetStreamCloser and FleetStreamCloseNotImplementedError for details.

	// Emit fleet node.disabled audit event.
	_ = s.emitAuditEvent(ctx, "agent.disabled", req.TenantID, req.NodeID, true)

	return &DisableFleetNodeResponse{
		NodeID:     req.NodeID,
		Disabled:   true,
		DisabledAt: now,
		Timestamp:  now,
	}, nil
}

// EnableFleetNode clears the disabled status from the registry.
func (s *fleetAdminService) EnableFleetNode(ctx context.Context, req *EnableFleetNodeRequest) (*EnableFleetNodeResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.NodeID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	agent, err := s.registry.GetFleetNode(ctx, req.TenantID, req.NodeID)
	if err != nil {
		return nil, s.convertRegistryError(err, req.TenantID, req.NodeID)
	}

	if agent.Status != orchestration.FleetNodeStatusDisabled {
		return nil, &FleetNodeNotDisabledError{TenantID: req.TenantID, NodeID: req.NodeID}
	}

	agent.Status = orchestration.FleetNodeStatusRegistered
	agent.DisabledAt = 0
	// Do not pre-increment Version: UpdateFleetNode uses CAS and handles version bumping.

	if err := s.registry.UpdateFleetNode(ctx, agent); err != nil {
		return nil, err
	}

	// Emit fleet node.enabled audit event.
	_ = s.emitAuditEvent(ctx, "agent.enabled", req.TenantID, req.NodeID, true)

	return &EnableFleetNodeResponse{
		NodeID:    req.NodeID,
		Enabled:   true,
		Timestamp: time.Now().UTC(),
	}, nil
}

// GetFleetNodeRegistration retrieves a single fleet node registration.
func (s *fleetAdminService) GetFleetNodeRegistration(ctx context.Context, req *GetFleetNodeRegistrationRequest) (*GetFleetNodeRegistrationResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.NodeID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	agent, err := s.registry.GetFleetNode(ctx, req.TenantID, req.NodeID)
	if err != nil {
		return nil, s.convertRegistryError(err, req.TenantID, req.NodeID)
	}

	return &GetFleetNodeRegistrationResponse{
		Agent:     agent,
		Timestamp: time.Now().UTC(),
	}, nil
}

// ListFleetNodeRegistrations returns all fleet nodes for a tenant.
func (s *fleetAdminService) ListFleetNodeRegistrations(ctx context.Context, req *ListFleetNodeRegistrationsRequest) (*ListFleetNodeRegistrationsResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	agents, err := s.registry.ListFleetNodes(ctx, req.TenantID, req.Filter)
	if err != nil {
		return nil, err
	}

	return &ListFleetNodeRegistrationsResponse{
		Agents:    agents,
		Total:     len(agents),
		Timestamp: time.Now().UTC(),
	}, nil
}

// convertRegistryError translates registry errors into service-layer errors.
func (s *fleetAdminService) convertRegistryError(err error, tenantID, nodeID string) error {
	var notFound *orchestration.FleetNodeNotFoundError
	if stderrors.As(err, &notFound) {
		return &FleetAdminNotFoundError{TenantID: tenantID, NodeID: nodeID}
	}
	return err
}

// emitAuditEvent emits an audit event when an audit logger is configured.
// Errors from the audit logger are intentionally discarded (best-effort).
func (s *fleetAdminService) emitAuditEvent(ctx context.Context, action, tenantID, nodeID string, success bool) error {
	if s.auditLogger == nil {
		return nil
	}
	outcome := iam.AuditOutcomeSuccess
	if !success {
		outcome = iam.AuditOutcomeFailure
	}
	return s.auditLogger.Log(ctx, &iam.AuditEvent{
		EventType: iam.AuditEventAppEvent,
		Category:  iam.AuditCategoryAdministration,
		TenantID:  tenantID,
		Action:    action,
		Outcome:   outcome,
		Resource: &iam.AuditResource{
			Type: "agent",
			ID:   nodeID,
		},
	})
}

// Compile-time interface satisfaction check.
var _ FleetAdminService = (*fleetAdminService)(nil)
