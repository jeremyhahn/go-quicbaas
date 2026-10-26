// Package qrdbservice is the identity go-quicbaas's OWN calls carry into its
// embedded qrdb node.
//
// From qrdb v0.47 the embedded transport authorizes every call like any
// other: a context with no identity is refused as unauthenticated. Work no
// end user is behind (the arbitration stores, CAS, schedulers) therefore runs
// as a node-sanctioned service principal (node.WithServicePrincipal) whose
// authority is exactly the grants declared here under its name, and nothing
// else. There is no wildcard grant and no embedded-caller certificate.
//
// The same principal and grant set back pkg/testutil/qrdbnode, so the tests
// that boot a real node exercise the production grants rather than a broader
// test-only identity.
package qrdbservice

import (
	"context"
	"sync"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	sdknode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// PrincipalName is the service principal go-quicbaas's own embedded calls run
// as. qrdb audits them as "qrdb-service:quicbaas".
const PrincipalName = "quicbaas"

// grantNames is the least grant the principal holds, one entry per call class
// go-quicbaas makes through Client.KV, Client.Transaction and, during
// first-boot only, Client.Roles. qrdb sdk/go does
// not re-export the KV, transaction and role permission constants, so each name is
// parsed (qrdbsdk.ParsePermission) and checked against qrdb's registry
// (qrdbsdk.IsPermissionRegistered): a name qrdb no longer registers fails
// Register instead of granting nothing silently.
var grantNames = []string{
	"kv:get",                // KVClient.Get
	"kv:put",                // KVClient.Put
	"kv:delete",             // KVClient.Delete
	"kv:list",               // KVClient.List
	"kv:exists",             // KVClient.Exists
	"kv:range",              // KVClient.Scan
	"transaction:execute",   // TransactionClient.ExecuteTransaction (CAS, claims)
	"batch:execute",         // BatchClient.ExecuteBatch: the entity service's BatchPut and BatchDelete
	"lease:grant",           // LeaseClient.LeaseGrant: placement node leases
	"lease:keepalive",       // LeaseClient.LeaseKeepAlive: placement node renewal
	"lease:revoke",          // LeaseClient.LeaseRevoke: placement deregistration
	"mvcc:put_with_lease",   // MVCCClient.PutWithLease: leased placement node facts
	"roles:read",            // RoleClient.GetRole: first-boot's admin role checks, token issuance's role names
	"permissions:resolve",   // RBACClient.ListPermissions: token issuance's effective permissions (scp)
	"users:read",            // UserClient.GetUser/GetUserByEmail: a WebAuthn ceremony resolving its account (UserReads)
	"user_authfactors:get",  // UserAuthFactorClient.GetAuthFactor: a login replaying its factor's attestation
	"vector:namespace_list", // VectorClient.ListNamespaces: the vector service's startup index load
	// The vector service's per-verb calls. Each runs after quicbaas's own
	// per-verb grant check on the caller, in a tenant-prefixed namespace.
	"vector:namespace_create",        // VectorClient.CreateNamespace
	"vector:namespace_delete",        // VectorClient.DeleteNamespace
	"vector:namespace_info",          // VectorClient.GetNamespaceInfo
	"vector:upsert",                  // VectorClient.UpsertVector
	"vector:get",                     // VectorClient.GetVector
	"vector:delete",                  // VectorClient.DeleteVector
	"vector:batch_upsert",            // VectorClient.BatchUpsert
	"vector:batch_delete",            // VectorClient.BatchDeleteVectors
	"vector:list",                    // VectorClient.ListVectors: namespace scans
	"vector:search_by_metadata",      // VectorClient.SearchByMetadata: multi-filter scans
	"vector:search_similar",          // VectorClient.SearchSimilar: the node's authorized search
	"vector:get_index_stats",         // VectorClient.GetIndexStats
	"vector:compact",                 // VectorClient.CompactNamespace
	"vector:get_compaction_progress", // VectorClient.GetCompactionProgress: async compaction jobs
	"vector:rebuild_index",           // VectorClient.RebuildIndex: a rebuild of the node index searches read
	"vector:get_rebuild_progress",    // VectorClient.GetRebuildProgress: the node's rebuild job
	"vector:hybrid_search",           // VectorClient.HybridSearch
	"vector:search_by_id",            // VectorClient.SearchByID
	// The bound kms:// resolver's secret reads and ensures. go-qrdb
	// intersects a delegation's scope with these grants, so a delegation
	// granting secrets:read or secrets:write holds it only if listed here.
	"secrets:read",  // a delegated kms:// resolve
	"secrets:write", // a delegated kms:// ?ensure=true create
	// The fleet device telemetry the server files and reads on its own
	// behalf (pkg/server fleetTelemetrySeries); a node daemon's grant holds
	// no time-series permission.
	"timeseries:write_batch",   // TimeSeriesClient.WriteBatch: a heartbeat's samples
	"timeseries:set_retention", // TimeSeriesClient.SetRetentionPolicy: the series' retention
	"timeseries:list_metrics",  // TimeSeriesClient.ListMetrics: startup retention sweep
	"timeseries:rich_query",    // TimeSeriesClient.QueryRich: a turn's measured energy
	"admin:peers",              // ClusterClient.Members: the scheduler's leader check
	"index:create",             // IndexClient.IndexCreate: a DAO registering its entity indexes
	"index:query",              // IndexClient.IndexQuery: a DAO's FindBy on an indexed field
	"index:scan",               // IndexClient.IndexScan: a DAO's range read on an indexed field
	// The composed go-xkms module opening its feature databases on the node
	// (go-xkms datastore ensureDatabase): a lookup, then a create when absent.
	"databases:read",   // DatabaseClient.GetDatabase
	"databases:create", // DatabaseClient.CreateDatabase
	// Startup creating the default volume pool barrier tenants are placed
	// on, in the node's replicated catalog (pkg/server
	// provisionDefaultVolumePool).
	"volume_pools:read",   // VolumePoolClient.GetPool
	"volume_pools:create", // VolumePoolClient.CreatePool
	// The CLI's embedded client acting as the system (cmd/quicbaas client.go
	// ServicePrincipalIdentity): its Security() barrier, ceremony,
	// credential and factor verbs. tenant_barrier:* is tenant-only in
	// go-qrdb and refused to the system tenant, so it is not granted. The
	// quorum, share-claim, enable-encryption and tenant init/unseal/status
	// verbs are identity:gated in go-qrdb and need no grant; init-shamir is
	// refused by the SDK before it is sent.
	"seal:status",                  // BarrierClient.BarrierStatus: barrier status
	"seal:bootstrap",               // BarrierClient.BarrierInitialize: barrier init
	"seal:unseal",                  // BarrierClient.BarrierUnseal: barrier unseal
	"seal:seal",                    // BarrierClient.BarrierSeal: barrier seal
	"seal:generate_recovery",       // BarrierClient.BarrierGenerateRecoveryKeys: barrier recovery generate
	"seal:rekey",                   // BarrierClient.BarrierRekeyStart: barrier rekey
	"seal:officer_revoke",          // BarrierClient.BarrierOfficerRevoke: barrier officer-revoke
	"seal:officer_register",        // CeremonyClient.OfficerRegister/RecoveryOfficerRegister: init officer register
	"seal:officer_list",            // CeremonyClient.OfficerList: init officer list
	"ceremony:status",              // CeremonyClient.CeremonyStatus: init status
	"ceremony:initialize",          // CeremonyClient.CeremonyInitialize: init initialize
	"ceremony:sign_csr",            // CeremonyClient.CeremonySignCSR: init sign-csr
	"credentials:submit",           // CredentialClient.CredentialSubmit: credential submit
	"credentials:list",             // CredentialClient.CredentialList: credential list
	"credentials:rotate",           // CredentialClient.CredentialRotate: credential rotate
	"credentials:retrieve_version", // CredentialClient.CredentialRetrieveVersionBuffer: credential retrieve
	"credentials:list_versions",    // CredentialClient.CredentialListVersions: credential versions
	"credentials:delete",           // CredentialClient.CredentialDelete: credential delete
	"credentials:gc_expired",       // CredentialClient.CredentialGCExpired: credential gc
	"user_authfactors:add",         // UserAuthFactorClient.AddAuthFactor: auth factor add
	"user_authfactors:list",        // UserAuthFactorClient.ListAuthFactors: auth factor list
	"user_authfactors:remove",      // UserAuthFactorClient.RemoveAuthFactor: auth factor remove
	"user_authfactors:rotate",      // UserAuthFactorClient.RotateAuthFactor: auth factor rotate
	"user_authfactors:relabel",     // UserAuthFactorClient.RelabelAuthFactor: auth factor relabel
}

// GrantError reports a grant name qrdb's permission registry does not know.
type GrantError struct {
	Name string
	Err  error
}

func (e *GrantError) Error() string {
	return "qrdbservice: grant " + e.Name + " is not a qrdb permission: " + e.Err.Error()
}

func (e *GrantError) Unwrap() error { return e.Err }

// UnregisteredPermissionError reports a well-formed permission name that
// qrdb's registry does not hold.
type UnregisteredPermissionError struct{}

func (UnregisteredPermissionError) Error() string { return "not registered" }

// Grants returns the principal's permissions, resolved against qrdb's
// registry.
func Grants() ([]qrdbsdk.Permission, error) {
	return parseGrants(grantNames)
}

func parseGrants(names []string) ([]qrdbsdk.Permission, error) {
	perms := make([]qrdbsdk.Permission, 0, len(names))
	for _, name := range names {
		p, err := qrdbsdk.ParsePermission(name)
		if err != nil {
			return nil, &GrantError{Name: name, Err: err}
		}
		if !qrdbsdk.IsPermissionRegistered(p) {
			return nil, &GrantError{Name: name, Err: UnregisteredPermissionError{}}
		}
		perms = append(perms, p)
	}
	return perms, nil
}

var register struct {
	once sync.Once
	err  error
}

// Register declares the principal's grants with qrdb's role-grant seam. It
// runs once per process; later calls return the first result.
func Register() error {
	register.once.Do(func() {
		perms, err := Grants()
		if err != nil {
			register.err = err
			return
		}
		qrdbsdk.RegisterPermissionRoleGrant(PrincipalName, perms...)
	})
	return register.err
}

// NodeOption registers the grants and returns the option that names the
// principal on a node under construction.
func NodeOption() (sdknode.NodeOption, error) {
	if err := Register(); err != nil {
		return nil, err
	}
	return sdknode.WithServicePrincipal(PrincipalName), nil
}

// Binder binds the service principal to a context. *sdknode.Node satisfies
// it through ServiceContext.
type Binder interface {
	ServiceContext(ctx context.Context) (context.Context, error)
}

// bind runs ctx as the service principal for the system tenant.
// Node.ServiceContext swaps the identity for the system principal but keeps
// the context's tenant, and qrdb's scope check refuses a system identity
// acting for another tenant. The records these wrappers carry are
// system-owned, so the call runs as the system tenant; the caller's tenant is
// not lost, it is already in each key's prefix.
func bind(ctx context.Context, b Binder) (context.Context, error) {
	return b.ServiceContext(tenantctx.WithSystem(ctx))
}

// KV returns kv with every call bound to the service principal. A kv that
// carries the guarded verbs keeps them, so arbitration still happens in the
// store.
func KV(b Binder, kv transport.KVClient) transport.KVClient {
	if g, ok := kv.(transport.GuardedKVClient); ok {
		return &serviceGuardedKV{serviceKV: serviceKV{b: b, kv: kv}, guarded: g}
	}
	return &serviceKV{b: b, kv: kv}
}

// Transaction returns txn with every call bound to the service principal.
func Transaction(b Binder, txn transport.TransactionClient) transport.TransactionClient {
	return &serviceTransaction{b: b, txn: txn}
}

// Membership is the cluster-membership read the scheduler's leader check
// makes. qrdbsdk.ClusterClient satisfies it.
type Membership interface {
	Members(ctx context.Context, shardID uint64) ([]qrdbsdk.Member, error)
}

// Cluster binds every membership read m makes. qrdb's embedded Peers refuses
// a call with no identity, and the scheduler reads that refusal as "not
// leader", so an unbound leader check would suppress every cluster-wide job.
func Cluster(b Binder, m Membership) Membership {
	return &serviceCluster{b: b, m: m}
}

// UserReads returns users with its account reads (GetUser, GetUserByUsername,
// GetUserByEmail) bound to the service principal; every other call runs as
// its caller. A WebAuthn ceremony resolves its account before anyone is
// authenticated (passkey login, invite registration), and qrdb's embedded
// transport refuses an unauthenticated read. go-qrdb lets the principal read
// the accounts of its own tenant only, so a caller wraps the system tenant's
// client and no other.
func UserReads(b Binder, users transport.UserClient) transport.UserClient {
	return &serviceUserReads{UserClient: users, b: b}
}

// Index returns idx with every call bound to the service principal.
func Index(b Binder, idx transport.IndexClient) transport.IndexClient {
	return &serviceIndex{b: b, idx: idx}
}

// Vector returns vec with its calls routed by who is calling. A request
// whose context carries an authenticated caller reaches qrdb as that caller,
// in its tenant, so qrdb's per-verb grant and tenant floor apply to the
// caller. Work the server does on its own behalf carries no caller and is
// bound to the service principal in the system tenant. The vector service
// prefixes every namespace with the tenant either way.
func Vector(b Binder, vec transport.VectorClient) transport.VectorClient {
	return &serviceVector{b: b, vec: vec}
}

// KVStore returns a DAO store over c whose KV and index calls are all bound
// to the service principal. qrdb's DAO registers its entity indexes with a
// context of its own, so binding the caller's context is not enough: the
// index client itself must carry the identity.
func KVStore(b Binder, c *qrdbsdk.Client) (qrdbsdk.KVStore, error) {
	return qrdbsdk.NewKVStoreAdapter(KV(b, c.KV()), Index(b, c.Index()))
}

type serviceKV struct {
	b  Binder
	kv transport.KVClient
}

func (s *serviceKV) Put(ctx context.Context, key string, value []byte, opts ...transport.WriteOption) error {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return err
	}
	return s.kv.Put(ctx, key, value, opts...)
}

