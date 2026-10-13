// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package iam

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func newInvite(fn func(ctx context.Context, service, method string, req interface{}) (interface{}, error)) InviteClient {
	return newInviteClient(&mockTransport{requestFunc: fn})
}

// TestInviteClient_RequiresTenant refuses every verb without a tenant before
// reaching the transport: a token only resolves inside its issuing tenant.
func TestInviteClient_RequiresTenant(t *testing.T) {
	ctx := context.Background()
	c := newInvite(func(context.Context, string, string, interface{}) (interface{}, error) {
		t.Fatal("transport reached without a tenant")
		return nil, nil
	})
	cred := map[string]interface{}{"id": "cred"}

	if _, err := c.GetInvite(ctx, "", "tok"); err == nil {
		t.Error("GetInvite: expected error")
	}
	if _, err := c.ActivateFIDO2Begin(ctx, "", "tok"); err == nil {
		t.Error("ActivateFIDO2Begin: expected error")
	}
	if _, err := c.ActivateFIDO2Finish(ctx, "", "tok", cred); err == nil {
		t.Error("ActivateFIDO2Finish: expected error")
	}
	if _, err := c.ActivatePIV(ctx, "", "tok", "pem"); err == nil {
		t.Error("ActivatePIV: expected error")
	}
}

// TestInviteClient_SendsScope sends the tenant and token on the wire under
// the names every transport reads.
func TestInviteClient_SendsScope(t *testing.T) {
	var gotMethod string
	var gotReq interface{}
	c := newInvite(func(_ context.Context, service, method string, req interface{}) (interface{}, error) {
		if service != inviteServiceName {
			t.Errorf("service = %q, want %q", service, inviteServiceName)
		}
		gotMethod, gotReq = method, req
		return map[string]interface{}{"user_id": "u1"}, nil
	})

	if _, err := c.ActivatePIV(context.Background(), "t1", "tok", "pem"); err != nil {
		t.Fatalf("ActivatePIV: %v", err)
	}
	req, ok := gotReq.(*activatePIVRequest)
	if gotMethod != "ActivatePIV" || !ok {
		t.Fatalf("method %q request %T, want ActivatePIV *activatePIVRequest", gotMethod, gotReq)
	}
	if req.TenantID != "t1" || req.Token != "tok" || req.CertPEM != "pem" {
		t.Errorf("request = %+v", req)
	}
}

func TestInviteClient_GetInvite(t *testing.T) {
	ctx := context.Background()
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.GetInvite(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.GetInvite(ctx, "t1", "tok"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.GetInvite(ctx, "t1", "tok"); err == nil {
			t.Error("expected error for nil response")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newInvite(okFn(&InviteMetadata{UserID: "u1"}))
		got, err := c.GetInvite(ctx, "t1", "tok")
		if err != nil || got.UserID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newInvite(okFn(map[string]interface{}{
			"user_id":      "u1",
			"email":        "a@b.com",
			"auth_methods": []interface{}{"fido2", "piv"},
		}))
		got, err := c.GetInvite(ctx, "t1", "tok")
		if err != nil || got.UserID != "u1" || len(got.AuthMethods) != 2 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestInviteClient_ActivateFIDO2Begin(t *testing.T) {
	ctx := context.Background()
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivateFIDO2Begin(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ActivateFIDO2Begin(ctx, "t1", "tok"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newInvite(okFn(map[string]interface{}{"publicKey": "opts"}))
		got, err := c.ActivateFIDO2Begin(ctx, "t1", "tok")
		if err != nil || got["publicKey"] != "opts" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("unexpected response type", func(t *testing.T) {
		c := newInvite(okFn("not-a-map"))
		if _, err := c.ActivateFIDO2Begin(ctx, "t1", "tok"); err == nil {
			t.Error("expected error")
		}
	})
}

func TestInviteClient_ActivateFIDO2Finish(t *testing.T) {
	ctx := context.Background()
	cred := map[string]interface{}{"id": "cred"}
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivateFIDO2Finish(ctx, "t1", "", cred); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil credential", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivateFIDO2Finish(ctx, "t1", "tok", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ActivateFIDO2Finish(ctx, "t1", "tok", cred); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newInvite(okFn(map[string]interface{}{"user_id": "u1", "message": "ok"}))
		got, err := c.ActivateFIDO2Finish(ctx, "t1", "tok", cred)
		if err != nil || got.UserID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestInviteClient_ActivatePIV(t *testing.T) {
	ctx := context.Background()
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivatePIV(ctx, "t1", "", "pem"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("empty pem", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivatePIV(ctx, "t1", "tok", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ActivatePIV(ctx, "t1", "tok", "pem"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivatePIV(ctx, "t1", "tok", "pem"); err == nil {
			t.Error("expected error for nil response")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newInvite(okFn(&ActivationResponse{UserID: "u1", Message: "ok"}))
		got, err := c.ActivatePIV(ctx, "t1", "tok", "pem")
		if err != nil || got.UserID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newInvite(okFn(map[string]interface{}{"user_id": "u2", "message": "done"}))
		got, err := c.ActivatePIV(ctx, "t1", "tok", "pem")
		if err != nil || got.UserID != "u2" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}
