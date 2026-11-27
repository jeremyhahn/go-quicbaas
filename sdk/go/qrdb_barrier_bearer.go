// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/metadata"
)

// qrdbBearerBarrierClient forwards to go-qrdb's barrier client and adds the
// platform bearer as outgoing gRPC authorization metadata on every call, the
// way the SDK's own gRPC transport does. Every verb is written out so a verb
// added to the interface fails to compile here instead of skipping the bearer.
type qrdbBearerBarrierClient struct {
	inner  QRDBBarrierClient
	bearer string
}

var _ QRDBBarrierClient = (*qrdbBearerBarrierClient)(nil)

func (b *qrdbBearerBarrierClient) ctx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer)
}

// BarrierInitialize forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error {
	return b.inner.BarrierInitialize(b.ctx(ctx), req)
}

// BarrierUnseal forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error {
	return b.inner.BarrierUnseal(b.ctx(ctx), req)
}

// BarrierSeal forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierSeal(ctx context.Context) error {
	return b.inner.BarrierSeal(b.ctx(ctx))
}

// BarrierStatus forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	return b.inner.BarrierStatus(b.ctx(ctx))
}

// BarrierInitializeShamir forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	return b.inner.BarrierInitializeShamir(b.ctx(ctx), req)
}

// BarrierUnsealWithShare forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierUnsealWithShare(ctx context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error) {
	return b.inner.BarrierUnsealWithShare(b.ctx(ctx), req)
}

// BarrierUnsealWithShares forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierUnsealWithShares(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error {
	return b.inner.BarrierUnsealWithShares(b.ctx(ctx), req)
}

// BarrierRekey forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierRekey(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error) {
	return b.inner.BarrierRekey(b.ctx(ctx), req)
}

// BarrierEnableEncryption forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierEnableEncryption(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	return b.inner.BarrierEnableEncryption(b.ctx(ctx), req)
}

// BarrierGenerateRecoveryKeys forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	return b.inner.BarrierGenerateRecoveryKeys(b.ctx(ctx), req)
}

// BarrierRecoverWithKeys forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierRecoverWithKeys(ctx context.Context, req *qrdbtransport.BarrierRecoverWithKeysRequest) error {
	return b.inner.BarrierRecoverWithKeys(b.ctx(ctx), req)
}

// BarrierDeleteRecoveryKeys forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	return b.inner.BarrierDeleteRecoveryKeys(b.ctx(ctx))
}

// BarrierHasRecoveryKeys forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierHasRecoveryKeys(ctx context.Context) (*qrdbtransport.BarrierHasRecoveryKeysResponse, error) {
	return b.inner.BarrierHasRecoveryKeys(b.ctx(ctx))
}

// BarrierGenerateRootToken forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) BarrierGenerateRootToken(ctx context.Context, req *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
	return b.inner.BarrierGenerateRootToken(b.ctx(ctx), req)
}

// OfficerRegister forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) OfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return b.inner.OfficerRegister(b.ctx(ctx), req)
}

// OfficerList forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	return b.inner.OfficerList(b.ctx(ctx))
}

// OfficerClaimStatus forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) OfficerClaimStatus(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	return b.inner.OfficerClaimStatus(b.ctx(ctx))
}

// ShareClaimBegin forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) ShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return b.inner.ShareClaimBegin(b.ctx(ctx), req)
}

// ShareClaimComplete forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) ShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return b.inner.ShareClaimComplete(b.ctx(ctx), req)
}

// ShamirSealVote forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) ShamirSealVote(ctx context.Context, req *qrdbtransport.ShamirSealVoteRequest) (*qrdbtransport.ShamirSealVoteResponse, error) {
	return b.inner.ShamirSealVote(b.ctx(ctx), req)
}

// RecoveryOfficerRegister forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return b.inner.RecoveryOfficerRegister(b.ctx(ctx), req)
}

// RecoveryShareClaimBegin forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return b.inner.RecoveryShareClaimBegin(b.ctx(ctx), req)
}

// RecoveryShareClaimComplete forwards with the bearer attached.
func (b *qrdbBearerBarrierClient) RecoveryShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return b.inner.RecoveryShareClaimComplete(b.ctx(ctx), req)
}
