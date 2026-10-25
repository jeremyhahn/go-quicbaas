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

package server

import (
	"errors"
	"strconv"
	"strings"
)

// Server package errors.
var (
	// ErrUnknownQuorumDomain indicates a barrier quorum verb named a key
	// domain other than unseal or recovery.
	ErrUnknownQuorumDomain = errors.New("unknown barrier quorum domain")

	// ErrDataDirRequired indicates a data directory path is required but was not provided.
	ErrDataDirRequired = errors.New("data directory is required")

	// ErrEmbeddingNotCompiled is returned when embedding operations are attempted
	// but the binary was not compiled with the vector build tag.
	ErrEmbeddingNotCompiled = errors.New("embedding support not compiled - build with -tags 'vector qrdb_vector'")

	// ErrRAGNotCompiled is returned when RAG operations are attempted
	// but the binary was not compiled with the rag build tag.
	ErrRAGNotCompiled = errors.New("rag support not compiled - build with -tags rag")

	// ErrServerNotRunning indicates an operation was attempted on a stopped server.
	ErrServerNotRunning = errors.New("server is not running")

	// ErrServerAlreadyRunning indicates the server is already running.
	ErrServerAlreadyRunning = errors.New("server is already running")

	// ErrInvalidServerMode indicates an invalid server mode was specified.
	ErrInvalidServerMode = errors.New("invalid server mode")

	// ErrMCPShutdownFailed indicates the MCP server failed to shut down gracefully.
	ErrMCPShutdownFailed = errors.New("failed to shutdown MCP server")

	// ErrRAGCloseFailed indicates the RAG system failed to close gracefully.
	ErrRAGCloseFailed = errors.New("failed to close RAG system")

	// ErrShardedClientClosed indicates ForShard was called after Close.
	ErrShardedClientClosed = errors.New("sharded client is closed")

	// ErrUnsupportedTransport indicates the configured qrdb transport type is not recognized.
	ErrUnsupportedTransport = errors.New("unsupported qrdb transport type")

	// ErrTransportCreationFailed indicates the qrdb SDK transport could not be created.
	ErrTransportCreationFailed = errors.New("qrdb transport creation failed")

	// ErrNetworkTransportMissingAddress indicates a network transport was configured without an address.
	ErrNetworkTransportMissingAddress = errors.New("address is required for network qrdb transport")

	// ErrUnixTransportMissingSocket indicates a unix transport was configured without a socket path.
	ErrUnixTransportMissingSocket = errors.New("unix socket path is required for unix qrdb transport")

	// ErrQRDBClientNotInitialized indicates GetKVStore was called before the
	// qrdb SDK client was initialized. This is a programming error: the SDK
	// must be initialized during server startup before any KVStore access.
	ErrQRDBClientNotInitialized = errors.New("qrdb SDK client not initialized")

	// ErrShardClientFailed indicates ForShard failed to return a client for
	// the requested shard ID.
	ErrShardClientFailed = errors.New("failed to get qrdb client for shard")

	// ErrIAMComponentInit indicates an IAM component failed to initialize.
	ErrIAMComponentInit = errors.New("IAM component initialization failed")

	// ErrQRDBUserStorageRequired indicates IAM initialization was attempted with
	// a qrdb client present but no embedded qrdb node. User persistence is owned
	// by the embedded node, so IAM cannot fail open to a local store — this is a
	// fail-closed startup invariant.
	ErrQRDBUserStorageRequired = errors.New("qrdb node required for IAM user storage")

	// ErrUpdateCANotReady is returned by UpdateCA when the server has not yet
	// started or when TLS is disabled (no CA adapter was wired at startup).
	ErrUpdateCANotReady = errors.New("server: CA adapter not ready — server may not be started or TLS is disabled")

	// ErrServerCertChainCAMissing indicates the CA returned no certificate
	// to chain the issued server TLS certificate to.
	ErrServerCertChainCAMissing = errors.New("server: CA certificate unavailable for the server TLS chain")

	// ErrServerCertPath is returned when a configured server TLS cert or key
	// path is unset or absent.
	ErrServerCertPath = errors.New("server: configured TLS cert/key path refused")

	// ErrNoEmbeddedNode is returned by DelegatedContext on a server with no
	// embedded qrdb node: a delegation needs the node to verify it.
	ErrNoEmbeddedNode = errors.New("server: no embedded qrdb node to verify the delegation")
)

