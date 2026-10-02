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

package grpc

import (
	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// brokerServiceDescs is the canonical list of gRPC service descriptors that the
// QuicBaaS broker registers. The exhaustive guard test
// (TestGRPCPermissions_EveryMethodMapped) iterates this slice and verifies that
// every method in every descriptor is either mapped in grpcMethodPermissions or
// declared public in publicGRPCMethods. Any new service registered by the
// broker MUST be added here.
//
// Services whose methods are intentionally unmapped (embedding, RAG, agent
// runtime) are NOT listed here; they are denied by default-deny and their
// absence from this slice documents that posture explicitly.
//
// The qrdb.v1 composition services (qrdb.v1.QRDBService and
// qrdb.v1.UserAuthFactorService) that quicbaasd registers through
// qrdbsdk.RegisterGRPCService (pkg/server/grpc_qrdb_service.go) do NOT appear
// here: their grpc.ServiceDesc values live in qrdb's generated pb package,
// which quicbaas must not import directly (SDK boundary). Their exhaustive
// guard is TestQRDBGRPCPermissions_EveryRegisteredMethodMapped, which
// enumerates the qrdb.v1 proto service descriptors from the protoregistry.
var brokerServiceDescs = []grpc.ServiceDesc{
	pb.QuicBaaS_ServiceDesc,
	pb.Barrier_ServiceDesc,
	pb.TenantBarrier_ServiceDesc,
	pb.Ceremony_ServiceDesc,
	pb.Login_ServiceDesc,
	pb.KeyManagement_ServiceDesc,
	pb.SecretsManagement_ServiceDesc,
	pb.Crypto_ServiceDesc,
	pb.DEKManagement_ServiceDesc,
	pb.CertificateService_ServiceDesc,
	pb.TrustService_ServiceDesc,
	pb.AttestationService_ServiceDesc,
	pb.RegistryService_ServiceDesc,
	pb.FIDO2Service_ServiceDesc,
	pb.PIVService_ServiceDesc,
	pb.VectorDB_ServiceDesc,
	pb.VectorAdmin_ServiceDesc,
	pb.ObjectStorageService_ServiceDesc,
	pb.ObjectStorageAdmin_ServiceDesc,
	pb.ObjectTransferService_ServiceDesc,
	pb.OrchestratorService_ServiceDesc,
	pb.SDLCService_ServiceDesc,
	pb.AgentCatalogService_ServiceDesc,
	pb.MessagingService_ServiceDesc,
	pb.SchedulerService_ServiceDesc,
	pb.ParameterStore_ServiceDesc,
	pb.DynamicSecretsService_ServiceDesc,
	pb.ProjectService_ServiceDesc,
	pb.AccountService_ServiceDesc,
	pb.SSHService_ServiceDesc,
	pb.OllamaService_ServiceDesc,
}

// This file maps gRPC FullMethod names onto qrdb-canonical (resource, action)
// permission tuples. The qrdb policy decision point joins the tuple into the
// registered "resource:action" permission (e.g. "kv:put"); the vocabulary here
// MUST match go-qrdb/pkg/permission/constants.go exactly. It mirrors the REST
// PEP mapping (pkg/api/rest/authz_pep_mapping.go), which established the broker
// resource -> qrdb resource translation (kv, keys, certs, credentials,
// tenant_barrier, users, roles, tenants, ...).
//
// The map is exact-match by FullMethod. A method that has no entry is reported
// unmapped: under RequireAuthz the gRPC authz interceptor is default-deny, so an
// unmapped protected method is denied rather than admitted (there is no
// registered qrdb permission to admit it). This is the same posture the REST PEP
// takes for an unmapped path.
//
// Stage 0.5 maps the broker-only data-plane surfaces that qrdb now registers a
// permission family for (go-qrdb/pkg/permission/broker_dataplane_scope.go):
// object storage (objstore), orchestrator, messaging, scheduler, parameter
// store (parameters), dynamic_secrets, project, ollama. Before Stage 0.5 these
// were unmapped and blanket-denied under RequireAuthz default-deny.
//
// Surfaces that the qrdb permission registry still does NOT define a resource
// family for — embedding, agent runtime, RAG — remain intentionally absent and
// denied under RequireAuthz default-deny. Granting them requires first
// registering a qrdb permission family for the surface; inventing a name here
// would not resolve against the PDP.

// publicGRPCMethods are reachable without authentication. Health and the login
// ceremony (which authenticates the caller via the mTLS handshake / signature,
// before any bearer token exists) bypass the authz gate entirely.
var publicGRPCMethods = map[string]bool{
	"/quicbaas.v1.QuicBaaS/Health":         true,
	"/quicbaas.v1.QuicBaaS/Login":          true, // legacy unauthenticated login entry point
	"/quicbaas.login.v1.Login/BeginLogin":  true,
	"/quicbaas.login.v1.Login/FinishLogin": true,

	// FIDO2 authentication ceremony: WebAuthn IS the authentication
	// mechanism — the caller holds no token until FinishAuthentication
	// mints one. This mirrors the pre-auth posture of the REST
	// /api/v1/iam/{tenant}/webauthn/login/{begin,finish} routes
	// (authz_public_routes.go). The registration pair stays GATED
	// (user_authfactors:add) because it binds a new authenticator to a
	// caller-supplied identity.
	"/quicbaas.fido2.v1.FIDO2Service/BeginAuthentication":  true,
	"/quicbaas.fido2.v1.FIDO2Service/FinishAuthentication": true,

	// qrdb.v1 composition surface: Health is a liveness probe that carries no
	// data, matching the public posture of /quicbaas.v1.QuicBaaS/Health. The
	// qrdb SDK's gRPC and Unix transports probe it when building a client
	// (sdk/go/qrdb.go buildQRDBClient), before any token exists.
	"/qrdb.v1.QRDBService/Health": true,
}

// methodPermission is a qrdb-canonical (resource, action) tuple.
type methodPermission struct {
	resource string
	action   string
}

// qrdbIdentityGatedMethods marks the qrdb.v1 barrier share-claim methods that
// qrdb's own transport permission maps gate with the permission.IdentityGated
// sentinel: any AUTHENTICATED identity may call, and the HANDLER enforces the
// real credential (registered-officer lookup, mTLS SPKI thumbprint match, and
// nonce signature verification). IdentityGated is deliberately NOT a
// registered qrdb permission, so no platform token can ever carry it in scp.
//
// The quicbaas gRPC PEP enforces exact qrdb parity for these verbs through
// the GRPCValidatorAuthenticator seam (platformtoken.Validator.Validate, the
// scope-free authenticated path): the token is fully validated offline
// (signature / iss / aud / exp + DPoP sender-constraint + per-tenant
// liveness) but NO scp permission is demanded — see authorizeGRPCRequest.
// An anonymous caller is denied Unauthenticated; an authorizer without the
// authenticated seam denies outright (fail closed); every deny is mapped by
// validatorGRPCStatus like any other decision. These methods therefore stay
// OUT of grpcMethodPermissions (mapping them would invent policy qrdb does
// not define) and OUT of publicGRPCMethods (public would drop qrdb's
// authenticated-identity requirement). While the daemon is sealed the
// sealed-window allowlist governs them separately (share claims are
// operationally a sealed-window activity).
//
// The ceremony share-claim verbs (the officer CeremonyBeginClaimShare /
// CeremonyCompleteClaimShare handshake and the custodian
// CeremonyClaimShareBegin / CeremonyClaimShareComplete pair) are NOT in this set
// even though qrdb's transport maps also gate them with IdentityGated: qrdb
// registers dedicated ceremony:claim_share_begin / ceremony:claim_share_complete
// permissions, and quicbaas already maps the identical verbs on its own
// quicbaas.ceremony.v1.Ceremony surface to those permissions, so the qrdb.v1
// mirror carries the same (strictly narrower than qrdb) scope requirement.
var qrdbIdentityGatedMethods = map[string]bool{
	"/qrdb.v1.QRDBService/BarrierShareClaimBegin":            true,
	"/qrdb.v1.QRDBService/BarrierShareClaimComplete":         true,
	"/qrdb.v1.QRDBService/BarrierRecoveryShareClaimBegin":    true,
	"/qrdb.v1.QRDBService/BarrierRecoveryShareClaimComplete": true,
}

// grpcMethodPermissions maps each protected gRPC FullMethod to the qrdb-canonical
// permission it requires. Every value is a registered qrdb permission (the join
// resource:action exists in go-qrdb/pkg/permission/constants.go).
var grpcMethodPermissions = map[string]methodPermission{
	// ---- quicbaas.v1.QuicBaaS: KV ----
	"/quicbaas.v1.QuicBaaS/Get":            {"kv", "get"},
	"/quicbaas.v1.QuicBaaS/List":           {"kv", "list"},
	"/quicbaas.v1.QuicBaaS/Query":          {"kv", "range"},
	"/quicbaas.v1.QuicBaaS/ScanIndex":      {"index", "scan"},
	"/quicbaas.v1.QuicBaaS/Put":            {"kv", "put"},
	"/quicbaas.v1.QuicBaaS/Delete":         {"kv", "delete"},
	"/quicbaas.v1.QuicBaaS/BatchGet":       {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/BatchPut":       {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/BatchDelete":    {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/StreamBatchPut": {"batch", "execute"},
	"/quicbaas.v1.QuicBaaS/Transaction":    {"transaction", "execute"},
	"/quicbaas.v1.QuicBaaS/Watch":          {"watch", "watch"},

	// ---- quicbaas.v1.QuicBaaS: cluster admin ----
	"/quicbaas.v1.QuicBaaS/ClusterStatus":        {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/GetLeader":            {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/ListEntityTypes":      {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/AddNode":              {"admin", "add_member"},
	"/quicbaas.v1.QuicBaaS/RemoveNode":           {"admin", "remove_member"},
	"/quicbaas.v1.QuicBaaS/CreateShard":          {"admin", "start_shard"},
	"/quicbaas.v1.QuicBaaS/RebalanceCluster":     {"rebalance", "execute"},
	"/quicbaas.v1.QuicBaaS/GetRebalanceProgress": {"rebalance", "report"},
	"/quicbaas.v1.QuicBaaS/MigrateData":          {"admin", "drain"},
	"/quicbaas.v1.QuicBaaS/GetMigrationProgress": {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/ReplaceNode":          {"admin", "add_member"},
	"/quicbaas.v1.QuicBaaS/UpgradeNode":          {"admin", "drain"},
	"/quicbaas.v1.QuicBaaS/GetUpgradeStatus":     {"admin", "status"},

	// ---- quicbaas.v1.QuicBaaS: cache (admin capabilities) ----
	"/quicbaas.v1.QuicBaaS/GetCacheStats":     {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/GetCacheConfig":    {"admin", "status"},
	"/quicbaas.v1.QuicBaaS/ClearCache":        {"admin", "capabilities"},
	"/quicbaas.v1.QuicBaaS/UpdateCacheConfig": {"admin", "capabilities"},

	// ---- quicbaas.v1.QuicBaaS: backup ----
	"/quicbaas.v1.QuicBaaS/CreateBackup":  {"backup", "create"},
	"/quicbaas.v1.QuicBaaS/ListBackups":   {"backup", "list"},
	"/quicbaas.v1.QuicBaaS/DeleteBackup":  {"backup", "delete"},
	"/quicbaas.v1.QuicBaaS/RestoreBackup": {"backup", "restore"},
	"/quicbaas.v1.QuicBaaS/VerifyBackup":  {"backup", "verify"},

	// ---- quicbaas.v1.QuicBaaS: role management ----
	"/quicbaas.v1.QuicBaaS/CreateRole": {"roles", "create"},
	"/quicbaas.v1.QuicBaaS/DeleteRole": {"roles", "delete"},
	"/quicbaas.v1.QuicBaaS/ListRoles":  {"roles", "list"},
	"/quicbaas.v1.QuicBaaS/AssignRole": {"roles", "assign"},
	"/quicbaas.v1.QuicBaaS/RevokeRole": {"roles", "unassign"},

	// ---- quicbaas.iam.v1.IAMService: WebAuthn credential administration ----
	//
	// These three are the ONLY IAMService methods mapped here, and that needs
	// saying out loud: the rest of quicbaas.iam.v1.IAMService is absent from
	// brokerServiceDescs and unmapped, so under RequireAuthz it is denied
	// wholesale. Mapping these three does not carve an exception into that
	// posture — it is the minimum needed for the surface to exist at all.
	// They are the operator's ONLY recovery path for a WebAuthn credential
	// stranded by a deleted user (go-quicbaas#273): the credential blocks its
	// email from ever registering an authenticator again, and every other way
	// of clearing it was removed with the ceremony-only surface. Left unmapped
	// the verbs would reach the server and be denied at the gate — wiring that
	// looks present and is dead, which is worse than not shipping it.
	//
	// The grants are the qrdb user-admin families the same verbs already
	// require on the MCP plane (pkg/api/mcp/authz_qrdb.go): read to see what
	// is stored, delete to remove it. Nothing new is granted — a caller needs
	// exactly the user-admin rights it would need to delete the user outright.
	//
	// Mapping the REST of IAMService is an authorization-policy decision for
	// the owner, not a wiring change; it is tracked separately.
	"/quicbaas.iam.v1.IAMService/ListWebAuthnCredentials":   {"users", "read"},
	"/quicbaas.iam.v1.IAMService/DeleteWebAuthnCredential":  {"users", "delete"},
	"/quicbaas.iam.v1.IAMService/DeleteWebAuthnCredentials": {"users", "delete"},

	// ---- quicbaas.iam.v1.IAMService: built-in role reconciliation ----
	//
	// ReconcileRoles takes the role-create family rather than a verb of its
	// own. A dedicated roles:reconcile would be absent from every role seeded
	// before it existed, so nobody would hold the grant needed to repair that
	// exact absence — the drift the verb exists to fix would lock out its own
	// remedy (go-qrdb's stated guidance on go-quicbaas#573). Reusing
	// roles:create grants nothing new: a caller able to define a role is
	// already able to set the permission sets this converges.
	"/quicbaas.iam.v1.IAMService/ReconcileRoles": {"roles", "create"},

	// ---- quicbaas.v1.QuicBaaS: auth (token lifecycle) ----
	"/quicbaas.v1.QuicBaaS/Logout":        {"user_authfactors", "login"},
	"/quicbaas.v1.QuicBaaS/RefreshToken":  {"user_authfactors", "login"},
	"/quicbaas.v1.QuicBaaS/ValidateToken": {"user_authfactors", "login"},

	// ---- quicbaas.v1.QuicBaaS: seal ----
	"/quicbaas.v1.QuicBaaS/Seal":       {"seal", "seal"},
	"/quicbaas.v1.QuicBaaS/Unseal":     {"seal", "unseal"},
	"/quicbaas.v1.QuicBaaS/SealInit":   {"seal", "bootstrap"},
	"/quicbaas.v1.QuicBaaS/SealRekey":  {"seal", "rekey"},
	"/quicbaas.v1.QuicBaaS/SealStatus": {"seal", "status"},

	// ---- quicbaas.v1.QuicBaaS: time-series ----
	"/quicbaas.v1.QuicBaaS/Aggregate":            {"timeseries", "aggregate"},
	"/quicbaas.v1.QuicBaaS/TimeSeriesQuery":      {"timeseries", "query"},
	"/quicbaas.v1.QuicBaaS/TSQuery":              {"timeseries", "query"},
	"/quicbaas.v1.QuicBaaS/TSGetRetentionPolicy": {"timeseries", "get_retention"},
	"/quicbaas.v1.QuicBaaS/TSListMetrics":        {"timeseries", "list_metrics"},
	"/quicbaas.v1.QuicBaaS/TSWritePoint":         {"timeseries", "write_point"},
	"/quicbaas.v1.QuicBaaS/TSWriteBatch":         {"timeseries", "write_batch"},
	"/quicbaas.v1.QuicBaaS/TSSetRetentionPolicy": {"timeseries", "set_retention"},
	"/quicbaas.v1.QuicBaaS/TSDeleteSeries":       {"timeseries", "delete_series"},

	// ---- quicbaas.kms.v1.KeyManagement (cryptographic keys) ----
	"/quicbaas.kms.v1.KeyManagement/GenerateKey":             {"keys", "create"},
	"/quicbaas.kms.v1.KeyManagement/ImportKey":               {"keys", "import"},
	"/quicbaas.kms.v1.KeyManagement/ExportKey":               {"keys", "export"},
	"/quicbaas.kms.v1.KeyManagement/UnwrapKey":               {"keys", "unwrap"},
	"/quicbaas.kms.v1.KeyManagement/GetKey":                  {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/GetPublicKey":            {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/GetKeyMetadata":          {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/GetKeyPolicy":            {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/GetSupportedAlgorithms":  {"keys", "list"},
	"/quicbaas.kms.v1.KeyManagement/ListKeys":                {"keys", "list"},
	"/quicbaas.kms.v1.KeyManagement/ListKeyVersions":         {"keys", "list"},
	"/quicbaas.kms.v1.KeyManagement/RotateKey":               {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/DeleteKey":               {"keys", "delete"},
	"/quicbaas.kms.v1.KeyManagement/DeleteKeyPolicy":         {"keys", "delete"},
	"/quicbaas.kms.v1.KeyManagement/RevokeKey":               {"keys", "delete"},
	"/quicbaas.kms.v1.KeyManagement/SetKeyPolicy":            {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/SetKeySafety":            {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/ClearKeySafety":          {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/UpdateKeyMetadata":       {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/UpdateKeyStatus":         {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/SetKeyRotationPolicy":    {"keys", "rotate"},
	"/quicbaas.kms.v1.KeyManagement/GetKeyRotationPolicy":    {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/DeleteKeyRotationPolicy": {"keys", "delete"},
	"/quicbaas.kms.v1.KeyManagement/SafetyCheck":             {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/SafetyReport":            {"keys", "get"},
	"/quicbaas.kms.v1.KeyManagement/BackendStatus":           {"backends", "read"},
	"/quicbaas.kms.v1.KeyManagement/BackendTest":             {"backends", "read"},

	// ---- quicbaas.kms.v1.Crypto ----
	"/quicbaas.kms.v1.Crypto/Encrypt":                {"keys", "encrypt"},
	"/quicbaas.kms.v1.Crypto/EncryptBatch":           {"keys", "encrypt"},
	"/quicbaas.kms.v1.Crypto/Decrypt":                {"keys", "decrypt"},
	"/quicbaas.kms.v1.Crypto/DecryptBatch":           {"keys", "decrypt"},
	"/quicbaas.kms.v1.Crypto/Sign":                   {"keys", "sign"},
	"/quicbaas.kms.v1.Crypto/Verify":                 {"keys", "verify"},
	"/quicbaas.kms.v1.Crypto/Rewrap":                 {"keys", "unwrap"},
	"/quicbaas.kms.v1.Crypto/GenerateDataKey":        {"keys", "derive"},
	"/quicbaas.kms.v1.Crypto/GenerateWrappedDataKey": {"keys", "wrap"},
	"/quicbaas.kms.v1.Crypto/GenerateHMAC":           {"keys", "sign"},
	"/quicbaas.kms.v1.Crypto/VerifyHMAC":             {"keys", "verify"},
	"/quicbaas.kms.v1.Crypto/Hash":                   {"keys", "sign"},
	"/quicbaas.kms.v1.Crypto/GenerateRandom":         {"keys", "derive"},

	// ---- quicbaas.kms.v1.DEKManagement ----
	"/quicbaas.kms.v1.DEKManagement/CreateDEK":               {"keys", "create"},
	"/quicbaas.kms.v1.DEKManagement/GetDEK":                  {"keys", "get"},
	"/quicbaas.kms.v1.DEKManagement/ListDEKs":                {"keys", "list"},
	"/quicbaas.kms.v1.DEKManagement/RotateDEK":               {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/DeleteDEK":               {"keys", "delete"},
	"/quicbaas.kms.v1.DEKManagement/SetDefaultDEK":           {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/GetDEKStatus":            {"keys", "get"},
	"/quicbaas.kms.v1.DEKManagement/SetDEKSafety":            {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/ClearDEKSafety":          {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/StartReencryption":       {"keys", "rotate"},
	"/quicbaas.kms.v1.DEKManagement/GetReencryptionProgress": {"keys", "get"},

	// ---- quicbaas.kms.v1.SecretsManagement (the tenant secret vault) ----
	//
	// This surface carries qrdb's secrets:read / secrets:write / secrets:delete
	// / secrets:list family (go-qrdb/pkg/permission/broker_scope.go), NOT
	// credentials:*. The two families guard different stores: credentials:* is
	// qrdb's named-credential store, which has no tenant namespacing at all and
	// is therefore registered ScopeCross — system tenant only — while every RPC
	// below reads or writes under the CALLER's tenant
	// (pkg/service/secrets_tenant.go secretsTenantScopedID). Under credentials:*
	// qrdb's RBAC cross-scope rule denied every tenant identity, so the whole
	// tenant-facing vault was unreachable over gRPC and the Unix socket while
	// the grant sat unusable on the tenant Admin role (go-quicbaas#534).
	//
	// The action split matches the REST route table entry for entry
	// (pkg/api/rest/routes_permissions_generated.go, the eight /api/v1/secrets
	// routes) so one operation needs one grant whichever transport carries it:
	// list is the names-only enumeration, read is anything that returns secret
	// material or its metadata (including a version listing and a prefix or
	// watch fetch, which return values), write is every mutation — store,
	// generate-on-first-access, rotate, rollback, status, rotation policy — and
	// delete is removal. The rotation-policy verbs have no REST route of their
	// own; they take read/write of the policy sub-resource on the same family.
	//
	// pkg/api/mcp/authz_qrdb.go maps the MCP twin of every verb below and
	// TestSecretsVaultGRPCPermissions pins this block; the proto annotations in
	// api/proto/kms.proto are the second gRPC enforcement point and are pinned
	// to this table by TestGRPCPermissions_ProtoOptionMatchesAuthzMapping.
	"/quicbaas.kms.v1.SecretsManagement/StoreSecret":          {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecret":            {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/ListSecrets":          {"secrets", "list"},
	"/quicbaas.kms.v1.SecretsManagement/DeleteSecret":         {"secrets", "delete"},
	"/quicbaas.kms.v1.SecretsManagement/RotateSecret":         {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/ListSecretVersions":   {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecretVersion":     {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/RollbackSecret":       {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/SetRotationPolicy":    {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/GetRotationPolicy":    {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecretsByPrefix":   {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/UpdateSecretStatus":   {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/WatchSecretsByPrefix": {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/EnsureSecret":         {"secrets", "write"},

	// ---- quicbaas.certificate.v1.CertificateService ----
	"/quicbaas.certificate.v1.CertificateService/IssueCertificate":  {"certs", "generate"},
	"/quicbaas.certificate.v1.CertificateService/SignCSR":           {"certs", "generate"},
	"/quicbaas.certificate.v1.CertificateService/RevokeCertificate": {"certs", "revoke"},
	"/quicbaas.certificate.v1.CertificateService/VerifyCertificate": {"certs", "read"},
	"/quicbaas.certificate.v1.CertificateService/GetCACertificate":  {"certs", "read"},
	"/quicbaas.certificate.v1.CertificateService/GetTLSConfig":      {"certs", "read"},

	// Certificate extension verbs (server_certificate_extensions.go).
	//
	// CrossSign mints a CA certificate: it re-certifies a foreign ROOT's
	// subject and public key with basicConstraints CA:TRUE. qrdb states the
	// boundary explicitly — "Holding certs:generate must not imply the
	// authority to mint a new signing CA" (go-qrdb/pkg/permission/
	// acme_admin_scope.go) — so it authorizes with ca:create.
	//
	// Chain runs the CA's Authority Information Access walk, which fetches
	// issuer certificates over the network and imports them into the CA
	// certificate store. That is the same mutation TrustService/AutoImportAIA
	// performs, so it carries the same write-strength verb; mapping it to
	// certs:read would let a read-only principal drive the importer.
	//
	// IssueDeviceIdentity mints TCG EK / AK / IEEE 802.1AR IDevID
	// certificates. Those are END-ENTITY certificates, so they stay in the
	// leaf-issuance family: certs:generate.
	"/quicbaas.certificate.v1.CertificateService/CrossSign":           {"ca", "create"},
	"/quicbaas.certificate.v1.CertificateService/Chain":               {"certs", "generate"},
	"/quicbaas.certificate.v1.CertificateService/IssueDeviceIdentity": {"certs", "generate"},

	// ---- quicbaas.attestation.v1.AttestationService ----
	//
	// Attestation gets its own resource vocabulary rather than borrowing
	// certs:* or ca:*. The verbs answer a different question — "is this
	// host and this workload in the state we approved?" — and a principal
	// that may read the certificate store has no business driving a TPM
	// quote or reading a host's PCR values, which are a fingerprint of the
	// platform's boot state.
	//
	// Verify and VerifyChain are pure computations over caller-supplied
	// material: they mutate nothing, so they carry the read-strength
	// attestation:verify.
	//
	// AttestContainer is NOT a read. It drives the registered attestor,
	// which on the standalone path extends a PCR at measured launch and
	// asks the host TPM for a signed quote — an interaction with hardware
	// that a fleet attestor additionally records against a device registry.
	// It therefore carries its own write-strength verb rather than sharing
	// one with the verification pair.
	"/quicbaas.attestation.v1.AttestationService/Verify":          {"attestation", "verify"},
	"/quicbaas.attestation.v1.AttestationService/VerifyChain":     {"attestation", "verify"},
	"/quicbaas.attestation.v1.AttestationService/AttestContainer": {"attestation", "attest"},

	// ---- quicbaas.registry.v1.RegistryService ----
	//
	// The service registry has its own resource family (pkg/permission):
	// register/renew/deregister mutate only the caller tenant's own
	// registrations and carry the write verb; resolve and the watch stream
	// only read and carry the read verb.
	"/quicbaas.registry.v1.RegistryService/Register":   {"service_registry", "register"},
	"/quicbaas.registry.v1.RegistryService/Renew":      {"service_registry", "register"},
	"/quicbaas.registry.v1.RegistryService/Deregister": {"service_registry", "register"},
	"/quicbaas.registry.v1.RegistryService/Resolve":    {"service_registry", "read"},
	"/quicbaas.registry.v1.RegistryService/Watch":      {"service_registry", "read"},

	// ---- quicbaas.certificate.v1.TrustService ----
	"/quicbaas.certificate.v1.TrustService/CrossSignedRootPool": {"certs", "read"},
	"/quicbaas.certificate.v1.TrustService/AutoImportAIA":       {"certs", "generate"},
	"/quicbaas.certificate.v1.TrustService/PublishDANE":         {"certs", "generate"},

	// ---- quicbaas.fido2.v1.FIDO2Service (registration pair) ----
	//
	// The registration ceremony binds a NEW authenticator to a
	// caller-supplied identity, so it carries the same tuple the REST PEP
	// assigns to /api/v1/iam/{tenant}/webauthn/registration/*
	// (authz_pep_mapping.go): user_authfactors:add. The authentication
	// pair is pre-auth and lives in publicGRPCMethods.
	"/quicbaas.fido2.v1.FIDO2Service/BeginRegistration":  {"user_authfactors", "add"},
	"/quicbaas.fido2.v1.FIDO2Service/FinishRegistration": {"user_authfactors", "add"},

	// ---- quicbaas.piv.v1.PIVService ----
	//
	// Same piv:* family the REST PEP assigns to /api/v1/xkms/piv/*
	// (authz_pep_mapping.go): slot reads are piv:read, certificate and key
	// mutations are piv:write / piv:delete, and CSR generation exercises
	// the slot's private key so it carries the use-strength verb.
	"/quicbaas.piv.v1.PIVService/ListPIVSlots":         {"piv", "read"},
	"/quicbaas.piv.v1.PIVService/GetPIVCertificate":    {"piv", "read"},
	"/quicbaas.piv.v1.PIVService/ExportPIVCertificate": {"piv", "read"},
	"/quicbaas.piv.v1.PIVService/StorePIVCertificate":  {"piv", "write"},
	"/quicbaas.piv.v1.PIVService/ImportPIVCertificate": {"piv", "write"},
	"/quicbaas.piv.v1.PIVService/DeletePIVCertificate": {"piv", "delete"},
	"/quicbaas.piv.v1.PIVService/GeneratePIVKey":       {"piv", "write"},
	"/quicbaas.piv.v1.PIVService/GeneratePIVCSR":       {"piv", "use"},

	// ---- quicbaas.barrier.v1.Barrier (system barrier lifecycle) ----
	"/quicbaas.barrier.v1.Barrier/BarrierStatus":                {"tenant_barrier", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierInitialize":            {"tenant_barrier", "init"},
	"/quicbaas.barrier.v1.Barrier/BarrierUnseal":                {"tenant_barrier", "unseal"},
	"/quicbaas.barrier.v1.Barrier/BarrierSeal":                  {"tenant_barrier", "seal"},
	"/quicbaas.barrier.v1.Barrier/BarrierRekey":                 {"seal", "rekey"},
	"/quicbaas.barrier.v1.Barrier/BarrierRootToken":             {"seal", "bootstrap"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryStatus":        {"seal", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryGenerate":      {"seal", "generate_recovery"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryRecover":       {"seal", "recover_with_keys"},
	"/quicbaas.barrier.v1.Barrier/BarrierRecoveryDelete":        {"seal", "generate_recovery"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirInitialize":      {"seal", "initialize_shamir"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirUnsealShare":     {"barrier", "unseal-share"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirUnsealShares":    {"barrier", "unseal-share"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirListShares":      {"seal", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirVerify":          {"seal", "status"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirDeleteShare":     {"seal", "rekey"},
	"/quicbaas.barrier.v1.Barrier/BarrierShamirDeleteAllShares": {"seal", "rekey"},
	"/quicbaas.barrier.v1.Barrier/BarrierEnableEncryption":      {"seal", "enable_encryption"},

	// ---- quicbaas.tenant.v1.TenantBarrier (per-tenant barrier lifecycle) ----
	"/quicbaas.tenant.v1.TenantBarrier/Init":               {"tenant_barrier", "init"},
	"/quicbaas.tenant.v1.TenantBarrier/Unseal":             {"tenant_barrier", "unseal"},
	"/quicbaas.tenant.v1.TenantBarrier/UnsealShare":        {"tenant_barrier", "unseal"},
	"/quicbaas.tenant.v1.TenantBarrier/Seal":               {"tenant_barrier", "seal"},
	"/quicbaas.tenant.v1.TenantBarrier/Status":             {"tenant_barrier", "status"},
	"/quicbaas.tenant.v1.TenantBarrier/EnableEncryption":   {"tenant_barrier", "enable_encryption"},
	"/quicbaas.tenant.v1.TenantBarrier/UpdateQuotas":       {"tenants", "set_quotas"},
	"/quicbaas.tenant.v1.TenantBarrier/UpdateEntitlements": {"tenants", "set_entitlements"},

	// ---- quicbaas.ceremony.v1.Ceremony ----
	"/quicbaas.ceremony.v1.Ceremony/CeremonyInitialize":            {"ceremony", "initialize"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyStatus":                {"ceremony", "status"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyEnrollExternalOfficer": {"ceremony", "enroll_custodian"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyListOfficers":          {"ceremony", "status"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyClaimCertBegin":        {"ceremony", "claim_cert"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyClaimCertComplete":     {"ceremony", "claim_cert"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyBeginClaimShare":       {"ceremony", "claim_share_begin"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonyCompleteClaimShare":    {"ceremony", "claim_share_complete"},
	"/quicbaas.ceremony.v1.Ceremony/CeremonySignCSR":               {"ceremony", "sign_csr"},

	// ---- quicbaas.vector.v1.VectorDB ----
	"/quicbaas.vector.v1.VectorDB/CreateNamespace":    {"vector", "namespace_create"},
	"/quicbaas.vector.v1.VectorDB/DeleteNamespace":    {"vector", "namespace_delete"},
	"/quicbaas.vector.v1.VectorDB/ListNamespaces":     {"vector", "namespace_list"},
	"/quicbaas.vector.v1.VectorDB/GetNamespaceInfo":   {"vector", "namespace_info"},
	"/quicbaas.vector.v1.VectorDB/UpdateMetadata":     {"vector", "update_metadata"},
	"/quicbaas.vector.v1.VectorDB/SearchSimilar":      {"vector", "search_similar"},
	"/quicbaas.vector.v1.VectorDB/SearchByMetadata":   {"vector", "search_by_metadata"},
	"/quicbaas.vector.v1.VectorDB/HybridSearch":       {"vector", "hybrid_search"},
	"/quicbaas.vector.v1.VectorDB/UpsertVector":       {"vector", "upsert"},
	"/quicbaas.vector.v1.VectorDB/GetVector":          {"vector", "get"},
	"/quicbaas.vector.v1.VectorDB/DeleteVector":       {"vector", "delete"},
	"/quicbaas.vector.v1.VectorDB/BatchDeleteVectors": {"vector", "batch_delete"},
	"/quicbaas.vector.v1.VectorDB/BatchUpsertVectors": {"vector", "batch_upsert"},
	"/quicbaas.vector.v1.VectorDB/SearchByID":         {"vector", "search_by_id"},
	"/quicbaas.vector.v1.VectorDB/ListVectors":        {"vector", "list"},
	"/quicbaas.vector.v1.VectorDB/UpdateNamespace":    {"vector", "namespace_create"},
	"/quicbaas.vector.v1.VectorDB/GetVectorStats":     {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorDB/GetIndexProgress":   {"vector", "get_rebuild_progress"},
	"/quicbaas.vector.v1.VectorDB/Health":             {"vector", "namespace_list"},
	"/quicbaas.vector.v1.VectorDB/RebuildIndex":       {"vector", "rebuild_index"},

	// ---- quicbaas.vector.v1.VectorAdmin (index lifecycle + cluster management) ----
	"/quicbaas.vector.v1.VectorAdmin/CompactNamespace":      {"vector", "compact"},
	"/quicbaas.vector.v1.VectorAdmin/GetCompactionProgress": {"vector", "get_compaction_progress"},
	"/quicbaas.vector.v1.VectorAdmin/GetClusterStats":       {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorAdmin/CreateScalarIndex":     {"vector", "rebuild_index"},
	"/quicbaas.vector.v1.VectorAdmin/DropScalarIndex":       {"vector", "rebuild_index"},
	"/quicbaas.vector.v1.VectorAdmin/ListScalarIndexes":     {"vector", "get_index_stats"},
	"/quicbaas.vector.v1.VectorAdmin/ExportVectors":         {"vector", "list"},
	"/quicbaas.vector.v1.VectorAdmin/ImportVectors":         {"vector", "batch_upsert"},

	// ---- Stage 0.5: broker data-plane surfaces (qrdb-registered) ----

	// ---- quicbaas.objectstorage.v1.ObjectStorageService ----
	"/quicbaas.objectstorage.v1.ObjectStorageService/PutObject":                    {"objstore", "put"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/StreamPutObject":              {"objstore", "put"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObject":                    {"objstore", "get"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/StreamGetObject":              {"objstore", "get"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/DeleteObject":                 {"objstore", "delete"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/HeadObject":                   {"objstore", "head"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListObjects":                  {"objstore", "list"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListByTag":                    {"objstore", "list"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListByTenant":                 {"objstore", "list"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/CopyObject":                   {"objstore", "copy"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ArchiveObject":                {"objstore", "archive"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/RestoreObject":                {"objstore", "restore"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/VerifyObject":                 {"objstore", "verify"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ReplicateObject":              {"objstore", "replicate"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/MigrateObject":                {"objstore", "migrate"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/BatchPutObjects":              {"objstore", "batch"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/BatchGetObjects":              {"objstore", "batch"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/BatchDeleteObjects":           {"objstore", "batch"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObjectPolicy":              {"objstore", "read_policy"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/SetObjectPolicy":              {"objstore", "write_policy"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/ListBackends":                 {"objstore", "read_backend"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetBackendHealth":             {"objstore", "read_backend"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObjectStorageTenantUsage":  {"objstore", "tenant_usage"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/GetObjectStorageTenantQuotas": {"objstore", "tenant_quota_read"},
	"/quicbaas.objectstorage.v1.ObjectStorageService/SetObjectStorageTenantQuotas": {"objstore", "tenant_quota_write"},

	// ---- quicbaas.objectstorage.v1.ObjectStorageAdmin ----
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/CreateBackend":                {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/UpdateBackend":                {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/DeleteBackend":                {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/SetBackendStatus":             {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/GetStorageStats":              {"objstore", "read_stats"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/RunGarbageCollection":         {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/GetGarbageCollectionProgress": {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/RepairObject":                 {"objstore", "admin"},
	"/quicbaas.objectstorage.v1.ObjectStorageAdmin/CompactBackend":               {"objstore", "admin"},

	// ---- quicbaas.v1.ObjectTransferService ----
	"/quicbaas.v1.ObjectTransferService/FetchObject":        {"objstore", "get"},
	"/quicbaas.v1.ObjectTransferService/StreamFetchObjects": {"objstore", "get"},

	// ---- quicbaas.orchestrator.v1.OrchestratorService ----
	"/quicbaas.orchestrator.v1.OrchestratorService/SpawnSession":  {"orchestrator", "spawn"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SpawnChildJob": {"orchestrator", "job_start"},
	"/quicbaas.orchestrator.v1.OrchestratorService/PromptSession": {"orchestrator", "prompt"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SessionStatus": {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CloseSession":  {"orchestrator", "close"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListSessions":  {"orchestrator", "list"},
	// Rotation closes the session's agent process and reconstitutes it against
	// a newly selected pooled credential — the authority a spawn carries, not a
	// close or a status read. orchestrator:spawn is therefore the deliberate
	// choice: it is exactly the permission the session was created with, and
	// the weaker close/status verbs must never let a caller consume a
	// different account's credential. Mirrored on REST
	// (/api/v1/orchestrator/.../rotate-account), QUIC (rotateSubOpPermissions)
	// and MCP (quicbaas_orchestrator_rotate_account).
	"/quicbaas.orchestrator.v1.OrchestratorService/RotateSessionAccount":   {"orchestrator", "spawn"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SubscribeSessionEvents": {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetSessionConversation": {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetSpendReport":         {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetTenantSpendPolicy":   {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SetTenantSpendPolicy":   {"spend_policy", "write"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetCostSettings":        {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SetCostSettings":        {"cost_settings", "write"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CreateTask":             {"orchestrator", "task_create"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetTask":                {"orchestrator", "task_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListTasks":              {"orchestrator", "task_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ClaimTask":              {"orchestrator", "task_claim"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CompleteTask":           {"orchestrator", "task_complete"},
	"/quicbaas.orchestrator.v1.OrchestratorService/BlockTask":              {"orchestrator", "task_block"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ReadmitTask":            {"orchestrator", "task_readmit"},
	"/quicbaas.orchestrator.v1.OrchestratorService/DeleteTask":             {"orchestrator", "task_create"},
	"/quicbaas.orchestrator.v1.OrchestratorService/DecomposeTask":          {"orchestrator", "task_decompose"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SubmitTaskArtifacts":    {"orchestrator", "artifact_submit"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetTaskArtifact":        {"orchestrator", "artifact_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListTaskArtifacts":      {"orchestrator", "artifact_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/StartJob":               {"orchestrator", "job_start"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetJobStatus":           {"orchestrator", "job_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListJobs":               {"orchestrator", "job_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/CancelJob":              {"orchestrator", "job_cancel"},
	"/quicbaas.orchestrator.v1.OrchestratorService/SubscribeJobEvents":     {"orchestrator", "job_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListBackends":           {"orchestrator", "backend_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListRunners":            {"orchestrator", "status"},
	"/quicbaas.orchestrator.v1.OrchestratorService/Health":                 {"orchestrator", "health"},
	"/quicbaas.orchestrator.v1.OrchestratorService/Inference":              {"orchestrator", "inference"},
	"/quicbaas.orchestrator.v1.OrchestratorService/InferenceStream":        {"orchestrator", "inference"},
	"/quicbaas.orchestrator.v1.OrchestratorService/RegisterFleetNode":      {"orchestrator", "agent_register"},
	"/quicbaas.orchestrator.v1.OrchestratorService/GetFleetNode":           {"orchestrator", "agent_read"},
	"/quicbaas.orchestrator.v1.OrchestratorService/ListFleetNodes":         {"orchestrator", "agent_list"},
	"/quicbaas.orchestrator.v1.OrchestratorService/RemoveFleetNode":        {"orchestrator", "agent_remove"},
	"/quicbaas.orchestrator.v1.OrchestratorService/FleetNodeHeartbeat":     {"orchestrator", "agent_heartbeat"},
	"/quicbaas.orchestrator.v1.OrchestratorService/PushEvents":             {"orchestrator", "hook"},

	// ---- quicbaas.messaging.v1.MessagingService ----
	"/quicbaas.messaging.v1.MessagingService/CreateChannel":       {"messaging", "channel_create"},
	"/quicbaas.messaging.v1.MessagingService/GetChannel":          {"messaging", "channel_read"},
	"/quicbaas.messaging.v1.MessagingService/ListChannels":        {"messaging", "channel_list"},
	"/quicbaas.messaging.v1.MessagingService/DeleteChannel":       {"messaging", "channel_delete"},
	"/quicbaas.messaging.v1.MessagingService/Publish":             {"messaging", "publish"},
	"/quicbaas.messaging.v1.MessagingService/PublishDelta":        {"messaging", "publish"},
	"/quicbaas.messaging.v1.MessagingService/Subscribe":           {"messaging", "subscribe"},
	"/quicbaas.messaging.v1.MessagingService/Unsubscribe":         {"messaging", "unsubscribe"},
	"/quicbaas.messaging.v1.MessagingService/WatchChannel":        {"messaging", "watch"},
	"/quicbaas.messaging.v1.MessagingService/StreamNotifications": {"messaging", "watch"},
	"/quicbaas.messaging.v1.MessagingService/GetStats":            {"messaging", "stats"},
	"/quicbaas.messaging.v1.MessagingService/GetChannelStats":     {"messaging", "stats"},
	"/quicbaas.messaging.v1.MessagingService/GetDedupStats":       {"messaging", "stats"},

	// ---- quicbaas.scheduler.v1.SchedulerService ----
	"/quicbaas.scheduler.v1.SchedulerService/CreateJob":          {"scheduler", "create"},
	"/quicbaas.scheduler.v1.SchedulerService/GetJob":             {"scheduler", "read"},
	"/quicbaas.scheduler.v1.SchedulerService/ListJobs":           {"scheduler", "list"},
	"/quicbaas.scheduler.v1.SchedulerService/UpdateJob":          {"scheduler", "update"},
	"/quicbaas.scheduler.v1.SchedulerService/DeleteJob":          {"scheduler", "delete"},
	"/quicbaas.scheduler.v1.SchedulerService/TriggerJob":         {"scheduler", "trigger"},
	"/quicbaas.scheduler.v1.SchedulerService/PauseJob":           {"scheduler", "pause"},
	"/quicbaas.scheduler.v1.SchedulerService/ResumeJob":          {"scheduler", "resume"},
	"/quicbaas.scheduler.v1.SchedulerService/CancelJob":          {"scheduler", "cancel"},
	"/quicbaas.scheduler.v1.SchedulerService/GetJobHistory":      {"scheduler", "history"},
	"/quicbaas.scheduler.v1.SchedulerService/GetSchedulerStatus": {"scheduler", "status"},

	// Workload placement rides the SchedulerService but carries its own
	// "placement" family: qrdb owns "scheduler" (see pkg/permission).
	// Reads read; Place/Release consume capacity; node registration,
	// cordon and drain reshape the pool.
	"/quicbaas.scheduler.v1.SchedulerService/GetNode":        {"placement", "read"},
	"/quicbaas.scheduler.v1.SchedulerService/ListNodes":      {"placement", "read"},
	"/quicbaas.scheduler.v1.SchedulerService/GetPlacement":   {"placement", "read"},
	"/quicbaas.scheduler.v1.SchedulerService/ListPlacements": {"placement", "read"},
	"/quicbaas.scheduler.v1.SchedulerService/Place":          {"placement", "place"},
	"/quicbaas.scheduler.v1.SchedulerService/Release":        {"placement", "place"},
	"/quicbaas.scheduler.v1.SchedulerService/RegisterNode":   {"placement", "manage"},
	"/quicbaas.scheduler.v1.SchedulerService/RenewNode":      {"placement", "manage"},
	"/quicbaas.scheduler.v1.SchedulerService/DeregisterNode": {"placement", "manage"},
	"/quicbaas.scheduler.v1.SchedulerService/CordonNode":     {"placement", "manage"},
	"/quicbaas.scheduler.v1.SchedulerService/UncordonNode":   {"placement", "manage"},
	"/quicbaas.scheduler.v1.SchedulerService/DrainNode":      {"placement", "manage"},

	// ---- quicbaas.parameterstore.v1.ParameterStore ----
	"/quicbaas.parameterstore.v1.ParameterStore/GetParameter":     {"parameters", "read"},
	"/quicbaas.parameterstore.v1.ParameterStore/GetParameters":    {"parameters", "read"},
	"/quicbaas.parameterstore.v1.ParameterStore/SetParameter":     {"parameters", "write"},
	"/quicbaas.parameterstore.v1.ParameterStore/SetParameters":    {"parameters", "write"},
	"/quicbaas.parameterstore.v1.ParameterStore/DeleteParameter":  {"parameters", "delete"},
	"/quicbaas.parameterstore.v1.ParameterStore/DeleteParameters": {"parameters", "delete"},
	"/quicbaas.parameterstore.v1.ParameterStore/ListParameters":   {"parameters", "list"},
	"/quicbaas.parameterstore.v1.ParameterStore/SearchByTags":     {"parameters", "search"},

	// ---- quicbaas.dynamicsecrets.v1.DynamicSecretsService ----
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/CreateRole":         {"dynamic_secrets", "role_create"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/GetRole":            {"dynamic_secrets", "role_read"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/ListRoles":          {"dynamic_secrets", "role_list"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/DeleteRole":         {"dynamic_secrets", "role_delete"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/GenerateCredential": {"dynamic_secrets", "generate"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/RenewLease":         {"dynamic_secrets", "lease_renew"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/RevokeLease":        {"dynamic_secrets", "lease_revoke"},
	"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/ListLeases":         {"dynamic_secrets", "lease_list"},

	// ---- quicbaas.agents.v1.AgentCatalogService ----
	// A tenant's AI agent definitions and pools, mirroring GET/POST/PUT/DELETE
	// /api/v1/agents and /api/v1/agent-pools and the MCP quicbaas_agents_* /
	// quicbaas_agent_pools_* tools. Reads are agents:read; every change —
	// including a delete, which the catalog refuses while anything uses the
	// Agent — is agents:write.
	"/quicbaas.agents.v1.AgentCatalogService/ListAgents":      {"agents", "read"},
	"/quicbaas.agents.v1.AgentCatalogService/GetAgent":        {"agents", "read"},
	"/quicbaas.agents.v1.AgentCatalogService/AgentUsedBy":     {"agents", "read"},
	"/quicbaas.agents.v1.AgentCatalogService/CreateAgent":     {"agents", "write"},
	"/quicbaas.agents.v1.AgentCatalogService/UpdateAgent":     {"agents", "write"},
	"/quicbaas.agents.v1.AgentCatalogService/DeleteAgent":     {"agents", "write"},
	"/quicbaas.agents.v1.AgentCatalogService/ListAgentPools":  {"agents", "read"},
	"/quicbaas.agents.v1.AgentCatalogService/GetAgentPool":    {"agents", "read"},
	"/quicbaas.agents.v1.AgentCatalogService/AgentPoolUsedBy": {"agents", "read"},
	"/quicbaas.agents.v1.AgentCatalogService/CreateAgentPool": {"agents", "write"},
	"/quicbaas.agents.v1.AgentCatalogService/UpdateAgentPool": {"agents", "write"},
	"/quicbaas.agents.v1.AgentCatalogService/DeleteAgentPool": {"agents", "write"},

	// ---- quicbaas.sdlc.v1.SDLCService ----
	// The SDLC module's verbs, mirroring the REST rules for the binding,
	// delivery and forges routes and the MCP sdlc_* tools. ListForges is
	// gated sdlc:read because GET /api/v1/sdlc/forges is.
	"/quicbaas.sdlc.v1.SDLCService/BindProject":   {"sdlc", "bind"},
	"/quicbaas.sdlc.v1.SDLCService/UnbindProject": {"sdlc", "unbind"},
	"/quicbaas.sdlc.v1.SDLCService/GetBinding":    {"sdlc", "read"},
	"/quicbaas.sdlc.v1.SDLCService/GetDelivery":   {"sdlc", "read"},
	"/quicbaas.sdlc.v1.SDLCService/ListForges":    {"sdlc", "read"},
	// Onboard WRITES to the forge, so it is gated with the bind permission
	// that PUT .../binding is; GetConformance only reads, and is gated with
	// the read permission GET .../conformance is. The split is the point:
	// a reader may ask what state a repository is in without being able to
	// change it.
	"/quicbaas.sdlc.v1.SDLCService/Onboard":        {"sdlc", "bind"},
	"/quicbaas.sdlc.v1.SDLCService/GetConformance": {"sdlc", "read"},
	// Open work is read from the forge and changes nothing, like conformance.
	"/quicbaas.sdlc.v1.SDLCService/GetOpenWork": {"sdlc", "read"},
	// Choosing which reviewer judges a pull request is a decision about the
	// project's review, so writing an override carries the bind permission
	// the binding does; reading one is sdlc:read.
	"/quicbaas.sdlc.v1.SDLCService/GetReviewerSelection":  {"sdlc", "read"},
	"/quicbaas.sdlc.v1.SDLCService/SetReviewerOverride":   {"sdlc", "bind"},
	"/quicbaas.sdlc.v1.SDLCService/ClearReviewerOverride": {"sdlc", "bind"},
	// Filing an issue is an operator write on the bound forge with its own
	// action; listing the collaborators a ruler is picked from only reads.
	"/quicbaas.sdlc.v1.SDLCService/FileIssue":         {"sdlc", "issue_create"},
	"/quicbaas.sdlc.v1.SDLCService/ListCollaborators": {"sdlc", "read"},
	// The worker image refresh is a HOST verb over every tenant's bindings,
	// so it has its own cross-tenant family rather than an sdlc:* action,
	// mirroring GET /api/v1/sdlc/worker-image and POST .../refresh.
	"/quicbaas.sdlc.v1.SDLCService/GetWorkerImageStatus": {"worker_images", "read"},
	"/quicbaas.sdlc.v1.SDLCService/RefreshWorkerImage":   {"worker_images", "refresh"},
	// Adopting an issue creates its task, and is gated as task creation is,
	// mirroring POST /api/v1/sdlc/projects/{projectID}/adopt.
	"/quicbaas.sdlc.v1.SDLCService/AdoptIssue": {"orchestrator", "task_create"},
	// The attention inbox, mirroring GET /api/v1/orchestrator/attention and
	// POST .../attention/{taskID}/resolve.
	"/quicbaas.sdlc.v1.SDLCService/ListAttention":    {"orchestrator", "status"},
	"/quicbaas.sdlc.v1.SDLCService/ResolveAttention": {"orchestrator", "task_readmit"},

	// ---- quicbaas.project.v1.ProjectService ----
	"/quicbaas.project.v1.ProjectService/CreateProject":    {"project", "create"},
	"/quicbaas.project.v1.ProjectService/GetProject":       {"project", "read"},
	"/quicbaas.project.v1.ProjectService/GetProjectByName": {"project", "read"},
	"/quicbaas.project.v1.ProjectService/ListProjects":     {"project", "list"},
	"/quicbaas.project.v1.ProjectService/UpdateProject":    {"project", "update"},
	"/quicbaas.project.v1.ProjectService/DeleteProject":    {"project", "delete"},
	// Membership verbs mirror the REST PEP rules for
	// PUT/DELETE /api/v1/projects/{id}/permissions/{node_id}.
	"/quicbaas.project.v1.ProjectService/GrantFleetNodeRole":  {"project", "grant"},
	"/quicbaas.project.v1.ProjectService/RevokeFleetNodeRole": {"project", "revoke"},

	// ---- quicbaas.account.v1.AccountService ----
	// Pooled provider accounts custody credential material (an Anthropic /
	// Codex / OpenCode login), so they authorize with the qrdb credentials:*
	// vocabulary — the same family the /api/v1/secrets and /v1/credentials
	// REST surfaces use (pkg/api/rest/authz_pep_mapping.go). qrdb registers
	// no account:* family; when one is added these mappings should move to it.
	//
	// The METADATA READS take QuicBaaS's own tenant-scoped agent_accounts:*
	// family rather than credentials:*, and that is a correction rather than a
	// split for its own sake: credentials:* is registered ScopeCross — system
	// tenant only — because qrdb's named-credential store has no tenant
	// namespacing, while a pooled account is written into the requesting
	// tenant's own keyspace. Under credentials:* the RBAC cross-scope rule
	// denied every tenant identity a surface that is tenant data, including the
	// enrolled fleet node daemon that has to select over it (go-quicbaas#734). Same
	// correction as go-quicbaas#534 made for /api/v1/secrets.
	//
	// The WRITE verbs deliberately still take credentials:*, so minting or
	// rotating a pooled login remains a system-tenant operation.
	"/quicbaas.account.v1.AccountService/CreateAccount":           {"credentials", "store"},
	"/quicbaas.account.v1.AccountService/GetAccount":              {"agent_accounts", "read"},
	"/quicbaas.account.v1.AccountService/ListAccounts":            {"agent_accounts", "list"},
	"/quicbaas.account.v1.AccountService/UpdateAccount":           {"credentials", "store"},
	"/quicbaas.account.v1.AccountService/DeleteAccount":           {"credentials", "delete"},
	"/quicbaas.account.v1.AccountService/RotateAccountCredential": {"credentials", "rotate"},
	// Revoking excludes the account from every selection strategy — a
	// destructive lifecycle transition, so it takes the delete-grade verb.
	"/quicbaas.account.v1.AccountService/RevokeAccount":     {"credentials", "delete"},
	"/quicbaas.account.v1.AccountService/SetDefaultAccount": {"credentials", "store"},
	"/quicbaas.account.v1.AccountService/GetAccountUsage":   {"agent_accounts", "read"},
	// Materializing RELEASES the credential envelope, so it is the ONE verb
	// here that does not take a credentials:* grant. It carries QuicBaaS's own
	// agent_accounts:materialize (pkg/permission), for two reasons the
	// paragraph above could not anticipate: credentials:* is registered
	// ScopeCross — system tenant only — and would deny the tenant-scoped agent
	// daemon this verb exists for; and an identity that may READ the pool's
	// metadata must not thereby be able to extract every login in it.
	"/quicbaas.account.v1.AccountService/MaterializeAccountCredential": {"agent_accounts", "materialize"},
	// The session reservation is the release's companion: taken immediately
	// before it, by the same principal, for the session the release serves.
	"/quicbaas.account.v1.AccountService/ReserveAccountSession": {"agent_accounts", "materialize"},
	"/quicbaas.account.v1.AccountService/ReleaseAccountSession": {"agent_accounts", "materialize"},

	// ---- quicbaas.ssh.v1.SSHService ----
	// SSH assets are cryptographic keys, so they authorize with the qrdb
	// keys:* vocabulary — byte-for-byte the same (resource, action) pairs the
	// REST PEP assigns to /api/v1/ssh (pkg/api/rest/authz_pep_mapping.go
	// sshOps). One grant therefore authorizes the same SSH verb identically
	// on REST, gRPC, Unix, QUIC, and MCP. qrdb does register an ssh:* family
	// (ssh:read / ssh:write / ssh:delete); migrating to it is a cross-surface
	// change that must move REST and this map together, not one of them.
	"/quicbaas.ssh.v1.SSHService/GenerateSSHKey":     {"keys", "create"},
	"/quicbaas.ssh.v1.SSHService/ImportSSHKey":       {"keys", "import"},
	"/quicbaas.ssh.v1.SSHService/GetSSHKey":          {"keys", "get"},
	"/quicbaas.ssh.v1.SSHService/ListSSHKeys":        {"keys", "list"},
	"/quicbaas.ssh.v1.SSHService/DeleteSSHKey":       {"keys", "delete"},
	"/quicbaas.ssh.v1.SSHService/ExportSSHPublicKey": {"keys", "get"},
	"/quicbaas.ssh.v1.SSHService/SignSSHDigest":      {"keys", "sign"},

	"/quicbaas.ssh.v1.SSHService/AddSSHKnownHost":    {"keys", "create"},
	"/quicbaas.ssh.v1.SSHService/ListSSHKnownHosts":  {"keys", "list"},
	"/quicbaas.ssh.v1.SSHService/RemoveSSHKnownHost": {"keys", "delete"},

	"/quicbaas.ssh.v1.SSHService/AddSSHAuthorizedKey":    {"keys", "create"},
	"/quicbaas.ssh.v1.SSHService/ListSSHAuthorizedKeys":  {"keys", "list"},
	"/quicbaas.ssh.v1.SSHService/RemoveSSHAuthorizedKey": {"keys", "delete"},
	// Emitting an authorized_keys line is a formatting read of a public key.
	"/quicbaas.ssh.v1.SSHService/EmitSSHAuthorizedKey": {"keys", "get"},

	// ---- quicbaas.ollama.v1.OllamaService ----
	"/quicbaas.ollama.v1.OllamaService/Health":         {"ollama", "health"},
	"/quicbaas.ollama.v1.OllamaService/ListModels":     {"ollama", "model_list"},
	"/quicbaas.ollama.v1.OllamaService/GetModelInfo":   {"ollama", "model_info"},
	"/quicbaas.ollama.v1.OllamaService/Embed":          {"ollama", "embed"},
	"/quicbaas.ollama.v1.OllamaService/EmbedBatch":     {"ollama", "embed"},
	"/quicbaas.ollama.v1.OllamaService/EmbedWithModel": {"ollama", "embed"},

	// ==== qrdb.v1 composition surface ====
	//
	// quicbaasd registers qrdb's QRDBServiceServer (and the standalone
	// qrdb.v1.UserAuthFactorService) on the SAME grpc.Server as the quicbaas
	// services (pkg/server/grpc_qrdb_service.go), so the qrdb SDK's gRPC and
	// Unix transports (go-qrdb sdk/go buildQRDBTransport) can dial the
	// barrier / ceremony / credential / user / user-auth-factor surfaces
	// through the broker listener. The entries below mirror go-qrdb's OWN
	// gRPC permission map (go-qrdb/pkg/auth/middleware/mapping.go
	// grpcPermissions) verbatim, so a permission grant authorizes the same
	// verb identically on a direct qrdb node and through the broker.
	//
	// Scope: ONLY the families whose servicers buildQRDBRegisterOptions
	// actually wires — barrier, ceremony, credential, user, user-auth-factor
	// — plus Health (public, above) and Status. Every other qrdb.v1 method
	// (KV, lease, lock, index, timeseries, vector, ...) dispatches to
	// codes.Unimplemented server-side and stays default-denied here; the
	// quicbaas-namespaced services are the canonical broker surface for
	// those capabilities.
	//
	// These mappings apply POST-unseal. While the daemon is sealed the authz
	// gate is not in force and the sealed-window allowlist governs instead.

	// ---- qrdb.v1.QRDBService: admin status ----
	// Health is public (see publicGRPCMethods). Status mirrors both qrdb's
	// AdminStatus entry and the broker's own ClusterStatus mapping.
	"/qrdb.v1.QRDBService/Status": {"admin", "status"},

	// ---- qrdb.v1.QRDBService: system barrier lifecycle ----
	"/qrdb.v1.QRDBService/BarrierInitialize":        {"seal", "bootstrap"},
	"/qrdb.v1.QRDBService/BarrierUnseal":            {"seal", "unseal"},
	"/qrdb.v1.QRDBService/BarrierSeal":              {"seal", "seal"},
	"/qrdb.v1.QRDBService/BarrierStatus":            {"seal", "status"},
	"/qrdb.v1.QRDBService/BarrierShamirInitialize":  {"seal", "initialize_shamir"},
	"/qrdb.v1.QRDBService/BarrierShamirUnsealShare": {"barrier", "unseal-share"},
	// UnsealShares (plural) submits a batch of shares toward the same quorum.
	"/qrdb.v1.QRDBService/BarrierShamirUnsealShares": {"barrier", "unseal-share"},
	"/qrdb.v1.QRDBService/BarrierRekey":              {"seal", "rekey"},
	"/qrdb.v1.QRDBService/BarrierEnableEncryption":   {"seal", "enable_encryption"},
	"/qrdb.v1.QRDBService/BarrierRootToken":          {"seal", "bootstrap"},
	// Security Officer registration / listing. The four share-claim methods
	// (BarrierShareClaimBegin/Complete, BarrierRecoveryShareClaimBegin/
	// Complete) are IdentityGated in qrdb and deliberately NOT mapped here —
	// see qrdbIdentityGatedMethods.
	"/qrdb.v1.QRDBService/BarrierOfficerRegister":         {"seal", "officer_register"},
	"/qrdb.v1.QRDBService/BarrierOfficerList":             {"seal", "officer_list"},
	"/qrdb.v1.QRDBService/BarrierOfficerClaimStatus":      {"seal", "officer_list"},
	"/qrdb.v1.QRDBService/BarrierShamirSealVote":          {"seal", "submit_seal_vote"},
	"/qrdb.v1.QRDBService/BarrierRecoveryOfficerRegister": {"seal", "officer_register"},
	// Recovery key lifecycle.
	"/qrdb.v1.QRDBService/BarrierRecoveryGenerate": {"seal", "generate_recovery"},
	"/qrdb.v1.QRDBService/BarrierRecoveryRecover":  {"seal", "recover_with_keys"},
	"/qrdb.v1.QRDBService/BarrierRecoveryDelete":   {"seal", "generate_recovery"},
	"/qrdb.v1.QRDBService/BarrierRecoveryStatus":   {"seal", "status"},

	// ---- qrdb.v1.QRDBService: Shamir initialization ceremony ----
	// The custodian claim-share verbs are IdentityGated in qrdb's own maps,
	// but qrdb registers dedicated ceremony:claim_share_begin/_complete
	// permissions and quicbaas already maps the identical verb on
	// quicbaas.ceremony.v1.Ceremony to them, so the qrdb.v1 mirror carries
	// the same (strictly narrower than qrdb) scope requirement: the token
	// must carry the claim_share scope AND the handler still verifies the
	// enrolled custodian's factor challenge.
	"/qrdb.v1.QRDBService/CeremonyInitialize":            {"ceremony", "initialize"},
	"/qrdb.v1.QRDBService/CeremonyStatus":                {"ceremony", "status"},
	"/qrdb.v1.QRDBService/CeremonyClaimCertBegin":        {"ceremony", "claim_cert"},
	"/qrdb.v1.QRDBService/CeremonyClaimCertComplete":     {"ceremony", "claim_cert"},
	"/qrdb.v1.QRDBService/CeremonyBeginClaimShare":       {"ceremony", "claim_share_begin"},
	"/qrdb.v1.QRDBService/CeremonyCompleteClaimShare":    {"ceremony", "claim_share_complete"},
	"/qrdb.v1.QRDBService/CeremonyClaimShareBegin":       {"ceremony", "claim_share_begin"},
	"/qrdb.v1.QRDBService/CeremonyClaimShareComplete":    {"ceremony", "claim_share_complete"},
	"/qrdb.v1.QRDBService/CeremonySignCSR":               {"ceremony", "sign_csr"},
	"/qrdb.v1.QRDBService/CeremonyEnrollExternalOfficer": {"ceremony", "enroll_custodian"},
	"/qrdb.v1.QRDBService/CeremonySubmitUnsealShare":     {"barrier", "unseal-share"},

	// ---- qrdb.v1.QRDBService: versioned named-credential rotation ----
	"/qrdb.v1.QRDBService/CredentialSubmit":          {"credentials", "submit"},
	"/qrdb.v1.QRDBService/CredentialStrategy":        {"credentials", "strategy"},
	"/qrdb.v1.QRDBService/CredentialList":            {"credentials", "list"},
	"/qrdb.v1.QRDBService/CredentialDelete":          {"credentials", "delete"},
	"/qrdb.v1.QRDBService/CredentialRotate":          {"credentials", "rotate"},
	"/qrdb.v1.QRDBService/CredentialGCExpired":       {"credentials", "gc_expired"},
	"/qrdb.v1.QRDBService/CredentialRetrieveVersion": {"credentials", "retrieve_version"},
	"/qrdb.v1.QRDBService/CredentialListVersions":    {"credentials", "list_versions"},
	"/qrdb.v1.QRDBService/CredentialStore":           {"credentials", "store"},
	"/qrdb.v1.QRDBService/CredentialRetrieve":        {"credentials", "retrieve"},

	// ---- qrdb.v1.QRDBService: user management ----
	// Backs the qrdb SDK's Security().Auth().Factors() subject resolution
	// (resolveSubjectUserID) and user CRUD. Same vocabulary as the REST
	// scope rules (pkg/api/rest/authz_default_deny.go) and the MCP
	// qrdb_user_* tool mappings (pkg/api/mcp/authz_qrdb.go).
	"/qrdb.v1.QRDBService/CreateUser":              {"users", "create"},
	"/qrdb.v1.QRDBService/GetUser":                 {"users", "read"},
	"/qrdb.v1.QRDBService/GetUserByUsername":       {"users", "read"},
	"/qrdb.v1.QRDBService/GetUserByCertThumbprint": {"users", "get_by_cert_thumbprint"},
	"/qrdb.v1.QRDBService/AddCertBinding":          {"users", "add_cert_binding"},
	"/qrdb.v1.QRDBService/UpdateUser":              {"users", "update"},
	"/qrdb.v1.QRDBService/DeleteUser":              {"users", "delete"},
	"/qrdb.v1.QRDBService/ListUsers":               {"users", "list"},
	"/qrdb.v1.QRDBService/EnableUser":              {"users", "enable"},
	"/qrdb.v1.QRDBService/DisableUser":             {"users", "disable"},

	// ---- qrdb.v1.UserAuthFactorService (user-login factor surface) ----
	// Registered as a separate proto service by qrdbsdk.RegisterGRPCService
	// alongside QRDBService. 1:1 with qrdb's UserAuthFactorService entries
	// and the MCP qrdb_user_authfactor_* tool mappings.
	"/qrdb.v1.UserAuthFactorService/Add":     {"user_authfactors", "add"},
	"/qrdb.v1.UserAuthFactorService/List":    {"user_authfactors", "list"},
	"/qrdb.v1.UserAuthFactorService/Get":     {"user_authfactors", "get"},
	"/qrdb.v1.UserAuthFactorService/Remove":  {"user_authfactors", "remove"},
	"/qrdb.v1.UserAuthFactorService/Rotate":  {"user_authfactors", "rotate"},
	"/qrdb.v1.UserAuthFactorService/Relabel": {"user_authfactors", "relabel"},
	"/qrdb.v1.UserAuthFactorService/Login":   {"user_authfactors", "login"},
}

// grpcResourceAndAction returns the qrdb-canonical (resource, action) for the
// gRPC FullMethod and whether a mapping exists. An unmapped method has ok=false:
// the default-deny authz gate denies it under RequireAuthz.
func grpcResourceAndAction(fullMethod string) (resource, action string, ok bool) {
	if perm, found := grpcMethodPermissions[fullMethod]; found {
		return perm.resource, perm.action, true
	}
	return "", "", false
}

// isPublicGRPCMethod reports whether the gRPC method bypasses authorization.
func isPublicGRPCMethod(fullMethod string) bool {
	return publicGRPCMethods[fullMethod]
}
