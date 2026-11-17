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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-qrdb/pkg/service"
)

// errQRDBBarrierNotConfigured is the answer every barrier RPC gives while the
// node has no barrier servicer wired. go-qrdb classifies delegate errors
// (errclass), so a raw gRPC status would come out as Internal; the typed
// BarrierManagerNotConfiguredError maps to Unimplemented on every transport.
var errQRDBBarrierNotConfigured error = &service.BarrierManagerNotConfiguredError{}

// qrdbBarrierServicerProxy is the qrdb.v1 barrier servicer registered on the
// node's transports when its seal is disabled, and on the Unix-socket gRPC
// server. It loads the direct servicer published on s.barrierServicer on
// every call: the listeners are registered before the sealed-window
// services or the bootstrap ceremony stack are wired, so a servicer
// captured at registration time would be nil and the barrier surface would
// never come online.
type qrdbBarrierServicerProxy struct {
	s *Server
}

var _ qrdbsdk.BarrierServicer = (*qrdbBarrierServicerProxy)(nil)

func newQRDBBarrierServicerProxy(s *Server) *qrdbBarrierServicerProxy {
	return &qrdbBarrierServicerProxy{s: s}
}

// servicer returns the barrier servicer wired at the time of the call, or
// errQRDBBarrierNotConfigured when there is none.
func (p *qrdbBarrierServicerProxy) servicer() (qrdbsdk.BarrierServicer, error) {
	if p.s == nil {
		return nil, errQRDBBarrierNotConfigured
	}
	if ref := p.s.barrierServicer.Load(); ref != nil && ref.servicer != nil {
		return ref.servicer, nil
	}
	return nil, errQRDBBarrierNotConfigured
}

// barrierServicerRef boxes the published barrier servicer so it can sit in
// an atomic.Pointer.
type barrierServicerRef struct {
	servicer qrdbsdk.BarrierServicer
}

// publishBarrierServicer makes bs the servicer the barrier proxy serves; a
// nil bs clears it. Only a direct servicer is published -- never the
// newQRDBBarrierClientServicer adapter, which drops the verified SO PIN
// and the Shamir counts.
func (s *Server) publishBarrierServicer(bs qrdbsdk.BarrierServicer) {
	if bs == nil {
		s.barrierServicer.Store(nil)
		return
	}
	s.barrierServicer.Store(&barrierServicerRef{servicer: bs})
}

func (p *qrdbBarrierServicerProxy) BarrierInitialize(ctx context.Context, secret []byte) error {
	bs, err := p.servicer()
	if err != nil {
		return err
	}
	return bs.BarrierInitialize(ctx, secret)
}

func (p *qrdbBarrierServicerProxy) BarrierUnseal(ctx context.Context, secret []byte) error {
	bs, err := p.servicer()
	if err != nil {
		return err
	}
	return bs.BarrierUnseal(ctx, secret)
}

func (p *qrdbBarrierServicerProxy) BarrierSeal(ctx context.Context) error {
	bs, err := p.servicer()
	if err != nil {
		return err
	}
	return bs.BarrierSeal(ctx)
}

func (p *qrdbBarrierServicerProxy) BarrierStatus(ctx context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierStatus(ctx)
}

func (p *qrdbBarrierServicerProxy) BarrierInitializeShamir(ctx context.Context, secret []byte, threshold, totalShares int) (*qrdbsdk.ShamirInitInfo, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierInitializeShamir(ctx, secret, threshold, totalShares)
}

func (p *qrdbBarrierServicerProxy) BarrierUnsealWithShare(ctx context.Context, share string) (*qrdbsdk.UnsealShareProgress, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierUnsealWithShare(ctx, share)
}

func (p *qrdbBarrierServicerProxy) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	bs, err := p.servicer()
	if err != nil {
		return err
	}
	return bs.BarrierUnsealWithShares(ctx, shares)
}

func (p *qrdbBarrierServicerProxy) BarrierRekey(ctx context.Context, threshold, total int) (*qrdbsdk.ShamirInitInfo, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierRekey(ctx, threshold, total)
}

func (p *qrdbBarrierServicerProxy) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*qrdbsdk.RecoveryKeysInfo, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierGenerateRecoveryKeys(ctx, threshold, total)
}

func (p *qrdbBarrierServicerProxy) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	bs, err := p.servicer()
	if err != nil {
		return err
	}
	return bs.BarrierRecoverWithKeys(ctx, keys)
}

func (p *qrdbBarrierServicerProxy) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	bs, err := p.servicer()
	if err != nil {
		return err
	}
	return bs.BarrierDeleteRecoveryKeys(ctx)
}

func (p *qrdbBarrierServicerProxy) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	bs, err := p.servicer()
	if err != nil {
		return false, err
	}
	return bs.BarrierHasRecoveryKeys(ctx)
}

func (p *qrdbBarrierServicerProxy) BarrierGenerateRootToken(ctx context.Context, shares []string) (*qrdbsdk.RootTokenInfo, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierGenerateRootToken(ctx, shares)
}

func (p *qrdbBarrierServicerProxy) BarrierOfficerRegister(ctx context.Context, req *qrdbsdk.BarrierOfficerRegisterRequest) (*qrdbsdk.BarrierOfficerRegisterResult, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierOfficerRegister(ctx, req)
}

func (p *qrdbBarrierServicerProxy) BarrierOfficerList(ctx context.Context) ([]*qrdbsdk.BarrierOfficerStatus, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierOfficerList(ctx)
}

func (p *qrdbBarrierServicerProxy) BarrierOfficerClaimStatus(ctx context.Context) ([]*qrdbsdk.BarrierOfficerStatus, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierOfficerClaimStatus(ctx)
}

func (p *qrdbBarrierServicerProxy) BarrierShareClaimBegin(ctx context.Context, username, callerCertThumbprint string) (*qrdbsdk.BarrierShareClaimChallenge, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierShareClaimBegin(ctx, username, callerCertThumbprint)
}

func (p *qrdbBarrierServicerProxy) BarrierShareClaimComplete(ctx context.Context, username, nonceHex string, signature []byte, callerCertThumbprint string) (*qrdbsdk.BarrierShareClaimResult, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierShareClaimComplete(ctx, username, nonceHex, signature, callerCertThumbprint)
}

func (p *qrdbBarrierServicerProxy) BarrierShamirSealVote(ctx context.Context, share string) (*qrdbsdk.UnsealShareProgress, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierShamirSealVote(ctx, share)
}

func (p *qrdbBarrierServicerProxy) BarrierRecoveryOfficerRegister(ctx context.Context, req *qrdbsdk.BarrierOfficerRegisterRequest) (*qrdbsdk.BarrierOfficerRegisterResult, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierRecoveryOfficerRegister(ctx, req)
}

func (p *qrdbBarrierServicerProxy) BarrierRecoveryShareClaimBegin(ctx context.Context, username, callerCertThumbprint string) (*qrdbsdk.BarrierShareClaimChallenge, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierRecoveryShareClaimBegin(ctx, username, callerCertThumbprint)
}

func (p *qrdbBarrierServicerProxy) BarrierRecoveryShareClaimComplete(ctx context.Context, username, nonceHex string, signature []byte, callerCertThumbprint string) (*qrdbsdk.BarrierShareClaimResult, error) {
	bs, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return bs.BarrierRecoveryShareClaimComplete(ctx, username, nonceHex, signature, callerCertThumbprint)
}
