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
	"errors"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Barrier Service — facade over go-qrdb's BarrierClient.
// =============================================================================
//
// BarrierServicer mirrors go-qrdb's pkg/service.BarrierServicer surface and
// re-exports the same operations through the QuicBaaS service layer. The
// implementation is a thin facade that delegates to the qrdb SDK
// transport.BarrierClient — QuicBaaS does not own the storage barrier
// itself, so this layer exists purely so the QuicBaaS REST/gRPC handlers
// have a stable, package-local target type.
//
// Lifecycle wiring (Phase 11) instantiates a BarrierService against the
// qrdb SDK client used by the rest of the daemon and threads it through
// the service facade. Phase 9 only delivers the facade and REST shim; the
// service is constructed lazily and may be nil in handlers that pre-date
// the wiring.
// =============================================================================

// BarrierServicer exposes barrier lifecycle operations through the QuicBaaS
// service facade. All methods delegate to a transport.BarrierClient at the
// SDK layer; semantics match go-qrdb exactly.
type BarrierServicer interface {
	// BarrierStatus returns the current barrier state.
	BarrierStatus(ctx context.Context) (*BarrierStatusInfo, error)

	// BarrierInitialize initialises the barrier with a single passphrase. The
	// caller owns secret and may wipe it as soon as the call returns; nothing
	// below this seam retains the slice.
	BarrierInitialize(ctx context.Context, secret []byte) error

	// BarrierUnseal unseals the barrier with a single passphrase. The caller
	// owns secret and may wipe it as soon as the call returns.
	BarrierUnseal(ctx context.Context, secret []byte) error

	// BarrierSeal transitions the barrier to the sealed state. When a PIN
	// backend is configured and an SO PIN is set on it, req must carry that
	// SO PIN; otherwise the seal is refused and the barrier keeps its state.
	BarrierSeal(ctx context.Context, req *BarrierSealRequest) error

	// BarrierClusterUnseal reports the cluster-wide unseal state.
	BarrierClusterUnseal(ctx context.Context) (*transport.ClusterSeal, error)

	// BarrierInitializeShamir is refused on this release with
	// sdkerrors.SystemShamirInitRefusedError; the initialization ceremony
	// bootstraps the system Shamir barrier.
	BarrierInitializeShamir(ctx context.Context, req *transport.BarrierInitializeShamirRequest) (*transport.BarrierInitializeShamirResponse, error)

	// BarrierGenerateRecoveryKeys splits recovery shares to the registered
	// recovery officers.
	BarrierGenerateRecoveryKeys(ctx context.Context, req *transport.BarrierGenerateRecoveryKeysRequest) (*transport.BarrierRecoveryKeysResponse, error)

	// BarrierDeleteRecoveryKeys removes recovery key metadata.
	BarrierDeleteRecoveryKeys(ctx context.Context) error

	// BarrierHasRecoveryKeys reports whether recovery key metadata exists.
	BarrierHasRecoveryKeys(ctx context.Context) (bool, error)

	// OfficerRegister enrols an unseal officer.
	OfficerRegister(ctx context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error)

	// OfficerList lists the enrolled officers.
	OfficerList(ctx context.Context) ([]*transport.OfficerStatus, error)

	// ShareClaimBegin starts an officer's unseal-share claim.
	ShareClaimBegin(ctx context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error)

	// ShareClaimComplete finishes an officer's unseal-share claim.
	ShareClaimComplete(ctx context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error)

	// RecoveryOfficerRegister enrols a recovery officer.
	RecoveryOfficerRegister(ctx context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error)

	// RecoveryShareClaimBegin starts a recovery officer's share claim.
	RecoveryShareClaimBegin(ctx context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error)

	// RecoveryShareClaimComplete finishes a recovery officer's share claim.
	RecoveryShareClaimComplete(ctx context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error)

	// BarrierQuorumBegin issues an officer's quorum challenge for an
	// operation (unseal, seal, root-token, rekey, recover).
	BarrierQuorumBegin(ctx context.Context, req *transport.QuorumBeginRequest) (*transport.QuorumChallenge, error)

	// BarrierQuorumSubmit submits an officer's answered challenge and share;
	// the quorum's last submission runs the operation.
	BarrierQuorumSubmit(ctx context.Context, req *transport.QuorumSubmitRequest) (*transport.QuorumOutcome, error)

	// BarrierRekeyStart opens a rekey quorum round.
	BarrierRekeyStart(ctx context.Context, req *transport.RekeyStartRequest) (*transport.QuorumRound, error)

	// BarrierOfficerRevoke opens a quorum round that revokes an officer.
	BarrierOfficerRevoke(ctx context.Context, req *transport.OfficerRevokeRequest) (*transport.QuorumRound, error)

	// EnableEncryption migrates a barrier from none-strategy to a keyed
	// strategy in place. An empty tenantID targets the system barrier;
	// a non-empty tenantID targets that tenant's dedicated barrier.
	// Requires an injected EncryptionMigrator (SetMigrator); returns
	// BarrierServiceMissingError when the migrator is nil.
	EnableEncryption(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error
}

// BarrierStatusInfo describes the current state of the storage barrier.
// Mirrors go-qrdb's service.BarrierStatusInfo so the REST handlers can
// shape responses identically across both daemons.
type BarrierStatusInfo struct {
	// Sealed is true when the barrier is in sealed state.
	Sealed bool

	// Strategy identifies which sealing strategy protects the root key.
	Strategy string

	// HardwareBacked is true when the active strategy uses dedicated
	// cryptographic hardware.
	HardwareBacked bool

	// InitializedAt records when the barrier was first initialised.
	InitializedAt time.Time
}

// BarrierServiceMissingError signals that the barrier facade was constructed
// without an underlying go-qrdb BarrierClient. The error is returned from
// every BarrierService method so handlers can report 503 Service Unavailable
// rather than nil-panic.
type BarrierServiceMissingError struct{}

// Error returns the error message.
func (e *BarrierServiceMissingError) Error() string {
	return "barrier service not configured"
}

// BarrierService implements BarrierServicer by delegating to a
// transport.BarrierClient from the go-qrdb SDK. The client field is allowed
// to be nil during early boot; every method short-circuits with
// BarrierServiceMissingError when the underlying transport is missing.
type BarrierService struct {
	// client is the qrdb SDK BarrierClient. May be nil before lifecycle
	// wiring completes.
	client transport.BarrierClient

	// migrator is the EncryptionMigrator used by EnableEncryption. Must be
	// the embedded *qrdbnode.Node (not the BarrierClient transport, which
	// stubs BarrierEnableEncryption). Wired via SetMigrator after node init.
	migrator qrdbsdk.EncryptionMigrator

	// pinBackend is the PIN backend the embedded qrdb node verifies SO PINs
	// against (the one wired through qrdb's WithPINBackend). BarrierSeal
	// checks the caller's SO PIN against it. Nil leaves seal PIN-less.
	pinBackend qrdbsdk.PINBackend
}

// BarrierSealRequest carries the Security Officer PIN that authorises a
// seal. The caller owns SOPIN and may wipe it as soon as BarrierSeal
// returns; nothing below this seam retains the slice.
type BarrierSealRequest struct {
	// SOPIN is the Security Officer PIN. It is required when a PIN backend
	// is configured and an SO PIN has been set on it.
	SOPIN []byte
}

// Compile-time interface check.
var _ BarrierServicer = (*BarrierService)(nil)

// NewBarrierService constructs a BarrierService backed by the supplied
// qrdb BarrierClient. Passing nil yields a service whose methods all
// return BarrierServiceMissingError; callers may therefore install the
// facade unconditionally and let lifecycle wiring populate the client
// once the qrdb SDK is online.
func NewBarrierService(client transport.BarrierClient) *BarrierService {
	return &BarrierService{client: client}
}

// BarrierStatus returns the current barrier status.
func (s *BarrierService) BarrierStatus(ctx context.Context) (*BarrierStatusInfo, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierStatus(ctx)
	if err != nil {
		return nil, err
	}
	info := &BarrierStatusInfo{
		Sealed:         resp.Sealed,
		Strategy:       resp.Strategy,
		HardwareBacked: resp.HardwareBacked,
	}
	if resp.InitializedAt != "" {
		// The qrdb wire format is RFC3339; parse defensively.
		if t, parseErr := time.Parse(time.RFC3339, resp.InitializedAt); parseErr == nil {
			info.InitializedAt = t
		}
	}
	return info, nil
}

// BarrierInitialize initialises the barrier with a passphrase. For
// hardware-backed and cloud KMS strategies the daemon auto-initialises the
// barrier on first boot, so a subsequent explicit init call from the CLI is
// idempotent — ErrAlreadyInitialized is treated as success rather than
// surfaced as an error to the caller.
func (s *BarrierService) BarrierInitialize(ctx context.Context, secret []byte) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	if err := s.client.BarrierInitialize(ctx, &transport.BarrierInitializeRequest{Secret: secret}); err != nil {
		if isAlreadyInitializedError(err) {
			return nil
		}
		return err
	}
	return nil
}

