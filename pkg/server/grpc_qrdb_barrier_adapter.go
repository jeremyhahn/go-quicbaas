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
func (s *qrdbBarrierClientServicer) BarrierInitialize(ctx context.Context, secret string) error {
	if err := s.client.BarrierInitialize(ctx, &transport.BarrierInitializeRequest{Secret: secret}); err != nil {
		if errors.Is(err, qrdbsdk.ErrAlreadyInitialized) {
			return nil
		}
		return err
	}
	return nil
}

// BarrierUnseal unseals the data-plane barrier with a single secret.
func (s *qrdbBarrierClientServicer) BarrierUnseal(ctx context.Context, secret string) error {
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
		Sealed:         resp.Sealed,
		Strategy:       resp.Strategy,
		HardwareBacked: resp.HardwareBacked,
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
// secret sharing and returns the generated shares.
func (s *qrdbBarrierClientServicer) BarrierInitializeShamir(ctx context.Context, secret string, threshold, totalShares int) (*qrdbsdk.ShamirInitInfo, error) {
	resp, err := s.client.BarrierInitializeShamir(ctx, &transport.BarrierInitializeShamirRequest{
		Secret:      secret,
		Threshold:   threshold,
		TotalShares: totalShares,
	})
	if err != nil {
		return nil, err
	}
	return &qrdbsdk.ShamirInitInfo{
		Shares:      resp.Shares,
		Threshold:   resp.Threshold,
		TotalShares: resp.TotalShares,
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
		Shares:      resp.Shares,
		Threshold:   resp.Threshold,
		TotalShares: resp.TotalShares,
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
		Keys:      resp.Keys,
		Threshold: resp.Threshold,
		Total:     resp.Total,
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
