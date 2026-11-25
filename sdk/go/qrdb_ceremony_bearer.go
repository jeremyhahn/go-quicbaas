// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/metadata"
)

// qrdbBearerCeremonyClient forwards to go-qrdb's ceremony client and adds
// the platform bearer as outgoing gRPC authorization metadata on every call,
// the way qrdbBearerBarrierClient does for the barrier verbs. Every verb is
// written out so a verb added to the interface fails to compile here instead
// of skipping the bearer.
type qrdbBearerCeremonyClient struct {
	inner  QRDBCeremonyClient
	bearer string
}

var _ QRDBCeremonyClient = (*qrdbBearerCeremonyClient)(nil)

func (b *qrdbBearerCeremonyClient) ctx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer)
}

// CeremonyInitialize forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyInitialize(ctx context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
	return b.inner.CeremonyInitialize(b.ctx(ctx), req)
}

// CeremonyStatus forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
	return b.inner.CeremonyStatus(b.ctx(ctx))
}

// CeremonyClaimCertBegin forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyClaimCertBegin(ctx context.Context, req *qrdbtransport.ClaimCertBeginRequest) (*qrdbtransport.ClaimCertBeginResponse, error) {
	return b.inner.CeremonyClaimCertBegin(b.ctx(ctx), req)
}

// CeremonyClaimCertComplete forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyClaimCertComplete(ctx context.Context, req *qrdbtransport.ClaimCertCompleteRequest) (*qrdbtransport.ClaimCertCompleteResponse, error) {
	return b.inner.CeremonyClaimCertComplete(b.ctx(ctx), req)
}

// CeremonyBeginClaimShare forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyBeginClaimShare(ctx context.Context, req *qrdbtransport.BeginClaimShareRequest) (*qrdbtransport.BeginClaimShareResponse, error) {
	return b.inner.CeremonyBeginClaimShare(b.ctx(ctx), req)
}

// CeremonyCompleteClaimShare forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyCompleteClaimShare(ctx context.Context, req *qrdbtransport.CompleteClaimShareRequest) (*qrdbtransport.CompleteClaimShareResponse, error) {
	return b.inner.CeremonyCompleteClaimShare(b.ctx(ctx), req)
}

// CeremonySignCSR forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonySignCSR(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error) {
	return b.inner.CeremonySignCSR(b.ctx(ctx), req)
}

// CeremonyClaimShareBegin forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyClaimShareBegin(ctx context.Context, req *qrdbtransport.CeremonyClaimShareBeginRequest) (*qrdbtransport.CeremonyClaimShareBeginResponse, error) {
	return b.inner.CeremonyClaimShareBegin(b.ctx(ctx), req)
}

// CeremonyClaimShareComplete forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyClaimShareComplete(ctx context.Context, req *qrdbtransport.CeremonyClaimShareCompleteRequest) (*qrdbtransport.CeremonyCustodianInfo, error) {
	return b.inner.CeremonyClaimShareComplete(b.ctx(ctx), req)
}

// CeremonySubmitUnsealShare forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonySubmitUnsealShare(ctx context.Context, req *qrdbtransport.CeremonySubmitUnsealShareRequest) (*qrdbtransport.CeremonySubmitUnsealShareResponse, error) {
	return b.inner.CeremonySubmitUnsealShare(b.ctx(ctx), req)
}

// CeremonyEnrollExternalOfficer forwards with the bearer attached.
func (b *qrdbBearerCeremonyClient) CeremonyEnrollExternalOfficer(ctx context.Context, req *qrdbtransport.EnrollExternalOfficerRequest) (*qrdbtransport.EnrollExternalOfficerResponse, error) {
	return b.inner.CeremonyEnrollExternalOfficer(b.ctx(ctx), req)
}
