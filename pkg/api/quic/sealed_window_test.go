//go:build quic

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// fakeSealedProbe is a deterministic sealed-window probe for gate tests.
type fakeSealedProbe struct{ sealed bool }

func (p *fakeSealedProbe) IsSealed() bool { return p.sealed }

// newGateTestServer returns a bare Server with only the sealed-gate state
// populated (the gate methods touch nothing else).
func newGateTestServer(probe SealedWindowProbe) *Server {
	s := &Server{}
	s.SetSealedWindowGate(probe, sealedwindow.QUICAllowlist(), sealedwindow.QRDBQUICAllowlist())
	return s
}

// TestSealedWindowTable_OpcodeLockstep pins the sealedwindow package's
// private opcode/sub-op mirror against the authoritative protocol constants
// in this package: the derivation table cannot import the tag-gated quic
// package, so this guard is what keeps the two in lock-step.
func TestSealedWindowTable_OpcodeLockstep(t *testing.T) {
	allow := sealedwindow.QUICAllowlist()

	expectAllowed := []sealedwindow.QUICOp{
		{Opcode: OpHealth},
		{Opcode: OpBarrier, SubOp: SubOpBarrierStatus},
		{Opcode: OpBarrier, SubOp: SubOpBarrierInitialize},
		{Opcode: OpBarrier, SubOp: SubOpBarrierUnseal},
		{Opcode: OpBarrier, SubOp: SubOpBarrierShamirInitialize},
		{Opcode: OpBarrier, SubOp: SubOpBarrierShamirUnsealShare},
		{Opcode: OpBarrier, SubOp: SubOpBarrierShamirUnsealShares},
		{Opcode: OpBarrier, SubOp: SubOpBarrierRekey},
		{Opcode: OpBarrier, SubOp: SubOpBarrierRecoveryGenerate},
		{Opcode: OpBarrier, SubOp: SubOpBarrierRecoveryRecover},
		{Opcode: OpBarrier, SubOp: SubOpBarrierRecoveryDelete},
		{Opcode: OpBarrier, SubOp: SubOpBarrierRecoveryStatus},
		{Opcode: OpBarrier, SubOp: SubOpBarrierRootToken},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyInitialize},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyStatus},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyClaimCertBegin},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyClaimCertComplete},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyBeginClaimShare},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyCompleteClaimShare},
		{Opcode: OpCeremony, SubOp: SubOpCeremonySignCSR},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyListOfficers},
		{Opcode: OpCeremony, SubOp: SubOpCeremonyEnrollExternalOfficer},
	}
	for _, op := range expectAllowed {
		_, ok := allow[op]
		assert.True(t, ok, "canonical table must admit opcode 0x%02X sub 0x%02X", op.Opcode, op.SubOp)
	}

	expectDenied := []sealedwindow.QUICOp{
		{Opcode: OpBarrier, SubOp: SubOpBarrierSeal},
		{Opcode: OpBarrier, SubOp: SubOpBarrierEnableEncryption},
		{Opcode: OpBarrier, SubOp: SubOpBarrierUpdateTenantQuotas},
		{Opcode: OpBarrier, SubOp: SubOpBarrierUpdateTenantEntitlements},
	}
	for _, op := range expectDenied {
		_, ok := allow[op]
		assert.False(t, ok, "canonical table must exclude opcode 0x%02X sub 0x%02X", op.Opcode, op.SubOp)
	}
}

// TestSealedWindowDeniesOp_SealedAdmitDenyTable is the table-driven
// admit/deny check for the native opcode gate while sealed, driven directly
// from the single canonical derivation.
func TestSealedWindowDeniesOp_SealedAdmitDenyTable(t *testing.T) {
	s := newGateTestServer(&fakeSealedProbe{sealed: true})

	cases := []struct {
		name    string
		opcode  byte
		payload []byte
		denied  bool
	}{
		{"health admitted", OpHealth, nil, false},
		{"barrier status admitted", OpBarrier, []byte{SubOpBarrierStatus}, false},
		{"barrier initialize admitted", OpBarrier, []byte{SubOpBarrierInitialize}, false},
		{"barrier unseal admitted", OpBarrier, []byte{SubOpBarrierUnseal}, false},
		{"shamir unseal-share admitted", OpBarrier, []byte{SubOpBarrierShamirUnsealShare}, false},
		{"ceremony status admitted", OpCeremony, []byte{SubOpCeremonyStatus}, false},
		{"ceremony claim-cert begin admitted", OpCeremony, []byte{SubOpCeremonyClaimCertBegin}, false},

		{"barrier seal denied", OpBarrier, []byte{SubOpBarrierSeal}, true},
		{"barrier enable-encryption denied", OpBarrier, []byte{SubOpBarrierEnableEncryption}, true},
		{"tenant quotas denied", OpBarrier, []byte{SubOpBarrierUpdateTenantQuotas}, true},
		{"kv get denied", OpGet, nil, true},
		{"kv put denied", OpPut, nil, true},
		{"barrier empty payload denied", OpBarrier, nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.denied, s.sealedWindowDeniesOp(tc.opcode, tc.payload))
		})
	}
}

// TestSealedWindowDeniesOp_UnsealedPassThrough asserts the gate is a
// pass-through the moment the probe reports unsealed, and when no probe is
// installed at all.
func TestSealedWindowDeniesOp_UnsealedPassThrough(t *testing.T) {
	s := newGateTestServer(&fakeSealedProbe{sealed: false})
	assert.False(t, s.sealedWindowDeniesOp(OpGet, nil))
	assert.False(t, s.sealedWindowDeniesOp(OpBarrier, []byte{SubOpBarrierSeal}))

	unwired := &Server{}
	assert.False(t, unwired.sealedWindowDeniesOp(OpGet, nil))
}

// TestSealedWindowDeniesQRDBMethod_SealedAdmitDenyTable is the table-driven
// admit/deny check for the qrdb-quic ALPN bridge gate while sealed.
func TestSealedWindowDeniesQRDBMethod_SealedAdmitDenyTable(t *testing.T) {
	s := newGateTestServer(&fakeSealedProbe{sealed: true})

	cases := []struct {
		method string
		denied bool
	}{
		{"admin.Health", false},
		{"barrier.Status", false},
		{"barrier.Initialize", false},
		{"barrier.Unseal", false},
		{"barrier.ShamirUnsealShare", false},
		{"barrier.RootToken", false},
		{"ceremony.Initialize", false},
		{"ceremony.SignCSR", false},
		{"ceremony.EnrollExternalOfficer", false},

		{"barrier.Seal", true},
		{"barrier.EnableEncryption", true},
		{"credential.Submit", true},
		{"credential.List", true},
		{"tenant.BarrierStatus", true},
		{"user.Get", true},
		{"userauthfactor.Login", true},
		{"no.SuchMethod", true},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			assert.Equal(t, tc.denied, s.sealedWindowDeniesQRDBMethod(tc.method))
		})
	}
}

// TestSealedWindowDeniesQRDBMethod_UnsealedPassThrough asserts the bridge
// gate opens on unseal and stays inert when unwired.
func TestSealedWindowDeniesQRDBMethod_UnsealedPassThrough(t *testing.T) {
	s := newGateTestServer(&fakeSealedProbe{sealed: false})
	assert.False(t, s.sealedWindowDeniesQRDBMethod("credential.Submit"))

	unwired := &Server{}
	assert.False(t, unwired.sealedWindowDeniesQRDBMethod("credential.Submit"))
}
