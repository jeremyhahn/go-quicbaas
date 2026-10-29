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
	return "quicbaas federation and session services are not yet available: IAM initialisation has not completed"
}

// GRPCStatus maps the error onto gRPC Unavailable.
func (e *HostGRPCNotReadyError) GRPCStatus() *status.Status {
	return status.New(codes.Unavailable, e.Error())
}

// hostGRPCServers returns the IAM gRPC servers built from facade state the
// service facade does not expose. They are built once and shared: the
// Unix-socket gRPC server registers them, and the quicbaas module mounts the
// same instances on the node's TCP gRPC server (WithHostGRPCServers), so both
// listeners serve one session store.
//
// The node accepts requests before startService attaches IAM, so a call can
// arrive while the facade has no federation manager or session storage yet.
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

// buildHostGRPCServers builds the federation and session servers when the
// facade carries IAM components; otherwise both stay nil.
func (s *Server) buildHostGRPCServers() *apimodule.HostGRPCServers {
	built := &apimodule.HostGRPCServers{}
	if getter, ok := s.quicBaaSServiceRef().(iamComponentsGetter); ok {
		built.Federation = s.buildFederationServer(getter)
		built.Session = s.buildSessionServer(getter)
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
	return grpcapi.NewSessionServer(grpcapi.NewSessionServiceAdapter(sessionStorage), callers, s.logger)
}