// ServerCauseError wraps a server operation error with an underlying cause.
type ServerCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ServerCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *ServerCauseError) Unwrap() error {
	return e.Cause
}

// ServerMessageError provides a simple typed error message for the server package.
type ServerMessageError struct {
	Message string
}

// Error implements the error interface.
func (e *ServerMessageError) Error() string {
	return e.Message
}

// NilLoginIdentityError is returned by the OIDC session issuer when the login
// ceremony attempts to issue a bearer for a nil resolved identity.
type NilLoginIdentityError struct{}

// Error implements the error interface.
func (e *NilLoginIdentityError) Error() string {
	return "quicbaas/server: cannot issue session token for nil login identity"
}

// LoginCeremonyUnavailableError is returned by the OIDC session issuer when a
// login ceremony operation runs before the OIDC provider has been bound (i.e.
// before IAM initialization completes). It fails closed rather than minting an
// unsigned or anonymous bearer.
type LoginCeremonyUnavailableError struct{}

// Error implements the error interface.
func (e *LoginCeremonyUnavailableError) Error() string {
	return "quicbaas/server: login ceremony unavailable (OIDC provider not yet bound)"
}

// EmptyLoginTenantError is returned by the tenant-aware WebAuthn login
// verifier when a challenge or verification is attempted without a tenant id.
// WebAuthn proofs MUST be verified against the exact login tenant's
// relying-party configuration, so an empty tenant fails closed rather than
// falling back to a default tenant (which would break tenant isolation).
type EmptyLoginTenantError struct {
	// Op names the verifier operation that rejected the empty tenant
	// ("BeginChallenge" or "VerifyTenant").
	Op string
}

// Error implements the error interface.
func (e *EmptyLoginTenantError) Error() string {
	if e == nil || e.Op == "" {
		return "quicbaas/server: login tenant id is required for webauthn verification"
	}
	return "quicbaas/server: login tenant id is required for webauthn " + e.Op
}

// UnboundWebAuthnFactoryError is returned by the tenant-aware WebAuthn login
// verifier when a challenge or verification runs before the per-tenant
// WebAuthn service factory has been bound (i.e. before IAM initialization
// completes). It fails closed rather than verifying against no service.
type UnboundWebAuthnFactoryError struct{}

// Error implements the error interface.
func (e *UnboundWebAuthnFactoryError) Error() string {
	return "quicbaas/server: webauthn login verifier unavailable (per-tenant service factory not yet bound)"
}

// NonTenantWebAuthnVerifyError is returned by the tenant-aware WebAuthn login
// verifier's non-tenant Verify entry point. WebAuthn verification requires the
// login tenant's relying-party service, so the verifier refuses to verify on
// the tenant-less path and directs callers to the tenant-scoped path
// (VerifyTenant). It never silently verifies against a default tenant.
type NonTenantWebAuthnVerifyError struct{}

// Error implements the error interface.
func (e *NonTenantWebAuthnVerifyError) Error() string {
	return "quicbaas/server: webauthn login verification requires a tenant; use the tenant-scoped login ceremony"
}

// UnboundPIVVerifierError is returned by the PIV login verifier when a
// verification runs before the xkms PIV verifier has been built from the CA
// trust roots (i.e. before the CA is initialized or IAM init completes). It
// fails closed rather than verifying against absent trust roots.
type UnboundPIVVerifierError struct{}

// Error implements the error interface.
func (e *UnboundPIVVerifierError) Error() string {
	return "quicbaas/server: piv login verifier unavailable (trust roots not yet bound)"
}

// ShardedClientClosedError is returned when ForShard is called after Close.
type ShardedClientClosedError struct{}

// Error implements the error interface.
func (e *ShardedClientClosedError) Error() string {
	return "sharded client is closed"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ShardedClientClosedError) Unwrap() error {
	return ErrShardedClientClosed
}

// UnsupportedTransportError is returned when the configured qrdb transport
// type is not a recognized transport name.
type UnsupportedTransportError struct {
	Transport string
}

// Error implements the error interface.
func (e *UnsupportedTransportError) Error() string {
	return "unsupported qrdb transport type: " + e.Transport
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnsupportedTransportError) Unwrap() error {
	return ErrUnsupportedTransport
}

// TransportCreationError is returned when a qrdb SDK transport fails to initialize.
type TransportCreationError struct {
	Transport string
	Cause     error
}