// isAlreadyInitializedError reports whether err indicates a barrier that was
// already initialized. It covers both the direct error value (embedded
// transport) and gRPC status errors that wrap the message (network
// transports).
func isAlreadyInitializedError(err error) bool {
	if errors.Is(err, qrdbsdk.ErrAlreadyInitialized) {
		return true
	}
	// gRPC transport wraps the error in a status; check the message.
	return err != nil && len(err.Error()) > 0 &&
		containsAlreadyInitializedMsg(err.Error())
}

// containsAlreadyInitializedMsg returns true when msg contains the canonical
// qrdb "barrier already initialized" diagnostic. The substr is stable; the
// seal package documents it as part of ErrAlreadyInitialized.
func containsAlreadyInitializedMsg(msg string) bool {
	const marker = "barrier already initialized"
	for i := 0; i+len(marker) <= len(msg); i++ {
		if msg[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}

// BarrierUnseal unseals the barrier with a passphrase.
func (s *BarrierService) BarrierUnseal(ctx context.Context, secret []byte) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierUnseal(ctx, &transport.BarrierUnsealRequest{Secret: secret})
}

// BarrierSeal transitions the barrier to the sealed state.
func (s *BarrierService) BarrierSeal(ctx context.Context, req *BarrierSealRequest) error {
	if err := s.verifySealSOPIN(req); err != nil {
		return err
	}
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierSeal(ctx)
}

// barrier returns the qrdb BarrierClient, or BarrierServiceMissingError
// before lifecycle wiring installs one.
func (s *BarrierService) barrier() (transport.BarrierClient, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	return s.client, nil
}

// BarrierClusterUnseal reports the cluster-wide unseal state.
func (s *BarrierService) BarrierClusterUnseal(ctx context.Context) (*transport.ClusterSeal, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.BarrierClusterUnseal(ctx)
}

// verifySealSOPIN checks the seal request's SO PIN against the configured
// PIN backend through VerifyBarrierSealSOPIN.
func (s *BarrierService) verifySealSOPIN(req *BarrierSealRequest) error {
	var soPIN []byte
	if req != nil {
		soPIN = req.SOPIN
	}
	return VerifyBarrierSealSOPIN(s.pinBackend, soPIN)
}

// VerifyBarrierSealSOPIN checks soPIN against backend before a seal. It
// mirrors qrdb's own seal gate: with no backend, or a backend on which no
// SO PIN has been set yet, there is nothing to verify. A refusal is an
// ErrForbidden QBError wrapping the backend's own error
// (qrdbsdk.ErrSOPINRequired for a missing PIN), so errors.Is still finds
// it. The BarrierService facade (REST, MCP, QUIC, quicbaas.v1 gRPC) seals
// through qrdb's in-process client, which checks no PIN, so this is its
// gate. The canonical qrdb.v1 BarrierSeal needs none here: qrdb's own
// handler verifies the PIN (pin.RequireSOPIN) before sealing.
func VerifyBarrierSealSOPIN(backend qrdbsdk.PINBackend, soPIN []byte) error {
	if backend == nil || !backend.SOPINSet() {
		return nil
	}
	if len(soPIN) == 0 {
		return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "barrier.Seal", Err: qrdbsdk.ErrSOPINRequired}
	}
	if err := backend.VerifySOPIN(string(soPIN)); err != nil {
		return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "barrier.Seal", Err: err}
	}
	return nil
}

