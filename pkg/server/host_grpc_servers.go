package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HostGRPCNotReadyError reports that the host-built IAM gRPC servers were
// requested before IAM initialisation completed. It answers gRPC
// Unavailable, so a caller retries instead of reaching a server built over
// missing IAM components.
type HostGRPCNotReadyError struct{}

func (e *HostGRPCNotReadyError) Error() string {
	return "quicbaas federation, session, IAM and audit services are not yet available: IAM initialisation has not completed"
}

// GRPCStatus maps the error onto gRPC Unavailable.
func (e *HostGRPCNotReadyError) GRPCStatus() *status.Status {
	return status.New(codes.Unavailable, e.Error())
}

// hostGRPCServers returns the IAM gRPC servers built from facade state the
// service facade does not expose. They are built once and shared: the
// Unix-socket gRPC server registers them, and the quicbaas module mounts the
// same instances on the node's TCP gRPC server (WithHostGRPCServers), so both
// listeners serve one session store. The servers are FederationService,
// SessionService, IAMService, AuditService and PEPService.
//
// The node accepts requests before startService attaches IAM, so a call can
// arrive while the facade has no federation manager, session storage, audit
// logger or IAM components yet.
// Until attachIAMComponents marks IAM ready, hostGRPCServers returns
// HostGRPCNotReadyError and builds nothing, so no server over missing IAM
// components is ever cached. Lock-free: concurrent first calls after IAM is
// ready may both build, and CompareAndSwap keeps exactly one (the builders
// only wrap existing components).
func (s *Server) hostGRPCServers() (apimodule.HostGRPCServers, error) {
	if built := s.hostGRPC.Load(); built != nil {
		return *built, nil
	}
	if !s.iamReady.Load() {
		return apimodule.HostGRPCServers{}, &HostGRPCNotReadyError{}
	}
	built := s.buildHostGRPCServers()
	if !s.hostGRPC.CompareAndSwap(nil, built) {
		built = s.hostGRPC.Load()
	}
	return *built, nil
}

// buildHostGRPCServers builds the federation, session and IAMService servers
// when the facade carries IAM components; otherwise all three stay nil.
func (s *Server) buildHostGRPCServers() *apimodule.HostGRPCServers {
	built := &apimodule.HostGRPCServers{}
	if getter, ok := s.quicBaaSServiceRef().(iamComponentsGetter); ok {
		built.Federation = s.buildFederationServer(getter)
		built.Session = s.buildSessionServer(getter)
		built.IAM = s.buildIAMServer(getter)
		built.Audit = s.buildAuditServer(getter)
		built.PEP = s.buildPEPServer(getter)
	}
	return built
}

// buildFederationServer returns the FederationService server, or nil when
// the facade has no federation manager.
func (s *Server) buildFederationServer(getter iamComponentsGetter) iampb.FederationServiceServer {
	fedMgr, err := getter.GetFederationManager(context.Background())
	if err != nil {
		s.logger.WithError(err).Warn("Failed to get federation manager, FederationService is not served")
		return nil
	}
	if fedMgr == nil {
		return nil
	}
	return grpcapi.NewFederationServer(fedMgr, s.logger)
}

// buildSessionServer returns the SessionService server over the facade's
// session storage, falling back to in-memory storage when it has none.
func (s *Server) buildSessionServer(getter iamComponentsGetter) iampb.SessionServiceServer {
	sessionStorage, err := getter.GetSessionStorage(context.Background())
	if err != nil {
		s.logger.WithError(err).Warn("Failed to get session storage, using in-memory session storage for gRPC")
	}
	if sessionStorage == nil {
		sessionStorage = iam.NewMemorySessionStorage()
		s.logger.Info("Using in-memory session storage fallback for gRPC")
	}
	// The session RPCs act only on the caller's own sessions, so they resolve
	// the caller's account the way the REST session routes do.
	callers, ok := s.quicBaaSServiceRef().(service.CallerAccountResolver)
	if !ok {
		s.logger.Warn("QuicBaaS service does not resolve caller accounts; gRPC session RPCs will refuse every caller")
	}
	return grpcapi.NewSessionServer(grpcapi.NewSessionServiceAdapter(sessionStorage), callers, s.logger).
		WithBearerRevoker(oidcBearerRevoker{s: s}).
		WithCredentialRevoker(oidcBearerRevoker{s: s})
}

// oidcBearerRevoker resolves the OIDC provider on each call, because the
// session server is built before IAM initialisation completes.
type oidcBearerRevoker struct{ s *Server }

var _ grpcapi.CredentialRevoker = oidcBearerRevoker{}

// provider resolves the OIDC provider for one revocation call.
func (r oidcBearerRevoker) provider(ctx context.Context) (*iam.OIDCProvider, error) {
	getter, ok := r.s.quicBaaSServiceRef().(interface {
		GetOIDCProvider(context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		return nil, status.Error(codes.Unavailable, "login bearer revocation not available: no OIDC provider")
	}
	provider, err := getter.GetOIDCProvider(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, status.Error(codes.Unavailable, "login bearer revocation not available: no OIDC provider")
	}
	return provider, nil
}

func (r oidcBearerRevoker) RevokeSubjectBearers(ctx context.Context, tenantID, subject, keepSessionID string) (int, error) {
	provider, err := r.provider(ctx)
	if err != nil {
		return 0, err
	}
	return provider.RevokeSubjectBearers(ctx, tenantID, subject, keepSessionID)
}

// RevokeSubjectCredentials ends the subject's login credentials in tenantID.
func (r oidcBearerRevoker) RevokeSubjectCredentials(ctx context.Context, tenantID, subject string) (int, error) {
	provider, err := r.provider(ctx)
	if err != nil {
		return 0, err
	}
	return provider.RevokeSubjectCredentials(ctx, tenantID, subject)
}

// buildAuditServer builds the AuditService server over the IAM audit logger.
// It returns nil when the audit logger is unavailable.
func (s *Server) buildAuditServer(getter iamComponentsGetter) iampb.AuditServiceServer {
	// The audit logger is ALWAYS the node-backed NodeAuditLogger
	// (node_audit_logger.go) once IAM composes — there is no in-memory
	// fallback (a fallback that loses records on restart is the defect the
	// unified audit plane exists to eliminate). A nil return here means IAM
	// itself failed to compose, in which case not serving AuditService is
	// correct: nothing else is served either.
	auditLogger, _ := getter.GetAuditLogger(context.Background())
	if auditLogger == nil {
		s.logger.Error("Audit logger unavailable, skipping Audit gRPC registration")
		return nil
	}
	return grpcapi.NewAuditServer(grpcapi.NewAuditServiceAdapter(auditLogger), s.logger)
}

// buildPEPServer builds the PEPService server over the IAM policy engine.
// It returns nil when the policy engine is unavailable.
func (s *Server) buildPEPServer(getter iamComponentsGetter) iampb.PEPServiceServer {
	engine, err := getter.GetPolicyEngine(context.Background())
	if err != nil || engine == nil {
		s.logger.WithError(err).Warn("Policy engine unavailable, PEPService is not served")
		return nil
	}
	return grpcapi.NewPEPServer(grpcapi.NewPEPServiceAdapter(engine), s.logger)
}

// The OIDC provider is what ends a caller's credentials on gRPC logout-all.
var _ grpcapi.CredentialRevoker = (*iam.OIDCProvider)(nil)
