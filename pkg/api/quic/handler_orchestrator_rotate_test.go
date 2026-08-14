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
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	sdkquic "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
)

// =============================================================================
// Session pooled-account rotation on OpOrchestrator sub-op 0x20
// =============================================================================

// rotatingTestAgentService is a testAgentService that also implements the
// optional rotation verb. The bare testAgentService deliberately does not,
// which is what makes the "deployment without rotation" branch reachable.
type rotatingTestAgentService struct {
	*testAgentService

	rotateFunc func(ctx context.Context, req *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error)

	lastRequest *orchestration.RotateSessionAccountRequest
	lastTenant  string
}

func (m *rotatingTestAgentService) RotateSessionAccount(
	ctx context.Context,
	req *orchestration.RotateSessionAccountRequest,
) (*orchestration.RotateSessionAccountResult, error) {
	m.lastRequest = req
	m.lastTenant = orchestration.TenantFromContext(ctx)
	if m.rotateFunc != nil {
		return m.rotateFunc(ctx, req)
	}
	return &orchestration.RotateSessionAccountResult{
		SessionID:         req.SessionID,
		PreviousAccountID: "acct-a",
		AccountID:         "acct-b",
		Strategy:          "least_usage",
		Mode:              orchestration.AccountRotationModeResume,
	}, nil
}

var _ orchestration.SessionAccountRotationService = (*rotatingTestAgentService)(nil)

func newRotatingOrchestratorHandler() (*Handler, *rotatingTestAgentService) {
	svc := &rotatingTestAgentService{testAgentService: &testAgentService{}}
	return createOrchestratorHandler(svc), svc
}

func TestHandleOrchestratorRotateAccount_Success(t *testing.T) {
	handler, svc := newRotatingOrchestratorHandler()

	availableAt := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Second)
	payload, err := EncodePayload(&orchestration.RotateSessionAccountRequest{
		SessionID:    "session-abc",
		Reason:       "seat retired",
		HoldOutgoing: true,
		AvailableAt:  availableAt,
	})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorRotateAccount, payload)
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Fatalf("Status = %d, want StatusOK: %s", resp.Status, string(resp.Payload))
	}

	var result orchestration.RotateSessionAccountResult
	if err := DecodePayload(resp.Payload, &result); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if result.PreviousAccountID != "acct-a" || result.AccountID != "acct-b" {
		t.Fatalf("rotation = %s -> %s, want acct-a -> acct-b", result.PreviousAccountID, result.AccountID)
	}
	if result.Mode != orchestration.AccountRotationModeResume {
		t.Fatalf("Mode = %q, want %q", result.Mode, orchestration.AccountRotationModeResume)
	}

	if svc.lastRequest == nil {
		t.Fatal("the service was never reached")
	}
	if svc.lastRequest.Reason != "seat retired" || !svc.lastRequest.HoldOutgoing {
		t.Fatalf("service saw %+v, want the operator's reason and hold", svc.lastRequest)
	}
	if !availableAt.Equal(svc.lastRequest.AvailableAt) {
		t.Fatalf("AvailableAt = %s, want %s", svc.lastRequest.AvailableAt, availableAt)
	}
}

// TestHandleOrchestratorRotateAccount_StampsTheWireTenant proves the sub-op is
// routed through handleOrchestratorDispatch's tenant injection like every other
// orchestrator verb — without it the service's tenant guard sees nothing.
func TestHandleOrchestratorRotateAccount_StampsTheWireTenant(t *testing.T) {
	handler, svc := newRotatingOrchestratorHandler()

	// The wire body carries tenant_id the way the SDK's injectTenantID writes
	// it; the dispatcher peeks it out and stamps the context.
	payload := []byte(`{"session_id":"session-abc","tenant_id":"tenant-a"}`)
	req := buildOrchestratorRequest(SubOpOrchestratorRotateAccount, payload)

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Fatalf("Status = %d, want StatusOK: %s", resp.Status, string(resp.Payload))
	}
	if svc.lastTenant != "tenant-a" {
		t.Fatalf("service context tenant = %q, want tenant-a", svc.lastTenant)
	}
}

func TestHandleOrchestratorRotateAccount_InvalidPayload(t *testing.T) {
	handler, svc := newRotatingOrchestratorHandler()

	req := buildOrchestratorRequest(SubOpOrchestratorRotateAccount, []byte("not json"))
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Fatal("a malformed payload was accepted")
	}
	if svc.lastRequest != nil {
		t.Fatal("a malformed payload reached the service")
	}
}

func TestHandleOrchestratorRotateAccount_NilService(t *testing.T) {
	handler := createOrchestratorHandler(nil)

	payload, err := EncodePayload(&orchestration.RotateSessionAccountRequest{SessionID: "session-abc"})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}
	resp := handler.HandleRequest(context.Background(),
		buildOrchestratorRequest(SubOpOrchestratorRotateAccount, payload))
	if resp.Status == StatusOK {
		t.Fatal("a nil agent service was accepted")
	}
}

// TestHandleOrchestratorRotateAccount_UnsupportedDeployment covers the
// wrong-type branch: an agent service without the rotation verb is answered
// with an error instead of panicking on a type assertion.
func TestHandleOrchestratorRotateAccount_UnsupportedDeployment(t *testing.T) {
	handler := createOrchestratorHandler(&testAgentService{})

	payload, err := EncodePayload(&orchestration.RotateSessionAccountRequest{SessionID: "session-abc"})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}
	resp := handler.HandleRequest(context.Background(),
		buildOrchestratorRequest(SubOpOrchestratorRotateAccount, payload))
	if resp.Status == StatusOK {
		t.Fatal("an agent service with no rotation verb was accepted")
	}
}

