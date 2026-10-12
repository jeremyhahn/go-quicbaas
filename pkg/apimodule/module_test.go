// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package apimodule

import (
	"context"
	"crypto"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	xkmstypes "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// stubKMSManager is a minimal interfaces.KMSManager implementation used only
// to prove KeyManagement's nil-gate reacts to KMSManager() being configured
// (TestModule_DEKManagement_NilGatedOnKMSServiceEvenWithManager). Every method
// beyond GetSupportedAlgorithms is a trivial zero-value stub: no test in this
// package exercises them.
type stubKMSManager struct{}

func (stubKMSManager) GenerateKey(context.Context, *xkmstypes.KeyAttributes) (interface{}, error) {
	return nil, nil
}
func (stubKMSManager) ImportKey(context.Context, string, []byte, string) (interface{}, error) {
	return nil, nil
}
func (stubKMSManager) GetKey(context.Context, string) (interface{}, error)  { return nil, nil }
func (stubKMSManager) GetPublicKey(context.Context, string) ([]byte, error) { return nil, nil }
func (stubKMSManager) UpdateStoredMetadata(interface{}) error               { return nil }
func (stubKMSManager) ListKeys(context.Context, string) ([]interface{}, error) {
	return nil, nil
}
func (stubKMSManager) RotateKey(context.Context, string) (interface{}, error) { return nil, nil }
func (stubKMSManager) RevokeKey(context.Context, string) error                { return nil }
func (stubKMSManager) EnableKey(context.Context, string) error                { return nil }
func (stubKMSManager) DisableKey(context.Context, string) error               { return nil }
func (stubKMSManager) DeleteKey(context.Context, string) error                { return nil }
func (stubKMSManager) Encrypt(context.Context, string, []byte, []byte) ([]byte, error) {
	return nil, nil
}
func (stubKMSManager) Decrypt(context.Context, string, []byte, []byte) ([]byte, error) {
	return nil, nil
}
func (stubKMSManager) Sign(context.Context, string, []byte, string) ([]byte, error) {
	return nil, nil
}
func (stubKMSManager) Signer(context.Context, string) (crypto.Signer, error) { return nil, nil }
func (stubKMSManager) Verify(context.Context, string, []byte, []byte, string) (bool, error) {
	return false, nil
}
func (stubKMSManager) GenerateDataKey(context.Context, *xkmstypes.KeyAttributes, int) ([]byte, []byte, error) {
	return nil, nil, nil
}
func (stubKMSManager) GetBackendType() config.BackendType { return "" }
func (stubKMSManager) IsAvailable(context.Context) bool   { return true }
func (stubKMSManager) GetSupportedAlgorithms(context.Context) ([]string, error) {
	return []string{"AES-256-GCM"}, nil
}
func (stubKMSManager) Close() error { return nil }

var _ interfaces.KMSManager = stubKMSManager{}

// testLogger builds the logger every module test constructs the Module with.
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return log
}

// TestNewModule_NilResolver proves construction fails closed with the typed
// error when no service resolver is supplied: a module that can never reach
// the QuicBaaS facade must not register a dead surface.
func TestNewModule_NilResolver(t *testing.T) {
	m, err := NewModule(nil, testLogger(t))
	require.Error(t, err)
	assert.Nil(t, m)

	var nilResolver *NilServiceResolverError
	assert.True(t, errors.As(err, &nilResolver), "error must be the typed *NilServiceResolverError, got %T", err)
	assert.NotEmpty(t, err.Error())
}

// TestNewModule_NilLogger proves construction fails closed with the typed
// error when no logger is supplied: every broker server constructor derives a
// component logger at bind time, so a nil logger would surface as a panic on
// the first RPC instead of a construction error.
func TestNewModule_NilLogger(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, nil)
	require.Error(t, err)
	assert.Nil(t, m)

	var nilLogger *NilLoggerError
	assert.True(t, errors.As(err, &nilLogger), "error must be the typed *NilLoggerError, got %T", err)
	assert.NotEmpty(t, err.Error())
}

