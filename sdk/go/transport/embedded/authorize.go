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

package embedded

import "context"

// OperationAuthorizer admits or refuses one operation for the identity the
// ContextBinder bound into ctx. operation is the "service/method" dispatch
// key and perms are the qrdb permissions that operation declares; perms is
// empty for an operation that declares none.
type OperationAuthorizer func(ctx context.Context, operation string, perms []string) error

// WithOperationAuthorizer makes the transport ask authorize before every
// unary and stream dispatch, after the identity is bound, so a refused
// operation never reaches the service.
func WithOperationAuthorizer(authorize OperationAuthorizer) Option {
	return func(t *Transport) {
		t.authorize = authorize
	}
}

// publicOperations are the dispatch keys every caller may reach without a
// grant: the login ceremonies that establish an identity and the OIDC
// discovery documents, the operations pkg/api/grpc serves as public methods.
// The transport does not ask the OperationAuthorizer about them.
var publicOperations = map[string]struct{}{
	"quicbaas.Auth/Login":                   {},
	"quicbaas.Auth/BeginLogin":              {},
	"quicbaas.Auth/FinishLogin":             {},
	"quicbaas.Auth/BeginFederatedLogin":     {},
	"quicbaas.Auth/HandleFederatedCallback": {},
	"quicbaas.Auth/GetJWKS":                 {},
	"quicbaas.Auth/GetOpenIDConfiguration":  {},

	// An invite is redeemed by the person it was issued to, who holds the
	// one-time token and no identity yet.
	"quicbaas.IAM.Invite/GetInvite":           {},
	"quicbaas.IAM.Invite/ActivateFIDO2Begin":  {},
	"quicbaas.IAM.Invite/ActivateFIDO2Finish": {},
	"quicbaas.IAM.Invite/ActivatePIV":         {},
}

