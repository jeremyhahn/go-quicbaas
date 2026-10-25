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

package quicbaas

import (
	"crypto"
	stderrors "errors"
	"testing"
)

// =============================================================================
// crypto_adapter.go error-type tests
// =============================================================================
//
// All error types in crypto_adapter.go implement Error() and (in most cases)
// Unwrap(). These short tests verify the basic contract for each.

func TestErrSignerKeyFetch_Error(t *testing.T) {
	inner := stderrors.New("inner")
	e := &ErrSignerKeyFetch{KeyID: "k", Err: inner}
	if got := e.Error(); got == "" {
		t.Error("expected non-empty error string")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return inner")
	}
}

func TestErrSignerSign_Error(t *testing.T) {
	inner := stderrors.New("inner")
	e := &ErrSignerSign{KeyID: "k", Err: inner}
	if got := e.Error(); got == "" {
		t.Error("expected non-empty error string")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return inner")
	}
}

func TestErrSignerUnsupportedHash_Error(t *testing.T) {
	e := &ErrSignerUnsupportedHash{Hash: crypto.MD5}
	if got := e.Error(); got == "" {
		t.Error("expected non-empty error string")
	}
}

func TestErrDecrypterKeyFetch_Error(t *testing.T) {
	inner := stderrors.New("inner")
	e := &ErrDecrypterKeyFetch{KeyID: "k", Err: inner}
	if got := e.Error(); got == "" {
		t.Error("expected non-empty error string")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return inner")
	}
}

func TestErrDecrypterDecrypt_Error(t *testing.T) {
	inner := stderrors.New("inner")
	e := &ErrDecrypterDecrypt{KeyID: "k", Err: inner}
	if got := e.Error(); got == "" {
		t.Error("expected non-empty error string")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return inner")
	}
}

func TestErrParsePublicKey_Error(t *testing.T) {
	inner := stderrors.New("inner")
	e := &ErrParsePublicKey{KeyID: "k", Err: inner}
	if got := e.Error(); got == "" {
		t.Error("expected non-empty error string")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return inner")
	}
}

func TestErrUnsupportedKeyType_Error(t *testing.T) {
	e := &ErrUnsupportedKeyType{KeyID: "k", KeyType: "DSA"}
	if got := e.Error(); got == "" {
		t.Error("expected non-empty error string")
	}
}

// =============================================================================
// crypto_adapter.go publicKeyTypeName tests
// =============================================================================

func TestPublicKeyTypeName_UnknownReturnsLabel(t *testing.T) {
	// An int isn't a recognized public key — should fall to the default
	// branch and return "unknown".
	if got := publicKeyTypeName(123); got != "unknown" {
		t.Errorf("expected unknown for int, got %q", got)
	}
}

// =============================================================================
// Client accessor tests for DEK/Orchestrator (lazy-init via sync paths).
// =============================================================================

func TestClient_DEK_NonNil_AndCached(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	first := c.dek()
	if first == nil {
		t.Fatal("dek() returned nil")
	}
	second := c.dek()
	if first != second {
		t.Error("dek() should return the same instance across calls")
	}
}

func TestClient_Orchestrator_NonNil_AndCached(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	first := c.Orchestrator()
	if first == nil {
		t.Fatal("Orchestrator() returned nil")
	}
	second := c.Orchestrator()
	if first != second {
		t.Error("Orchestrator() should return the same instance across calls")
	}
}

// =============================================================================
// SecurityService DEK accessor delegates to client.DEK.
// =============================================================================

func TestSecurityService_DEK_DelegatesToClient(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	svc := c.Security()
	if svc.DEK() != c.dek() {
		t.Error("Security().DEK() should delegate to client.dek()")
	}
}

// =============================================================================
// qrdb.go database error helpers
// =============================================================================

func TestNewDatabaseError_NotNil(t *testing.T) {
	e := NewDatabaseError(ErrNotFound, "op", "msg")
	if e == nil {
		t.Fatal("expected non-nil DatabaseError")
	}
	if e.Error() == "" {
		t.Error("expected non-empty Error() string")
	}
}

func TestIsDatabaseError_FoundAndNot(t *testing.T) {
	wrapped := NewDatabaseError(ErrNotFound, "op", "msg")
	var target *DatabaseError
	if !IsDatabaseError(wrapped, &target) {
		t.Error("expected wrapped DatabaseError to be detected")
	}
	if target == nil {
		t.Error("expected target to be populated")
	}

	var nopeTarget *DatabaseError
	if IsDatabaseError(stderrors.New("plain"), &nopeTarget) {
		t.Error("expected plain error to not be a DatabaseError")
	}
}

func TestIsDatabaseErrorCode_Matches(t *testing.T) {
	e := NewDatabaseError(ErrTimeout, "op", "msg")
	if !IsDatabaseErrorCode(e, ErrTimeout) {
		t.Error("expected timeout code to match")
	}
	if IsDatabaseErrorCode(e, ErrNotFound) {
		t.Error("did not expect notfound code to match a timeout error")
	}
}

// =============================================================================
// BarrierCeremonyMgmt / TenantMgmtService accessor smoke tests
// =============================================================================

func TestClient_BarrierCeremonyMgmt_NonNil(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	if c.BarrierCeremonyMgmt() == nil {
		t.Fatal("BarrierCeremonyMgmt() returned nil")
	}
}

func TestClient_TenantMgmtService_NonNil(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	if c.TenantMgmtService() == nil {
		t.Fatal("TenantMgmtService() returned nil")
	}
}

// =============================================================================
// QRDB-backed accessor error propagation tests
// =============================================================================
//
// SealStore/TenantMgmt/CustodianGroup/Share/Tenant all funnel through
// c.qrdbClient() which fails fast under TransportEmbedded without
// WithEmbeddedQRDB. Asserting the resulting error covers the accessor's
// success-path call and the early-return wiring without needing a network.

func newBrokenEmbeddedClient(t *testing.T) *client {
	t.Helper()
	opts := defaultOptions()
	opts.transportType = TransportEmbedded
	return &client{opts: opts, address: "embedded://broken"}
}

func TestClient_SealStore_PropagatesInitError(t *testing.T) {
	c := newBrokenEmbeddedClient(t)
	if _, err := c.SealStore(); err == nil {
		t.Fatal("expected error from misconfigured embedded qrdb")
	}
}

func TestClient_TenantMgmt_PropagatesInitError(t *testing.T) {
	c := newBrokenEmbeddedClient(t)
	if _, err := c.TenantMgmt(); err == nil {
		t.Fatal("expected error from misconfigured embedded qrdb")
	}
}

// barrierCeremonyService Ceremony / Tenant accessors

func TestBarrierCeremonyService_Ceremony_PropagatesError(t *testing.T) {
	c := newBrokenEmbeddedClient(t)
	if _, err := c.BarrierCeremonyMgmt().Ceremony(); err == nil {
		t.Fatal("expected error")
	}
}

func TestTenantService_Tenant_PropagatesError(t *testing.T) {
	c := newBrokenEmbeddedClient(t)
	if _, err := c.TenantMgmtService().Tenant(); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_Agents_NonNil_AndCached(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	first := c.Agents()
	if first == nil {
		t.Fatal("Agents() returned nil")
	}
	if second := c.Agents(); first != second {
		t.Error("Agents() should return the same instance across calls")
	}
	if newAgentsClient(nil) != nil {
		t.Error("no transport, no Agents client")
	}
}
