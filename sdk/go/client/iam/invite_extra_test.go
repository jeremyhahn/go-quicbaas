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

func TestInviteClient_GetInvite(t *testing.T) {
	ctx := context.Background()
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.GetInvite(ctx, ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.GetInvite(ctx, "tok"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.GetInvite(ctx, "tok"); err == nil {
			t.Error("expected error for nil response")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newInvite(okFn(&InviteMetadata{UserID: "u1"}))
		got, err := c.GetInvite(ctx, "tok")
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
		got, err := c.GetInvite(ctx, "tok")
		if err != nil || got.UserID != "u1" || len(got.AuthMethods) != 2 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestInviteClient_ActivateFIDO2Begin(t *testing.T) {
	ctx := context.Background()
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivateFIDO2Begin(ctx, ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ActivateFIDO2Begin(ctx, "tok"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newInvite(okFn(map[string]interface{}{"publicKey": "opts"}))
		got, err := c.ActivateFIDO2Begin(ctx, "tok")
		if err != nil || got["publicKey"] != "opts" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("unexpected response type", func(t *testing.T) {
		c := newInvite(okFn("not-a-map"))
		if _, err := c.ActivateFIDO2Begin(ctx, "tok"); err == nil {
			t.Error("expected error")
		}
	})
}

func TestInviteClient_ActivateFIDO2Finish(t *testing.T) {
	ctx := context.Background()
	cred := map[string]interface{}{"id": "cred"}
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivateFIDO2Finish(ctx, "", cred); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil credential", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivateFIDO2Finish(ctx, "tok", nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ActivateFIDO2Finish(ctx, "tok", cred); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newInvite(okFn(map[string]interface{}{"user_id": "u1", "message": "ok"}))
		got, err := c.ActivateFIDO2Finish(ctx, "tok", cred)
		if err != nil || got.UserID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestInviteClient_ActivatePIV(t *testing.T) {
	ctx := context.Background()
	t.Run("empty token", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivatePIV(ctx, "", "pem"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("empty pem", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivatePIV(ctx, "tok", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newInvite(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ActivatePIV(ctx, "tok", "pem"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := newInvite(okFn(nil))
		if _, err := c.ActivatePIV(ctx, "tok", "pem"); err == nil {
			t.Error("expected error for nil response")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newInvite(okFn(&ActivationResponse{UserID: "u1", Message: "ok"}))
		got, err := c.ActivatePIV(ctx, "tok", "pem")
		if err != nil || got.UserID != "u1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newInvite(okFn(map[string]interface{}{"user_id": "u2", "message": "done"}))
		got, err := c.ActivatePIV(ctx, "tok", "pem")
		if err != nil || got.UserID != "u2" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}
