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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// AgentAdminService — Interface
// =============================================================================

// AgentAdminService manages global agent lifecycle operations such as
// disabling and re-enabling agents across all projects.
type AgentAdminService interface {
	// DisableAgent administratively disables an agent globally. All active
	// work claims for this agent should be cancelled by the caller. The
	// agent's OAuth2 client and mTLS cert pin are revoked via the IAM layer.
	// Returns AgentAlreadyDisabledError if already disabled.
	DisableAgent(ctx context.Context, req *DisableAgentRequest) (*DisableAgentResponse, error)

	// EnableAgent re-activates a previously disabled agent.
	// Returns AgentNotDisabledError if the agent is not currently disabled.
	EnableAgent(ctx context.Context, req *EnableAgentRequest) (*EnableAgentResponse, error)

	// GetAgentRegistration retrieves a single agent registration.
	GetAgentRegistration(ctx context.Context, req *GetAgentRegistrationRequest) (*GetAgentRegistrationResponse, error)

	// ListAgentRegistrations returns all registered agents for a tenant.
	ListAgentRegistrations(ctx context.Context, req *ListAgentRegistrationsRequest) (*ListAgentRegistrationsResponse, error)
}

// =============================================================================
// Request / Response Types
// =============================================================================

// DisableAgentRequest contains the parameters for disabling an agent.
type DisableAgentRequest struct {
	TenantID string
	AgentID  string
}

// DisableAgentResponse is returned after an agent has been disabled.
type DisableAgentResponse struct {
	AgentID    string
	Disabled   bool
	DisabledAt time.Time
	Timestamp  time.Time
}

// EnableAgentRequest contains the parameters for re-enabling an agent.
type EnableAgentRequest struct {
	TenantID string
	AgentID  string
}

// EnableAgentResponse is returned after an agent has been re-enabled.
type EnableAgentResponse struct {
	AgentID   string
	Enabled   bool
	Timestamp time.Time
}

// GetAgentRegistrationRequest identifies an agent by tenant and agent ID.
type GetAgentRegistrationRequest struct {
	TenantID string
	AgentID  string
}

// GetAgentRegistrationResponse wraps the retrieved agent registration.
type GetAgentRegistrationResponse struct {
	Agent     *orchestration.AgentRegistration
	Timestamp time.Time
}

// ListAgentRegistrationsRequest specifies which tenant's agents to list.
type ListAgentRegistrationsRequest struct {
	TenantID string
	Filter   *orchestration.AgentFilter
}

// ListAgentRegistrationsResponse contains the list of agent registrations.
type ListAgentRegistrationsResponse struct {
	Agents    []*orchestration.AgentRegistration
	Total     int
	Timestamp time.Time
}

// =============================================================================
// Typed Errors
// =============================================================================

// AgentAdminNotFoundError is returned when the target agent does not exist.
type AgentAdminNotFoundError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentAdminNotFoundError) Error() string {
	return "agent not found: tenant=" + e.TenantID + " agent=" + e.AgentID
}

// AgentAlreadyDisabledError is returned when the agent is already disabled.
type AgentAlreadyDisabledError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentAlreadyDisabledError) Error() string {
	return "agent already disabled: tenant=" + e.TenantID + " agent=" + e.AgentID
}

// AgentNotDisabledError is returned when the agent is not disabled.
type AgentNotDisabledError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentNotDisabledError) Error() string {
	return "agent not disabled: tenant=" + e.TenantID + " agent=" + e.AgentID
}

// AgentDisabledError is returned when a disabled agent attempts an operation.
type AgentDisabledError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentDisabledError) Error() string {
	return "agent is disabled: tenant=" + e.TenantID + " agent=" + e.AgentID
}

// =============================================================================
// agentAdminServiceImpl — concrete implementation
// =============================================================================

// AppClientRevoker is the minimal interface required to revoke an OAuth2 application.
// It is satisfied by *iam.AppManager. Using a narrow interface keeps the
// agent admin service free of a hard dependency on the full IAM package.
type AppClientRevoker interface {
	DeleteApp(ctx context.Context, clientID string) error
}

