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

package grpc

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// --- KV handler ---

func TestKVHandler_AllMethods_Success(t *testing.T) {
	h := newKVHandler(newQuicBaaSConn(t, false))
	ctx := context.Background()
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"Put", "Put", &service.PutRequest{EntityType: "user", ID: 1, Data: []byte("v")}},
		{"Get", "Get", &service.GetRequest{EntityType: "user", ID: 1}},
		{"Delete", "Delete", &service.DeleteRequest{EntityType: "user", ID: 1}},
		{"List", "List", &service.ListRequest{EntityType: "user", PageSize: 10}},
		{"ListEntityTypes", "ListEntityTypes", &service.ListEntityTypesRequest{PageSize: 10}},
		{"BatchPut", "BatchPut", &service.BatchPutRequest{Requests: []*service.PutRequest{{EntityType: "user", ID: 1, Data: []byte("v")}}}},
		{"BatchGet", "BatchGet", &service.BatchGetRequest{Requests: []*service.GetRequest{{EntityType: "user", ID: 1}}}},
		{"BatchDelete", "BatchDelete", &service.BatchDeleteRequest{Requests: []*service.DeleteRequest{{EntityType: "user", ID: 1}}}},
		{"Transaction", "Transaction", &service.TransactionRequest{
			Compare: []*service.CompareCondition{{EntityType: "user", ID: 1, Target: service.CompareTargetVersion, Operator: service.CompareOperatorEqual, Version: 1}},
			Success: []*service.TransactionOp{{OpType: service.TransactionOpTypePut, EntityType: "user", ID: 1, Data: []byte("v")}},
			Failure: []*service.TransactionOp{{OpType: service.TransactionOpTypeGet, EntityType: "user", ID: 1}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.KV", tc.method, tc.req); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestKVHandler_AllMethods_ServerError(t *testing.T) {
	h := newKVHandler(newQuicBaaSConn(t, true))
	ctx := context.Background()
	cases := []struct {
		method string
		req    interface{}
	}{
		{"Put", &service.PutRequest{EntityType: "u", ID: 1}},
		{"Get", &service.GetRequest{EntityType: "u", ID: 1}},
		{"Delete", &service.DeleteRequest{EntityType: "u", ID: 1}},
		{"List", &service.ListRequest{EntityType: "u"}},
		{"ListEntityTypes", &service.ListEntityTypesRequest{}},
		{"BatchPut", &service.BatchPutRequest{}},
		{"BatchGet", &service.BatchGetRequest{}},
		{"BatchDelete", &service.BatchDeleteRequest{}},
		{"Transaction", &service.TransactionRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.KV", tc.method, tc.req); err == nil {
				t.Fatalf("%s: expected error", tc.method)
			}
		})
	}
}

func TestKVHandler_UnsupportedMethod(t *testing.T) {
	h := newKVHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KV", "Nope", nil); err == nil {
		t.Fatal("expected unsupported-method error")
	}
}

func TestKVHandler_InvalidRequestTypes(t *testing.T) {
	h := newKVHandler(nil)
	methods := []string{"Put", "Get", "Delete", "List", "ListEntityTypes", "BatchPut", "BatchGet", "BatchDelete", "Transaction"}
	for _, m := range methods {
		if _, err := h.handleRequest(context.Background(), "quicbaas.KV", m, "wrong-type"); err == nil {
			t.Errorf("%s: expected invalid-request-type error", m)
		}
	}
}

// --- Cluster handler ---

func TestClusterHandler_AllMethods_Success(t *testing.T) {
	h := newClusterHandler(newQuicBaaSConn(t, false))
	ctx := context.Background()
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"Status", "Status", nil},
		{"Nodes", "Nodes", nil},
		{"AddNode", "AddNode", &cluster.AddNodeRequest{Address: "addr"}},
		{"RemoveNode", "RemoveNode", &cluster.RemoveNodeRequest{NodeID: "5"}},
		{"Rebalance", "Rebalance", &cluster.RebalanceClusterRequest{Strategy: "even"}},
		{"GetRebalanceStatus", "GetRebalanceStatus", &cluster.GetRebalanceProgressRequest{OperationID: "op1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.Cluster", tc.method, tc.req); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestClusterHandler_ServerError(t *testing.T) {
	h := newClusterHandler(newQuicBaaSConn(t, true))
	ctx := context.Background()
	cases := []struct {
		method string
		req    interface{}
	}{
		{"Status", nil},
		{"Nodes", nil},
		{"AddNode", &cluster.AddNodeRequest{Address: "a"}},
		{"RemoveNode", &cluster.RemoveNodeRequest{NodeID: "5"}},
		{"Rebalance", &cluster.RebalanceClusterRequest{}},
		{"GetRebalanceStatus", &cluster.GetRebalanceProgressRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.Cluster", tc.method, tc.req); err == nil {
				t.Fatalf("%s: expected error", tc.method)
			}
		})
	}
}

func TestClusterHandler_UnsupportedMethod(t *testing.T) {
	h := newClusterHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Cluster", "Nope", nil); err == nil {
		t.Fatal("expected unsupported-method error")
	}
}

func TestClusterHandler_RemoveNode_InvalidNodeID(t *testing.T) {
	h := newClusterHandler(newQuicBaaSConn(t, false))
	if _, err := h.handleRequest(context.Background(), "quicbaas.Cluster", "RemoveNode", &cluster.RemoveNodeRequest{NodeID: "not-a-number"}); err == nil {
		t.Fatal("expected invalid node ID error")
	}
}

func TestClusterHandler_InvalidRequestTypes(t *testing.T) {
	h := newClusterHandler(nil)
	for _, m := range []string{"AddNode", "RemoveNode", "Rebalance", "GetRebalanceStatus"} {
		if _, err := h.handleRequest(context.Background(), "quicbaas.Cluster", m, "wrong"); err == nil {
			t.Errorf("%s: expected invalid-request-type error", m)
		}
	}
}