// TestModule_Name pins the unique module name qrdb's WithModule duplicate
// detection keys on.
func TestModule_Name(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)
	assert.Equal(t, "quicbaas", m.Name())
	assert.Equal(t, ModuleName, m.Name())
}

// TestModule_LifecycleNoOps proves Start and Stop are the inherited
// BaseModule no-ops: the QuicBaaS facade lifecycle is owned by pkg/server, so
// the module must neither fail nor perform work in either hook.
func TestModule_LifecycleNoOps(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	assert.NoError(t, m.Start(context.Background()))
	assert.NoError(t, m.Stop(context.Background()))
	// Idempotent: a second Stop (LIFO shutdown retries) is also a no-op.
	assert.NoError(t, m.Stop(context.Background()))
}

// TestModule_RegisterGRPC_RegistersFullGRPCSurface proves RegisterGRPC
// attaches the quicbaas-native core plane (QuicBaaS, Login, FIDO2Service),
// all seven broker capability services, the five quicbaas-native broker
// data-plane services (ParameterStore, DynamicSecretsService,
// ObjectStorageService, ObjectStorageAdmin, OllamaService), and
// SecretsManagement (the one genuinely quicbaas-native xkms-wire service —
// KeyManagement/DEKManagement/Crypto/CertificateService/TrustService/
// AttestationService/PIVService moved to go-xkms's own ServiceModule, Phase 3
// of the login-plane plan; see module.go's package doc and
// TestModule_RegisterGRPC_ExcludesXKMSCanonicalSurface), each with a method
// set identical to its generated service descriptor — the same wire surface
// pkg/server/lifecycle.go registers on the legacy transport (minus the
// qrdb-proxy planes that qrdb's own unified server already owns natively,
// and minus ObjectTransferService, deliberately deferred — see module.go's
// package doc and TestModule_RegisterGRPC_ObjectTransferServiceDeferred).
func TestModule_RegisterGRPC_RegistersFullGRPCSurface(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	server := grpc.NewServer()
	m.RegisterGRPC(server)

	generated := []*grpc.ServiceDesc{
		&pb.QuicBaaS_ServiceDesc,
		&pb.Login_ServiceDesc,
		&pb.FIDO2Service_ServiceDesc,
		&pb.SchedulerService_ServiceDesc,
		&pb.OrchestratorService_ServiceDesc,
		&pb.ProjectService_ServiceDesc,
		&pb.AccountService_ServiceDesc,
		&pb.SSHService_ServiceDesc,
		&pb.MessagingService_ServiceDesc,
		&pb.EmbeddingService_ServiceDesc,
		&pb.ParameterStore_ServiceDesc,
		&pb.DynamicSecretsService_ServiceDesc,
		&pb.ObjectStorageService_ServiceDesc,
		&pb.ObjectStorageAdmin_ServiceDesc,
		&pb.OllamaService_ServiceDesc,
		&pb.SecretsManagement_ServiceDesc,
	}
	info := server.GetServiceInfo()
	require.Len(t, info, len(generated), "exactly the sixteen quicbaas-native and broker services must be registered")

	for _, desc := range generated {
		svcInfo, ok := info[desc.ServiceName]
		require.True(t, ok, "service %s must be registered", desc.ServiceName)

		require.Len(t, svcInfo.Methods, len(desc.Methods)+len(desc.Streams),
			"service %s: the registered method set must be the full generated surface", desc.ServiceName)
		registered := make(map[string]struct{}, len(svcInfo.Methods))
		for _, method := range svcInfo.Methods {
			registered[method.Name] = struct{}{}
		}
		for _, md := range desc.Methods {
			_, found := registered[md.MethodName]
			assert.True(t, found, "service %s: generated method %q missing from the registered surface",
				desc.ServiceName, md.MethodName)
		}
		for _, sd := range desc.Streams {
			_, found := registered[sd.StreamName]
			assert.True(t, found, "service %s: generated stream %q missing from the registered surface",
				desc.ServiceName, sd.StreamName)
		}
	}
}