// operationPermissions maps each dispatch key to the qrdb permissions the
// same RPC declares on the gRPC transport (pkg/api/grpc authz_mapping.go).
// An operation absent from this map declares no permission.
var operationPermissions = map[string][]string{
	"quicbaas.KV/Put":             {"kv:put"},
	"quicbaas.KV/Get":             {"kv:get"},
	"quicbaas.KV/Delete":          {"kv:delete"},
	"quicbaas.KV/List":            {"kv:range"},
	"quicbaas.KV/ListEntityTypes": {"kv:range"},

	"quicbaas.Batch/Put":    {"batch:execute"},
	"quicbaas.Batch/Get":    {"batch:execute"},
	"quicbaas.Batch/Delete": {"batch:execute"},

	"quicbaas.Transaction/Execute": {"transaction:execute"},

	"quicbaas.Cluster/Status":             {"admin:topology"},
	"quicbaas.Cluster/GetLeader":          {"admin:shard_state"},
	"quicbaas.Cluster/Nodes":              {"admin:topology"},
	"quicbaas.Cluster/AddNode":            {"admin:add_member"},
	"quicbaas.Cluster/RemoveNode":         {"admin:remove_member"},
	"quicbaas.Cluster/CreateShard":        {"admin:start_shard"},
	"quicbaas.Cluster/Rebalance":          {"rebalance:execute"},
	"quicbaas.Cluster/GetRebalanceStatus": {"rebalance:report"},

	"quicbaas.Backup/Create":  {"backup:create"},
	"quicbaas.Backup/List":    {"backup:list"},
	"quicbaas.Backup/Restore": {"backup:restore"},
	"quicbaas.Backup/Delete":  {"backup:delete"},
	"quicbaas.Backup/Verify":  {"backup:verify"},

	"quicbaas.Auth/Logout":          {"user_authfactors:login"},
	"quicbaas.Auth/RefreshToken":    {"user_authfactors:login"},
	"quicbaas.Auth/IntrospectToken": {"user_authfactors:login"},
	"quicbaas.Auth/ValidateToken":   {"user_authfactors:login"},

	// The session verbs act only on the caller's own sessions, as Logout
	// does, so they declare Logout's permission; the IAM.Session verbs
	// below administer other users' sessions. The provider reads declare
	// what the gRPC FederationService declares.
	"quicbaas.Auth/GetSession":        {"user_authfactors:login"},
	"quicbaas.Auth/ListSessions":      {"user_authfactors:login"},
	"quicbaas.Auth/RevokeSession":     {"user_authfactors:login"},
	"quicbaas.Auth/RevokeAllSessions": {"user_authfactors:login"},
	"quicbaas.Auth/ListProviders":     {"tenants:read"},
	"quicbaas.Auth/GetProvider":       {"tenants:read"},

	"quicbaas.RBAC/CreateRole": {"roles:create"},
	"quicbaas.RBAC/DeleteRole": {"roles:delete"},
	"quicbaas.RBAC/ListRoles":  {"roles:list"},
	"quicbaas.RBAC/AssignRole": {"roles:assign"},
	"quicbaas.RBAC/RevokeRole": {"roles:unassign"},

	"quicbaas.Cache/GetCacheStats":     {"admin:status"},
	"quicbaas.Cache/ClearCache":        {"admin:capabilities"},
	"quicbaas.Cache/GetCacheConfig":    {"admin:status"},
	"quicbaas.Cache/UpdateCacheConfig": {"admin:capabilities"},

	"quicbaas.Health/Check":     {"admin:health"},
	"quicbaas.Health/Liveness":  {"admin:health"},
	"quicbaas.Health/Readiness": {"admin:health"},
	"quicbaas.Health/Ping":      {"admin:health"},

	"quicbaas.Secrets/Store":             {"secrets:write"},
	"quicbaas.Secrets/Get":               {"secrets:read"},
	"quicbaas.Secrets/GetData":           {"secrets:read"},
	"quicbaas.Secrets/List":              {"secrets:list"},
	"quicbaas.Secrets/Delete":            {"secrets:delete"},
	"quicbaas.Secrets/Ensure":            {"secrets:read", "secrets:write"},
	"quicbaas.Secrets/Rotate":            {"secrets:write"},
	"quicbaas.Secrets/UpdateStatus":      {"secrets:write"},
	"quicbaas.Secrets/SetRotationPolicy": {"secrets:write"},
	"quicbaas.Secrets/GetRotationPolicy": {"secrets:read"},
	"quicbaas.Secrets/GetVersion":        {"secrets:read"},
	"quicbaas.Secrets/ListVersions":      {"secrets:read"},
	"quicbaas.Secrets/Rollback":          {"secrets:write"},

	"quicbaas.ParameterStore/GetParameter":     {"parameters:read"},
	"quicbaas.ParameterStore/SetParameter":     {"parameters:write"},
	"quicbaas.ParameterStore/DeleteParameter":  {"parameters:delete"},
	"quicbaas.ParameterStore/GetParameters":    {"parameters:read"},
	"quicbaas.ParameterStore/SetParameters":    {"parameters:write"},
	"quicbaas.ParameterStore/DeleteParameters": {"parameters:delete"},
	"quicbaas.ParameterStore/ListParameters":   {"parameters:list"},
	"quicbaas.ParameterStore/SearchByTags":     {"parameters:search"},

	"quicbaas.TimeSeries/Aggregate":          {"kv:range"},
	"quicbaas.TimeSeries/Write":              {"timeseries:write"},
	"quicbaas.TimeSeries/WritePoint":         {"timeseries:write_point"},
	"quicbaas.TimeSeries/WriteBatch":         {"timeseries:write_batch"},
	"quicbaas.TimeSeries/Query":              {"timeseries:query"},
	"quicbaas.TimeSeries/DeleteSeries":       {"timeseries:delete_series"},
	"quicbaas.TimeSeries/GetRetentionPolicy": {"timeseries:get_retention"},
	"quicbaas.TimeSeries/SetRetentionPolicy": {"timeseries:set_retention"},
	"quicbaas.TimeSeries/ListMetrics":        {"timeseries:list_metrics"},

	"quicbaas.Operations/MigrateData":          {"admin:drain"},
	"quicbaas.Operations/GetMigrationProgress": {"admin:status"},
	"quicbaas.Operations/ReplaceNode":          {"admin:add_member"},
	"quicbaas.Operations/UpgradeNode":          {"admin:drain"},
	"quicbaas.Operations/GetUpgradeStatus":     {"admin:status"},

	"quicbaas.Vector/CreateNamespace":       {"vector:namespace_create"},
	"quicbaas.Vector/DeleteNamespace":       {"vector:namespace_delete"},
	"quicbaas.Vector/ListNamespaces":        {"vector:namespace_list"},
	"quicbaas.Vector/GetNamespaceInfo":      {"vector:namespace_info"},
	"quicbaas.Vector/Upsert":                {"vector:upsert"},
	"quicbaas.Vector/BatchUpsert":           {"vector:batch_upsert"},
	"quicbaas.Vector/Get":                   {"vector:get"},
	"quicbaas.Vector/Delete":                {"vector:delete"},
	"quicbaas.Vector/BatchDelete":           {"vector:batch_delete"},
	"quicbaas.Vector/List":                  {"vector:list"},
	"quicbaas.Vector/UpdateMetadata":        {"vector:update_metadata"},
	"quicbaas.Vector/SearchSimilar":         {"vector:search_similar"},
	"quicbaas.Vector/SearchByID":            {"vector:search_by_id"},
	"quicbaas.Vector/HybridSearch":          {"vector:hybrid_search"},
	"quicbaas.Vector/SearchByMetadata":      {"vector:search_by_metadata"},
	"quicbaas.Vector/RebuildIndex":          {"vector:rebuild_index"},
	"quicbaas.Vector/GetIndexProgress":      {"vector:get_rebuild_progress"},
	"quicbaas.Vector/Compact":               {"vector:compact"},
	"quicbaas.Vector/GetCompactionProgress": {"vector:get_compaction_progress"},
	"quicbaas.Vector/GetStats":              {"vector:get_index_stats"},
	"quicbaas.Vector/Export":                {"vector:list"},
	"quicbaas.Vector/Import":                {"vector:batch_upsert"},

	"quicbaas.KMS/GenerateKey":             {"keys:create"},
	"quicbaas.KMS/ImportKey":               {"keys:import"},
	"quicbaas.KMS/GetKey":                  {"keys:get"},
	"quicbaas.KMS/GetPublicKey":            {"keys:get"},
	"quicbaas.KMS/ListKeys":                {"keys:list"},
	"quicbaas.KMS/RotateKey":               {"keys:rotate"},
	"quicbaas.KMS/RevokeKey":               {"keys:delete"},
	"quicbaas.KMS/DeleteKey":               {"keys:delete"},
	"quicbaas.KMS/GetKeyMetadata":          {"keys:get"},
	"quicbaas.KMS/UpdateKeyMetadata":       {"keys:rotate"},
	"quicbaas.KMS/UpdateKeyStatus":         {"keys:rotate"},
	"quicbaas.KMS/ListKeyVersions":         {"keys:list"},
	"quicbaas.KMS/SetKeyPolicy":            {"keys:rotate"},
	"quicbaas.KMS/GetKeyPolicy":            {"keys:get"},
	"quicbaas.KMS/SetKeyRotationPolicy":    {"keys:rotate"},
	"quicbaas.KMS/GetKeyRotationPolicy":    {"keys:get"},
	"quicbaas.KMS/DeleteKeyRotationPolicy": {"keys:delete"},
	"quicbaas.KMS/GetSupportedAlgorithms":  {"keys:list"},
	"quicbaas.KMS/BackendStatus":           {"backends:read"},
	"quicbaas.KMS/BackendTest":             {"backends:read"},
	"quicbaas.KMS/ExportKey":               {"keys:export"},
	"quicbaas.KMS/UnwrapKey":               {"keys:unwrap"},
	"quicbaas.KMS/SetKeySafety":            {"keys:rotate"},
	"quicbaas.KMS/ClearKeySafety":          {"keys:rotate"},
	"quicbaas.KMS/SafetyCheck":             {"keys:get"},
	"quicbaas.KMS/SafetyReport":            {"keys:get"},
	"quicbaas.KMS/CreateDEK":               {"keys:create"},
	"quicbaas.KMS/GetDEK":                  {"keys:get"},
	"quicbaas.KMS/ListDEKs":                {"keys:list"},
	"quicbaas.KMS/RotateDEK":               {"keys:rotate"},
	"quicbaas.KMS/DeleteDEK":               {"keys:delete"},
	"quicbaas.KMS/GetDEKStatus":            {"keys:get"},
	"quicbaas.KMS/SetDefaultDEK":           {"keys:rotate"},
	"quicbaas.KMS/SetDEKSafety":            {"keys:rotate"},
	"quicbaas.KMS/ClearDEKSafety":          {"keys:rotate"},
	"quicbaas.KMS/StartReencryption":       {"keys:rotate"},
	"quicbaas.KMS/GetReencryptionProgress": {"keys:get"},

	"quicbaas.DEK/CreateDEK":               {"keys:create"},
	"quicbaas.DEK/GetDEK":                  {"keys:get"},
	"quicbaas.DEK/ListDEKs":                {"keys:list"},
	"quicbaas.DEK/RotateDEK":               {"keys:rotate"},
	"quicbaas.DEK/DeleteDEK":               {"keys:delete"},
	"quicbaas.DEK/GetDEKStatus":            {"keys:get"},
	"quicbaas.DEK/SetDefaultDEK":           {"keys:rotate"},
	"quicbaas.DEK/SetDEKSafety":            {"keys:rotate"},
	"quicbaas.DEK/ClearDEKSafety":          {"keys:rotate"},
	"quicbaas.DEK/StartReencryption":       {"keys:rotate"},
	"quicbaas.DEK/GetReencryptionProgress": {"keys:get"},

	"quicbaas.Crypto/Encrypt":                {"keys:encrypt"},
	"quicbaas.Crypto/Decrypt":                {"keys:decrypt"},
	"quicbaas.Crypto/Rewrap":                 {"keys:unwrap"},
	"quicbaas.Crypto/EncryptBatch":           {"keys:encrypt"},
	"quicbaas.Crypto/DecryptBatch":           {"keys:decrypt"},
	"quicbaas.Crypto/GenerateDataKey":        {"keys:derive"},
	"quicbaas.Crypto/GenerateWrappedDataKey": {"keys:wrap"},
	"quicbaas.Crypto/Sign":                   {"keys:sign"},
	"quicbaas.Crypto/Verify":                 {"keys:verify"},
	"quicbaas.Crypto/GenerateHMAC":           {"keys:sign"},
	"quicbaas.Crypto/VerifyHMAC":             {"keys:verify"},
	"quicbaas.Crypto/GenerateRandom":         {"keys:derive"},
	"quicbaas.Crypto/Hash":                   {"keys:sign"},

	"quicbaas.Certificate/IssueCertificate":    {"certs:issue"},
	"quicbaas.Certificate/SignCSR":             {"certs:issue"},
	"quicbaas.Certificate/RevokeCertificate":   {"certs:revoke"},
	"quicbaas.Certificate/VerifyCertificate":   {"certs:read"},
	"quicbaas.Certificate/GetCACertificate":    {"certs:read"},
	"quicbaas.Certificate/GetCABundle":         {"certs:read"},
	"quicbaas.Certificate/GetTLSConfig":        {"certs:read"},
	"quicbaas.Certificate/IsInitialized":       {"certs:read"},
	"quicbaas.Certificate/GenerateCRL":         {"certs:revoke"},
	"quicbaas.Certificate/IsRevoked":           {"certs:read"},
	"quicbaas.Certificate/CAInfo":              {"ca:read"},
	"quicbaas.Certificate/ListCertificates":    {"certs:list"},
	"quicbaas.Certificate/CrossSign":           {"ca:create"},
	"quicbaas.Certificate/Chain":               {"certs:enroll"},
	"quicbaas.Certificate/IssueDeviceIdentity": {"certs:issue"},

	"quicbaas.SSH/GenerateKey":         {"keys:create"},
	"quicbaas.SSH/ImportKey":           {"keys:import"},
	"quicbaas.SSH/GetKey":              {"keys:get"},
	"quicbaas.SSH/ListKeys":            {"keys:list"},
	"quicbaas.SSH/DeleteKey":           {"keys:delete"},
	"quicbaas.SSH/ExportPublicKey":     {"keys:get"},
	"quicbaas.SSH/SignDigest":          {"keys:sign"},
	"quicbaas.SSH/AddKnownHost":        {"keys:create"},
	"quicbaas.SSH/ListKnownHosts":      {"keys:list"},
	"quicbaas.SSH/RemoveKnownHost":     {"keys:delete"},
	"quicbaas.SSH/AddAuthorizedKey":    {"keys:create"},
	"quicbaas.SSH/ListAuthorizedKeys":  {"keys:list"},
	"quicbaas.SSH/RemoveAuthorizedKey": {"keys:delete"},
	"quicbaas.SSH/EmitAuthorizedKey":   {"keys:get"},

	"quicbaas.Attestation/Verify":                {"attestation:verify"},
	"quicbaas.Attestation/VerifyChain":           {"attestation:verify"},
	"quicbaas.Attestation/AttestContainer":       {"attestation:attest"},
	"quicbaas.Attestation/PutEndorsement":        {"attestation:provision"},
	"quicbaas.Attestation/ListEndorsements":      {"attestation:verify"},
	"quicbaas.Attestation/DeleteEndorsement":     {"attestation:provision"},
	"quicbaas.Attestation/PutReferenceValues":    {"attestation:provision"},
	"quicbaas.Attestation/GetReferenceValues":    {"attestation:verify"},
	"quicbaas.Attestation/ListReferenceValues":   {"attestation:verify"},
	"quicbaas.Attestation/DeleteReferenceValues": {"attestation:provision"},
	"quicbaas.Attestation/AppraiseQuote":         {"attestation:verify"},

	"quicbaas.DynamicSecrets/CreateRole":         {"dynamic_secrets:role_create"},
	"quicbaas.DynamicSecrets/GetRole":            {"dynamic_secrets:role_read"},
	"quicbaas.DynamicSecrets/ListRoles":          {"dynamic_secrets:role_list"},
	"quicbaas.DynamicSecrets/DeleteRole":         {"dynamic_secrets:role_delete"},
	"quicbaas.DynamicSecrets/GenerateCredential": {"dynamic_secrets:generate"},
	"quicbaas.DynamicSecrets/RenewLease":         {"dynamic_secrets:lease_renew"},
	"quicbaas.DynamicSecrets/RevokeLease":        {"dynamic_secrets:lease_revoke"},
	"quicbaas.DynamicSecrets/ListLeases":         {"dynamic_secrets:lease_list"},

	"quicbaas.Trust/CrossSignedRootPool": {"trust:read"},
	"quicbaas.Trust/AutoImportAIA":       {"trust:write"},
	"quicbaas.Trust/PublishDANE":         {"trust:write"},

	"quicbaas.PIV/ListPIVSlots":         {"piv:read"},
	"quicbaas.PIV/GetPIVCertificate":    {"piv:read"},
	"quicbaas.PIV/StorePIVCertificate":  {"piv:write"},
	"quicbaas.PIV/DeletePIVCertificate": {"piv:delete"},
	"quicbaas.PIV/GeneratePIVKey":       {"piv:write"},
	"quicbaas.PIV/ImportPIVCertificate": {"piv:write"},
	"quicbaas.PIV/ExportPIVCertificate": {"piv:read"},
	"quicbaas.PIV/GeneratePIVCSR":       {"piv:use"},

	"quicbaas.FIDO2/BeginRegistration":    {"user_authfactors:add"},
	"quicbaas.FIDO2/FinishRegistration":   {"user_authfactors:add"},
	"quicbaas.FIDO2/BeginAddCredential":   {"user_authfactors:add"},
	"quicbaas.FIDO2/BeginAuthentication":  {"user_authfactors:login"},
	"quicbaas.FIDO2/FinishAuthentication": {"user_authfactors:login"},

	"quicbaas.ACME/RegisterChallenge": {"acme:write"},
	"quicbaas.ACME/Issue":             {"acme:write"},
	"quicbaas.ACME/NewOrder":          {"acme:write"},
	"quicbaas.ACME/FinalizeOrder":     {"acme:write"},
	"quicbaas.ACME/Renew":             {"acme:write"},

	"quicbaas.Account/Create":           {"agent_accounts:write"},
	"quicbaas.Account/Get":              {"agent_accounts:read"},
	"quicbaas.Account/List":             {"agent_accounts:list"},
	"quicbaas.Account/Update":           {"agent_accounts:write"},
	"quicbaas.Account/Delete":           {"agent_accounts:delete"},
	"quicbaas.Account/RotateCredential": {"agent_accounts:rotate"},
	"quicbaas.Account/Revoke":           {"agent_accounts:write"},
	"quicbaas.Account/SetDefault":       {"agent_accounts:write"},
	"quicbaas.Account/Usage":            {"agent_accounts:read"},
	"quicbaas.Account/Materialize":      {"agent_accounts:materialize"},
	"quicbaas.Account/ReserveSession":   {"agent_accounts:materialize"},
	"quicbaas.Account/ReleaseSession":   {"agent_accounts:materialize"},

	"quicbaas.Orchestrator/Inference":              {"orchestrator:inference"},
	"quicbaas.Orchestrator/SpawnSession":           {"orchestrator:spawn"},
	"quicbaas.Orchestrator/PromptSession":          {"orchestrator:prompt"},
	"quicbaas.Orchestrator/SessionStatus":          {"orchestrator:status"},
	"quicbaas.Orchestrator/CloseSession":           {"orchestrator:close"},
	"quicbaas.Orchestrator/RotateSessionAccount":   {"orchestrator:spawn"},
	"quicbaas.Orchestrator/ListSessions":           {"orchestrator:list"},
	"quicbaas.Orchestrator/GetSessionConversation": {"orchestrator:status"},
	"quicbaas.Orchestrator/GetSpendReport":         {"spend_report:read"},
	"quicbaas.Orchestrator/GetTenantSpendPolicy":   {"orchestrator:status"},
	"quicbaas.Orchestrator/SetTenantSpendPolicy":   {"spend_policy:write"},
	"quicbaas.Orchestrator/CreateTask":             {"orchestrator:task_create"},
	"quicbaas.Orchestrator/GetTask":                {"orchestrator:task_read"},
	"quicbaas.Orchestrator/ListTasks":              {"orchestrator:task_list"},
	"quicbaas.Orchestrator/ClaimTask":              {"orchestrator:task_claim"},
	"quicbaas.Orchestrator/CompleteTask":           {"orchestrator:task_complete"},
	"quicbaas.Orchestrator/BlockTask":              {"orchestrator:task_block"},
	"quicbaas.Orchestrator/ReadmitTask":            {"orchestrator:task_readmit"},
	"quicbaas.Orchestrator/DeleteTask":             {"orchestrator:task_create"},
	"quicbaas.Orchestrator/AttentionList":          {"orchestrator:status"},
	"quicbaas.Orchestrator/AttentionResolve":       {"orchestrator:task_readmit"},
	"quicbaas.Orchestrator/SubmitTaskArtifacts":    {"orchestrator:artifact_submit"},
	"quicbaas.Orchestrator/GetTaskArtifact":        {"orchestrator:artifact_read"},
	"quicbaas.Orchestrator/ListTaskArtifacts":      {"orchestrator:artifact_read"},
	"quicbaas.Orchestrator/RegisterFleetNode":      {"orchestrator:agent_register"},
	"quicbaas.Orchestrator/GetFleetNode":           {"orchestrator:agent_read"},
	"quicbaas.Orchestrator/ListFleetNodes":         {"orchestrator:agent_list"},
	"quicbaas.Orchestrator/RemoveFleetNode":        {"orchestrator:agent_remove"},
	"quicbaas.Orchestrator/FleetNodeHeartbeat":     {"orchestrator:agent_heartbeat"},
	"quicbaas.Orchestrator/StartJob":               {"orchestrator:job_start"},
	"quicbaas.Orchestrator/GetJobStatus":           {"orchestrator:job_read"},
	"quicbaas.Orchestrator/ListJobs":               {"orchestrator:job_list"},
	"quicbaas.Orchestrator/CancelJob":              {"orchestrator:job_cancel"},
	"quicbaas.Orchestrator/SpawnChildJob":          {"orchestrator:job_start"},
	"quicbaas.Orchestrator/Health":                 {"orchestrator:health"},
	"quicbaas.Orchestrator/ListBackends":           {"orchestrator:backend_list"},
	"quicbaas.Orchestrator/ListRunners":            {"orchestrator:status"},
	"quicbaas.Orchestrator/ListWorkflows":          {"orchestrator:workflow_read"},
	"quicbaas.Orchestrator/CreateWorkflow":         {"orchestrator:workflow_write"},
	"quicbaas.Orchestrator/GetWorkflow":            {"orchestrator:workflow_read"},
	"quicbaas.Orchestrator/UpdateWorkflow":         {"orchestrator:workflow_write"},
	"quicbaas.Orchestrator/DeleteWorkflow":         {"orchestrator:workflow_write"},
	"quicbaas.Orchestrator/ListBackendModels":      {"orchestrator:backend_list"},
	"quicbaas.Orchestrator/DecomposeTask":          {"orchestrator:task_decompose"},
	"quicbaas.Orchestrator/GetCostSettings":        {"orchestrator:status"},
	"quicbaas.Orchestrator/SetCostSettings":        {"cost_settings:write"},

	// Scheduler: each entry is the permission the RPC declares in
	// api/proto/scheduler.proto. GetStatus is the gRPC GetSchedulerStatus.
	"quicbaas.Scheduler/CreateJob":      {"scheduler:create"},
	"quicbaas.Scheduler/GetJob":         {"scheduler:read"},
	"quicbaas.Scheduler/UpdateJob":      {"scheduler:update"},
	"quicbaas.Scheduler/DeleteJob":      {"scheduler:delete"},
	"quicbaas.Scheduler/ListJobs":       {"scheduler:list"},
	"quicbaas.Scheduler/TriggerJob":     {"scheduler:trigger"},
	"quicbaas.Scheduler/PauseJob":       {"scheduler:pause"},
	"quicbaas.Scheduler/ResumeJob":      {"scheduler:resume"},
	"quicbaas.Scheduler/CancelJob":      {"scheduler:cancel"},
	"quicbaas.Scheduler/GetJobHistory":  {"scheduler:history"},
	"quicbaas.Scheduler/GetStatus":      {"scheduler:status"},
	"quicbaas.Scheduler/RegisterNode":   {"placement:manage"},
	"quicbaas.Scheduler/RenewNode":      {"placement:manage"},
	"quicbaas.Scheduler/DeregisterNode": {"placement:manage"},
	"quicbaas.Scheduler/CordonNode":     {"placement:manage"},
	"quicbaas.Scheduler/UncordonNode":   {"placement:manage"},
	"quicbaas.Scheduler/DrainNode":      {"placement:manage"},
	"quicbaas.Scheduler/GetNode":        {"placement:read"},
	"quicbaas.Scheduler/ListNodes":      {"placement:read"},
	"quicbaas.Scheduler/GetPlacement":   {"placement:read"},
	"quicbaas.Scheduler/ListPlacements": {"placement:read"},
	"quicbaas.Scheduler/Place":          {"placement:place"},
	"quicbaas.Scheduler/Release":        {"placement:place"},

	"quicbaas.Project/Create":              {"project:create"},
	"quicbaas.Project/Get":                 {"project:read"},
	"quicbaas.Project/GetByName":           {"project:read"},
	"quicbaas.Project/Update":              {"project:update"},
	"quicbaas.Project/Delete":              {"project:delete"},
	"quicbaas.Project/List":                {"project:list"},
	"quicbaas.Project/GrantFleetNodeRole":  {"project:grant"},
	"quicbaas.Project/RevokeFleetNodeRole": {"project:revoke"},

	agentsService + "/ListAgents":  {"agents:read"},
	agentsService + "/GetAgent":    {"agents:read"},
	agentsService + "/CreateAgent": {"agents:write"},
	agentsService + "/UpdateAgent": {"agents:write"},
	agentsService + "/DeleteAgent": {"agents:write"},
	agentsService + "/AgentUsedBy": {"agents:read"},
	agentsService + "/ListPools":   {"agents:read"},
	agentsService + "/GetPool":     {"agents:read"},
	agentsService + "/CreatePool":  {"agents:write"},
	agentsService + "/UpdatePool":  {"agents:write"},
	agentsService + "/DeletePool":  {"agents:write"},
	agentsService + "/PoolUsedBy":  {"agents:read"},

	"quicbaas.SDLC/Bind":               {"sdlc:bind"},
	"quicbaas.SDLC/Unbind":             {"sdlc:unbind"},
	"quicbaas.SDLC/Binding":            {"sdlc:read"},
	"quicbaas.SDLC/Delivery":           {"sdlc:read"},
	"quicbaas.SDLC/ListForges":         {"sdlc:read"},
	"quicbaas.SDLC/Onboard":            {"sdlc:bind"},
	"quicbaas.SDLC/OnboardRepo":        {"sdlc:bind"},
	"quicbaas.SDLC/Conformance":        {"sdlc:read"},
	"quicbaas.SDLC/OpenWork":           {"sdlc:read"},
	"quicbaas.SDLC/Reviewer":           {"sdlc:read"},
	"quicbaas.SDLC/SetReviewer":        {"sdlc:bind"},
	"quicbaas.SDLC/AdoptIssue":         {"orchestrator:task_create"},
	"quicbaas.SDLC/ClearReviewer":      {"sdlc:bind"},
	"quicbaas.SDLC/WorkerImageStatus":  {"worker_images:read"},
	"quicbaas.SDLC/RefreshWorkerImage": {"worker_images:refresh"},
	"quicbaas.SDLC/FileIssue":          {"sdlc:issue_create"},
	"quicbaas.SDLC/Collaborators":      {"sdlc:read"},

	"quicbaas.Prompts/List":      {"prompts:read"},
	"quicbaas.Prompts/Get":       {"prompts:read"},
	"quicbaas.Prompts/Effective": {"prompts:read"},
	"quicbaas.Prompts/Diff":      {"prompts:read"},
	"quicbaas.Prompts/History":   {"prompts:history"},
	"quicbaas.Prompts/Put":       {"prompts:write"},
	"quicbaas.Prompts/Rollback":  {"prompts:write"},
	"quicbaas.Prompts/Reset":     {"prompts:write"},

	registryService + "/Register":   {"service_registry:register"},
	registryService + "/Renew":      {"service_registry:register"},
	registryService + "/Deregister": {"service_registry:register"},
	registryService + "/Resolve":    {"service_registry:read"},

	"quicbaas.ObjectStorage/PutObject":          {"objstore:put"},
	"quicbaas.ObjectStorage/GetObject":          {"objstore:get"},
	"quicbaas.ObjectStorage/DeleteObject":       {"objstore:delete"},
	"quicbaas.ObjectStorage/HeadObject":         {"objstore:head"},
	"quicbaas.ObjectStorage/ListObjects":        {"objstore:list"},
	"quicbaas.ObjectStorage/ListByTag":          {"objstore:list"},
	"quicbaas.ObjectStorage/ListByTenant":       {"objstore:list"},
	"quicbaas.ObjectStorage/CopyObject":         {"objstore:copy"},
	"quicbaas.ObjectStorage/ReplicateObject":    {"objstore:replicate"},
	"quicbaas.ObjectStorage/MigrateObject":      {"objstore:migrate"},
	"quicbaas.ObjectStorage/ArchiveObject":      {"objstore:archive"},
	"quicbaas.ObjectStorage/RestoreObject":      {"objstore:restore"},
	"quicbaas.ObjectStorage/VerifyObject":       {"objstore:verify"},
	"quicbaas.ObjectStorage/SetObjectPolicy":    {"objstore:write_policy"},
	"quicbaas.ObjectStorage/GetObjectPolicy":    {"objstore:read_policy"},
	"quicbaas.ObjectStorage/ListBackends":       {"objstore:read_backend"},
	"quicbaas.ObjectStorage/GetBackendHealth":   {"objstore:read_backend"},
	"quicbaas.ObjectStorage/GetTenantUsage":     {"objstore:tenant_usage"},
	"quicbaas.ObjectStorage/GetTenantQuotas":    {"objstore:tenant_quota_read"},
	"quicbaas.ObjectStorage/SetTenantQuotas":    {"objstore:tenant_quota_write"},
	"quicbaas.ObjectStorage/BatchPutObjects":    {"objstore:batch"},
	"quicbaas.ObjectStorage/BatchGetObjects":    {"objstore:batch"},
	"quicbaas.ObjectStorage/BatchDeleteObjects": {"objstore:batch"},

	"quicbaas.Messaging/CreateChannel":   {"messaging:channel_create"},
	"quicbaas.Messaging/DeleteChannel":   {"messaging:channel_delete"},
	"quicbaas.Messaging/GetChannel":      {"messaging:channel_read"},
	"quicbaas.Messaging/ListChannels":    {"messaging:channel_list"},
	"quicbaas.Messaging/Subscribe":       {"messaging:subscribe"},
	"quicbaas.Messaging/Unsubscribe":     {"messaging:unsubscribe"},
	"quicbaas.Messaging/Publish":         {"messaging:publish"},
	"quicbaas.Messaging/PublishDelta":    {"messaging:publish"},
	"quicbaas.Messaging/WatchChannel":    {"messaging:watch"},
	"quicbaas.Messaging/GetStats":        {"messaging:stats"},
	"quicbaas.Messaging/GetDedupStats":   {"messaging:stats"},
	"quicbaas.Messaging/GetChannelStats": {"messaging:stats"},

	"quicbaas.KV/Watch": {"watch:watch"},

	"quicbaas.Query/Query":     {"kv:range"},
	"quicbaas.Query/ScanIndex": {"index:scan"},

	"quicbaas.Orchestrator/SubscribeJobEvents": {"orchestrator:job_read"},
	"quicbaas.Orchestrator/InferenceStream":    {"orchestrator:inference"},

	registryService + "/Watch": {"service_registry:read"},

	"quicbaas.IAM.Federation/CreateProvider": {"tenants:create"},
	"quicbaas.IAM.Federation/GetProvider":    {"tenants:read"},
	"quicbaas.IAM.Federation/UpdateProvider": {"tenants:create"},
	"quicbaas.IAM.Federation/DeleteProvider": {"tenants:delete"},
	"quicbaas.IAM.Federation/ListProviders":  {"tenants:read"},

	"quicbaas.IAM.Audit/Query": {"audit:query"},
	"quicbaas.IAM.Audit/Get":   {"audit:read"},

	"quicbaas.IAM.Tenant/Create":    {"tenants:create"},
	"quicbaas.IAM.Tenant/Get":       {"tenants:read"},
	"quicbaas.IAM.Tenant/Update":    {"tenants:update"},
	"quicbaas.IAM.Tenant/Delete":    {"tenants:delete"},
	"quicbaas.IAM.Tenant/List":      {"tenants:list"},
	"quicbaas.IAM.Tenant/Provision": {"tenants:create"},

	"quicbaas.IAM.User/Create":         {"users:create"},
	"quicbaas.IAM.User/Get":            {"users:read"},
	"quicbaas.IAM.User/Update":         {"users:update"},
	"quicbaas.IAM.User/Delete":         {"users:delete"},
	"quicbaas.IAM.User/List":           {"users:list"},
	"quicbaas.IAM.User/GetByEmail":     {"users:read"},
	"quicbaas.IAM.User/FindByUsername": {"users:read"},
	"quicbaas.IAM.User/AssignRole":     {"roles:assign"},
	"quicbaas.IAM.User/RevokeRole":     {"roles:unassign"},
	"quicbaas.IAM.User/Approve":        {"users:enable"},

	"quicbaas.IAM.Role/Create":    {"roles:create"},
	"quicbaas.IAM.Role/Get":       {"roles:read"},
	"quicbaas.IAM.Role/Update":    {"roles:update"},
	"quicbaas.IAM.Role/Delete":    {"roles:delete"},
	"quicbaas.IAM.Role/List":      {"roles:list"},
	"quicbaas.IAM.Role/Reconcile": {"roles:create"},

	"quicbaas.IAM.Session/Get":          {"users:read"},
	"quicbaas.IAM.Session/Touch":        {"users:update"},
	"quicbaas.IAM.Session/List":         {"users:read"},
	"quicbaas.IAM.Session/Revoke":       {"users:update"},
	"quicbaas.IAM.Session/RevokeAll":    {"users:update"},
	"quicbaas.IAM.Session/RevokeOthers": {"users:update"},
	"quicbaas.IAM.Session/RevokeOther":  {"users:update"},

	"quicbaas.IAM.WebAuthn/ListWebAuthnCredentials":   {"users:read"},
	"quicbaas.IAM.WebAuthn/DeleteWebAuthnCredential":  {"users:delete"},
	"quicbaas.IAM.WebAuthn/DeleteWebAuthnCredentials": {"users:delete"},
}

// streamOperationPermissions overrides operationPermissions for a stream
// whose dispatch key a unary handler shares while serving another go-qrdb
// verb. The unary TimeSeries/Query is TSQuery (timeseries:query); the stream
// is TimeSeriesQuery, which scans entities (kv:range).
var streamOperationPermissions = map[string][]string{
	"quicbaas.TimeSeries/Query": {"kv:range"},
}

// streamPermissions returns the permissions a stream at key declares.
func streamPermissions(key string) []string {
	if perms, ok := streamOperationPermissions[key]; ok {
		return perms
	}
	return operationPermissions[key]
}
