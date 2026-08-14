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

package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
)

const dsService = "quicbaas.DynamicSecrets"

// errDynSecrets drives the error-propagation paths through the embedded
// handlers.
var errDynSecrets = errors.New("dynamic secrets: boom")

// fakeDynamicSecretsService is a configurable DynamicSecretsService. When
// failNext is set every verb returns errDynSecrets.
type fakeDynamicSecretsService struct {
	failNext bool
	now      time.Time
}

func (f *fakeDynamicSecretsService) CreateRole(_ context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.CreateRoleResponse{
		Role: &credentials.Role{
			Name:          req.Name,
			GeneratorType: req.GeneratorType,
			Config:        req.Config,
			DefaultTTL:    req.DefaultTTL,
			MaxTTL:        req.MaxTTL,
		},
		CreatedAt: f.now,
	}, nil
}

func (f *fakeDynamicSecretsService) GetRole(_ context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.GetRoleResponse{
		Role: &credentials.Role{Name: req.Name, DefaultTTL: time.Hour},
	}, nil
}

func (f *fakeDynamicSecretsService) ListRoles(_ context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.ListRolesResponse{
		Roles: []*credentials.Role{{Name: req.Prefix + "a"}, {Name: req.Prefix + "b"}},
		Count: 2,
	}, nil
}

func (f *fakeDynamicSecretsService) DeleteRole(_ context.Context, _ *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.DeleteRoleResponse{DeletedAt: f.now}, nil
}

func (f *fakeDynamicSecretsService) GenerateCredential(_ context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.GenerateCredentialResponse{
		Credential: &credentials.Credential{
			CredentialID: "cred-1",
			Type:         "database",
			Data:         map[string]string{"username": "u", "password": "p"},
			LeaseID:      "lease-1",
			CreatedAt:    f.now,
			ExpiresAt:    f.now.Add(time.Hour),
		},
		Lease: &lease.Lease{
			LeaseID:     "lease-1",
			SecretPath:  "postgres/creds/" + req.RoleName,
			IssuedAt:    f.now,
			ExpiresAt:   f.now.Add(time.Hour),
			TTL:         time.Hour,
			RenewCount:  2,
			MaxRenewals: 5,
			Renewable:   true,
			Metadata:    map[string]string{"k": "v"},
		},
		LeaseID: "lease-1",
	}, nil
}

func (f *fakeDynamicSecretsService) RenewLease(_ context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.RenewLeaseResponse{
		Lease:     &lease.Lease{LeaseID: req.LeaseID, RenewCount: 3, Renewable: true},
		RenewedAt: f.now,
	}, nil
}

func (f *fakeDynamicSecretsService) RevokeLease(_ context.Context, _ *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.RevokeLeaseResponse{RevokedAt: f.now}, nil
}

func (f *fakeDynamicSecretsService) ListLeases(_ context.Context, _ *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
	if f.failNext {
		return nil, errDynSecrets
	}
	return &service.ListLeasesResponse{
		Leases: []*lease.Lease{{LeaseID: "lease-1", TTL: time.Minute}},
		Count:  1,
	}, nil
}

// dynamicSecretsMock embeds the base mock and overrides DynamicSecrets().
type dynamicSecretsMock struct {
	*mockQuicBaaS
	dyn service.DynamicSecretsService
}

func (m *dynamicSecretsMock) DynamicSecrets() service.DynamicSecretsService { return m.dyn }

func newDynamicSecretsTransport(t *testing.T, dyn service.DynamicSecretsService) *Transport {
	t.Helper()
	tr, err := New(&dynamicSecretsMock{mockQuicBaaS: &mockQuicBaaS{}, dyn: dyn})
	require.NoError(t, err)
	return tr
}

func newFakeDynamicSecrets() *fakeDynamicSecretsService {
	return &fakeDynamicSecretsService{now: time.Unix(1700000000, 0).UTC()}
}