// BarrierInitializeShamir refuses with a PreconditionFailed QBError wrapping
// sdkerrors.SystemShamirInitRefusedError: the initialization ceremony is the
// only bootstrap path for the system Shamir barrier.
func (s *BarrierService) BarrierInitializeShamir(_ context.Context, _ *transport.BarrierInitializeShamirRequest) (*transport.BarrierInitializeShamirResponse, error) {
	// The system Shamir barrier is created only by the initialization
	// ceremony. The verb stays on the surface and refuses with one typed
	// error, whether or not a share distributor is configured.
	return nil, &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   "barrier.InitializeShamir",
		Err:  &sdkerrors.SystemShamirInitRefusedError{},
	}
}

// BarrierGenerateRecoveryKeys splits recovery shares to the recovery officers.
func (s *BarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, req *transport.BarrierGenerateRecoveryKeysRequest) (*transport.BarrierRecoveryKeysResponse, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.BarrierGenerateRecoveryKeys(ctx, req)
}

// BarrierDeleteRecoveryKeys removes recovery key metadata.
func (s *BarrierService) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	c, err := s.barrier()
	if err != nil {
		return err
	}
	return c.BarrierDeleteRecoveryKeys(ctx)
}

// BarrierHasRecoveryKeys reports whether recovery key metadata exists.
func (s *BarrierService) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	c, err := s.barrier()
	if err != nil {
		return false, err
	}
	resp, err := c.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		return false, err
	}
	return resp.HasKeys, nil
}