func (s *serviceKV) Get(ctx context.Context, key string, opts ...transport.ReadOption) ([]byte, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.kv.Get(ctx, key, opts...)
}

func (s *serviceKV) Delete(ctx context.Context, key string, opts ...transport.WriteOption) error {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return err
	}
	return s.kv.Delete(ctx, key, opts...)
}

func (s *serviceKV) List(ctx context.Context, prefix string, opts ...transport.ReadOption) ([]string, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.kv.List(ctx, prefix, opts...)
}

func (s *serviceKV) Scan(ctx context.Context, prefix string, opts ...transport.ReadOption) (map[string][]byte, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.kv.Scan(ctx, prefix, opts...)
}

func (s *serviceKV) Exists(ctx context.Context, key string, opts ...transport.ReadOption) (bool, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return false, err
	}
	return s.kv.Exists(ctx, key, opts...)
}

// serviceGuardedKV is serviceKV plus the guarded verbs of a
// transport.GuardedKVClient.
type serviceGuardedKV struct {
	serviceKV
	guarded transport.GuardedKVClient
}

func (s *serviceGuardedKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return false, err
	}
	return s.guarded.PutIfAbsent(ctx, key, value)
}

func (s *serviceGuardedKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return false, err
	}
	return s.guarded.CompareAndDelete(ctx, key, expected)
}

