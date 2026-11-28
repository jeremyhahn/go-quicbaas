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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
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
	BarrierStatus(ctx context.Context) (*transport.BarrierStatusResponse, error)

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

	// BarrierInitializeShamir initialises the barrier with Shamir secret
	// sharing and returns the generated shares.
	BarrierInitializeShamir(ctx context.Context, secret []byte, threshold, total int) (*transport.BarrierInitializeShamirResponse, error)

	// BarrierUnsealWithShare submits a single Shamir share toward the unseal
	// quorum.
	BarrierUnsealWithShare(ctx context.Context, share string) (*transport.BarrierUnsealShareResponse, error)

	// BarrierUnsealWithShares unseals the barrier using a complete set of
	// Shamir shares in a single call.
	BarrierUnsealWithShares(ctx context.Context, shares []string) error

	// BarrierRekey generates new Shamir shares for the existing root key.
	BarrierRekey(ctx context.Context, threshold, total int) (*transport.BarrierRekeyResponse, error)

	// BarrierGenerateRecoveryKeys generates recovery shares for disaster
	// recovery.
	BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*transport.BarrierRecoveryKeysResponse, error)

	// BarrierRecoverWithKeys unseals the barrier using recovery shares.
	BarrierRecoverWithKeys(ctx context.Context, keys []string) error

	// BarrierDeleteRecoveryKeys removes recovery key metadata.
	BarrierDeleteRecoveryKeys(ctx context.Context) error

	// BarrierHasRecoveryKeys reports whether recovery key metadata exists.
	BarrierHasRecoveryKeys(ctx context.Context) (bool, error)

	// BarrierGenerateRootToken creates a new root token by proving knowledge
	// of the master key through Shamir shares.
	BarrierGenerateRootToken(ctx context.Context, shares []string) (*transport.BarrierRootTokenResponse, error)

	// EnableEncryption migrates a barrier from none-strategy to a keyed
	// strategy in place. An empty tenantID targets the system barrier;
	// a non-empty tenantID targets that tenant's dedicated barrier.
	// Requires an injected EncryptionMigrator (SetMigrator); returns
	// BarrierServiceMissingError when the migrator is nil.
	EnableEncryption(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error
}

// The facade has no result types of its own (#1559): every verb answers with
// go-qrdb's transport response, unchanged, so each transport shapes the same
// fields go-qrdb serves and none is dropped or reformatted on the way.

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
func (s *BarrierService) BarrierStatus(ctx context.Context) (*transport.BarrierStatusResponse, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierStatus(ctx)
	if err != nil {
		return nil, err
	}
	return resp, nil
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
// it. Every seal route verifies through this one function: the
// BarrierService facade (REST, MCP, quicbaas.v1 gRPC) and the interceptor
// guarding the canonical qrdb.v1 BarrierSeal on the gRPC and Unix servers.
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

// BarrierInitializeShamir initialises the barrier with Shamir secret sharing.
func (s *BarrierService) BarrierInitializeShamir(ctx context.Context, secret []byte, threshold, total int) (*transport.BarrierInitializeShamirResponse, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierInitializeShamir(ctx, &transport.BarrierInitializeShamirRequest{
		Secret:      secret,
		Threshold:   threshold,
		TotalShares: total,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// BarrierUnsealWithShare submits a single Shamir share.
func (s *BarrierService) BarrierUnsealWithShare(ctx context.Context, share string) (*transport.BarrierUnsealShareResponse, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierUnsealWithShare(ctx, &transport.BarrierUnsealShareRequest{Share: share})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// BarrierUnsealWithShares unseals the barrier using a complete set of shares.
func (s *BarrierService) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierUnsealWithShares(ctx, &transport.BarrierUnsealSharesRequest{Shares: shares})
}

// BarrierRekey generates new Shamir shares for the existing root key.
func (s *BarrierService) BarrierRekey(ctx context.Context, threshold, total int) (*transport.BarrierRekeyResponse, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierRekey(ctx, &transport.BarrierRekeyRequest{
		Threshold: threshold,
		Total:     total,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// BarrierGenerateRecoveryKeys generates recovery shares.
func (s *BarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*transport.BarrierRecoveryKeysResponse, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierGenerateRecoveryKeys(ctx, &transport.BarrierGenerateRecoveryKeysRequest{
		Threshold: threshold,
		Total:     total,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// BarrierRecoverWithKeys unseals the barrier using recovery shares.
func (s *BarrierService) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierRecoverWithKeys(ctx, &transport.BarrierRecoverWithKeysRequest{Keys: keys})
}

// BarrierDeleteRecoveryKeys removes recovery key metadata.
func (s *BarrierService) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierDeleteRecoveryKeys(ctx)
}

// BarrierHasRecoveryKeys reports whether recovery key metadata exists.
func (s *BarrierService) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	if s.client == nil {
		return false, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		return false, err
	}
	return resp.HasKeys, nil
}

// BarrierGenerateRootToken creates a new root token from Shamir shares.
func (s *BarrierService) BarrierGenerateRootToken(ctx context.Context, shares []string) (*transport.BarrierRootTokenResponse, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierGenerateRootToken(ctx, &transport.BarrierGenerateRootTokenRequest{Shares: shares})
	if err != nil {
		return nil, err
	}
	return resp, nil
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