// OfficerRegister enrols an unseal officer.
func (s *BarrierService) OfficerRegister(ctx context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.OfficerRegister(ctx, req)
}

// OfficerList lists the enrolled officers.
func (s *BarrierService) OfficerList(ctx context.Context) ([]*transport.OfficerStatus, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.OfficerList(ctx)
}

// ShareClaimBegin starts an officer's unseal-share claim.
func (s *BarrierService) ShareClaimBegin(ctx context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.ShareClaimBegin(ctx, req)
}

// ShareClaimComplete finishes an officer's unseal-share claim.
func (s *BarrierService) ShareClaimComplete(ctx context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.ShareClaimComplete(ctx, req)
}

// RecoveryOfficerRegister enrols a recovery officer.
func (s *BarrierService) RecoveryOfficerRegister(ctx context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.RecoveryOfficerRegister(ctx, req)
}

// RecoveryShareClaimBegin starts a recovery officer's share claim.
func (s *BarrierService) RecoveryShareClaimBegin(ctx context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.RecoveryShareClaimBegin(ctx, req)
}

// RecoveryShareClaimComplete finishes a recovery officer's share claim.
func (s *BarrierService) RecoveryShareClaimComplete(ctx context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.RecoveryShareClaimComplete(ctx, req)
}

// BarrierQuorumBegin issues an officer's quorum challenge.
func (s *BarrierService) BarrierQuorumBegin(ctx context.Context, req *transport.QuorumBeginRequest) (*transport.QuorumChallenge, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.BarrierQuorumBegin(ctx, req)
}

// BarrierQuorumSubmit submits an officer's answered challenge and share.
func (s *BarrierService) BarrierQuorumSubmit(ctx context.Context, req *transport.QuorumSubmitRequest) (*transport.QuorumOutcome, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.BarrierQuorumSubmit(ctx, req)
}

// BarrierRekeyStart opens a rekey quorum round.
func (s *BarrierService) BarrierRekeyStart(ctx context.Context, req *transport.RekeyStartRequest) (*transport.QuorumRound, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.BarrierRekeyStart(ctx, req)
}

// BarrierOfficerRevoke opens a quorum round that revokes an officer.
func (s *BarrierService) BarrierOfficerRevoke(ctx context.Context, req *transport.OfficerRevokeRequest) (*transport.QuorumRound, error) {
	c, err := s.barrier()
	if err != nil {
		return nil, err
	}
	return c.BarrierOfficerRevoke(ctx, req)
}

// SetMigrator installs the EncryptionMigrator used by EnableEncryption.
// Pass the embedded *qrdbnode.Node — NOT the BarrierClient transport, which
// stubs BarrierEnableEncryption with NotImplementedError. Safe to call
// concurrently with request handling.
func (s *BarrierService) SetMigrator(m qrdbsdk.EncryptionMigrator) {
	s.migrator = m
}

// SetPINBackend injects the PIN backend BarrierSeal verifies the SO PIN
// against. Pass the backend the embedded qrdb node was given through
// WithPINBackend so seal and the qrdb ceremony check one PIN.
func (s *BarrierService) SetPINBackend(b qrdbsdk.PINBackend) {
	s.pinBackend = b
}

// EnableEncryption migrates the target barrier from none-strategy to a keyed
// strategy in place. An empty tenantID targets the system barrier; a non-empty
// tenantID targets that tenant's dedicated barrier. Requires SetMigrator to
// have been called with the embedded qrdb node.
func (s *BarrierService) EnableEncryption(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
	if s.migrator == nil {
		return &dberrors.QBError{
			Code: dberrors.ErrUnavailable,
			Op:   "barrier.EnableEncryption",
			Err:  &BarrierServiceMissingError{},
		}
	}
	scope := qrdbsdk.BarrierScope{Kind: qrdbsdk.ScopeSystem}
	if tenantID != "" {
		// Tenant-scoped enable-encryption is a destructive, irreversible barrier
		// migration. Like every other per-tenant barrier op, it must be bound to
		// the caller's own tenant: a tenant principal may not migrate another
		// tenant's barrier even when holding tenant_barrier:enable_encryption.
		// The system scope (empty tenantID) is admin-gated by its own permission
		// and carries no per-tenant binding. This shared check covers all
		// transports (REST/gRPC/QUIC/MCP) that delegate here.
		if err := enforceTenantBinding(ctx, "barrier.enable_encryption", tenantID); err != nil {
			return err
		}
		scope = qrdbsdk.BarrierScope{Kind: qrdbsdk.ScopeTenant, TenantID: tenantID}
	}
	return s.migrator.EnableEncryption(ctx, scope, opts)
}