func (s *serviceGuardedKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return false, err
	}
	return s.guarded.CompareAndSwap(ctx, key, expected, next)
}

type serviceTransaction struct {
	b   Binder
	txn transport.TransactionClient
}

func (s *serviceTransaction) ExecuteTransaction(ctx context.Context, req *transport.TransactionRequest, opts ...transport.WriteOption) (*transport.TransactionResponse, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.txn.ExecuteTransaction(ctx, req, opts...)
}

type serviceCluster struct {
	b Binder
	m Membership
}

func (s *serviceCluster) Members(ctx context.Context, shardID uint64) ([]qrdbsdk.Member, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.m.Members(ctx, shardID)
}

type serviceUserReads struct {
	transport.UserClient
	b Binder
}

func (s *serviceUserReads) GetUser(ctx context.Context, id uint64) (*transport.User, error) {
	return boundCall(ctx, s.b, id, s.UserClient.GetUser)
}

func (s *serviceUserReads) GetUserByUsername(ctx context.Context, tenantID, username string) (*transport.User, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.UserClient.GetUserByUsername(ctx, tenantID, username)
}

func (s *serviceUserReads) GetUserByEmail(ctx context.Context, tenantID, email string) (*transport.User, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.UserClient.GetUserByEmail(ctx, tenantID, email)
}

