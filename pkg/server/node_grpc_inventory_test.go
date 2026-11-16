//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// Capability doubles. Each embeds its interface so the gRPC servers can be
// constructed around it; the inventory never calls a method on them.
type (
	inventoryKMSManager  struct{ interfaces.KMSManager }
	inventoryCrypto      struct{ service.CryptoService }
	inventoryVector      struct{ service.VectorService }
	inventoryEmbedding   struct{ service.EmbeddingService }
	inventoryOllama      struct{ service.OllamaService }
	inventoryCertificate struct{ service.CertificateService }
	inventoryTrust       struct{ service.TrustService }
	inventoryAttestation struct{ service.AttestationService }
	inventoryRegistry    struct{ service.RegistryService }
	inventoryPIV         struct{ service.PIVService }
	inventoryFIDO2       struct{ service.FIDO2Service }
	inventoryDynSecrets  struct{ service.DynamicSecretsService }
	inventoryParameters  struct{ service.ParameterStoreService }
	inventoryProject     struct{ service.ProjectService }
	inventoryAccount     struct{ service.AccountService }
	inventoryAgent       struct{ orchestration.AgentService }
	inventoryMessaging   struct{ messaging.Service }
	inventoryFederation  struct{ iam.FederationManager }
)

// inventoryFacade is a QuicBaaS facade with every capability enabled, so
// startGRPCServer registers every service it can serve over the Unix socket.
type inventoryFacade struct {
	*mockIAMComponentsGetterFull
}

var _ iamComponentsGetter = (*inventoryFacade)(nil)

func (f *inventoryFacade) GetFederationManager(_ context.Context) (iam.FederationManager, error) {
	return inventoryFederation{}, nil
}

func newInventoryFacade() *inventoryFacade {
	facade := &inventoryFacade{mockIAMComponentsGetterFull: &mockIAMComponentsGetterFull{
		MockQuicBaaS:  *testutil.NewMockQuicBaaS(),
		tenantMgr:     &mockTenantManagerF91{},
		identityStore: &iam.IdentityStore{},
		policyEngine:  &iam.PolicyEngine{},
		appManager:    &iam.AppManager{},
		mfaManager:    &mockMFAManagerFull{},
		auditLogger:   &mockAuditLoggerFull{},
		sessionStore:  &mockSessionStorageF91{},
	}}
	mock := &facade.MockQuicBaaS
	mock.CryptoService = inventoryCrypto{}
	mock.VectorService = inventoryVector{}
	mock.EmbeddingService = inventoryEmbedding{}
	mock.OllamaService = inventoryOllama{}
	mock.CertificateSvc = inventoryCertificate{}
	mock.TrustSvc = inventoryTrust{}
	mock.AttestationSvc = inventoryAttestation{}
	mock.RegistrySvc = inventoryRegistry{}
	mock.PIVSvc = inventoryPIV{}
	mock.FIDO2Svc = inventoryFIDO2{}
	mock.DynamicSecretsSvc = inventoryDynSecrets{}
	mock.ParameterStoreSvc = inventoryParameters{}
	mock.AgentServiceOverride = inventoryAgent{}
	mock.On("KMSManager").Return(inventoryKMSManager{})
	mock.On("Project").Return(inventoryProject{})
	mock.On("Account").Return(inventoryAccount{})
	mock.On("Messaging").Return(inventoryMessaging{})
	return facade
}

// unixOnlyServices are served on the Unix socket but deliberately not
// mounted on the node, each with the reason it stays off. The inventory
// fails if one of them gets mounted, so the entry is dropped with the change.
var unixOnlyServices = map[string]string{
	// Their RPCs carry no (qrdb.v1.permission) option in
	// api/proto/iam/iam.proto (IAMService 6 of 60 methods, PEPService,
	// MFAService and AuditService none): the node default-denies them and the
	// QUIC projection assertion refuses them. The Unix listener has no
	// permission for them either (pkg/api/grpc/authz_mapping.go leaves them
	// unmapped, so it denies them too): annotating them is an owner
	// authorization-policy decision, not a copy of an existing grant.
	"quicbaas.iam.v1.IAMService":   "unannotated",
	"quicbaas.iam.v1.PEPService":   "unannotated",
	"quicbaas.iam.v1.MFAService":   "unannotated",
	"quicbaas.iam.v1.AuditService": "unannotated",
	// qrdb-proxy planes: go-qrdb's own server owns the canonical surface
	// (pkg/apimodule TestModule_RegisterGRPC_ExcludesProxyPlanes).
	"quicbaas.barrier.v1.Barrier":      "qrdb proxy plane",
	"quicbaas.ceremony.v1.Ceremony":    "qrdb proxy plane",
	"quicbaas.tenant.v1.TenantBarrier": "qrdb proxy plane",
	// VectorDB and VectorAdmin are mounted: go-qrdb registers no
	// quicbaas.vector.v1 name.
	// go-xkms's own ServiceModule serves the canonical xkms.v1 surface on the
	// node (pkg/apimodule TestModule_RegisterGRPC_ExcludesXKMSCanonicalSurface).
	// KeyManagement, DEKManagement and Crypto are mounted: the pinned
	// go-xkms serves no DEK surface and no quicbaas.kms.v1 name.
	"quicbaas.certificate.v1.CertificateService": "xkms canonical surface",
	"quicbaas.certificate.v1.TrustService":       "xkms canonical surface",
	"quicbaas.attestation.v1.AttestationService": "xkms canonical surface",
	"quicbaas.piv.v1.PIVService":                 "xkms canonical surface",
}

// serviceNames returns the sorted names of the services registered on gs.
func serviceNames(gs *ggrpc.Server) []string {
	names := make([]string, 0, len(gs.GetServiceInfo()))
	for name := range gs.GetServiceInfo() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestNodeGRPC_MountsEveryUnixSocketService is the #1696 inventory: every
// service the Unix-socket gRPC server registers must also be mounted on the
// node's TCP gRPC server, which is the embedded qrdb node's server serving
// qrdb's native services plus the quicbaas ServiceModule. A service missing
// from the node answers Unimplemented "unknown service" over TCP.
func TestNodeGRPC_MountsEveryUnixSocketService(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.Unix.Socket = ""
	srv.quicBaaSService = newInventoryFacade()

	seamMu.Lock()
	validateRoutingFn = func(_ []uint64, _ map[string]uint64) error { return nil }
	seamMu.Unlock()

	require.NoError(t, srv.startGRPCServer())
	t.Cleanup(srv.grpcServer.Stop)
	unix := serviceNames(srv.grpcServer)

	module, err := srv.newQuicBaaSModule()
	require.NoError(t, err)
	node := ggrpc.NewServer()
	module.RegisterGRPC(node)
	qrdbsdk.RegisterGRPCService(node, nil, nil, srv.buildQRDBRegisterOptions()...)
	mounted := node.GetServiceInfo()

	var missing, excluded []string
	for _, name := range unix {
		_, ok := mounted[name]
		switch {
		case unixOnlyServices[name] != "":
			require.False(t, ok, "%s is mounted: drop it from unixOnlyServices", name)
			excluded = append(excluded, name)
		case !ok:
			missing = append(missing, name)
		}
	}
	require.Empty(t, missing,
		"Unix-socket gRPC services not mounted on the node's TCP gRPC server")
	require.Len(t, excluded, len(unixOnlyServices),
		"an excluded service is no longer served on the Unix socket")
}