// Error implements the error interface.
func (e *TransportCreationError) Error() string {
	return "qrdb " + e.Transport + " transport creation failed: " + e.Cause.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TransportCreationError) Unwrap() error {
	return ErrTransportCreationFailed
}

// NetworkTransportMissingAddressError is returned when a network-based
// transport (gRPC, REST, QUIC, MCP) is configured without a target address.
type NetworkTransportMissingAddressError struct {
	Transport string
}

// Error implements the error interface.
func (e *NetworkTransportMissingAddressError) Error() string {
	return "address is required for " + e.Transport + " qrdb transport"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NetworkTransportMissingAddressError) Unwrap() error {
	return ErrNetworkTransportMissingAddress
}

// UnixTransportMissingSocketError is returned when the unix transport is
// configured without a socket path.
type UnixTransportMissingSocketError struct{}

// Error implements the error interface.
func (e *UnixTransportMissingSocketError) Error() string {
	return "unix socket path is required for unix qrdb transport"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnixTransportMissingSocketError) Unwrap() error {
	return ErrUnixTransportMissingSocket
}

// QRDBClientNotInitializedError is returned (as a panic) when GetKVStore
// is called before the qrdb SDK client has been initialized. This is a
// programming error indicating incorrect server startup sequencing.
type QRDBClientNotInitializedError struct{}

// Error implements the error interface.
func (e *QRDBClientNotInitializedError) Error() string {
	return "GetKVStore called before qrdb SDK client initialized"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *QRDBClientNotInitializedError) Unwrap() error {
	return ErrQRDBClientNotInitialized
}

// ShardClientError is returned (as a panic) when ForShard fails to return
// a client for the requested shard ID.
type ShardClientError struct {
	ShardID uint64
	Cause   error
}

// Error implements the error interface.
func (e *ShardClientError) Error() string {
	return "failed to get qrdb client for shard: " + e.Cause.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ShardClientError) Unwrap() error {
	return ErrShardClientFailed
}

// StorageInitError indicates a failure during storage initialization.
type StorageInitError struct {
	Backend string
	Err     error
}

// Error implements the error interface.
func (e *StorageInitError) Error() string {
	return "storage init (" + e.Backend + "): " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *StorageInitError) Unwrap() error {
	return e.Err
}

// DatabaseStartError indicates a failure during database subsystem initialization.
type DatabaseStartError struct {
	Err error
}

// Error implements the error interface.
func (e *DatabaseStartError) Error() string {
	return "database subsystem init: " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *DatabaseStartError) Unwrap() error {
	return e.Err
}

// IAMComponentInitError indicates an IAM component failed during initialization.
type IAMComponentInitError struct {
	Component string
	Cause     error
}

// Error implements the error interface.
func (e *IAMComponentInitError) Error() string {
	return "failed to create " + e.Component + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *IAMComponentInitError) Unwrap() error {
	return ErrIAMComponentInit
}

// QRDBUserStorageRequiredError is returned when IAM initialization runs with a
// qrdb client configured but no embedded qrdb node available. The embedded node
// is the single source of truth for user records, so IAM fails closed rather
// than silently falling back to a local (empty) store.
type QRDBUserStorageRequiredError struct{}

// Error implements the error interface.
func (e *QRDBUserStorageRequiredError) Error() string {
	return "IAM user storage requires an embedded qrdb node, but none is available"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *QRDBUserStorageRequiredError) Unwrap() error {
	return ErrQRDBUserStorageRequired
}

// ErrFirstBootBootstrap is returned when the first-boot admin bootstrap fails.
var ErrFirstBootBootstrap = errors.New("first-boot bootstrap failed")

// ErrAdminRoleMissing is returned by reconcileAdminRole when qrdb's canonical
// admin role is absent from the system tenant after first-boot. qrdb's
// first-boot is the sole creator of the built-in admin role; this error means
// first-boot was skipped or the role store was wiped, and a fresh first-boot is
// required before admin authorization can resolve.
var ErrAdminRoleMissing = errors.New("canonical admin role not found in system tenant")

// TenantProvisionUnavailableError reports that a seam tenant provisioning
// composes is not wired on this server, so nothing was written.
type TenantProvisionUnavailableError struct {
	// Seam names the missing collaborator.
	Seam string
}

// Error implements the error interface.
func (e *TenantProvisionUnavailableError) Error() string {
	return "tenant provisioning: " + e.Seam + " not available"
}

