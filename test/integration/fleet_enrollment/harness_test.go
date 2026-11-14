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

//go:build integration && rest

package fleet_enrollment_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qrdbpermission "github.com/jeremyhahn/go-qrdb/pkg/permission"
	qrdbrole "github.com/jeremyhahn/go-qrdb/pkg/role"
	qrdbuser "github.com/jeremyhahn/go-qrdb/pkg/user"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const (
	enrollTenant = "automatethethings"
	enrollAgent  = "agent-agent-04"
)

// -----------------------------------------------------------------------------
// Tenant-routing store
// -----------------------------------------------------------------------------

// tenantRoutingStore mirrors production's pkg/server tenantRoutingKVStore for
// the ORCHESTRATION plane: every operation resolves to the namespace named by
// the tenant bound to the context, never by the tenant segment in the key, and
// the empty tenant folds onto the system tenant exactly as go-qrdb's
// IdentityNamespace does.
//
// A flat store cannot reproduce go-quicbaas#663's second half, because the
// defect IS the two namespaces.
type tenantRoutingStore struct {
	mu     sync.Mutex
	stores map[string]map[string][]byte
}

// newTenantRoutingStore returns an empty routing store.
func newTenantRoutingStore() *tenantRoutingStore {
	return &tenantRoutingStore{stores: make(map[string]map[string][]byte)}
}

var _ interfaces.KVStore = (*tenantRoutingStore)(nil)

// namespaceFor resolves the keyspace a context addresses; callers hold the lock.
func (s *tenantRoutingStore) namespaceFor(ctx context.Context) map[string][]byte {
	tenantID := orchestration.TenantFromContext(ctx)
	if tenantID == "" {
		if bound, ok := tenantctx.Lookup(ctx); ok {
			tenantID = bound
		}
	}
	if tenantID == "" {
		tenantID = "system"
	}
	store, ok := s.stores[tenantID]
	if !ok {
		store = make(map[string][]byte)
		s.stores[tenantID] = store
	}
	return store
}

// Put writes into the context tenant's namespace.
func (s *tenantRoutingStore) Put(ctx context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.namespaceFor(ctx)[key] = append([]byte(nil), value...)
	return nil
}

// PutIfAbsent is the guarded create the fleet registry requires.
func (s *tenantRoutingStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns := s.namespaceFor(ctx)
	if _, exists := ns[key]; exists {
		return false, nil
	}
	ns[key] = append([]byte(nil), value...)
	return true, nil
}

// Get reads from the context tenant's namespace.
func (s *tenantRoutingStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.namespaceFor(ctx)[key]
	if !ok {
		return nil, nil
	}
	return append([]byte(nil), value...), nil
}

// Delete removes from the context tenant's namespace.
func (s *tenantRoutingStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.namespaceFor(ctx), key)
	return nil
}

// Exists reports presence in the context tenant's namespace.
func (s *tenantRoutingStore) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.namespaceFor(ctx)[key]
	return ok, nil
}

// Scan walks the context tenant's namespace.
func (s *tenantRoutingStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	for key, value := range s.matching(ctx, prefix) {
		if err := fn(key, value); err != nil {
			return err
		}
	}
	return nil
}

// List returns the keys under prefix in the context tenant's namespace.
func (s *tenantRoutingStore) List(ctx context.Context, prefix string) ([]string, error) {
	keys := make([]string, 0)
	for key := range s.matching(ctx, prefix) {
		keys = append(keys, key)
	}
	return keys, nil
}

// matching copies the entries under prefix so iteration never holds the lock.
func (s *tenantRoutingStore) matching(ctx context.Context, prefix string) map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte)
	for key, value := range s.namespaceFor(ctx) {
		if len(key) < len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		out[key] = append([]byte(nil), value...)
	}
	return out
}

// -----------------------------------------------------------------------------
// Service double
// -----------------------------------------------------------------------------

// enrollmentService supplies the register handler and the orchestrator
// handlers with the collaborators they resolve from the service layer. Every
// one of them is a production type; the double only supplies the accessors.
type enrollmentService struct {
	*testutil.MockQuicBaaS
	appManager     *iam.AppManager
	appStorage     iam.AppStorage
	identityStore  *iam.IdentityStore
	agentService   orchestration.AgentService
	accountService service.AccountService
}