// TestTransport_DynamicSecrets_AllVerbs proves all eight verbs round-trip
// in-process and return the leaf SDK types every other transport returns.
func TestTransport_DynamicSecrets_AllVerbs(t *testing.T) {
	fake := newFakeDynamicSecrets()
	tr := newDynamicSecretsTransport(t, fake)
	ctx := context.Background()

	t.Run("CreateRole", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "CreateRole", &dsclient.CreateRoleRequest{
			Name:          "db-readonly",
			GeneratorType: "postgres",
			Config:        map[string]string{"db": "app"},
			DefaultTTL:    time.Hour,
			MaxTTL:        24 * time.Hour,
		})
		require.NoError(t, err)
		typed, ok := resp.(*dsclient.CreateRoleResponse)
		require.True(t, ok, "got %T", resp)
		assert.Equal(t, "db-readonly", typed.Role.Name)
		assert.Equal(t, "postgres", typed.Role.GeneratorType)
		assert.Equal(t, time.Hour, typed.Role.DefaultTTL)
		assert.Equal(t, 24*time.Hour, typed.Role.MaxTTL)
		assert.Equal(t, fake.now, typed.CreatedAt)
	})

	t.Run("GetRole", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "GetRole", &dsclient.GetRoleRequest{Name: "db-readonly"})
		require.NoError(t, err)
		typed := resp.(*dsclient.GetRoleResponse)
		assert.Equal(t, "db-readonly", typed.Role.Name)
	})

	t.Run("ListRoles", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "ListRoles", &dsclient.ListRolesRequest{Prefix: "db-"})
		require.NoError(t, err)
		typed := resp.(*dsclient.ListRolesResponse)
		assert.Equal(t, 2, typed.Count)
		require.Len(t, typed.Roles, 2)
		assert.Equal(t, "db-a", typed.Roles[0].Name)
	})

	t.Run("DeleteRole", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "DeleteRole", &dsclient.DeleteRoleRequest{Name: "db-readonly"})
		require.NoError(t, err)
		typed := resp.(*dsclient.DeleteRoleResponse)
		assert.Equal(t, fake.now, typed.DeletedAt)
	})

	t.Run("GenerateCredential", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "GenerateCredential",
			&dsclient.GenerateCredentialRequest{RoleName: "db-readonly", TTL: 30 * time.Minute})
		require.NoError(t, err)
		typed := resp.(*dsclient.GenerateCredentialResponse)
		assert.Equal(t, "lease-1", typed.LeaseID)
		require.NotNil(t, typed.Credential)
		assert.Equal(t, "cred-1", typed.Credential.CredentialID)
		assert.Equal(t, "database", typed.Credential.Type)
		assert.Equal(t, "p", typed.Credential.Data["password"])
		require.NotNil(t, typed.Lease)
		assert.Equal(t, "postgres/creds/db-readonly", typed.Lease.SecretPath)
		assert.Equal(t, 2, typed.Lease.RenewCount)
		assert.Equal(t, 5, typed.Lease.MaxRenewals)
		assert.Equal(t, "v", typed.Lease.Metadata["k"])
	})

	t.Run("RenewLease", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "RenewLease",
			&dsclient.RenewLeaseRequest{LeaseID: "lease-1", Increment: time.Hour})
		require.NoError(t, err)
		typed := resp.(*dsclient.RenewLeaseResponse)
		assert.Equal(t, 3, typed.Lease.RenewCount)
		assert.Equal(t, fake.now, typed.RenewedAt)
	})

	t.Run("RevokeLease", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "RevokeLease",
			&dsclient.RevokeLeaseRequest{LeaseID: "lease-1"})
		require.NoError(t, err)
		typed := resp.(*dsclient.RevokeLeaseResponse)
		assert.Equal(t, fake.now, typed.RevokedAt)
	})

	t.Run("ListLeases", func(t *testing.T) {
		resp, err := tr.Request(ctx, dsService, "ListLeases", &dsclient.ListLeasesRequest{})
		require.NoError(t, err)
		typed := resp.(*dsclient.ListLeasesResponse)
		assert.Equal(t, 1, typed.Count)
		require.Len(t, typed.Leases, 1)
	})
}