// TenantProvisionError wraps a failure from qrdb's ProvisionTenant.
type TenantProvisionError struct {
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *TenantProvisionError) Error() string {
	return "tenant provisioning: tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *TenantProvisionError) Unwrap() error { return e.Cause }

// TenantProvisionInviteError reports that the tenant and its pending admin
// were provisioned but the activation invite could not be minted. Nothing is
// rolled back: provisioning is idempotent, so re-running it resolves the same
// pending admin and mints a fresh invite.
type TenantProvisionInviteError struct {
	TenantID    string
	AdminUserID uint64
	Cause       error
}

// Error implements the error interface.
func (e *TenantProvisionInviteError) Error() string {
	return "tenant provisioning: tenant " + e.TenantID + " admin " + strconv.FormatUint(e.AdminUserID, 10) +
		" is provisioned but its invite was not minted (re-run to retry): " + e.Cause.Error()
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *TenantProvisionInviteError) Unwrap() error { return e.Cause }

// TenantProvisionLoginFactorError reports that the admin certificate was
// issued and bound but could not be enrolled as the admin's mtls login factor,
// so it cannot log in. The certificate is withheld and what the call created
// (the admin, its binding, and the tenant if it was new) is deleted again, so
// re-running provisioning starts over and issues a certificate that logs in.
type TenantProvisionLoginFactorError struct {
	TenantID    string
	AdminUserID uint64
	Cause       error
}

// Error implements the error interface.
func (e *TenantProvisionLoginFactorError) Error() string {
	return "tenant provisioning: tenant " + e.TenantID + " admin " + strconv.FormatUint(e.AdminUserID, 10) +
		" certificate was not enrolled as a login factor and was rolled back (re-run to retry): " + e.Cause.Error()
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *TenantProvisionLoginFactorError) Unwrap() error { return e.Cause }

// TenantProvisionRollbackError reports that a cert-mode call failed after it
// had created the tenant admin (the certificate could not be signed, bound or
// enrolled as a login factor) and that deleting what the call created then
// failed too. The admin is left without a usable login credential; a re-run
// reports it as TenantAdminLoginCredentialMissingError. AdminUserID is empty
// when the admin could not even be looked up.
type TenantProvisionRollbackError struct {
	TenantID    string
	AdminUserID uint64
	// Cause is the provisioning failure.
	Cause error
	// RollbackErr is the failure to delete what the call created.
	RollbackErr error
}

// Error implements the error interface.
func (e *TenantProvisionRollbackError) Error() string {
	return "tenant provisioning: tenant " + e.TenantID + " admin " + strconv.FormatUint(e.AdminUserID, 10) +
		" was not given a login credential (" + e.Cause.Error() +
		") and the rollback failed: " + e.RollbackErr.Error()
}

// Unwrap exposes both failures for errors.Is / errors.As.
func (e *TenantProvisionRollbackError) Unwrap() []error { return []error{e.Cause, e.RollbackErr} }

// TenantAdminLoginCredentialMissingError reports that a re-run found the
// tenant's admin already provisioned but holding no active login factor, so
// no one can log in as it. Provisioning cannot repair it without rotating the
// admin's credential: the operator deletes the admin user (or the tenant) and
// re-runs provisioning.
type TenantAdminLoginCredentialMissingError struct {
	TenantID      string
	AdminUserID   uint64
	AdminUsername string
}

// Error implements the error interface.
func (e *TenantAdminLoginCredentialMissingError) Error() string {
	return "tenant provisioning: tenant " + e.TenantID + " admin " + e.AdminUsername + " (" + strconv.FormatUint(e.AdminUserID, 10) +
		") exists without a login credential; delete the admin user or the tenant and re-run provisioning"
}

// TenantAdminAccountMismatchError reports that a re-run found an account under
// the admin username that is not the tenant's administrator: it does not hold
// the tenant-admin role, or it cannot authenticate (pending, disabled,
// suspended, locked). Provisioning does not repair it; the operator deletes or
// fixes the account and re-runs provisioning.
type TenantAdminAccountMismatchError struct {
	TenantID      string
	AdminUserID   uint64
	AdminUsername string
	// Lifecycle is the account's lifecycle state.
	Lifecycle string
	// HasAdminRole reports whether the account holds the tenant-admin role.
	HasAdminRole bool
}

// Error implements the error interface.
func (e *TenantAdminAccountMismatchError) Error() string {
	return "tenant provisioning: tenant " + e.TenantID + " account " + e.AdminUsername + " (" + strconv.FormatUint(e.AdminUserID, 10) +
		") is not an active tenant administrator (lifecycle " + strconv.Quote(e.Lifecycle) +
		", admin role " + strconv.FormatBool(e.HasAdminRole) + "); delete or fix the account and re-run provisioning"
}

// ErrPromptReconcile is returned when the boot-time prompt-set reconcile
// fails. It is the sentinel PromptReconcileError unwraps to.
var ErrPromptReconcile = errors.New("prompt set reconciliation failed")

// PromptReconcileError names the tenant whose prompt set could not be
// reconciled. An empty TenantID means the failure happened while enumerating
// the tenants, before any one of them was reached.
type PromptReconcileError struct {
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *PromptReconcileError) Error() string {
	if e.TenantID == "" {
		return "prompt set reconciliation: enumerate tenants: " + e.Cause.Error()
	}
	return "prompt set reconciliation: tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *PromptReconcileError) Unwrap() []error {
	return []error{ErrPromptReconcile, e.Cause}
}

// FirstBootBootstrapError captures the operation that failed during the
// first-boot admin bootstrap flow.
type FirstBootBootstrapError struct {
	Op    string
	Cause error
}

// Error implements the error interface.
func (e *FirstBootBootstrapError) Error() string {
	if e.Cause == nil {
		return "first-boot bootstrap: " + e.Op
	}
	return "first-boot bootstrap: " + e.Op + ": " + e.Cause.Error()
}

// Unwrap returns BOTH the sentinel and the cause.
//
// It used to return the sentinel alone, which silently swallowed the cause:
// errors.Is(err, ErrNoSPIFFETrustDomain) could never resolve through this
// wrapper even though the message printed the trust-domain text, so the failure
// was legible to a human and invisible to a program. Returning a slice keeps
// every existing errors.Is(err, ErrFirstBootBootstrap) working and makes the
// specific cause reachable as well.
func (e *FirstBootBootstrapError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrFirstBootBootstrap}
	}
	return []error{ErrFirstBootBootstrap, e.Cause}
}

