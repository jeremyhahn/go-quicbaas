//go:build quic

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

package quic

import (
	"context"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// capturingIAMService records the CreateUser request the QUIC handler decodes
// from the wire so tests can assert field-level forwarding (roles parity).
type capturingIAMService struct {
	mockIAMService91
	createUserReq *service.IAMCreateUserRequest
	createUserErr error
}

func (m *capturingIAMService) CreateUser(_ context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
	m.createUserReq = req
	if m.createUserErr != nil {
		return nil, m.createUserErr
	}
	return &service.IAMCreateUserResponse{
		User: &iam.User{ID: "u1", TenantID: req.TenantID, Email: req.Email, Name: req.Name, Roles: req.Roles},
	}, nil
}

// TestIAMCreateUser_RolesForwarded proves the QUIC transport decodes the
// snake_case "roles" wire field and forwards it to the IAM service untouched.
func TestIAMCreateUser_RolesForwarded(t *testing.T) {
	h := createTestHandler()
	svc := &capturingIAMService{}
	WithIAMService(svc)(h)
	defer h.clearIAMService()

	payload := []byte(`{"tenant_id":"t-1","email":"roles@example.com","name":"Role User","roles":["role-a","role-b"]}`)
	resp := h.handleIAMCreateUser(context.Background(), &Request{Payload: payload})

	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d (payload: %s)", resp.Status, resp.Payload)
	}
	if svc.createUserReq == nil {
		t.Fatal("Expected CreateUser to be invoked")
	}
	if len(svc.createUserReq.Roles) != 2 ||
		svc.createUserReq.Roles[0] != "role-a" || svc.createUserReq.Roles[1] != "role-b" {
		t.Fatalf("Expected roles [role-a role-b] forwarded, got %v", svc.createUserReq.Roles)
	}
}

// TestIAMCreateUser_GroupsAndAttributesForwarded proves the QUIC transport
// decodes the snake_case "groups" and "attributes" wire fields and forwards
// them to the IAM service untouched (sibling gap of the roles parity fix).
func TestIAMCreateUser_GroupsAndAttributesForwarded(t *testing.T) {
	h := createTestHandler()
	svc := &capturingIAMService{}
	WithIAMService(svc)(h)
	defer h.clearIAMService()

	payload := []byte(`{"tenant_id":"t-1","email":"groups@example.com","name":"Group User",` +
		`"groups":["group-a","group-b"],"attributes":{"department":"engineering"}}`)
	resp := h.handleIAMCreateUser(context.Background(), &Request{Payload: payload})

	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d (payload: %s)", resp.Status, resp.Payload)
	}
	if svc.createUserReq == nil {
		t.Fatal("Expected CreateUser to be invoked")
	}
	if len(svc.createUserReq.Groups) != 2 ||
		svc.createUserReq.Groups[0] != "group-a" || svc.createUserReq.Groups[1] != "group-b" {
		t.Fatalf("Expected groups [group-a group-b] forwarded, got %v", svc.createUserReq.Groups)
	}
	if svc.createUserReq.Attributes == nil || svc.createUserReq.Attributes["department"] != "engineering" {
		t.Fatalf("Expected attributes forwarded, got %v", svc.createUserReq.Attributes)
	}
}

// TestIAMCreateUser_UnknownGroup_NotFoundStatus proves a typed not-found error
// from group validation surfaces as the QUIC StatusNotFound wire status.
func TestIAMCreateUser_UnknownGroup_NotFoundStatus(t *testing.T) {
	h := createTestHandler()
	svc := &capturingIAMService{
		createUserErr: &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.group.get"},
	}
	WithIAMService(svc)(h)
	defer h.clearIAMService()

	payload := []byte(`{"tenant_id":"t-1","email":"phantom@example.com","name":"Phantom","groups":["no-such-group"]}`)
	resp := h.handleIAMCreateUser(context.Background(), &Request{Payload: payload})

	if resp.Status != StatusNotFound {
		t.Fatalf("Expected StatusNotFound, got %d (payload: %s)", resp.Status, resp.Payload)
	}
}

// TestIAMCreateUser_UnknownRole_NotFoundStatus proves a typed not-found error
// from role validation surfaces as the QUIC StatusNotFound wire status.
func TestIAMCreateUser_UnknownRole_NotFoundStatus(t *testing.T) {
	h := createTestHandler()
	svc := &capturingIAMService{
		createUserErr: &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.role.get"},
	}
	WithIAMService(svc)(h)
	defer h.clearIAMService()

	payload := []byte(`{"tenant_id":"t-1","email":"phantom@example.com","name":"Phantom","roles":["no-such-role"]}`)
	resp := h.handleIAMCreateUser(context.Background(), &Request{Payload: payload})

	if resp.Status != StatusNotFound {
		t.Fatalf("Expected StatusNotFound, got %d (payload: %s)", resp.Status, resp.Payload)
	}
}

// TestIAMCreateUserRequest_WireRoundTrip_SnakeCase is the wire-format
// regression gate for the QUIC create-user frame: roles MUST serialize under
// the snake_case "roles" key (July wire-bug class) and survive a
// Encode/Decode round trip intact.
func TestIAMCreateUserRequest_WireRoundTrip_SnakeCase(t *testing.T) {
	in := service.IAMCreateUserRequest{
		TenantID:   "t-1",
		Email:      "wire@example.com",
		Username:   "wireuser",
		Name:       "Wire User",
		Roles:      []string{"role-a"},
		Groups:     []string{"group-a"},
		Attributes: map[string]interface{}{"department": "engineering"},
	}

	raw, err := EncodePayload(&in)
	if err != nil {
		t.Fatalf("EncodePayload failed: %v", err)
	}

	// snake_case wire keys.
	for _, key := range []string{`"tenant_id"`, `"roles"`, `"groups"`, `"attributes"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("Expected wire payload to contain %s, got %s", key, raw)
		}
	}

	var out service.IAMCreateUserRequest
	if err := DecodePayload(raw, &out); err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}
	if out.TenantID != in.TenantID || out.Email != in.Email || out.Username != in.Username {
		t.Errorf("Round-trip mismatch: got %+v, want %+v", out, in)
	}
	if len(out.Roles) != 1 || out.Roles[0] != "role-a" {
		t.Errorf("Expected roles [role-a] after round trip, got %v", out.Roles)
	}
	if len(out.Groups) != 1 || out.Groups[0] != "group-a" {
		t.Errorf("Expected groups [group-a] after round trip, got %v", out.Groups)
	}
	if out.Attributes == nil || out.Attributes["department"] != "engineering" {
		t.Errorf("Expected attributes to survive round trip, got %v", out.Attributes)
	}
}