// TestModule_RegisterGRPC_ExcludesProxyPlanes proves the qrdb-proxy planes
// (Barrier, TenantBarrier, Ceremony, VectorDB, VectorAdmin) stay OFF this
// module's surface: go-qrdb's own unified server already registers the
// identical capability natively, so duplicating it here would register a
// redundant proto surface.
func TestModule_RegisterGRPC_ExcludesProxyPlanes(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	server := grpc.NewServer()
	m.RegisterGRPC(server)
	info := server.GetServiceInfo()

	excluded := []string{
		pb.Barrier_ServiceDesc.ServiceName,
		pb.TenantBarrier_ServiceDesc.ServiceName,
		pb.Ceremony_ServiceDesc.ServiceName,
		pb.VectorDB_ServiceDesc.ServiceName,
		pb.VectorAdmin_ServiceDesc.ServiceName,
	}
	for _, name := range excluded {
		_, ok := info[name]
		assert.False(t, ok, "service %s must NOT be registered on the quicbaas module: "+
			"it is a qrdb-proxy plane owned by go-qrdb's own unified server", name)
	}
}

// TestModule_RegisterGRPC_ExcludesXKMSCanonicalSurface proves KeyManagement,
// DEKManagement, Crypto, CertificateService, TrustService, AttestationService,
// and PIVService stay OFF the quicbaas module's surface (Phase 3 of the
// login-plane plan, corrective to the prior design documented in
// module.go's package doc): they were quicbaas.v1 wrappers over the SAME
// composed go-xkms library go-xkms's OWN ServiceModule ("xkms") now serves
// as its canonical xkms.v1 KeystoreService surface on this same unified
// server — registering both would double-register a redundant proto
// surface, exactly the failure mode go-qrdb's route/service-collision guard
// exists to catch. SecretsManagement is deliberately absent from this
// exclusion list: it is genuinely quicbaas-native (see
// TestModule_RegisterGRPC_RegistersFullGRPCSurface).
func TestModule_RegisterGRPC_ExcludesXKMSCanonicalSurface(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	server := grpc.NewServer()
	m.RegisterGRPC(server)
	info := server.GetServiceInfo()

	excluded := []string{
		pb.KeyManagement_ServiceDesc.ServiceName,
		pb.DEKManagement_ServiceDesc.ServiceName,
		pb.Crypto_ServiceDesc.ServiceName,
		pb.CertificateService_ServiceDesc.ServiceName,
		pb.TrustService_ServiceDesc.ServiceName,
		pb.AttestationService_ServiceDesc.ServiceName,
		pb.PIVService_ServiceDesc.ServiceName,
	}
	for _, name := range excluded {
		_, ok := info[name]
		assert.False(t, ok, "service %s must NOT be registered on the quicbaas module: "+
			"go-xkms's own ServiceModule now owns this canonical xkms.v1 surface", name)
	}
}