// NoAdministratorError reports that first-boot could not create an
// administrator, so the server has nobody who can administer it. It is what
// initializeIAMComponents returns when firstBootBootstrap fails, and it refuses
// the boot: attachIAMComponents aborts startup on any IAM initialization error.
//
// The message is assembled from a fixed preamble, the WRAPPED cause, and a
// fixed closing line. The cause carries its own remedy — see
// ErrNoSPIFFETrustDomain for the shape — because each cause needs a different
// one and a hardcoded example would be wrong for every other cause.
//
// The closing line tells the operator that restarting resumes rather than
// corrupts, and it is there because an operator who believes a failed boot
// poisoned their state will wipe it — worse than the misconfiguration they
// started with. It holds because the super-admin grant is written strictly
// LAST: a bootstrap that failed anywhere earlier left no grant behind, so the
// next boot re-enters the (idempotent) sequence.
//
// It is WITHHELD for the one cause it would be false for. A bundle that was
// never delivered (ErrAdminBundleNotDelivered) is not resumable by restarting:
// the certificate is already bound, qrdb's idempotent first-boot will not
// reissue it, so every restart reaches the same refusal. That cause states its
// own terminal remedy, and appending "will resume from where it stopped" after
// it told the operator to wipe and re-bootstrap gave them two contradictory
// instructions in one sentence with the wrong one last — an operator following
// it restarts forever.
type NoAdministratorError struct {
	Cause error
}

// Error implements the error interface.
func (e *NoAdministratorError) Error() string {
	msg := "server refuses to start: first-boot created no administrator, so nobody can administer this server"
	if e.Cause != nil {
		msg += ". cause: " + e.Cause.Error()
		if errors.Is(e.Cause, ErrAdminBundleNotDelivered) {
			// The cause owns the remedy here, and it is not "restart". Saying
			// otherwise would contradict it — see the type comment.
			return msg
		}
	}
	return msg + ". Fix the cause and start again — first-boot has not completed and will resume from where it stopped."
}

// Unwrap exposes the cause so errors.Is and errors.As reach the underlying
// failure. Without it the refusal would be a dead end: a program could tell
// that the boot failed but not why, which is the whole reason the cause is
// carried rather than formatted into a string.
func (e *NoAdministratorError) Unwrap() error { return e.Cause }

// ErrAdminBundleNotDelivered is the sentinel AdminBundleNotDeliveredError
// unwraps to.
var ErrAdminBundleNotDelivered = errors.New("first-boot admin bundle was not delivered")

