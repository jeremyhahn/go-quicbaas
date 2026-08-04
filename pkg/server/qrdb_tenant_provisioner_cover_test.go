// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"errors"
	"testing"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// fakeServiceTenantServicer overrides only CreateTenant/DeleteTenant; the rest
// of the TenantServicer surface is the embedded nil interface and must not be
// invoked by the provisioner adapter.
type fakeServiceTenantServicer struct {
	qrdbsdk.ServiceTenantServicer

	createErr error
	deleteErr error

	lastCreateID   string
	lastCreateName string
	lastCreateIso  qrdbtenant.IsolationType
	lastDeleteID   string
}

func (f *fakeServiceTenantServicer) CreateTenant(_ context.Context, id, name string, iso qrdbtenant.IsolationType, _ ...qrdbtenant.CreateTenantOption) (*qrdbtenant.TenantInfo, error) {
	f.lastCreateID = id
	f.lastCreateName = name
	f.lastCreateIso = iso
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &qrdbtenant.TenantInfo{ID: id, Name: name}, nil
}

func (f *fakeServiceTenantServicer) DeleteTenant(_ context.Context, id string) error {
	f.lastDeleteID = id
	return f.deleteErr
}

func TestQRDBTenantProvisionerAdapter_CreateTenant(t *testing.T) {
	ctx := context.Background()

	t.Run("success maps isolation type", func(t *testing.T) {
		svc := &fakeServiceTenantServicer{}
		adapter := newQRDBTenantProvisionerAdapter(svc)
		if err := adapter.CreateTenant(ctx, "acme", "Acme Corp", "barrier"); err != nil {
			t.Fatalf("CreateTenant: %v", err)
		}
		if svc.lastCreateID != "acme" || svc.lastCreateName != "Acme Corp" {
			t.Fatalf("create args not propagated: %+v", svc)
		}
		if svc.lastCreateIso != qrdbtenant.IsolationType("barrier") {
			t.Fatalf("isolation type = %q, want barrier", svc.lastCreateIso)
		}
	})

	t.Run("error propagated", func(t *testing.T) {
		want := errors.New("create failed")
		svc := &fakeServiceTenantServicer{createErr: want}
		adapter := newQRDBTenantProvisionerAdapter(svc)
		if err := adapter.CreateTenant(ctx, "x", "y", ""); !errors.Is(err, want) {
			t.Fatalf("expected %v, got %v", want, err)
		}
	})
}

func TestQRDBTenantProvisionerAdapter_DeleteTenant(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		svc := &fakeServiceTenantServicer{}
		adapter := newQRDBTenantProvisionerAdapter(svc)
		if err := adapter.DeleteTenant(ctx, "acme"); err != nil {
			t.Fatalf("DeleteTenant: %v", err)
		}
		if svc.lastDeleteID != "acme" {
			t.Fatalf("delete id not propagated: %q", svc.lastDeleteID)
		}
	})

	t.Run("error propagated", func(t *testing.T) {
		want := errors.New("delete failed")
		svc := &fakeServiceTenantServicer{deleteErr: want}
		adapter := newQRDBTenantProvisionerAdapter(svc)
		if err := adapter.DeleteTenant(ctx, "x"); !errors.Is(err, want) {
			t.Fatalf("expected %v, got %v", want, err)
		}
	})
}