// startBufconnModule registers the module on a bufconn-backed gRPC server and
// returns the client connection.
func startBufconnModule(t *testing.T, m *Module) *grpc.ClientConn {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	m.RegisterGRPC(server)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

// TestModule_LateBinding is the ordering regression test: quicbaasd
// constructs the embedded qrdb node (which registers this module) long before
// the QuicBaaS facade is composed. An RPC arriving before the resolver yields
// the facade must answer Unavailable — never a nil dereference — and the same
// registration must start serving once the resolver resolves, without
// re-registration.
func TestModule_LateBinding(t *testing.T) {
	var svc atomic.Pointer[mocks.MockQuicBaaS]
	m, err := NewModule(func() service.QuicBaaS {
		if composed := svc.Load(); composed != nil {
			return composed
		}
		return nil
	}, testLogger(t))
	require.NoError(t, err)

	client := pb.NewSchedulerServiceClient(startBufconnModule(t, m))
	ctx := context.Background()

	// Phase 1: the facade is not composed yet — fail-safe Unavailable.
	_, err = client.GetSchedulerStatus(ctx, &pb.GetSchedulerStatusRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(),
		"an RPC before composition must answer Unavailable, got %v", st.Code())

	// Phase 2: composition completes; the SAME registration now serves.
	svc.Store(&mocks.MockQuicBaaS{
		SchedulerServiceFunc: func() service.SchedulerService {
			return &mocks.MockSchedulerService{}
		},
	})
	resp, err := client.GetSchedulerStatus(ctx, &pb.GetSchedulerStatusRequest{})
	require.NoError(t, err, "the late-bound facade must serve without re-registration")
	assert.True(t, resp.Running)
}

// TestModule_QuicBaaSHealth_LateBinding proves the quicbaas-native core plane
// added by this change follows the exact same fail-safe-then-serve contract
// as the broker capability plane: Health answers Unavailable before the
// facade composes, and serves once it does — without re-registration, and
// via the SAME shared *grpcapi.Server facade instance that also backs
// FIDO2Service and (when enabled) Embedding.
func TestModule_QuicBaaSHealth_LateBinding(t *testing.T) {
	var svc atomic.Pointer[mocks.MockQuicBaaS]
	m, err := NewModule(func() service.QuicBaaS {
		if composed := svc.Load(); composed != nil {
			return composed
		}
		return nil
	}, testLogger(t))
	require.NoError(t, err)

	client := pb.NewQuicBaaSClient(startBufconnModule(t, m))
	ctx := context.Background()

	// Phase 1: the facade is not composed yet — fail-safe Unavailable.
	_, err = client.Health(ctx, &pb.HealthRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(),
		"a Health RPC before composition must answer Unavailable, got %v", st.Code())

	// Phase 2: composition completes; the QuicBaaS main service now serves.
	// MockQuicBaaS reports HealthStatusHealthy by default without any
	// HealthFunc override.
	svc.Store(&mocks.MockQuicBaaS{})
	resp, err := client.Health(ctx, &pb.HealthRequest{})
	require.NoError(t, err, "the late-bound QuicBaaS facade must serve Health without re-registration")
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Status)
}

// TestModule_FIDO2BeginAuthentication_LateBinding proves FIDO2Service — the
// one xkms-backed surface this module registers because it is quicbaas's own
// tenant-scoped login ceremony, not a raw crypto-resource proxy — follows the
// same fail-safe-then-serve contract and is reachable through the shared
// facade once the resolver yields the composed service.
func TestModule_FIDO2BeginAuthentication_LateBinding(t *testing.T) {
	var svc atomic.Pointer[mocks.MockQuicBaaS]
	m, err := NewModule(func() service.QuicBaaS {
		if composed := svc.Load(); composed != nil {
			return composed
		}
		return nil
	}, testLogger(t))
	require.NoError(t, err)

	client := pb.NewFIDO2ServiceClient(startBufconnModule(t, m))
	ctx := context.Background()

	// Phase 1: the facade is not composed yet — fail-safe Unavailable.
	_, err = client.BeginAuthentication(ctx, &pb.FIDO2BeginAuthenticationRequest{TenantId: "t1"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(),
		"a FIDO2 RPC before composition must answer Unavailable, got %v", st.Code())

	// Phase 2: composition completes with a FIDO2Service configured — the
	// SAME facade instance now serves it too.
	svc.Store(&mocks.MockQuicBaaS{
		FIDO2ServiceFunc: func() service.FIDO2Service {
			return &mocks.MockFIDO2Service{}
		},
	})
	_, err = client.BeginAuthentication(ctx, &pb.FIDO2BeginAuthenticationRequest{TenantId: "t1"})
	require.NoError(t, err, "the late-bound facade must serve FIDO2Service without re-registration")
}

// TestModule_DisabledCapabilityAnswersUnavailable proves the nil-gating
// parity with lifecycle.go: a capability the composed facade does not enable
// (here Messaging, whose accessor returns nil) answers Unavailable on the
// module surface, while an enabled capability on the SAME bound bundle keeps
// serving.
func TestModule_DisabledCapabilityAnswersUnavailable(t *testing.T) {
	composed := &mocks.MockQuicBaaS{
		SchedulerServiceFunc: func() service.SchedulerService {
			return &mocks.MockSchedulerService{}
		},
		// MessagingServiceFunc left nil: Messaging() returns nil, exactly the
		// condition under which lifecycle.go skips the legacy registration.
	}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)

	conn := startBufconnModule(t, m)
	ctx := context.Background()

	_, err = pb.NewMessagingServiceClient(conn).GetStats(ctx, &pb.MessagingGetStatsRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(),
		"a disabled capability must answer Unavailable, got %v", st.Code())

	_, err = pb.NewSchedulerServiceClient(conn).GetSchedulerStatus(ctx, &pb.GetSchedulerStatusRequest{})
	require.NoError(t, err, "an enabled capability on the same bound bundle must serve")
}