func TestHandleOrchestratorRotateAccount_ServiceError(t *testing.T) {
	handler, svc := newRotatingOrchestratorHandler()
	svc.rotateFunc = func(context.Context, *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error) {
		return nil, &orchestration.SessionNotRotatableError{
			SessionID: "session-abc",
			Reason:    "the session is pinned to a named account",
		}
	}

	payload, err := EncodePayload(&orchestration.RotateSessionAccountRequest{SessionID: "session-abc"})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}
	resp := handler.HandleRequest(context.Background(),
		buildOrchestratorRequest(SubOpOrchestratorRotateAccount, payload))
	if resp.Status == StatusOK {
		t.Fatal("a pinned session was rotated")
	}
}

// =============================================================================
// Sub-op allocation and authorization
// =============================================================================

// TestQUICOrchestratorRotateSubOpParity asserts the literal byte on both sides
// of the wire. The server and SDK protocol tables are separate files; a
// divergence here would silently route an operator's rotation into whatever
// sub-op happened to own the byte on the other side.
func TestQUICOrchestratorRotateSubOpParity(t *testing.T) {
	if SubOpOrchestratorRotateAccount != 0x20 {
		t.Fatalf("server SubOpOrchestratorRotateAccount = 0x%02X, want 0x20", SubOpOrchestratorRotateAccount)
	}
	if sdkquic.SubOpOrchestratorRotateAccount != 0x20 {
		t.Fatalf("SDK SubOpOrchestratorRotateAccount = 0x%02X, want 0x20", sdkquic.SubOpOrchestratorRotateAccount)
	}
	if SubOpOrchestratorRotateAccount != sdkquic.SubOpOrchestratorRotateAccount {
		t.Fatalf("server 0x%02X != SDK 0x%02X — pkg/api/quic/protocol.go and sdk/go/transport/quic/protocol.go are out of sync",
			SubOpOrchestratorRotateAccount, sdkquic.SubOpOrchestratorRotateAccount)
	}

	subOp, ok := sdkquic.OrchestratorMethodToSubOp("RotateSessionAccount")
	if !ok {
		t.Fatal("the SDK does not map RotateSessionAccount to a sub-op; the QUIC transport would reject the call")
	}
	if subOp != SubOpOrchestratorRotateAccount {
		t.Fatalf("SDK method map gives 0x%02X, want 0x%02X", subOp, SubOpOrchestratorRotateAccount)
	}
}

// TestQUICOrchestratorRotateSubOpDoesNotCollide keeps 0x20 outside every other
// vocabulary that rides the OpOrchestrator dispatch byte.
func TestQUICOrchestratorRotateSubOpDoesNotCollide(t *testing.T) {
	subOp := SubOpOrchestratorRotateAccount

	if subOp >= SubOpOrchestratorSpawn && subOp <= SubOpOrchestratorInference {
		t.Fatalf("0x%02X falls inside the contiguous orchestrator range, which authorizes as orchestrator:status", subOp)
	}
	collisions := map[string]bool{
		"account":     isAccountSubOp(subOp),
		"project":     isProjectSubOp(subOp),
		"ssh":         isSSHSubOp(subOp),
		"cert-ext":    isCertExtSubOp(subOp),
		"attestation": isAttestationSubOp(subOp),
	}
	for name, collides := range collisions {
		if collides {
			t.Fatalf("0x%02X collides with the %s sub-op range", subOp, name)
		}
	}
	if !isRotateSubOp(subOp) {
		t.Fatalf("isRotateSubOp(0x%02X) = false", subOp)
	}
}

// TestQUICRotateSubOpAuthorizesAsSpawn pins the deliberate choice of verb.
// Rotation closes the agent process and reconstitutes it against a newly
// selected pooled credential — a spawn's authority. Resolving it as the
// surface-level orchestrator:status would let a read-only caller move a
// session onto another account's credential.
func TestQUICRotateSubOpAuthorizesAsSpawn(t *testing.T) {
	resource, action, ok := getQRDBPermission(OpOrchestrator,
		[]byte{SubOpOrchestratorRotateAccount, '{', '}'})
	if !ok {
		t.Fatal("the rotate sub-op is unmapped; the default-deny gate would reject every rotation")
	}
	if resource != "orchestrator" || action != "spawn" {
		t.Fatalf("permission = %s:%s, want orchestrator:spawn", resource, action)
	}
}

// TestQUICRotateSubOpNeighboursKeepTheSurfaceMapping proves the new resolution
// branch is exactly one byte wide: everything around it still resolves the
// surface-level orchestrator:status the rest of OpOrchestrator uses, so the
// stronger verb cannot be reached by an off-by-one sub-op.
func TestQUICRotateSubOpNeighboursKeepTheSurfaceMapping(t *testing.T) {
	for _, subOp := range []byte{0x1F, 0x21, SubOpOrchestratorSessionStatus} {
		resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{subOp})
		if !ok {
			t.Fatalf("sub-op 0x%02X is unmapped", subOp)
		}
		if resource != "orchestrator" || action != "status" {
			t.Fatalf("sub-op 0x%02X permission = %s:%s, want the surface-level orchestrator:status",
				subOp, resource, action)
		}
	}
}