type serviceIndex struct {
	b   Binder
	idx transport.IndexClient
}

func (s *serviceIndex) IndexCreate(ctx context.Context, name, fieldName string, config transport.IndexConfig) error {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return err
	}
	return s.idx.IndexCreate(ctx, name, fieldName, config)
}

func (s *serviceIndex) IndexDrop(ctx context.Context, name string) error {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return err
	}
	return s.idx.IndexDrop(ctx, name)
}

func (s *serviceIndex) IndexQuery(ctx context.Context, indexName, value string) (*transport.IndexQueryResponse, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.idx.IndexQuery(ctx, indexName, value)
}

func (s *serviceIndex) IndexScan(ctx context.Context, indexName string, start, end *string) (*transport.IndexScanResponse, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.idx.IndexScan(ctx, indexName, start, end)
}

func (s *serviceIndex) IndexList(ctx context.Context) (*transport.IndexListResponse, error) {
	ctx, err := bind(ctx, s.b)
	if err != nil {
		return nil, err
	}
	return s.idx.IndexList(ctx)
}

// serviceVector binds the service principal to every vector call.
type serviceVector struct {
	b   Binder
	vec transport.VectorClient
}

// boundCall runs call with ctx bound to the service principal. A context
// that cannot be bound never reaches the node.
func boundCall[Req, Resp any](ctx context.Context, b Binder, req Req, call func(context.Context, Req) (Resp, error)) (Resp, error) {
	ctx, err := bind(ctx, b)
	if err != nil {
		var zero Resp
		return zero, err
	}
	return call(ctx, req)
}