// TestModule_LateBinding_ConcurrentFirstUse exercises the lock-free bind:
// many concurrent first RPCs race the memoizing CompareAndSwap and every one
// must succeed against a single coherent server bundle.
func TestModule_LateBinding_ConcurrentFirstUse(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS {
		return &mocks.MockQuicBaaS{
			SchedulerServiceFunc: func() service.SchedulerService {
				return &mocks.MockSchedulerService{}
			},
		}
	}, testLogger(t))
	require.NoError(t, err)

	client := pb.NewSchedulerServiceClient(startBufconnModule(t, m))

	const callers = 16
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, callErr := client.GetSchedulerStatus(context.Background(), &pb.GetSchedulerStatusRequest{})
			errs <- callErr
		}()
	}
	wg.Wait()
	close(errs)
	for callErr := range errs {
		assert.NoError(t, callErr)
	}
}

// wantPublicGRPCMethods is the verified legacy-exempt set: every method the
// legacy gRPC interceptor (pkg/api/grpc/authz_mapping.go publicGRPCMethods)
// admits without authentication that ALSO belongs to a service this module
// registers. The qrdb.v1.QRDBService/Health entry in the legacy map is
// intentionally absent here: that service belongs to qrdb's own module, not
// this one.
var wantPublicGRPCMethods = map[string]bool{
	"/quicbaas.v1.QuicBaaS/Health":                         true,
	"/quicbaas.v1.QuicBaaS/Login":                          true,
	"/quicbaas.login.v1.Login/BeginLogin":                  true,
	"/quicbaas.login.v1.Login/FinishLogin":                 true,
	"/quicbaas.fido2.v1.FIDO2Service/BeginAuthentication":  true,
	"/quicbaas.fido2.v1.FIDO2Service/FinishAuthentication": true,
}

// TestModule_PublicGRPCMethods_ExactSet proves PublicGRPCMethods returns
// exactly the verified legacy-exempt set — no more, no less — and that the
// FIDO2 REGISTRATION pair (which binds a new authenticator and stays
// authz-gated behind user_authfactors:add on the legacy listener) is NOT
// among them.
func TestModule_PublicGRPCMethods_ExactSet(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	got := m.PublicGRPCMethods()
	require.Len(t, got, len(wantPublicGRPCMethods))

	seen := make(map[string]bool, len(got))
	for _, method := range got {
		assert.False(t, seen[method], "PublicGRPCMethods must not repeat %q", method)
		seen[method] = true
		assert.True(t, wantPublicGRPCMethods[method], "unexpected public method %q", method)
	}
	for method := range wantPublicGRPCMethods {
		assert.True(t, seen[method], "missing expected public method %q", method)
	}

	notPublic := []string{
		"/quicbaas.fido2.v1.FIDO2Service/BeginRegistration",
		"/quicbaas.fido2.v1.FIDO2Service/FinishRegistration",
		"/quicbaas.fido2.v1.FIDO2Service/BeginAddCredential",
	}
	for _, method := range notPublic {
		assert.False(t, seen[method], "%q binds a new authenticator and must stay authz-gated", method)
	}
}

