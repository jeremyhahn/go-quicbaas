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
	"context"
	"errors"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// qrdb gRPC barrier servicer — data-plane client adapter
// =============================================================================
//
// The qrdb.v1.QRDBService gRPC barrier handlers expect a
// qrdbsdk.BarrierServicer. go-qrdb's only concrete implementation
// (qrdbsdk.NewBarrierService) wraps a *seal.StorageBarrier — the live
// barrier object — which quicbaasd does not hold: quicbaasd composes go-qrdb
// as a client, reaching the data-plane barrier through the qrdb SDK
// transport.BarrierClient bound to the platform shard.
//
// This adapter bridges that gap. It implements qrdbsdk.BarrierServicer by
// delegating every operation to a transport.BarrierClient — the EXACT same
// backend that the MCP, QUIC, and REST barrier servers use (they consume the
// quicbaas service.BarrierService facade over the same client). Routing the
// gRPC and Unix transports through this adapter guarantees all five transports
// operate the single platform-shard data-plane barrier, with no split-brain
// state.
//
// The adapter carries no barrier state of its own; it is a pure protocol
// shim. Wire responses (RFC3339 timestamp strings) are converted to the
// service-layer types (time.Time) the qrdb gRPC handlers expect.
// =============================================================================

// qrdbBarrierClientServicer adapts a qrdb SDK transport.BarrierClient to the
// qrdbsdk.BarrierServicer interface consumed by qrdb's gRPC barrier
// handlers. The client reaches the platform-shard data-plane barrier, the same
// instance the other transports operate, so barrier state stays consistent
// across gRPC, Unix, MCP, QUIC, and REST.
type qrdbBarrierClientServicer struct {
	client transport.BarrierClient
}

// Compile-time interface check.
var _ qrdbsdk.BarrierServicer = (*qrdbBarrierClientServicer)(nil)

// newQRDBBarrierClientServicer wraps the supplied qrdb BarrierClient in a
// qrdbsdk.BarrierServicer. The client MUST be non-nil; callers gate
// construction on a live platform-shard client so the qrdb gRPC barrier RPCs
// fall back to codes.Unimplemented only when no client is wired at all.
func newQRDBBarrierClientServicer(client transport.BarrierClient) *qrdbBarrierClientServicer {
	return &qrdbBarrierClientServicer{client: client}
}

// BarrierInitialize initializes the data-plane barrier with a single secret.
// For hardware-backed and cloud KMS strategies the daemon auto-initialises the
// barrier on first boot, so a subsequent explicit init call from the CLI is
// idempotent — ErrAlreadyInitialized is treated as success.
func (s *qrdbBarrierClientServicer) BarrierInitialize(ctx context.Context, secret []byte) error {
	if err := s.client.BarrierInitialize(ctx, &transport.BarrierInitializeRequest{Secret: secret}); err != nil {
		if errors.Is(err, qrdbsdk.ErrAlreadyInitialized) {
			return nil
		}
		return err
	}
	return nil
}

// BarrierUnseal unseals the data-plane barrier with a single secret.
func (s *qrdbBarrierClientServicer) BarrierUnseal(ctx context.Context, secret []byte) error {
	return s.client.BarrierUnseal(ctx, &transport.BarrierUnsealRequest{Secret: secret})
}

// BarrierSeal transitions the data-plane barrier to the sealed state.
func (s *qrdbBarrierClientServicer) BarrierSeal(ctx context.Context) error {
	return s.client.BarrierSeal(ctx)
}

