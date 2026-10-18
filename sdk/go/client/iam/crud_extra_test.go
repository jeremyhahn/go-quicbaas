// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package iam

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func boom() error { return errors.NewConnection("op", "boom") }

// ---------------------------------------------------------------------------
// User CRUD
// ---------------------------------------------------------------------------

func TestUserClient_Create_Branches(t *testing.T) {
	ctx := context.Background()
	req := &CreateUserRequest{Username: "alice", Email: "a@b.com"}

	t.Run("nil ctx", func(t *testing.T) {
		c := NewUserClient(&mockTransport{})
		if _, err := c.Create(nil, "t1", req); err == nil {
			t.Error("expected error for nil ctx")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: errFn(boom())})
		if _, err := c.Create(ctx, "t1", req); err == nil {
			t.Error("expected transport error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(nil)})
		if _, err := c.Create(ctx, "t1", req); err == nil {
			t.Error("expected error for nil response")
		}
	})
	t.Run("invalid response type", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(42)})
		if _, err := c.Create(ctx, "t1", req); err == nil {
			t.Error("expected error for invalid type")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"id": "u1"})})
		got, err := c.Create(ctx, "t1", req)
		if err != nil || got.ID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestUserClient_Get_Branches(t *testing.T) {
	ctx := context.Background()
	t.Run("nil ctx", func(t *testing.T) {
		c := NewUserClient(&mockTransport{})
		if _, err := c.Get(nil, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: errFn(boom())})
		if _, err := c.Get(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response not found", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(nil)})
		if _, err := c.Get(ctx, "t1", "u1"); err == nil {
			t.Error("expected not found")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(42)})
		if _, err := c.Get(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"id": "u1"})})
		got, err := c.Get(ctx, "t1", "u1")
		if err != nil || got.ID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestUserClient_GetByEmail_Branches(t *testing.T) {
	ctx := context.Background()
	t.Run("transport error", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: errFn(boom())})
		if _, err := c.GetByEmail(ctx, "t1", "a@b.com"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"id": "u1", "email": "a@b.com"})})
		got, err := c.GetByEmail(ctx, "t1", "a@b.com")
		if err != nil || got.Email != "a@b.com" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestUserClient_Update_Branches(t *testing.T) {
	ctx := context.Background()
	req := &UpdateUserRequest{DisplayName: "New"}
	t.Run("transport error", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: errFn(boom())})
		if _, err := c.Update(ctx, "t1", "u1", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(nil)})
		if _, err := c.Update(ctx, "t1", "u1", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(42)})
		if _, err := c.Update(ctx, "t1", "u1", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"id": "u1"})})
		got, err := c.Update(ctx, "t1", "u1", req)
		if err != nil || got.ID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestUserClient_Delete_Branches(t *testing.T) {
	ctx := context.Background()
	t.Run("transport error", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: errFn(boom())})
		if err := c.Delete(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("failure response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"success": false})})
		if err := c.Delete(ctx, "t1", "u1"); err == nil {
			t.Error("expected failure error")
		}
	})
	t.Run("success", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(nil)})
		if err := c.Delete(ctx, "t1", "u1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestUserClient_List_Branches(t *testing.T) {
	ctx := context.Background()
	t.Run("nil ctx", func(t *testing.T) {
		c := NewUserClient(&mockTransport{})
		if _, err := c.List(nil, "t1", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: errFn(boom())})
		if _, err := c.List(ctx, "t1", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(nil)})
		got, err := c.List(ctx, "t1", nil)
		if err != nil || len(got.Users) != 0 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := NewUserClient(&mockTransport{requestFunc: okFn(42)})
		if _, err := c.List(ctx, "t1", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response with users and pagination", func(t *testing.T) {
		resp := map[string]interface{}{
			"users": []interface{}{
				map[string]interface{}{"id": "u1"},
				map[string]interface{}{"id": "u2"},
			},
			"pagination": map[string]interface{}{
				"total_count":     float64(2),
				"page_size":       float64(10),
				"next_page_token": "tok",
			},
			"limit": float64(10),
			"total": float64(2),
		}
		c := NewUserClient(&mockTransport{requestFunc: okFn(resp)})
		got, err := c.List(ctx, "t1", &ListUsersRequest{})
		if err != nil || len(got.Users) != 2 || got.Pagination.NextPageToken != "tok" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestUserClient_MemberOps_Branches(t *testing.T) {
	ctx := context.Background()
	ok := &mockTransport{requestFunc: okFn(map[string]interface{}{"success": true})}
	fail := &mockTransport{requestFunc: okFn(map[string]interface{}{"success": false})}
	errT := &mockTransport{requestFunc: errFn(boom())}

	t.Run("AssignRole success/fail/error/nilctx", func(t *testing.T) {
		if err := NewUserClient(ok).AssignRole(ctx, "t", "u", "r"); err != nil {
			t.Error(err)
		}
		if err := NewUserClient(fail).AssignRole(ctx, "t", "u", "r"); err == nil {
			t.Error("expected fail")
		}
		if err := NewUserClient(errT).AssignRole(ctx, "t", "u", "r"); err == nil {
			t.Error("expected error")
		}
		if err := NewUserClient(ok).AssignRole(nil, "t", "u", "r"); err == nil {
			t.Error("expected nil ctx error")
		}
	})
	t.Run("RevokeRole", func(t *testing.T) {
		if err := NewUserClient(ok).RevokeRole(ctx, "t", "u", "r"); err != nil {
			t.Error(err)
		}
		if err := NewUserClient(fail).RevokeRole(ctx, "t", "u", "r"); err == nil {
			t.Error("expected fail")
		}
		if err := NewUserClient(errT).RevokeRole(ctx, "t", "u", "r"); err == nil {
			t.Error("expected error")
		}
		if err := NewUserClient(ok).RevokeRole(nil, "t", "u", "r"); err == nil {
			t.Error("expected nil ctx error")
		}
	})
}

func TestUserClient_ApproveReject(t *testing.T) {
	ctx := context.Background()
	ok := &mockTransport{requestFunc: okFn(map[string]interface{}{"success": true})}
	fail := &mockTransport{requestFunc: okFn(map[string]interface{}{"success": false})}
	errT := &mockTransport{requestFunc: errFn(boom())}

	t.Run("Approve validation", func(t *testing.T) {
		c := NewUserClient(ok)
		if err := c.Approve(ctx, "", "u"); err == nil {
			t.Error("expected tenant error")
		}
		if err := c.Approve(ctx, "t", ""); err == nil {
			t.Error("expected user error")
		}
		if err := c.Approve(nil, "t", "u"); err == nil {
			t.Error("expected ctx error")
		}
	})
	t.Run("Approve success/fail/error", func(t *testing.T) {
		if err := NewUserClient(ok).Approve(ctx, "t", "u"); err != nil {
			t.Error(err)
		}
		if err := NewUserClient(fail).Approve(ctx, "t", "u"); err == nil {
			t.Error("expected fail")
		}
		if err := NewUserClient(errT).Approve(ctx, "t", "u"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("Reject validation", func(t *testing.T) {
		c := NewUserClient(ok)
		if err := c.Reject(ctx, "", "u", "r"); err == nil {
			t.Error("expected tenant error")
		}
		if err := c.Reject(ctx, "t", "", "r"); err == nil {
			t.Error("expected user error")
		}
		if err := c.Reject(nil, "t", "u", "r"); err == nil {
			t.Error("expected ctx error")
		}
	})
	t.Run("Reject success/fail/error", func(t *testing.T) {
		if err := NewUserClient(ok).Reject(ctx, "t", "u", "reason"); err != nil {
			t.Error(err)
		}
		if err := NewUserClient(fail).Reject(ctx, "t", "u", "reason"); err == nil {
			t.Error("expected fail")
		}
		if err := NewUserClient(errT).Reject(ctx, "t", "u", "reason"); err == nil {
			t.Error("expected error")
		}
	})
}

// ---------------------------------------------------------------------------
// Group CRUD
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Role CRUD
// ---------------------------------------------------------------------------

func TestRoleClient_CRUD_Branches(t *testing.T) {
	ctx := context.Background()
	errT := &mockTransport{requestFunc: errFn(boom())}

	t.Run("Get", func(t *testing.T) {
		if _, err := NewRoleClient(&mockTransport{}).Get(nil, "t", "r"); err == nil {
			t.Error("expected nil ctx error")
		}
		if _, err := NewRoleClient(errT).Get(ctx, "t", "r"); err == nil {
			t.Error("expected transport error")
		}
		if _, err := NewRoleClient(&mockTransport{requestFunc: okFn(42)}).Get(ctx, "t", "r"); err == nil {
			t.Error("expected invalid type")
		}
		got, err := NewRoleClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"id": "r1"})}).Get(ctx, "t", "r")
		if err != nil || got.ID != "r1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("Update", func(t *testing.T) {
		req := &UpdateRoleRequest{DisplayName: "X"}
		if _, err := NewRoleClient(&mockTransport{}).Update(nil, "t", "r", req); err == nil {
			t.Error("expected nil ctx error")
		}
		if _, err := NewRoleClient(errT).Update(ctx, "t", "r", req); err == nil {
			t.Error("expected transport error")
		}
		if _, err := NewRoleClient(&mockTransport{requestFunc: okFn(42)}).Update(ctx, "t", "r", req); err == nil {
			t.Error("expected invalid type")
		}
		got, err := NewRoleClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"id": "r1"})}).Update(ctx, "t", "r", req)
		if err != nil || got.ID != "r1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("Delete", func(t *testing.T) {
		if err := NewRoleClient(&mockTransport{}).Delete(nil, "t", "r"); err == nil {
			t.Error("expected nil ctx error")
		}
		if err := NewRoleClient(errT).Delete(ctx, "t", "r"); err == nil {
			t.Error("expected transport error")
		}
		if err := NewRoleClient(&mockTransport{requestFunc: okFn(nil)}).Delete(ctx, "t", "r"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if err := NewRoleClient(&mockTransport{requestFunc: okFn(map[string]interface{}{})}).Delete(ctx, "t", "r"); err != nil {
			t.Errorf("map response should succeed: %v", err)
		}
		if err := NewRoleClient(&mockTransport{requestFunc: okFn(42)}).Delete(ctx, "t", "r"); err == nil {
			t.Error("expected invalid type error")
		}
	})
}