// --- Health handler ---

func TestHealthHandler_AllMethods_Success(t *testing.T) {
	h := newHealthHandler(newQuicBaaSConn(t, false))
	ctx := context.Background()
	for _, m := range []string{"Ping", "Liveness", "Readiness", "Health"} {
		t.Run(m, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.Health", m, nil); err != nil {
				t.Fatalf("%s: unexpected error: %v", m, err)
			}
		})
	}
}

func TestHealthHandler_ServerError(t *testing.T) {
	h := newHealthHandler(newQuicBaaSConn(t, true))
	ctx := context.Background()
	for _, m := range []string{"Ping", "Liveness", "Readiness", "Health"} {
		t.Run(m, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.Health", m, nil); err == nil {
				t.Fatalf("%s: expected error", m)
			}
		})
	}
}

func TestHealthHandler_UnsupportedMethod(t *testing.T) {
	h := newHealthHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Health", "Nope", nil); err == nil {
		t.Fatal("expected unsupported-method error")
	}
}

// --- RBAC handler ---

func TestRBACHandler_AllMethods_Success(t *testing.T) {
	h := newRBACHandler(newQuicBaaSConn(t, false))
	ctx := context.Background()
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"CreateRole", "CreateRole", map[string]interface{}{"RoleName": "admin", "Permissions": []string{"kv"}}},
		{"DeleteRole", "DeleteRole", map[string]interface{}{"RoleName": "admin"}},
		{"ListRoles", "ListRoles", map[string]interface{}{}},
		{"AssignRole", "AssignRole", map[string]interface{}{"Username": "alice", "RoleName": "admin"}},
		{"RevokeRole", "RevokeRole", map[string]interface{}{"Username": "alice", "RoleName": "admin"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.RBAC", tc.method, tc.req); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestRBACHandler_ServerError(t *testing.T) {
	h := newRBACHandler(newQuicBaaSConn(t, true))
	ctx := context.Background()
	cases := []struct {
		method string
		req    interface{}
	}{
		{"CreateRole", map[string]interface{}{"RoleName": "r"}},
		{"DeleteRole", map[string]interface{}{"RoleName": "r"}},
		{"ListRoles", map[string]interface{}{}},
		{"AssignRole", map[string]interface{}{"Username": "a", "RoleName": "r"}},
		{"RevokeRole", map[string]interface{}{"Username": "a", "RoleName": "r"}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.RBAC", tc.method, tc.req); err == nil {
				t.Fatalf("%s: expected error", tc.method)
			}
		})
	}
}

// TestRBACHandler_UserMethodsAreGone pins the issue #905 removal: quicbaas.RBAC
// carries no user verbs over gRPC. Users are administered over
// quicbaas.iam.v1.IAMService (Identity().IAM().User() on the SDK).
func TestRBACHandler_UserMethodsAreGone(t *testing.T) {
	h := newRBACHandler(newQuicBaaSConn(t, false))
	for _, m := range []string{"CreateUser", "DeleteUser", "ListUsers", "GetUser", "UpdateUser"} {
		if _, err := h.handleRequest(context.Background(), "quicbaas.RBAC", m, map[string]interface{}{}); err == nil {
			t.Errorf("%s: expected unknown-method error", m)
		}
	}
}

func TestRBACHandler_UnknownMethod(t *testing.T) {
	h := newRBACHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.RBAC", "Nope", nil); err == nil {
		t.Fatal("expected unknown-method error")
	}
}

func TestRBACHandler_InvalidRequestTypes(t *testing.T) {
	h := newRBACHandler(nil)
	for _, m := range []string{"CreateRole", "DeleteRole", "AssignRole", "RevokeRole"} {
		if _, err := h.handleRequest(context.Background(), "quicbaas.RBAC", m, 12345); err == nil {
			t.Errorf("%s: expected invalid-request-type error", m)
		}
	}
}

// --- Auth handler ---

func TestAuthHandler_AllMethods_Success(t *testing.T) {
	h := newAuthHandler(newQuicBaaSConn(t, false))
	ctx := context.Background()
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"Logout", "Logout", &service.LogoutRequest{AccessToken: "at"}},
		{"RefreshToken", "RefreshToken", &service.RefreshTokenRequest{RefreshToken: "rt"}},
		{"ValidateToken", "ValidateToken", &service.ValidateTokenRequest{AccessToken: "at"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.Auth", tc.method, tc.req); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestAuthHandler_ServerError(t *testing.T) {
	h := newAuthHandler(newQuicBaaSConn(t, true))
	ctx := context.Background()
	cases := []struct {
		method string
		req    interface{}
	}{
		{"Logout", &service.LogoutRequest{AccessToken: "t"}},
		{"RefreshToken", &service.RefreshTokenRequest{RefreshToken: "t"}},
		{"ValidateToken", &service.ValidateTokenRequest{AccessToken: "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, "quicbaas.Auth", tc.method, tc.req); err == nil {
				t.Fatalf("%s: expected error", tc.method)
			}
		})
	}
}

func TestAuthHandler_UnsupportedMethod(t *testing.T) {
	h := newAuthHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Auth", "Nope", nil); err == nil {
		t.Fatal("expected unsupported-method error")
	}
}

func TestAuthHandler_InvalidRequestTypes(t *testing.T) {
	h := newAuthHandler(nil)
	for _, m := range []string{"Logout", "RefreshToken", "ValidateToken"} {
		if _, err := h.handleRequest(context.Background(), "quicbaas.Auth", m, "wrong"); err == nil {
			t.Errorf("%s: expected invalid-request-type error", m)
		}
	}
}