// TestTransport_DynamicSecrets_ListVerbsTolerateNilRequest documents that the
// two prefix-filtered list verbs are usable without a request struct, matching
// the other transports.
func TestTransport_DynamicSecrets_ListVerbsTolerateNilRequest(t *testing.T) {
	tr := newDynamicSecretsTransport(t, newFakeDynamicSecrets())

	resp, err := tr.Request(context.Background(), dsService, "ListRoles", nil)
	require.NoError(t, err)
	_, ok := resp.(*dsclient.ListRolesResponse)
	assert.True(t, ok)

	resp, err = tr.Request(context.Background(), dsService, "ListLeases", nil)
	require.NoError(t, err)
	_, ok = resp.(*dsclient.ListLeasesResponse)
	assert.True(t, ok)
}

// TestTransport_DynamicSecrets_ServiceError proves service failures propagate
// unwrapped rather than being masked as a transport fault.
func TestTransport_DynamicSecrets_ServiceError(t *testing.T) {
	tr := newDynamicSecretsTransport(t, &fakeDynamicSecretsService{failNext: true})
	ctx := context.Background()

	cases := []struct {
		method string
		req    interface{}
	}{
		{"CreateRole", &dsclient.CreateRoleRequest{Name: "r", GeneratorType: "postgres"}},
		{"GetRole", &dsclient.GetRoleRequest{Name: "r"}},
		{"ListRoles", &dsclient.ListRolesRequest{}},
		{"DeleteRole", &dsclient.DeleteRoleRequest{Name: "r"}},
		{"GenerateCredential", &dsclient.GenerateCredentialRequest{RoleName: "r"}},
		{"RenewLease", &dsclient.RenewLeaseRequest{LeaseID: "l"}},
		{"RevokeLease", &dsclient.RevokeLeaseRequest{LeaseID: "l"}},
		{"ListLeases", &dsclient.ListLeasesRequest{}},
	}
	for _, tc := range cases {
		_, err := tr.Request(ctx, dsService, tc.method, tc.req)
		require.ErrorIs(t, err, errDynSecrets, "method %s", tc.method)
	}
}

// TestTransport_DynamicSecrets_NotConfigured proves a deployment without the
// engine reports a typed not-configured error instead of panicking.
func TestTransport_DynamicSecrets_NotConfigured(t *testing.T) {
	tr := newDynamicSecretsTransport(t, nil)

	_, err := tr.Request(context.Background(), dsService, "ListRoles", &dsclient.ListRolesRequest{})
	require.Error(t, err)
	var notConfigured *ServiceNotConfiguredError
	require.ErrorAs(t, err, &notConfigured)
	assert.Equal(t, "DynamicSecrets", notConfigured.Service)
}

// TestTransport_DynamicSecrets_InvalidRequestType proves each verb rejects a
// foreign request type with a typed error naming the expected shape.
func TestTransport_DynamicSecrets_InvalidRequestType(t *testing.T) {
	tr := newDynamicSecretsTransport(t, newFakeDynamicSecrets())
	ctx := context.Background()

	for _, method := range []string{
		"CreateRole", "GetRole", "DeleteRole",
		"GenerateCredential", "RenewLease", "RevokeLease",
	} {
		_, err := tr.Request(ctx, dsService, method, "not-a-request")
		require.Error(t, err, "method %s", method)
		var typed *InvalidRequestTypeError
		require.ErrorAs(t, err, &typed, "method %s", method)
		assert.Equal(t, "quicbaas.DynamicSecrets", typed.Service)
		assert.Equal(t, method, typed.Method)
	}
}

// TestDynamicSecretsConvertersHandleNil covers the nil-in/nil-out contract the
// handlers rely on when a service returns a partially populated response.
func TestDynamicSecretsConvertersHandleNil(t *testing.T) {
	assert.Nil(t, dynamicSecretsRoleFromService(nil))
	assert.Nil(t, dynamicSecretsCredentialFromService(nil))
	assert.Nil(t, dynamicSecretsLeaseFromService(nil))
}