func TestRoleClient_List_Branches(t *testing.T) {
	ctx := context.Background()
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := NewRoleClient(&mockTransport{}).List(nil, "t", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		if _, err := NewRoleClient(&mockTransport{requestFunc: errFn(boom())}).List(ctx, "t", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		got, err := NewRoleClient(&mockTransport{requestFunc: okFn(nil)}).List(ctx, "t", nil)
		if err != nil || len(got.Roles) != 0 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		if _, err := NewRoleClient(&mockTransport{requestFunc: okFn(42)}).List(ctx, "t", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map with roles", func(t *testing.T) {
		resp := map[string]interface{}{
			"roles": []interface{}{map[string]interface{}{"id": "r1"}},
			"pagination": map[string]interface{}{
				"total_count":     float64(1),
				"page_size":       float64(5),
				"next_page_token": "tok",
			},
			"limit": float64(5),
			"total": float64(1),
		}
		got, err := NewRoleClient(&mockTransport{requestFunc: okFn(resp)}).List(ctx, "t", &ListRolesRequest{})
		if err != nil || len(got.Roles) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

// ---------------------------------------------------------------------------
// Policy CRUD
// ---------------------------------------------------------------------------

func TestPolicyClient_GetUpdateDelete_Branches(t *testing.T) {
	ctx := context.Background()
	errT := &mockTransport{requestFunc: errFn(boom())}

	t.Run("Get", func(t *testing.T) {
		if _, err := NewPolicyClient(errT).Get(ctx, "t", "p"); err == nil {
			t.Error("expected transport error")
		}
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn(42)}).Get(ctx, "t", "p"); err == nil {
			t.Error("expected invalid type")
		}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"name": "p1"})}).Get(ctx, "t", "p")
		if err != nil || got.Name != "p1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("Update", func(t *testing.T) {
		desc := "d"
		req := &UpdatePolicyRequest{Description: &desc}
		if _, err := NewPolicyClient(errT).Update(ctx, "t", "p", req); err == nil {
			t.Error("expected transport error")
		}
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn(42)}).Update(ctx, "t", "p", req); err == nil {
			t.Error("expected invalid type")
		}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"name": "p1"})}).Update(ctx, "t", "p", req)
		if err != nil || got.Name != "p1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("Delete", func(t *testing.T) {
		if err := NewPolicyClient(errT).Delete(ctx, "t", "p"); err == nil {
			t.Error("expected transport error")
		}
		if err := NewPolicyClient(&mockTransport{requestFunc: okFn(nil)}).Delete(ctx, "t", "p"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if err := NewPolicyClient(&mockTransport{requestFunc: okFn(map[string]interface{}{"success": true})}).Delete(ctx, "t", "p"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestPolicyClient_List_Branches(t *testing.T) {
	ctx := context.Background()
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{}).List(nil, "t", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: errFn(boom())}).List(ctx, "t", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(nil)}).List(ctx, "t", nil)
		if err != nil || len(got.Policies) != 0 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn(42)}).List(ctx, "t", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map with policies", func(t *testing.T) {
		resp := map[string]interface{}{
			"policies":    []interface{}{map[string]interface{}{"name": "p1"}},
			"next_cursor": "cur",
			"has_more":    true,
			"total_count": float64(1),
			"limit":       float64(10),
		}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(resp)}).List(ctx, "t", &ListPoliciesRequest{})
		if err != nil || len(got.Policies) != 1 || got.NextCursor != "cur" || !got.HasMore {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestPolicyClient_Validate_Branches(t *testing.T) {
	ctx := context.Background()
	policy := &Policy{Name: "p1"}
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{}).Validate(nil, "t", policy); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: errFn(boom())}).Validate(ctx, "t", policy); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn(nil)}).Validate(ctx, "t", policy); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn(42)}).Validate(ctx, "t", policy); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map with error objects", func(t *testing.T) {
		resp := map[string]interface{}{
			"valid": false,
			"errors": []interface{}{
				map[string]interface{}{"field": "name", "code": "E1", "message": "bad"},
			},
		}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(resp)}).Validate(ctx, "t", policy)
		if err != nil || got.Valid || len(got.Errors) != 1 || got.Errors[0].Field != "name" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map with string errors", func(t *testing.T) {
		resp := map[string]interface{}{
			"valid":  true,
			"errors": []string{"warn1"},
		}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(resp)}).Validate(ctx, "t", policy)
		if err != nil || len(got.Errors) != 1 || got.Errors[0].Message != "warn1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestPolicyClient_Simulate_Branches(t *testing.T) {
	ctx := context.Background()
	req := &SimulateRequest{Subject: Subject{ID: "u1"}, Action: "read", Resource: "doc"}
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{}).Simulate(nil, "t", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: errFn(boom())}).Simulate(ctx, "t", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn(nil)}).Simulate(ctx, "t", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn("nope")}).Simulate(ctx, "t", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		resp := map[string]interface{}{
			"allowed":            true,
			"effect":             float64(int(EffectAllow)),
			"evaluation_time_ns": float64(50),
			"matched_policies":   []interface{}{"p1"},
			"matched_rules":      []interface{}{"r1"},
		}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(resp)}).Simulate(ctx, "t", req)
		if err != nil || !got.Allowed || len(got.MatchedPolicies) != 1 || len(got.MatchedRules) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestPolicyClient_TestAndEffectivePerms_Branches(t *testing.T) {
	ctx := context.Background()
	t.Run("Test transport error", func(t *testing.T) {
		req := &TestPolicyRequest{PolicyName: "p", TestCases: []TestCase{{Name: "tc"}}}
		if _, err := NewPolicyClient(&mockTransport{requestFunc: errFn(boom())}).Test(ctx, "t", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("Test nil response", func(t *testing.T) {
		req := &TestPolicyRequest{PolicyName: "p", TestCases: []TestCase{{Name: "tc"}}}
		if _, err := NewPolicyClient(&mockTransport{requestFunc: okFn(nil)}).Test(ctx, "t", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("Test map response", func(t *testing.T) {
		req := &TestPolicyRequest{PolicyName: "p", TestCases: []TestCase{{Name: "tc"}}}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(map[string]interface{}{})}).Test(ctx, "t", req)
		if err != nil || got == nil {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("EffectivePerms transport error", func(t *testing.T) {
		req := &EffectivePermissionsRequest{Subject: Subject{ID: "u1"}}
		if _, err := NewPolicyClient(&mockTransport{requestFunc: errFn(boom())}).GetEffectivePermissions(ctx, "t", req); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("EffectivePerms map response", func(t *testing.T) {
		req := &EffectivePermissionsRequest{Subject: Subject{ID: "u1"}}
		got, err := NewPolicyClient(&mockTransport{requestFunc: okFn(map[string]interface{}{})}).GetEffectivePermissions(ctx, "t", req)
		if err != nil || got == nil {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}