// AdminBundleNotDeliveredError reports a bootstrap that reached bundle delivery
// with a certificate authority configured and delivered nothing.
//
// This is the RE-ENTRY case. qrdb's first-boot is idempotent and returns no
// certificate once the admin user exists, so a second pass has nothing to write
// and the operator would be left without the private key for a certificate that
// is already bound to them. Reporting it is what keeps the re-entry from
// completing into a server whose administrator holds an unusable credential —
// the same fail-open this refusal exists to remove, reached by a longer route.
//
// A CA-less deployment is a different case and a supported one: there was never
// a certificate to deliver, the banner tells the operator how to finish
// enrollment, and that IS its completed state. Hence the caller's CA check
// rather than a bare empty-path check.
type AdminBundleNotDeliveredError struct{}

// Error implements the error interface.
func (e *AdminBundleNotDeliveredError) Error() string {
	return "first-boot: the admin bundle was not delivered, so the operator has " +
		"no private key for the bound certificate; reissue-on-re-entry is not " +
		"implemented — wipe and re-bootstrap"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AdminBundleNotDeliveredError) Unwrap() error {
	return ErrAdminBundleNotDelivered
}

// UpdateCANotReadyError is returned by Server.UpdateCA when the CA adapter
// was not initialized. This occurs when the server has not yet been started
// or when TLS is disabled so no adapter was wired during startService().
type UpdateCANotReadyError struct{}

// Error implements the error interface.
func (e UpdateCANotReadyError) Error() string {
	return "server: CA adapter not ready — server may not be started or TLS is disabled"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e UpdateCANotReadyError) Unwrap() error {
	return ErrUpdateCANotReady
}

// RaftMTLSLoadError indicates a failure loading PEM certificate material
// from the Raft TLS file paths for inter-node mTLS.
type RaftMTLSLoadError struct {
	File  string
	Cause error
}

// Error implements the error interface.
func (e *RaftMTLSLoadError) Error() string {
	if e.Cause == nil {
		return "raft mtls: failed to load file " + e.File
	}
	return "raft mtls: failed to load file " + e.File + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *RaftMTLSLoadError) Unwrap() error { return e.Cause }

// ClusterMembersError indicates a failure building the cluster members map
// from the configured peer list.
type ClusterMembersError struct {
	Cause error
}

// Error implements the error interface.
func (e *ClusterMembersError) Error() string {
	if e.Cause == nil {
		return "cluster members: invalid peer configuration"
	}
	return "cluster members: invalid peer configuration: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *ClusterMembersError) Unwrap() error { return e.Cause }

// ClusterCAPassphraseError indicates the cluster CA key passphrase could not
// be moved into protected memory. The cluster CA does not run without it
// rather than running with passphrase protection silently off.
type ClusterCAPassphraseError struct {
	Cause error
}

// Error implements the error interface.
func (e *ClusterCAPassphraseError) Error() string {
	if e.Cause == nil {
		return "cluster ca: passphrase could not be protected"
	}
	return "cluster ca: passphrase could not be protected: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *ClusterCAPassphraseError) Unwrap() error { return e.Cause }

// ServerCertPathProblem says why a configured server TLS path is refused.
type ServerCertPathProblem string

const (
	// ServerCertPathUnset: only one of security.tls_cert_file and
	// security.tls_key_file is configured.
	ServerCertPathUnset ServerCertPathProblem = "unset"
	// ServerCertPathAbsent: a configured path has no file behind it.
	ServerCertPathAbsent ServerCertPathProblem = "absent"
)

// ServerCertPathError refuses a boot whose configured server TLS paths
// cannot be served. The server self-issues only when neither path is
// configured, so it never writes a certificate to a configured path that
// holds none. Keys names the config keys that are unset or absent.
type ServerCertPathError struct {
	CertFile string
	KeyFile  string
	Problem  ServerCertPathProblem
	Keys     []string
}

// Error implements the error interface.
func (e *ServerCertPathError) Error() string {
	return "server TLS: " + strings.Join(e.Keys, " and ") + " " + string(e.Problem) +
		" (security.tls_cert_file=" + strconv.Quote(e.CertFile) +
		", security.tls_key_file=" + strconv.Quote(e.KeyFile) +
		"); configure both files, or neither to let the server issue its own"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ServerCertPathError) Unwrap() error { return ErrServerCertPath }