var _ service.QuicBaaS = (*enrollmentService)(nil)

// GetAppManager returns the IAM AppManager the register handler mints the
// daemon's OAuth2 client with.
func (s *enrollmentService) GetAppManager(_ context.Context) (*iam.AppManager, error) {
	return s.appManager, nil
}

// GetAppStorage returns the storage the self_signed_tls_client_auth client is
// written to.
func (s *enrollmentService) GetAppStorage(_ context.Context) (iam.AppStorage, error) {
	return s.appStorage, nil
}

// GetIdentityStore returns the identity store the certificate principal is
// enrolled in.
func (s *enrollmentService) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return s.identityStore, nil
}

// Agent returns the orchestration agent service the orchestrator REST handlers
// resolve.
func (s *enrollmentService) Agent() orchestration.AgentService {
	return s.agentService
}

// Account returns the pooled provider account service the credential-release
// route resolves (go-quicbaas#734).
func (s *enrollmentService) Account() service.AccountService {
	return s.accountService
}

// -----------------------------------------------------------------------------
// go-qrdb identity plane over the quicbaas identity store
// -----------------------------------------------------------------------------

// enrolledUsers answers go-qrdb's store-resolved role enrichment from the
// records the register endpoint's enrollment wrote, projected through the
// production converter. This is the joint the whole scenario turns on: what
// registration writes has to be what the authenticator reads.
type enrolledUsers struct {
	qrdbuser.UserServicer
	records *iam.MemoryStorage
}

// GetUserByUsername resolves the certificate subject qrdb authenticated.
func (u *enrolledUsers) GetUserByUsername(ctx context.Context, tenantID, username string) (*qrdbuser.User, error) {
	record, err := u.records.GetUserByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, &qrdbuser.UserNotFoundError{Lookup: tenantID + "/" + username}
	}
	core, err := qrdbadapter.FromIAMUser(record)
	if err != nil {
		return nil, err
	}
	return &core, nil
}

// enrolledRoles resolves a role's permissions from the same store.
type enrolledRoles struct {
	qrdbrole.RoleServicer
	records *iam.MemoryStorage
	tenant  string
}

