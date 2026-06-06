// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package iam

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// newFedClient builds an *client (the concrete type) wired with a request func.
func newFedClient(fn func(ctx context.Context, service, method string, req interface{}) (interface{}, error)) Client {
	return NewClient(&mockTransport{requestFunc: fn})
}

func errFn(err error) func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	return func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		return nil, err
	}
}

func okFn(resp interface{}) func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	return func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		return resp, nil
	}
}

func TestClient_CreateProvider(t *testing.T) {
	ctx := context.Background()

	t.Run("nil options", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.CreateProvider(ctx, nil); err == nil {
			t.Error("expected error for nil options")
		}
	})
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.CreateProvider(ctx, &cli.CreateProviderOptions{}); err == nil {
			t.Error("expected error for missing tenant")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		_, err := c.CreateProvider(ctx, &cli.CreateProviderOptions{TenantID: "t1"})
		if err == nil {
			t.Error("expected transport error")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		want := &cli.Provider{ID: "p1", TenantID: "t1"}
		c := newFedClient(okFn(want))
		got, err := c.CreateProvider(ctx, &cli.CreateProviderOptions{TenantID: "t1"})
		if err != nil || got.ID != "p1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"id": "p2", "tenant_id": "t1"}))
		got, err := c.CreateProvider(ctx, &cli.CreateProviderOptions{TenantID: "t1"})
		if err != nil || got.ID != "p2" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_GetProvider(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GetProvider(ctx, "", "p1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing provider", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GetProvider(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewNotFound("op", "nope")))
		if _, err := c.GetProvider(ctx, "t1", "p1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newFedClient(okFn(&cli.Provider{ID: "p1"}))
		got, err := c.GetProvider(ctx, "t1", "p1")
		if err != nil || got.ID != "p1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"id": "p1"}))
		got, err := c.GetProvider(ctx, "t1", "p1")
		if err != nil || got.ID != "p1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_UpdateProvider(t *testing.T) {
	ctx := context.Background()
	t.Run("nil options", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.UpdateProvider(ctx, nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.UpdateProvider(ctx, &cli.UpdateProviderOptions{ProviderID: "p1"}); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing provider", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.UpdateProvider(ctx, &cli.UpdateProviderOptions{TenantID: "t1"}); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.UpdateProvider(ctx, &cli.UpdateProviderOptions{TenantID: "t1", ProviderID: "p1"}); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newFedClient(okFn(&cli.Provider{ID: "p1"}))
		got, err := c.UpdateProvider(ctx, &cli.UpdateProviderOptions{TenantID: "t1", ProviderID: "p1"})
		if err != nil || got.ID != "p1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"id": "p9"}))
		got, err := c.UpdateProvider(ctx, &cli.UpdateProviderOptions{TenantID: "t1", ProviderID: "p1"})
		if err != nil || got.ID != "p9" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_DeleteProvider(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.DeleteProvider(ctx, "", "p1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing provider", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.DeleteProvider(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if err := c.DeleteProvider(ctx, "t1", "p1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("success", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.DeleteProvider(ctx, "t1", "p1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestClient_ListProviders(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.ListProviders(ctx, ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ListProviders(ctx, "t1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed slice", func(t *testing.T) {
		c := newFedClient(okFn([]*cli.Provider{{ID: "p1"}}))
		got, err := c.ListProviders(ctx, "t1")
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn([]interface{}{map[string]interface{}{"id": "p1"}}))
		got, err := c.ListProviders(ctx, "t1")
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_LinkIdentity(t *testing.T) {
	ctx := context.Background()
	t.Run("nil options", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.LinkIdentity(ctx, nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.LinkIdentity(ctx, &cli.LinkIdentityOptions{UserID: "u1"}); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.LinkIdentity(ctx, &cli.LinkIdentityOptions{TenantID: "t1"}); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.LinkIdentity(ctx, &cli.LinkIdentityOptions{TenantID: "t1", UserID: "u1"}); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newFedClient(okFn(&cli.FederatedIdentity{ID: "fi1"}))
		got, err := c.LinkIdentity(ctx, &cli.LinkIdentityOptions{TenantID: "t1", UserID: "u1"})
		if err != nil || got.ID != "fi1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"id": "fi2"}))
		got, err := c.LinkIdentity(ctx, &cli.LinkIdentityOptions{TenantID: "t1", UserID: "u1"})
		if err != nil || got.ID != "fi2" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_UnlinkIdentity(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.UnlinkIdentity(ctx, "", "u1", "p1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.UnlinkIdentity(ctx, "t1", "", "p1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing provider", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.UnlinkIdentity(ctx, "t1", "u1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if err := c.UnlinkIdentity(ctx, "t1", "u1", "p1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("success", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.UnlinkIdentity(ctx, "t1", "u1", "p1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestClient_ListUserIdentities(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.ListUserIdentities(ctx, "", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.ListUserIdentities(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.ListUserIdentities(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed slice", func(t *testing.T) {
		c := newFedClient(okFn([]*cli.FederatedIdentity{{ID: "fi1"}}))
		got, err := c.ListUserIdentities(ctx, "t1", "u1")
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("array response", func(t *testing.T) {
		c := newFedClient(okFn([]interface{}{map[string]interface{}{"id": "fi1"}}))
		got, err := c.ListUserIdentities(ctx, "t1", "u1")
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("identities key wrapper", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{
			"identities": []interface{}{map[string]interface{}{"id": "fi1"}},
		}))
		got, err := c.ListUserIdentities(ctx, "t1", "u1")
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("links key wrapper", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{
			"links": []interface{}{map[string]interface{}{"id": "fi2"}},
		}))
		got, err := c.ListUserIdentities(ctx, "t1", "u1")
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("empty map fallback returns empty", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{}))
		got, err := c.ListUserIdentities(ctx, "t1", "u1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty, got %+v", got)
		}
	})
}

func TestClient_GetMFAStatus(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GetMFAStatus(ctx, "", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GetMFAStatus(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.GetMFAStatus(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newFedClient(okFn(&cli.MFAStatus{Enabled: true}))
		got, err := c.GetMFAStatus(ctx, "t1", "u1")
		if err != nil || !got.Enabled {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"enabled": true}))
		got, err := c.GetMFAStatus(ctx, "t1", "u1")
		if err != nil || !got.Enabled {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_BeginMFAEnrollment(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.BeginMFAEnrollment(ctx, "", "u1", "totp"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.BeginMFAEnrollment(ctx, "t1", "", "totp"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.BeginMFAEnrollment(ctx, "t1", "u1", "totp"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newFedClient(okFn(&cli.MFAEnrollment{EnrollmentID: "e1"}))
		got, err := c.BeginMFAEnrollment(ctx, "t1", "u1", "totp")
		if err != nil || got.EnrollmentID != "e1" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"enrollment_id": "e2"}))
		got, err := c.BeginMFAEnrollment(ctx, "t1", "u1", "totp")
		if err != nil || got.EnrollmentID != "e2" {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_CompleteMFAEnrollment(t *testing.T) {
	ctx := context.Background()
	t.Run("missing enrollment", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.CompleteMFAEnrollment(ctx, "", "123456"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing code", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.CompleteMFAEnrollment(ctx, "e1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if err := c.CompleteMFAEnrollment(ctx, "e1", "123456"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("success", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.CompleteMFAEnrollment(ctx, "e1", "123456"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestClient_DisableMFA(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.DisableMFA(ctx, "", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.DisableMFA(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if err := c.DisableMFA(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("success", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if err := c.DisableMFA(ctx, "t1", "u1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestClient_GetBackupCodeStatus(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GetBackupCodeStatus(ctx, "", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GetBackupCodeStatus(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.GetBackupCodeStatus(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("typed response", func(t *testing.T) {
		c := newFedClient(okFn(&cli.BackupCodeStatus{RemainingCodes: 5}))
		got, err := c.GetBackupCodeStatus(ctx, "t1", "u1")
		if err != nil || got.RemainingCodes != 5 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map response", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"remaining_codes": float64(3)}))
		got, err := c.GetBackupCodeStatus(ctx, "t1", "u1")
		if err != nil || got.RemainingCodes != 3 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
}

func TestClient_GenerateBackupCodes(t *testing.T) {
	ctx := context.Background()
	t.Run("missing tenant", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GenerateBackupCodes(ctx, "", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("missing user", func(t *testing.T) {
		c := newFedClient(okFn(nil))
		if _, err := c.GenerateBackupCodes(ctx, "t1", ""); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := newFedClient(errFn(errors.NewConnection("op", "boom")))
		if _, err := c.GenerateBackupCodes(ctx, "t1", "u1"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("string slice", func(t *testing.T) {
		c := newFedClient(okFn([]string{"a", "b"}))
		got, err := c.GenerateBackupCodes(ctx, "t1", "u1")
		if err != nil || len(got) != 2 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map codes string slice", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"codes": []string{"x"}}))
		got, err := c.GenerateBackupCodes(ctx, "t1", "u1")
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("map codes iface slice", func(t *testing.T) {
		c := newFedClient(okFn(map[string]interface{}{"codes": []interface{}{"x", "y", 1}}))
		got, err := c.GenerateBackupCodes(ctx, "t1", "u1")
		if err != nil || len(got) != 2 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("iface slice direct", func(t *testing.T) {
		c := newFedClient(okFn([]interface{}{"a", 1, "b"}))
		got, err := c.GenerateBackupCodes(ctx, "t1", "u1")
		if err != nil || len(got) != 2 {
			t.Errorf("got %+v err %v", got, err)
		}
	})
	t.Run("unexpected type", func(t *testing.T) {
		c := newFedClient(okFn(42))
		if _, err := c.GenerateBackupCodes(ctx, "t1", "u1"); err == nil {
			t.Error("expected error for unexpected type")
		}
	})
}

func TestClient_Invite(t *testing.T) {
	c := newFedClient(okFn(nil))
	if c.Invite() == nil {
		t.Error("Invite() should return non-nil")
	}
}

func TestClient_Close(t *testing.T) {
	c := newFedClient(okFn(nil))
	if err := c.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