// AgentStreamCloser is the minimal interface for cancelling active gRPC streams
// on behalf of a specific agent. Implementations hold a per-agent context cancel
// registry keyed by agentID.
//
// TODO(gap2-stream-close): A control plane stream-cancel registry does not yet exist.
// When the gRPC agent event stream server (pkg/api/grpc) is extended to track
// per-agent cancels, wire an implementation here via SetStreamCloser.
// Until then, streams time out naturally once the agent's OAuth2 client is
// revoked and token refresh fails.
type AgentStreamCloser interface {
	// CloseAgentStreams cancels all active server-side gRPC streams for agentID.
	CloseAgentStreams(ctx context.Context, agentID string) error
}

// ErrAgentStreamCloseNotImplemented is returned when no stream-close registry
// has been wired. Callers should treat this as a non-fatal advisory.
type ErrAgentStreamCloseNotImplemented struct {
	AgentID string
}

// Error implements the error interface.
func (e *ErrAgentStreamCloseNotImplemented) Error() string {
	return "agent stream close not implemented: no stream registry wired for agent=" + e.AgentID
}

// agentAdminServiceImpl implements AgentAdminService backed by an AgentRegistry.
type agentAdminServiceImpl struct {
	registry     orchestration.AgentRegistry
	appRevoker   AppClientRevoker   // optional: revokes OAuth2 client on disable
	streamCloser AgentStreamCloser  // optional: cancels active gRPC streams on disable
	auditLogger  iam.AuditLogger    // optional: emits audit events
}

// NewAgentAdminService creates a new AgentAdminService backed by the given registry.
func NewAgentAdminService(registry orchestration.AgentRegistry) AgentAdminService {
	return &agentAdminServiceImpl{registry: registry}
}

// SetAppClientRevoker wires the OAuth2 app revoker used by DisableAgent to
// invalidate the agent's client credentials at the IAM layer.
func (s *agentAdminServiceImpl) SetAppClientRevoker(r AppClientRevoker) {
	s.appRevoker = r
}

// SetStreamCloser wires the stream-cancel registry used by DisableAgent to
// force-close active gRPC connections for the disabled agent.
func (s *agentAdminServiceImpl) SetStreamCloser(sc AgentStreamCloser) {
	s.streamCloser = sc
}

// SetAuditLogger wires the audit logger used to emit agent.disabled /
// agent.enabled events for compliance and observability.
func (s *agentAdminServiceImpl) SetAuditLogger(l iam.AuditLogger) {
	s.auditLogger = l
}

// DisableAgent marks the agent as disabled in the registry.
func (s *agentAdminServiceImpl) DisableAgent(ctx context.Context, req *DisableAgentRequest) (*DisableAgentResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AgentID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	agent, err := s.registry.GetAgent(ctx, req.TenantID, req.AgentID)
	if err != nil {
		return nil, s.convertRegistryError(err, req.TenantID, req.AgentID)
	}

	if agent.Status == orchestration.AgentStatusDisabled {
		return nil, &AgentAlreadyDisabledError{TenantID: req.TenantID, AgentID: req.AgentID}
	}

	now := time.Now().UTC()
	agent.Status = orchestration.AgentStatusDisabled
	agent.DisabledAt = now.UnixNano()
	// Do not pre-increment Version: UpdateAgent uses CAS and handles version bumping.

	if err := s.registry.UpdateAgent(ctx, agent); err != nil {
		return nil, err
	}

	// Revoke the agent's OAuth2 client so subsequent token requests are rejected.
	// The client_id is stored in the registration metadata under "oauth2_client_id".
	// If the key is absent (legacy registration), the app is looked up by agent ID
	// convention (agentID == OAuth2 app name).
	if s.appRevoker != nil {
		clientID := req.AgentID
		if v, ok := agent.Metadata["oauth2_client_id"]; ok && v != "" {
			clientID = v
		}
		if revokeErr := s.appRevoker.DeleteApp(ctx, clientID); revokeErr != nil {
			// Non-fatal: the agent is already disabled in the registry.
			// Log via audit if available, but don't fail the operation.
			_ = s.emitAuditEvent(ctx, "agent.disable.oauth2_revoke_failed", req.TenantID, req.AgentID, false)
		} else {
			_ = s.emitAuditEvent(ctx, "agent.disable.oauth2_revoked", req.TenantID, req.AgentID, true)
		}

		// mTLS thumbprint pin: also revoke the secondary self_signed_tls_client_auth
		// app registered for the agent's peer certificate. Stored under "mtls_client_id".
		if mtlsID, ok := agent.Metadata["mtls_client_id"]; ok && mtlsID != "" {
			_ = s.appRevoker.DeleteApp(ctx, mtlsID) // best-effort
		}
	}

	// Force-close active gRPC streams for this agent.
	if s.streamCloser != nil {
		if closeErr := s.streamCloser.CloseAgentStreams(ctx, req.AgentID); closeErr != nil {
			_ = s.emitAuditEvent(ctx, "agent.disable.stream_close_failed", req.TenantID, req.AgentID, false)
		}
	}
	// NOTE: If no stream-closer is wired, streams will time out naturally once
	// the agent's token refresh fails after OAuth2 client revocation.
	// See AgentStreamCloser and ErrAgentStreamCloseNotImplemented for details.

	// Emit agent.disabled audit event.
	_ = s.emitAuditEvent(ctx, "agent.disabled", req.TenantID, req.AgentID, true)

	return &DisableAgentResponse{
		AgentID:    req.AgentID,
		Disabled:   true,
		DisabledAt: now,
		Timestamp:  now,
	}, nil
}