// TestModule_PublicGRPCMethods_DefensiveCopy proves the returned slice is a
// copy: a caller mutating it must not corrupt the module's declared set for
// subsequent callers (the unified server's gRPC auth middleware calls this
// once per module at startup, but a shared backing array would still be a
// latent bug).
func TestModule_PublicGRPCMethods_DefensiveCopy(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	first := m.PublicGRPCMethods()
	require.NotEmpty(t, first)
	first[0] = "/corrupted/Method"

	second := m.PublicGRPCMethods()
	assert.NotEqual(t, first[0], second[0], "mutating a previously returned slice must not affect later calls")
	assert.True(t, wantPublicGRPCMethods[second[0]], "the module's declared set must be unaffected by the mutation")
}

// TestModule_PublicGRPCMethods_OnlyRegisteredServices proves every declared
// public method belongs to a service RegisterGRPC actually registers: none
// of the seven broker capability services carry a public method, and a
// method for a service this module does not register would silently bypass
// authentication on the unified server for a surface nothing here serves.
func TestModule_PublicGRPCMethods_OnlyRegisteredServices(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	registeredServices := make(map[string]struct{}, len(grpcRegistrations))
	for _, entry := range grpcRegistrations {
		registeredServices[entry.desc.ServiceName] = struct{}{}
	}

	for _, method := range m.PublicGRPCMethods() {
		trimmed := strings.TrimPrefix(method, "/")
		idx := strings.LastIndex(trimmed, "/")
		require.Greater(t, idx, 0, "malformed method name %q", method)
		svcName := trimmed[:idx]
		_, ok := registeredServices[svcName]
		assert.True(t, ok, "public method %q declares service %q, which RegisterGRPC does not register",
			method, svcName)
	}
}

// TestModule_ParameterStore_LateBinding proves ParameterStore follows the
// same fail-safe-then-serve contract as the broker capability plane: before
// the facade composes every RPC answers Unavailable, and once composed with
// a configured ParameterStoreService the SAME registration serves without
// re-registration.
func TestModule_ParameterStore_LateBinding(t *testing.T) {
	var svc atomic.Pointer[mocks.MockQuicBaaS]
	m, err := NewModule(func() service.QuicBaaS {
		if composed := svc.Load(); composed != nil {
			return composed
		}
		return nil
	}, testLogger(t))
	require.NoError(t, err)

	client := pb.NewParameterStoreClient(startBufconnModule(t, m))
	ctx := context.Background()

	// Phase 1: the facade is not composed yet — fail-safe Unavailable.
	_, err = client.GetParameter(ctx, &pb.GetParameterRequest{Path: "/app/db/password"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(),
		"a ParameterStore RPC before composition must answer Unavailable, got %v", st.Code())

	// Phase 2: composition completes with ParameterStoreService configured —
	// the SAME registration now serves.
	svc.Store(&mocks.MockQuicBaaS{
		ParameterStoreServiceFunc: func() service.ParameterStoreService {
			return &mocks.MockParameterStoreService{}
		},
	})
	_, err = client.GetParameter(ctx, &pb.GetParameterRequest{Path: "/app/db/password"})
	require.NoError(t, err, "the late-bound ParameterStoreService must serve without re-registration")
}

// TestModule_NewBrokerDataPlaneServices_DisabledAnswersUnavailable proves
// ParameterStore, DynamicSecretsService, ObjectStorageService, and
// ObjectStorageAdmin follow the same nil-gating parity as the rest of the
// broker plane (TestModule_DisabledCapabilityAnswersUnavailable): a bound
// facade whose corresponding accessor returns nil answers Unavailable, never
// a nil dereference. The bundle binds once (memoized on first successful
// bind, TestModule_LateBinding_ConcurrentFirstUse), so — unlike
// TestModule_ParameterStore_LateBinding's unbound-to-bound transition —
// "disabled" here means the SAME already-composed facade never enabled the
// capability, not a mid-flight configuration change.
func TestModule_NewBrokerDataPlaneServices_DisabledAnswersUnavailable(t *testing.T) {
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)

	conn := startBufconnModule(t, m)
	ctx := context.Background()

	_, err = pb.NewParameterStoreClient(conn).GetParameter(ctx, &pb.GetParameterRequest{Path: "/x"})
	requireUnavailable(t, err, "ParameterStore")

	_, err = pb.NewDynamicSecretsServiceClient(conn).GetRole(ctx, &pb.DynSecretsGetRoleRequest{Name: "role"})
	requireUnavailable(t, err, "DynamicSecretsService")

	_, err = pb.NewObjectStorageServiceClient(conn).GetObject(ctx, &pb.GetObjectRequest{Key: "k"})
	requireUnavailable(t, err, "ObjectStorageService")

	_, err = pb.NewObjectStorageAdminClient(conn).GetStorageStats(ctx, &pb.GetStorageStatsRequest{})
	requireUnavailable(t, err, "ObjectStorageAdmin")
}