// BarrierStatus returns the current data-plane barrier status, parsing the
// RFC3339 wire timestamp into the time.Time the qrdb gRPC handler re-encodes.
func (s *qrdbBarrierClientServicer) BarrierStatus(ctx context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	resp, err := s.client.BarrierStatus(ctx)
	if err != nil {
		return nil, err
	}
	info := &qrdbsdk.BarrierStatusInfo{
		Sealed:          resp.Sealed,
		Strategy:        resp.Strategy,
		HardwareBacked:  resp.HardwareBacked,
		ShamirThreshold: resp.ShamirThreshold,
		ShamirTotal:     resp.ShamirTotal,
	}
	if resp.InitializedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, resp.InitializedAt); parseErr == nil {
			info.InitializedAt = t
		} else if t, parseErr := time.Parse(time.RFC3339Nano, resp.InitializedAt); parseErr == nil {
			info.InitializedAt = t
		}
	}
	return info, nil
}

// BarrierInitializeShamir initializes the data-plane barrier with Shamir
// secret sharing. Share material is never returned; Security Officers claim
// their shares via the qrdb share-claim flow.
func (s *qrdbBarrierClientServicer) BarrierInitializeShamir(ctx context.Context, secret []byte, threshold, totalShares int) (*qrdbsdk.ShamirInitInfo, error) {
	resp, err := s.client.BarrierInitializeShamir(ctx, &transport.BarrierInitializeShamirRequest{
		Secret:      secret,
		Threshold:   threshold,
		TotalShares: totalShares,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.ShamirInitInfo{
		Threshold:   resp.Threshold,
		TotalShares: resp.TotalShares,
		ShareCount:  resp.ShareCount,
	}, nil
}

// BarrierUnsealWithShare submits a single Shamir share toward the quorum.
func (s *qrdbBarrierClientServicer) BarrierUnsealWithShare(ctx context.Context, share string) (*qrdbsdk.UnsealShareProgress, error) {
	resp, err := s.client.BarrierUnsealWithShare(ctx, &transport.BarrierUnsealShareRequest{Share: share})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.UnsealShareProgress{
		Required:  resp.Required,
		Submitted: resp.Submitted,
		Complete:  resp.Complete,
	}, nil
}

// BarrierUnsealWithShares unseals the data-plane barrier with a complete set
// of Shamir shares in a single call.
func (s *qrdbBarrierClientServicer) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	return s.client.BarrierUnsealWithShares(ctx, &transport.BarrierUnsealSharesRequest{Shares: shares})
}

// BarrierRekey generates new Shamir shares for the existing root key.
func (s *qrdbBarrierClientServicer) BarrierRekey(ctx context.Context, threshold, total int) (*qrdbsdk.ShamirInitInfo, error) {
	resp, err := s.client.BarrierRekey(ctx, &transport.BarrierRekeyRequest{
		Threshold: threshold,
		Total:     total,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.ShamirInitInfo{
		Threshold:   resp.Threshold,
		TotalShares: resp.TotalShares,
		ShareCount:  resp.ShareCount,
	}, nil
}

// BarrierGenerateRecoveryKeys generates recovery shares for disaster recovery.
func (s *qrdbBarrierClientServicer) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*qrdbsdk.RecoveryKeysInfo, error) {
	resp, err := s.client.BarrierGenerateRecoveryKeys(ctx, &transport.BarrierGenerateRecoveryKeysRequest{
		Threshold: threshold,
		Total:     total,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.RecoveryKeysInfo{
		Threshold:  resp.Threshold,
		Total:      resp.Total,
		ShareCount: resp.ShareCount,
	}, nil
}

// BarrierRecoverWithKeys unseals the data-plane barrier using recovery shares.
func (s *qrdbBarrierClientServicer) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	return s.client.BarrierRecoverWithKeys(ctx, &transport.BarrierRecoverWithKeysRequest{Keys: keys})
}

// BarrierDeleteRecoveryKeys removes recovery key metadata.
func (s *qrdbBarrierClientServicer) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	return s.client.BarrierDeleteRecoveryKeys(ctx)
}

// BarrierHasRecoveryKeys reports whether recovery key metadata exists.
func (s *qrdbBarrierClientServicer) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	resp, err := s.client.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		return false, err
	}
	return resp.HasKeys, nil
}