// ResolvePermissions returns the permissions the named roles carry.
func (r *enrolledRoles) ResolvePermissions(ctx context.Context, roleIDs []string) ([]qrdbpermission.Permission, error) {
	out := make([]qrdbpermission.Permission, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		role, err := r.records.GetRole(ctx, r.tenant, roleID)
		if err != nil {
			continue
		}
		for _, granted := range role.Permissions {
			out = append(out, qrdbpermission.Permission(granted))
		}
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Fixture
// -----------------------------------------------------------------------------

// enrollmentFixture is one control plane: a REST server over the production
// register and orchestrator handlers, a fleet registry over a tenant-routing
// store, and the IAM records both write through.
type enrollmentFixture struct {
	server   *restapi.Server
	registry orchestration.FleetRegistry
	identity *iam.MemoryStorage
	store    *tenantRoutingStore

	// accountStore is the same metadata store the service writes through, kept
	// so a scenario can move an account into a refusing state directly.
	accountStore orchestration.AccountStore

	// admin is the tenant admin's edge to the pool, minted on first use by
	// adminEdge.
	admin *httptest.Server
}

// newEnrollmentFixture wires the control plane.
func newEnrollmentFixture(t *testing.T) *enrollmentFixture {
	t.Helper()

	iamKV := iamtest.NewTenantRoutingKV()
	iamKV.BarrierTenant(enrollTenant)
	appStorage := iam.NewBackendStorage(iamKV)

	identityRecords := iam.NewMemoryStorage()
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: identityRecords,
		RoleStorage: identityRecords,
	})

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)

	store := newTenantRoutingStore()
	registry := orchestration.NewFleetRegistry(store)

	agentStore := orchestration.NewInMemoryAgentStore()
	agentSvc, err := orchestration.NewAgentService(
		agentStore,
		map[string]orchestration.AgentBackend{},
		logger.NewAdapter(log),
	)
	require.NoError(t, err)
	wirer, ok := agentSvc.(interface {
		SetFleetRegistry(orchestration.FleetRegistry)
	})
	require.True(t, ok, "the agent service must accept a fleet registry, as server lifecycle wires it")
	wirer.SetFleetRegistry(registry)

	// Pooled provider accounts, wired the way pkg/server wires them: an
	// account metadata store, a barrier-shaped secret store over an in-memory
	// backend, and the token manager the credential release resolves through.
	secretsMgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	require.NoError(t, err)
	accountStore := orchestration.NewInMemoryAccountStore()
	accountSecrets := orchestration.NewAccountSecretStore(secretsMgr)
	accountSvc := service.NewAccountService(accountStore, accountSecrets)
	accountWirer, ok := accountSvc.(interface {
		SetTokenManager(*orchestration.AccountTokenManager)
	})
	require.True(t, ok, "the account service must accept a token manager, as server lifecycle wires it")
	accountWirer.SetTokenManager(
		orchestration.NewAccountTokenManager(accountSecrets, accountStore, 0))

	// The usage ledger and the session reservation table, bound to the
	// account service as pkg/server binds them (lifecycle_orchestration.go:
	// SetUsageTracker, SetSessionReserver). The daemon's resolver reads an
	// account's usage before every release, for the spend verdict and the
	// live-session count, and then claims a session slot in this table. A
	// control plane without them answers both calls with 503, and no pooled
	// credential reaches the daemon.
	//
	// The ledger is over a memory KV, which reports an absent counter as not
	// found; the ledger reads that as zero usage. The reservation table counts
	// the session records of the agent store above.
	usageWirer, ok := accountSvc.(interface {
		SetUsageTracker(orchestration.CostTracker)
	})
	require.True(t, ok, "the account service must accept a usage tracker, as server lifecycle wires it")
	usageWirer.SetUsageTracker(
		orchestration.NewCostTracker(kvstore.NewMemory(), logger.NewAdapter(log)))

	sessionCounter, err := orchestration.NewAccountSessionCounter(agentStore)
	require.NoError(t, err)
	sessionWirer, ok := accountSvc.(interface {
		SetSessionReserver(orchestration.AccountSessionReserver)
	})
	require.True(t, ok, "the account service must accept a session reserver, as server lifecycle wires it")
	sessionWirer.SetSessionReserver(sessionCounter)

	svc := &enrollmentService{
		MockQuicBaaS:   testutil.NewMockQuicBaaS(),
		appManager:     iam.NewAppManager(appStorage, &enrollmentTenants{}),
		appStorage:     appStorage,
		identityStore:  identityStore,
		agentService:   agentSvc,
		accountService: accountSvc,
	}

	cfg := restapi.DefaultConfig()
	cfg.Address = ":0"
	srv, err := restapi.NewServer(svc, cfg, log)
	require.NoError(t, err)
	srv.SetFleetRegistrar(server.NewFleetRegistrar(registry))

	return &enrollmentFixture{
		server:       srv,
		registry:     registry,
		identity:     identityRecords,
		store:        store,
		accountStore: accountStore,
	}
}

// enrollmentTenants is the minimal iam.TenantManager AppManager needs: this
// scenario is about identity and namespaces, not quota.
type enrollmentTenants struct {
	iam.TenantManager
}

// GetTenant returns the tenant by id.
func (m *enrollmentTenants) GetTenant(_ context.Context, id string) (*iam.Tenant, error) {
	return &iam.Tenant{ID: id}, nil
}

// CheckQuota always admits.
func (m *enrollmentTenants) CheckQuota(_ context.Context, _, _ string) error { return nil }

// agentCertificate mints the certificate the deploy service issues: the tenant
// on Organization and OU (#1388), ClientAuth EKU, and the tenant-scoped SPIFFE
// SAN of kind svc go-qrdb derives the identity's tenant from.
func agentCertificate(t *testing.T, tenantID, commonName string) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	spiffeID, err := url.Parse("spiffe://burnin.example.internal/tenant/" +
		tenantID + "/svc/" + commonName)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{tenantID},
			OrganizationalUnit: []string{tenantID},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:        []*url.URL{spiffeID},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}