// requireUnavailable asserts err is a gRPC status error carrying
// codes.Unavailable, identifying which service's RPC was under test.
func requireUnavailable(t *testing.T, err error, service string) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(),
		"%s must answer Unavailable when disabled, got %v", service, st.Code())
}

// TestModule_Secrets_LateBinding proves SecretsManagement follows the same
// unbound-to-bound late-binding contract as ParameterStore
// (TestModule_ParameterStore_LateBinding): Unavailable before the facade
// composes, serving the SAME registration once it does.
func TestModule_Secrets_LateBinding(t *testing.T) {
	var svc atomic.Pointer[mocks.MockQuicBaaS]
	m, err := NewModule(func() service.QuicBaaS {
		if composed := svc.Load(); composed != nil {
			return composed
		}
		return nil
	}, testLogger(t))
	require.NoError(t, err)

	client := pb.NewSecretsManagementClient(startBufconnModule(t, m))
	ctx := context.Background()

	_, err = client.GetSecret(ctx, &pb.GetSecretRequest{SecretId: "app-db-password"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(),
		"a SecretsManagement RPC before composition must answer Unavailable, got %v", st.Code())

	svc.Store(&mocks.MockQuicBaaS{
		SecretsServiceFunc: func() service.SecretsService { return &mocks.MockSecretsService{} },
	})
	_, err = client.GetSecret(ctx, &pb.GetSecretRequest{SecretId: "app-db-password"})
	require.NoError(t, err, "the late-bound SecretsService must serve without re-registration")
}

// TestModule_OllamaService_LateBinding proves OllamaService answers
// Unavailable before the facade composes, regardless of the rag build tag.
// The disabled-after-composition and enabled-after-composition cases are
// exercised by TestModule_NewBrokerDataPlaneServices_DisabledAnswersUnavailable
// (via a small addition below) and per-build-tag in module_ollama_rag_test.go
// / module_ollama_norag_test.go, since only the rag build can actually serve.
func TestModule_OllamaService_LateBinding(t *testing.T) {
	var svc atomic.Pointer[mocks.MockQuicBaaS]
	m, err := NewModule(func() service.QuicBaaS {
		if composed := svc.Load(); composed != nil {
			return composed
		}
		return nil
	}, testLogger(t))
	require.NoError(t, err)

	client := pb.NewOllamaServiceClient(startBufconnModule(t, m))
	ctx := context.Background()

	// Phase 1: the facade is not composed yet — fail-safe Unavailable.
	_, err = client.Health(ctx, &pb.OllamaHealthRequest{})
	requireUnavailable(t, err, "OllamaService (pre-composition)")

	// Phase 2: composition completes but Ollama() is disabled (no
	// OllamaServiceFunc configured) — still Unavailable, never a
	// re-registration or a nil dereference.
	svc.Store(&mocks.MockQuicBaaS{})
	_, err = client.Health(ctx, &pb.OllamaHealthRequest{})
	requireUnavailable(t, err, "OllamaService (disabled)")
}

// TestModule_RegisterGRPC_ObjectTransferServiceDeferred proves
// ObjectTransferService stays OFF this module's surface. Unlike the qrdb-
// proxy and xkms-crypto-proxy planes (TestModule_RegisterGRPC_ExcludesProxyPlanes),
// this is not a duplicate-ownership exclusion: it is deferred because no
// production code anywhere (legacy gRPC, REST, or Unix transport) ever
// populates a concrete service.ObjectTransferService, and the one existing
// pb.ObjectTransferServiceServer implementation is an unrelated, unwired
// per-shard ObjectStoreReader registry — see module.go's package doc.
func TestModule_RegisterGRPC_ObjectTransferServiceDeferred(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	server := grpc.NewServer()
	m.RegisterGRPC(server)
	info := server.GetServiceInfo()

	_, ok := info[pb.ObjectTransferService_ServiceDesc.ServiceName]
	assert.False(t, ok, "ObjectTransferService must NOT be registered: it is deliberately deferred, "+
		"not wired to a proxy-owning module")
}

// TestModule_ShardRouting_DefaultWhenUnset proves that without
// WithShardRouting the shared *grpcapi.Server facade keeps its own NewServer
// defaults (single-shard routing.New(), shards.PlatformShardID) — prior
// behavior, unchanged.
func TestModule_ShardRouting_DefaultWhenUnset(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return &mocks.MockQuicBaaS{} }, testLogger(t))
	require.NoError(t, err)

	servers, err := m.bind()
	require.NoError(t, err)
	facade, ok := servers.quicbaas.(*grpcapi.Server)
	require.True(t, ok, "quicbaas must be backed by the shared *grpcapi.Server facade")

	assert.Equal(t, shards.PlatformShardID, facade.VectorShardID(),
		"without WithShardRouting the facade must keep NewServer's default vector shard ID")
	require.NotNil(t, facade.ShardRouter(), "the facade must always carry a router, even the default")
	assert.Equal(t, uint64(1), facade.ShardRouter().Route("any-entity", 42),
		"the default single-shard router must route everything to shard 1")
}

