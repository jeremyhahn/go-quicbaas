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

	// BarrierInitialize initialises the barrier with a single passphrase.
	BarrierInitialize(ctx context.Context, secret string) error

	// BarrierUnseal unseals the barrier with a single passphrase.
	BarrierUnseal(ctx context.Context, secret string) error

	// BarrierSeal transitions the barrier to the sealed state.
	BarrierSeal(ctx context.Context) error

	// BarrierInitializeShamir initialises the barrier with Shamir secret
	// sharing and returns the generated shares.
	BarrierInitializeShamir(ctx context.Context, secret string, threshold, total int) (*ShamirInitInfo, error)

	// BarrierUnsealWithShare submits a single Shamir share toward the unseal
	// quorum.
	BarrierUnsealWithShare(ctx context.Context, share string) (*UnsealShareProgress, error)

	// BarrierUnsealWithShares unseals the barrier using a complete set of
	// Shamir shares in a single call.
	BarrierUnsealWithShares(ctx context.Context, shares []string) error

	// BarrierRekey generates new Shamir shares for the existing root key.
	BarrierRekey(ctx context.Context, threshold, total int) (*ShamirInitInfo, error)

	// BarrierGenerateRecoveryKeys generates recovery shares for disaster
	// recovery.
	BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*RecoveryKeysInfo, error)

	// BarrierRecoverWithKeys unseals the barrier using recovery shares.
	BarrierRecoverWithKeys(ctx context.Context, keys []string) error

	// BarrierDeleteRecoveryKeys removes recovery key metadata.
	BarrierDeleteRecoveryKeys(ctx context.Context) error

	// BarrierHasRecoveryKeys reports whether recovery key metadata exists.
	BarrierHasRecoveryKeys(ctx context.Context) (bool, error)

	// BarrierGenerateRootToken creates a new root token by proving knowledge
	// of the master key through Shamir shares.
	BarrierGenerateRootToken(ctx context.Context, shares []string) (*RootTokenInfo, error)

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

// ShamirInitInfo contains the output of a Shamir initialisation or rekey.
// Share material is never present here; Security Officers retrieve their
// shares via the qrdb share-claim flow using their mTLS certificate.
type ShamirInitInfo struct {
	// Threshold is the minimum number of shares required for reconstruction.
	Threshold int

	// TotalShares is the total number of shares generated.
	TotalShares int

	// ShareCount is the number of shares successfully sealed to officer enclaves.
	ShareCount int
}

// UnsealShareProgress reports the current state of quorum-based unsealing.
type UnsealShareProgress struct {
	// Required is the minimum number of shares needed to unseal.
	Required int

	// Submitted is the number of shares provided so far.
	Submitted int

	// Complete is true when the quorum threshold has been met.
	Complete bool
}

// RecoveryKeysInfo contains the output of recovery key generation. Recovery
// share material is never present here; recovery officers retrieve their
// shares via the qrdb recovery share-claim flow.
type RecoveryKeysInfo struct {
	// Threshold is the minimum number of keys required for recovery.
	Threshold int

	// Total is the total number of keys generated.
	Total int

	// ShareCount is the number of recovery shares successfully sealed to officer enclaves.
	ShareCount int
}

// RootTokenInfo represents a generated root token.
type RootTokenInfo struct {
	// Token is the hex-encoded root token value.
	Token string

	// CreatedAt records when the token was generated.
	CreatedAt time.Time
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
func (s *BarrierService) BarrierInitialize(ctx context.Context, secret string) error {
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
func (s *BarrierService) BarrierUnseal(ctx context.Context, secret string) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierUnseal(ctx, &transport.BarrierUnsealRequest{Secret: secret})
}

// BarrierSeal transitions the barrier to the sealed state.
func (s *BarrierService) BarrierSeal(ctx context.Context) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierSeal(ctx)
}

// BarrierInitializeShamir initialises the barrier with Shamir secret sharing.
func (s *BarrierService) BarrierInitializeShamir(ctx context.Context, secret string, threshold, total int) (*ShamirInitInfo, error) {
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
	return &ShamirInitInfo{
		Threshold:   resp.Threshold,
		TotalShares: resp.TotalShares,
		ShareCount:  resp.ShareCount,
	}, nil
}

// BarrierUnsealWithShare submits a single Shamir share.
func (s *BarrierService) BarrierUnsealWithShare(ctx context.Context, share string) (*UnsealShareProgress, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierUnsealWithShare(ctx, &transport.BarrierUnsealShareRequest{Share: share})
	if err != nil {
		return nil, err
	}
	return &UnsealShareProgress{
		Required:  resp.Required,
		Submitted: resp.Submitted,
		Complete:  resp.Complete,
	}, nil
}

// BarrierUnsealWithShares unseals the barrier using a complete set of shares.
func (s *BarrierService) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	if s.client == nil {
		return &BarrierServiceMissingError{}
	}
	return s.client.BarrierUnsealWithShares(ctx, &transport.BarrierUnsealSharesRequest{Shares: shares})
}

// BarrierRekey generates new Shamir shares for the existing root key.
func (s *BarrierService) BarrierRekey(ctx context.Context, threshold, total int) (*ShamirInitInfo, error) {
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
	return &ShamirInitInfo{
		Threshold:   resp.Threshold,
		TotalShares: resp.TotalShares,
		ShareCount:  resp.ShareCount,
	}, nil
}

// BarrierGenerateRecoveryKeys generates recovery shares.
func (s *BarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*RecoveryKeysInfo, error) {
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
	return &RecoveryKeysInfo{
		Threshold:  resp.Threshold,
		Total:      resp.Total,
		ShareCount: resp.ShareCount,
	}, nil
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
func (s *BarrierService) BarrierGenerateRootToken(ctx context.Context, shares []string) (*RootTokenInfo, error) {
	if s.client == nil {
		return nil, &BarrierServiceMissingError{}
	}
	resp, err := s.client.BarrierGenerateRootToken(ctx, &transport.BarrierGenerateRootTokenRequest{Shares: shares})
	if err != nil {
		return nil, err
	}
	info := &RootTokenInfo{Token: resp.Token}
	if resp.CreatedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, resp.CreatedAt); parseErr == nil {
			info.CreatedAt = t
		}
	}
	return info, nil
}

// SetMigrator installs the EncryptionMigrator used by EnableEncryption.
// Pass the embedded *qrdbnode.Node — NOT the BarrierClient transport, which
// stubs BarrierEnableEncryption with NotImplementedError. Safe to call
// concurrently with request handling.
func (s *BarrierService) SetMigrator(m qrdbsdk.EncryptionMigrator) {
	s.migrator = m
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