// EnableAgent clears the disabled status from the registry.
func (s *agentAdminServiceImpl) EnableAgent(ctx context.Context, req *EnableAgentRequest) (*EnableAgentResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AgentID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	agent, err := s.registry.GetAgent(ctx, req.TenantID, req.AgentID)
	if err != nil {
		return nil, s.convertRegistryError(err, req.TenantID, req.AgentID)
	}

	if agent.Status != orchestration.AgentStatusDisabled {
		return nil, &AgentNotDisabledError{TenantID: req.TenantID, AgentID: req.AgentID}
	}

	agent.Status = orchestration.AgentStatusRegistered
	agent.DisabledAt = 0
	// Do not pre-increment Version: UpdateAgent uses CAS and handles version bumping.

	if err := s.registry.UpdateAgent(ctx, agent); err != nil {
		return nil, err
	}

	// Emit agent.enabled audit event.
	_ = s.emitAuditEvent(ctx, "agent.enabled", req.TenantID, req.AgentID, true)

	return &EnableAgentResponse{
		AgentID:   req.AgentID,
		Enabled:   true,
		Timestamp: time.Now().UTC(),
	}, nil
}

// GetAgentRegistration retrieves a single agent registration.
func (s *agentAdminServiceImpl) GetAgentRegistration(ctx context.Context, req *GetAgentRegistrationRequest) (*GetAgentRegistrationResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AgentID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	agent, err := s.registry.GetAgent(ctx, req.TenantID, req.AgentID)
	if err != nil {
		return nil, s.convertRegistryError(err, req.TenantID, req.AgentID)
	}

	return &GetAgentRegistrationResponse{
		Agent:     agent,
		Timestamp: time.Now().UTC(),
	}, nil
}

// ListAgentRegistrations returns all agents for a tenant.
func (s *agentAdminServiceImpl) ListAgentRegistrations(ctx context.Context, req *ListAgentRegistrationsRequest) (*ListAgentRegistrationsResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	agents, err := s.registry.ListAgents(ctx, req.TenantID, req.Filter)
	if err != nil {
		return nil, err
	}

	return &ListAgentRegistrationsResponse{
		Agents:    agents,
		Total:     len(agents),
		Timestamp: time.Now().UTC(),
	}, nil
}

// convertRegistryError translates registry errors into service-layer errors.
func (s *agentAdminServiceImpl) convertRegistryError(err error, tenantID, agentID string) error {
	switch err.(type) {
	case *orchestration.AgentNotFoundError:
		return &AgentAdminNotFoundError{TenantID: tenantID, AgentID: agentID}
	default:
		return err
	}
}

// emitAuditEvent emits an audit event when an audit logger is configured.
// Errors from the audit logger are intentionally discarded (best-effort).
func (s *agentAdminServiceImpl) emitAuditEvent(ctx context.Context, action, tenantID, agentID string, success bool) error {
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
			ID:   agentID,
		},
	})
}

// Compile-time interface satisfaction check.
var _ AgentAdminService = (*agentAdminServiceImpl)(nil)