// stubShardRouter is a minimal ShardRouter test double whose Route output is
// independent of the facade's own default router, so
// TestModule_ShardRouting_AppliedToFacade can distinguish "the supplied
// router is in effect" from "the facade's default router happened to agree".
type stubShardRouter struct{}

func (stubShardRouter) Route(entityType string, id uint64) uint64 { return 7 }

// TestModule_ShardRouting_AppliedToFacade proves WithShardRouting threads the
// supplied router and vector shard ID into the shared *grpcapi.Server facade
// at bind time — the same facade instance that also backs FIDO2Service and,
// when enabled, Embedding, exactly as pkg/server/lifecycle.go's
// SetShardRouter/SetVectorShardID calls apply to its own facade instance.
func TestModule_ShardRouting_AppliedToFacade(t *testing.T) {
	router := stubShardRouter{}
	m, err := NewModule(func() service.QuicBaaS { return &mocks.MockQuicBaaS{} }, testLogger(t),
		WithShardRouting(router, 99))
	require.NoError(t, err)

	servers, err := m.bind()
	require.NoError(t, err)
	facade, ok := servers.quicbaas.(*grpcapi.Server)
	require.True(t, ok, "quicbaas must be backed by the shared *grpcapi.Server facade")

	assert.Equal(t, uint64(99), facade.VectorShardID(),
		"WithShardRouting's vector shard ID must reach the facade")
	require.NotNil(t, facade.ShardRouter())
	assert.Equal(t, uint64(7), facade.ShardRouter().Route("any-entity", 42),
		"WithShardRouting's router must be the router the facade routes through")
}