// callerCall runs call as the caller when ctx carries an authenticated
// identity, and bound to the service principal otherwise. Rebinding a
// caller would run a tenant's request as the system tenant.
func callerCall[Req, Resp any](ctx context.Context, b Binder, req Req, call func(context.Context, Req) (Resp, error)) (Resp, error) {
	if qrdbsdk.IdentityFromContext(ctx) != nil {
		return call(ctx, req)
	}
	return boundCall(ctx, b, req, call)
}

func (s *serviceVector) CreateNamespace(ctx context.Context, req *transport.CreateNamespaceRequest) (*transport.CreateNamespaceResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.CreateNamespace)
}

func (s *serviceVector) DeleteNamespace(ctx context.Context, req *transport.DeleteNamespaceRequest) (*transport.DeleteNamespaceResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.DeleteNamespace)
}

func (s *serviceVector) ListNamespaces(ctx context.Context, req *transport.ListNamespacesRequest) (*transport.ListNamespacesResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.ListNamespaces)
}

func (s *serviceVector) GetNamespaceInfo(ctx context.Context, req *transport.GetNamespaceInfoRequest) (*transport.GetNamespaceInfoResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.GetNamespaceInfo)
}

func (s *serviceVector) UpsertVector(ctx context.Context, req *transport.UpsertVectorRequest) (*transport.UpsertVectorResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.UpsertVector)
}

func (s *serviceVector) GetVector(ctx context.Context, req *transport.GetVectorRequest) (*transport.GetVectorResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.GetVector)
}

func (s *serviceVector) DeleteVector(ctx context.Context, req *transport.DeleteVectorRequest) (*transport.DeleteVectorResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.DeleteVector)
}

func (s *serviceVector) BatchUpsert(ctx context.Context, req *transport.BatchUpsertRequest) (*transport.BatchUpsertResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.BatchUpsert)
}

func (s *serviceVector) BatchDeleteVectors(ctx context.Context, req *transport.BatchDeleteVectorsRequest) (*transport.BatchDeleteVectorsResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.BatchDeleteVectors)
}

func (s *serviceVector) ListVectors(ctx context.Context, req *transport.ListVectorsRequest) (*transport.ListVectorsResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.ListVectors)
}

func (s *serviceVector) UpdateMetadata(ctx context.Context, req *transport.UpdateVectorMetadataRequest) (*transport.UpdateVectorMetadataResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.UpdateMetadata)
}

func (s *serviceVector) SearchSimilar(ctx context.Context, req *transport.SearchSimilarRequest) (*transport.SearchSimilarResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.SearchSimilar)
}

func (s *serviceVector) SearchByMetadata(ctx context.Context, req *transport.SearchByMetadataRequest) (*transport.SearchByMetadataResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.SearchByMetadata)
}

func (s *serviceVector) HybridSearch(ctx context.Context, req *transport.HybridSearchRequest) (*transport.HybridSearchResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.HybridSearch)
}

func (s *serviceVector) SearchByID(ctx context.Context, req *transport.SearchByIDRequest) (*transport.SearchByIDResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.SearchByID)
}

func (s *serviceVector) RebuildIndex(ctx context.Context, req *transport.RebuildIndexRequest) (*transport.RebuildIndexResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.RebuildIndex)
}

func (s *serviceVector) GetRebuildProgress(ctx context.Context, req *transport.GetRebuildProgressRequest) (*transport.GetRebuildProgressResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.GetRebuildProgress)
}

func (s *serviceVector) GetIndexStats(ctx context.Context, req *transport.GetIndexStatsRequest) (*transport.GetIndexStatsResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.GetIndexStats)
}

func (s *serviceVector) CompactNamespace(ctx context.Context, req *transport.CompactNamespaceRequest) (*transport.CompactNamespaceResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.CompactNamespace)
}

func (s *serviceVector) GetCompactionProgress(ctx context.Context, req *transport.GetCompactionProgressRequest) (*transport.GetCompactionProgressResponse, error) {
	return callerCall(ctx, s.b, req, s.vec.GetCompactionProgress)
}