// BarrierOfficerRegister registers a Security Officer for the system barrier
// on the data-plane node. The signed certificate in the result must be
// delivered to the officer out of band.
func (s *qrdbBarrierClientServicer) BarrierOfficerRegister(ctx context.Context, req *qrdbsdk.BarrierOfficerRegisterRequest) (*qrdbsdk.BarrierOfficerRegisterResult, error) {
	resp, err := s.client.OfficerRegister(ctx, &transport.OfficerRegisterRequest{
		Username:          req.Username,
		CSRPEM:            req.CSRPEM,
		AuthenticatorType: req.AuthenticatorType,
		AuthenticatorRef:  req.AuthenticatorRef,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.BarrierOfficerRegisterResult{
		CertPEM:    resp.CertPEM,
		ShareIndex: resp.ShareIndex,
	}, nil
}

// BarrierOfficerList returns all enrolled Security Officers for the
// data-plane system barrier. Only non-secret metadata is carried.
func (s *qrdbBarrierClientServicer) BarrierOfficerList(ctx context.Context) ([]*qrdbsdk.BarrierOfficerStatus, error) {
	officers, err := s.client.OfficerList(ctx)
	if err != nil {
		return nil, err
	}
	return officerStatusesToService(officers), nil
}

// BarrierOfficerClaimStatus returns a per-officer claim status snapshot for
// the data-plane system barrier. No share material is present.
func (s *qrdbBarrierClientServicer) BarrierOfficerClaimStatus(ctx context.Context) ([]*qrdbsdk.BarrierOfficerStatus, error) {
	officers, err := s.client.OfficerClaimStatus(ctx)
	if err != nil {
		return nil, err
	}
	return officerStatusesToService(officers), nil
}

// BarrierShareClaimBegin issues a challenge nonce to the named Security
// Officer on the data-plane node. The callerCertThumbprint argument is not
// forwarded on the wire: the qrdb node re-derives the caller identity from
// the data-plane connection's client certificate and fails the claim closed
// on any mismatch (defense in depth — quicbaas never vouches for SO identity).
func (s *qrdbBarrierClientServicer) BarrierShareClaimBegin(ctx context.Context, username, _ string) (*qrdbsdk.BarrierShareClaimChallenge, error) {
	resp, err := s.client.ShareClaimBegin(ctx, &transport.ShareClaimBeginRequest{Username: username})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.BarrierShareClaimChallenge{
		NonceHex:  resp.NonceHex,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}

// BarrierShareClaimComplete verifies the officer's nonce signature on the
// data-plane node and returns the share bytes exactly once. As with
// BarrierShareClaimBegin, the caller thumbprint is re-derived remotely from
// the data-plane connection's client certificate, never trusted from here.
func (s *qrdbBarrierClientServicer) BarrierShareClaimComplete(ctx context.Context, username, nonceHex string, signature []byte, _ string) (*qrdbsdk.BarrierShareClaimResult, error) {
	resp, err := s.client.ShareClaimComplete(ctx, &transport.ShareClaimCompleteRequest{
		Username:  username,
		NonceHex:  nonceHex,
		Signature: signature,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.BarrierShareClaimResult{
		ShareIndex: resp.ShareIndex,
		ShareBytes: resp.ShareBytes,
	}, nil
}

// BarrierShamirSealVote submits a Shamir share as a seal vote toward the
// data-plane barrier's seal quorum.
func (s *qrdbBarrierClientServicer) BarrierShamirSealVote(ctx context.Context, share string) (*qrdbsdk.UnsealShareProgress, error) {
	resp, err := s.client.ShamirSealVote(ctx, &transport.ShamirSealVoteRequest{Share: share})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.UnsealShareProgress{
		Required:  resp.Required,
		Submitted: resp.Submitted,
		Complete:  resp.Complete,
	}, nil
}

// BarrierRecoveryOfficerRegister registers a Security Officer for recovery
// key distribution on the data-plane node.
func (s *qrdbBarrierClientServicer) BarrierRecoveryOfficerRegister(ctx context.Context, req *qrdbsdk.BarrierOfficerRegisterRequest) (*qrdbsdk.BarrierOfficerRegisterResult, error) {
	resp, err := s.client.RecoveryOfficerRegister(ctx, &transport.OfficerRegisterRequest{
		Username:          req.Username,
		CSRPEM:            req.CSRPEM,
		AuthenticatorType: req.AuthenticatorType,
		AuthenticatorRef:  req.AuthenticatorRef,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.BarrierOfficerRegisterResult{
		CertPEM:    resp.CertPEM,
		ShareIndex: resp.ShareIndex,
	}, nil
}

// BarrierRecoveryShareClaimBegin issues a challenge nonce for a recovery
// officer on the data-plane node. Caller identity is re-derived remotely from
// the data-plane connection's client certificate (see BarrierShareClaimBegin).
func (s *qrdbBarrierClientServicer) BarrierRecoveryShareClaimBegin(ctx context.Context, username, _ string) (*qrdbsdk.BarrierShareClaimChallenge, error) {
	resp, err := s.client.RecoveryShareClaimBegin(ctx, &transport.ShareClaimBeginRequest{Username: username})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.BarrierShareClaimChallenge{
		NonceHex:  resp.NonceHex,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}

// BarrierRecoveryShareClaimComplete verifies the recovery officer's nonce
// signature on the data-plane node and returns the recovery share bytes
// exactly once. Caller identity is re-derived remotely (see
// BarrierShareClaimComplete).
func (s *qrdbBarrierClientServicer) BarrierRecoveryShareClaimComplete(ctx context.Context, username, nonceHex string, signature []byte, _ string) (*qrdbsdk.BarrierShareClaimResult, error) {
	resp, err := s.client.RecoveryShareClaimComplete(ctx, &transport.ShareClaimCompleteRequest{
		Username:  username,
		NonceHex:  nonceHex,
		Signature: signature,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.BarrierShareClaimResult{
		ShareIndex: resp.ShareIndex,
		ShareBytes: resp.ShareBytes,
	}, nil
}

// officerStatusesToService converts wire OfficerStatus entries to the
// service-layer BarrierOfficerStatus the qrdb gRPC handlers expect.
func officerStatusesToService(in []*transport.OfficerStatus) []*qrdbsdk.BarrierOfficerStatus {
	out := make([]*qrdbsdk.BarrierOfficerStatus, 0, len(in))
	for _, o := range in {
		if o == nil {
			continue
		}
		out = append(out, &qrdbsdk.BarrierOfficerStatus{
			Username:       o.Username,
			CertThumbprint: o.CertThumbprint,
			ShareIndex:     o.ShareIndex,
			AuthFactor:     o.AuthFactor,
			EnrolledAt:     o.EnrolledAt,
			ClaimedAt:      o.ClaimedAt,
			SharePending:   o.SharePending,
		})
	}
	return out
}

// BarrierGenerateRootToken creates a new root token from Shamir shares,
// parsing the RFC3339 wire timestamp into the time.Time the service expects.
func (s *qrdbBarrierClientServicer) BarrierGenerateRootToken(ctx context.Context, shares []string) (*qrdbsdk.RootTokenInfo, error) {
	resp, err := s.client.BarrierGenerateRootToken(ctx, &transport.BarrierGenerateRootTokenRequest{Shares: shares})
	if err != nil {
		return nil, err
	}
	info := &qrdbsdk.RootTokenInfo{Token: resp.Token}
	if resp.CreatedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, resp.CreatedAt); parseErr == nil {
			info.CreatedAt = t
		} else if t, parseErr := time.Parse(time.RFC3339Nano, resp.CreatedAt); parseErr == nil {
			info.CreatedAt = t
		}
	}
	return info, nil
}
