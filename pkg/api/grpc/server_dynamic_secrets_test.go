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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// CreateDynamicSecretsRole Tests
// =============================================================================

func TestServer_CreateDynamicSecretsRole(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.CreateDynamicSecretsRole(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_name", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
			Name: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_generator_type", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
			Name:          "test-role",
			GeneratorType: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
			Name:          "test-role",
			GeneratorType: "postgres",
			DefaultTtl:    durationpb.New(3600),
			MaxTtl:        durationpb.New(86400),
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			CreateRoleFunc: func(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
				assert.Equal(t, "db-readonly", req.Name)
				assert.Equal(t, "postgres", req.GeneratorType)
				return &service.CreateRoleResponse{
					Role: &credentials.Role{
						Name:          "db-readonly",
						GeneratorType: "postgres",
						Config:        map[string]string{"host": "localhost"},
						DefaultTTL:    time.Hour,
						MaxTTL:        24 * time.Hour,
					},
				}, nil
			},
		}

		resp, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
			Name:          "db-readonly",
			GeneratorType: "postgres",
			Config:        map[string]string{"host": "localhost"},
			DefaultTtl:    durationpb.New(time.Hour),
			MaxTtl:        durationpb.New(24 * time.Hour),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Role)
		assert.Equal(t, "db-readonly", resp.Role.Name)
		assert.Equal(t, "postgres", resp.Role.GeneratorType)
		assert.NotNil(t, resp.Timestamp)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			CreateRoleFunc: func(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
				return nil, errors.New("duplicate role")
			},
		}

		_, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
			Name:          "db-readonly",
			GeneratorType: "postgres",
		})
		require.Error(t, err)
	})
}

// =============================================================================
// GetDynamicSecretsRole Tests
// =============================================================================

func TestServer_GetDynamicSecretsRole(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GetDynamicSecretsRole(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_name", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GetDynamicSecretsRole(context.Background(), &pb.DynSecretsGetRoleRequest{
			Name: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GetDynamicSecretsRole(context.Background(), &pb.DynSecretsGetRoleRequest{
			Name: "test-role",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			GetRoleFunc: func(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
				assert.Equal(t, "db-readonly", req.Name)
				return &service.GetRoleResponse{
					Role: &credentials.Role{
						Name:          "db-readonly",
						GeneratorType: "postgres",
						DefaultTTL:    time.Hour,
						MaxTTL:        24 * time.Hour,
					},
				}, nil
			},
		}

		resp, err := srv.GetDynamicSecretsRole(context.Background(), &pb.DynSecretsGetRoleRequest{
			Name: "db-readonly",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Role)
		assert.Equal(t, "db-readonly", resp.Role.Name)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			GetRoleFunc: func(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
				return nil, errors.New("not found")
			},
		}

		_, err := srv.GetDynamicSecretsRole(context.Background(), &pb.DynSecretsGetRoleRequest{
			Name: "nonexistent",
		})
		require.Error(t, err)
	})
}

// =============================================================================
// ListDynamicSecretsRoles Tests
// =============================================================================

func TestServer_ListDynamicSecretsRoles(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.ListDynamicSecretsRoles(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.ListDynamicSecretsRoles(context.Background(), &pb.DynSecretsListRolesRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			ListRolesFunc: func(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
				assert.Equal(t, "db/", req.Prefix)
				return &service.ListRolesResponse{
					Roles: []*credentials.Role{
						{Name: "db-readonly", GeneratorType: "postgres"},
						{Name: "db-admin", GeneratorType: "postgres"},
					},
					Count: 2,
				}, nil
			},
		}

		resp, err := srv.ListDynamicSecretsRoles(context.Background(), &pb.DynSecretsListRolesRequest{
			Prefix: "db/",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Roles, 2)
		assert.Equal(t, int32(2), resp.Count)
		assert.Equal(t, "db-readonly", resp.Roles[0].Name)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			ListRolesFunc: func(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
				return nil, errors.New("list failed")
			},
		}

		_, err := srv.ListDynamicSecretsRoles(context.Background(), &pb.DynSecretsListRolesRequest{})
		require.Error(t, err)
	})
}

// =============================================================================
// DeleteDynamicSecretsRole Tests
// =============================================================================

func TestServer_DeleteDynamicSecretsRole(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.DeleteDynamicSecretsRole(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_name", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.DeleteDynamicSecretsRole(context.Background(), &pb.DynSecretsDeleteRoleRequest{
			Name: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.DeleteDynamicSecretsRole(context.Background(), &pb.DynSecretsDeleteRoleRequest{
			Name: "test-role",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			DeleteRoleFunc: func(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
				assert.Equal(t, "db-readonly", req.Name)
				return &service.DeleteRoleResponse{}, nil
			},
		}

		resp, err := srv.DeleteDynamicSecretsRole(context.Background(), &pb.DynSecretsDeleteRoleRequest{
			Name: "db-readonly",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			DeleteRoleFunc: func(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
				return nil, errors.New("role in use")
			},
		}

		_, err := srv.DeleteDynamicSecretsRole(context.Background(), &pb.DynSecretsDeleteRoleRequest{
			Name: "db-readonly",
		})
		require.Error(t, err)
	})
}

// =============================================================================
// GenerateCredential Tests
// =============================================================================

func TestServer_GenerateCredential(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GenerateCredential(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_role_name", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GenerateCredential(context.Background(), &pb.GenerateCredentialRequest{
			RoleName: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GenerateCredential(context.Background(), &pb.GenerateCredentialRequest{
			RoleName: "test-role",
			Ttl:      durationpb.New(3600),
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			GenerateCredentialFunc: func(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
				assert.Equal(t, "db-readonly", req.RoleName)
				return &service.GenerateCredentialResponse{
					Credential: &credentials.Credential{
						Data: map[string]string{
							"username":          "dynamic_user_abc",
							"password":          "secret123",
							"connection_string": "postgres://localhost/mydb",
							"host":              "localhost",
							"port":              "5432",
							"database":          "mydb",
						},
					},
					Lease: &lease.Lease{
						LeaseID:    "lease-abc",
						SecretPath: "postgres/creds/db-readonly",
						IssuedAt:   now,
						ExpiresAt:  now.Add(time.Hour),
						TTL:        time.Hour,
						Renewable:  true,
					},
					LeaseID: "lease-abc",
				}, nil
			},
		}

		resp, err := srv.GenerateCredential(context.Background(), &pb.GenerateCredentialRequest{
			RoleName: "db-readonly",
			Ttl:      durationpb.New(time.Hour),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Credential)
		assert.Equal(t, "dynamic_user_abc", resp.Credential.Username)
		assert.Equal(t, "secret123", resp.Credential.Password)
		assert.Equal(t, int32(5432), resp.Credential.Port)
		assert.Equal(t, "lease-abc", resp.LeaseId)
		require.NotNil(t, resp.Lease)
		assert.Equal(t, "lease-abc", resp.Lease.LeaseId)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			GenerateCredentialFunc: func(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
				return nil, errors.New("role not found")
			},
		}

		_, err := srv.GenerateCredential(context.Background(), &pb.GenerateCredentialRequest{
			RoleName: "nonexistent",
		})
		require.Error(t, err)
	})
}

// =============================================================================
// RenewLease Tests
// =============================================================================

func TestServer_RenewLease(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RenewLease(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_lease_id", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RenewLease(context.Background(), &pb.RenewLeaseRequest{
			LeaseId: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RenewLease(context.Background(), &pb.RenewLeaseRequest{
			LeaseId:   "lease-123",
			Increment: durationpb.New(3600),
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			RenewLeaseFunc: func(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
				assert.Equal(t, "lease-abc", req.LeaseID)
				return &service.RenewLeaseResponse{
					Lease: &lease.Lease{
						LeaseID:    "lease-abc",
						SecretPath: "postgres/creds/readonly",
						IssuedAt:   now.Add(-time.Hour),
						ExpiresAt:  now.Add(2 * time.Hour),
						TTL:        3 * time.Hour,
						Renewable:  true,
						RenewCount: 1,
					},
					RenewedAt: now,
				}, nil
			},
		}

		resp, err := srv.RenewLease(context.Background(), &pb.RenewLeaseRequest{
			LeaseId:   "lease-abc",
			Increment: durationpb.New(time.Hour),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Lease)
		assert.Equal(t, "lease-abc", resp.Lease.LeaseId)
		assert.NotNil(t, resp.Lease.RenewedAt)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			RenewLeaseFunc: func(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
				return nil, errors.New("lease expired")
			},
		}

		_, err := srv.RenewLease(context.Background(), &pb.RenewLeaseRequest{
			LeaseId: "lease-expired",
		})
		require.Error(t, err)
	})
}

// =============================================================================
// RevokeLease Tests
// =============================================================================

func TestServer_RevokeLease(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RevokeLease(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_lease_id", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RevokeLease(context.Background(), &pb.RevokeLeaseRequest{
			LeaseId: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RevokeLease(context.Background(), &pb.RevokeLeaseRequest{
			LeaseId: "lease-123",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			RevokeLeaseFunc: func(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
				assert.Equal(t, "lease-abc", req.LeaseID)
				return &service.RevokeLeaseResponse{
					RevokedAt: now,
				}, nil
			},
		}

		resp, err := srv.RevokeLease(context.Background(), &pb.RevokeLeaseRequest{
			LeaseId: "lease-abc",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
		assert.NotNil(t, resp.Timestamp)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			RevokeLeaseFunc: func(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
				return nil, errors.New("lease not found")
			},
		}

		_, err := srv.RevokeLease(context.Background(), &pb.RevokeLeaseRequest{
			LeaseId: "nonexistent",
		})
		require.Error(t, err)
	})
}

// =============================================================================
// ListLeases Tests
// =============================================================================

func TestServer_ListLeases(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.ListLeases(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.ListLeases(context.Background(), &pb.ListLeasesRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		now := time.Now()
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			ListLeasesFunc: func(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
				assert.Equal(t, "postgres/", req.Prefix)
				return &service.ListLeasesResponse{
					Leases: []*lease.Lease{
						{
							LeaseID:    "lease-1",
							SecretPath: "postgres/creds/readonly",
							IssuedAt:   now.Add(-30 * time.Minute),
							ExpiresAt:  now.Add(30 * time.Minute),
							TTL:        time.Hour,
							Renewable:  true,
						},
						{
							LeaseID:    "lease-2",
							SecretPath: "postgres/creds/admin",
							IssuedAt:   now.Add(-time.Hour),
							ExpiresAt:  now.Add(time.Hour),
							TTL:        2 * time.Hour,
							Renewable:  true,
							RenewCount: 1,
						},
					},
					Count: 2,
				}, nil
			},
		}

		resp, err := srv.ListLeases(context.Background(), &pb.ListLeasesRequest{
			Prefix: "postgres/",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Leases, 2)
		assert.Equal(t, int32(2), resp.Count)
		assert.Equal(t, "lease-1", resp.Leases[0].LeaseId)
		assert.Equal(t, "lease-2", resp.Leases[1].LeaseId)
		// lease-2 has RenewCount > 0 so RenewedAt should be set
		assert.NotNil(t, resp.Leases[1].RenewedAt)
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			ListLeasesFunc: func(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
				return nil, errors.New("list failed")
			},
		}

		_, err := srv.ListLeases(context.Background(), &pb.ListLeasesRequest{})
		require.Error(t, err)
	})
}

// =============================================================================
// Conversion Helper Tests
// =============================================================================

func TestServiceRoleToProto(t *testing.T) {
	t.Run("nil_role", func(t *testing.T) {
		result := serviceRoleToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_role", func(t *testing.T) {
		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: "postgres",
			Config:        map[string]string{"host": "localhost"},
		}
		result := serviceRoleToProto(role)
		require.NotNil(t, result)
		assert.Equal(t, "test-role", result.Name)
		assert.Equal(t, "postgres", result.GeneratorType)
		assert.Equal(t, "localhost", result.Config["host"])
	})
}

func TestServiceCredentialToProto(t *testing.T) {
	t.Run("nil_credential", func(t *testing.T) {
		result := serviceCredentialToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_credential_with_all_fields", func(t *testing.T) {
		cred := &credentials.Credential{
			Data: map[string]string{
				"username":          "db-user",
				"password":          "db-pass",
				"connection_string": "postgres://localhost/mydb",
				"host":              "localhost",
				"port":              "5432",
				"database":          "mydb",
			},
		}
		result := serviceCredentialToProto(cred)
		require.NotNil(t, result)
		assert.Equal(t, "db-user", result.Username)
		assert.Equal(t, "db-pass", result.Password)
		assert.Equal(t, "postgres://localhost/mydb", result.ConnectionString)
		assert.Equal(t, "localhost", result.Host)
		assert.Equal(t, int32(5432), result.Port)
		assert.Equal(t, "mydb", result.Database)
	})

	t.Run("credential_with_invalid_port", func(t *testing.T) {
		cred := &credentials.Credential{
			Data: map[string]string{
				"port": "not-a-number",
			},
		}
		result := serviceCredentialToProto(cred)
		require.NotNil(t, result)
		// Port should remain at default zero value when invalid
		assert.Equal(t, int32(0), result.Port)
	})

	t.Run("credential_with_out_of_range_port", func(t *testing.T) {
		cred := &credentials.Credential{
			Data: map[string]string{
				"port": "99999",
			},
		}
		result := serviceCredentialToProto(cred)
		require.NotNil(t, result)
		assert.Equal(t, int32(0), result.Port)
	})

	t.Run("credential_with_zero_port", func(t *testing.T) {
		cred := &credentials.Credential{
			Data: map[string]string{
				"port": "0",
			},
		}
		result := serviceCredentialToProto(cred)
		require.NotNil(t, result)
		assert.Equal(t, int32(0), result.Port)
	})

	t.Run("credential_with_negative_port", func(t *testing.T) {
		cred := &credentials.Credential{
			Data: map[string]string{
				"port": "-1",
			},
		}
		result := serviceCredentialToProto(cred)
		require.NotNil(t, result)
		assert.Equal(t, int32(0), result.Port)
	})
}

func TestServiceLeaseToProto(t *testing.T) {
	t.Run("nil_lease", func(t *testing.T) {
		result := serviceLeaseToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid_active_lease", func(t *testing.T) {
		now := time.Now()
		l := &lease.Lease{
			LeaseID:     "lease-123",
			SecretPath:  "postgres/creds/readonly",
			IssuedAt:    now.Add(-1 * time.Hour),
			ExpiresAt:   now.Add(1 * time.Hour),
			TTL:         2 * time.Hour,
			Renewable:   true,
			RenewCount:  0,
			MaxRenewals: 5,
		}
		result := serviceLeaseToProto(l)
		require.NotNil(t, result)
		assert.Equal(t, "lease-123", result.LeaseId)
		assert.Equal(t, "postgres/creds/readonly", result.SecretPath)
		assert.Equal(t, pb.LeaseState_LEASE_STATE_ACTIVE, result.State)
		assert.True(t, result.Renewable)
	})

	t.Run("valid_renewed_lease", func(t *testing.T) {
		now := time.Now()
		l := &lease.Lease{
			LeaseID:     "lease-456",
			SecretPath:  "mysql/creds/admin",
			IssuedAt:    now.Add(-2 * time.Hour),
			ExpiresAt:   now.Add(1 * time.Hour),
			TTL:         3 * time.Hour,
			Renewable:   true,
			RenewCount:  2, // Has been renewed
			MaxRenewals: 10,
		}
		result := serviceLeaseToProto(l)
		require.NotNil(t, result)
		assert.Equal(t, "lease-456", result.LeaseId)
		assert.NotNil(t, result.RenewedAt) // Should be set when RenewCount > 0
	})

	t.Run("valid_expired_lease", func(t *testing.T) {
		now := time.Now()
		l := &lease.Lease{
			LeaseID:    "lease-expired",
			SecretPath: "mongodb/creds/app",
			IssuedAt:   now.Add(-3 * time.Hour),
			ExpiresAt:  now.Add(-1 * time.Hour), // Expired 1 hour ago
			TTL:        2 * time.Hour,
			Renewable:  false,
		}
		result := serviceLeaseToProto(l)
		require.NotNil(t, result)
		assert.Equal(t, "lease-expired", result.LeaseId)
		assert.Equal(t, pb.LeaseState_LEASE_STATE_EXPIRED, result.State)
	})

	t.Run("valid_revoked_lease", func(t *testing.T) {
		now := time.Now()
		revokedAt := now.Add(-30 * time.Minute)
		l := &lease.Lease{
			LeaseID:    "lease-revoked",
			SecretPath: "redis/creds/cache",
			IssuedAt:   now.Add(-2 * time.Hour),
			ExpiresAt:  now.Add(1 * time.Hour),
			TTL:        3 * time.Hour,
			Renewable:  true,
			RevokedAt:  &revokedAt,
		}
		result := serviceLeaseToProto(l)
		require.NotNil(t, result)
		assert.Equal(t, "lease-revoked", result.LeaseId)
		assert.Equal(t, pb.LeaseState_LEASE_STATE_REVOKED, result.State)
	})
}

func TestLeaseStateToProto(t *testing.T) {
	tests := []struct {
		name     string
		state    lease.LeaseState
		expected pb.LeaseState
	}{
		{name: "active", state: lease.LeaseStateActive, expected: pb.LeaseState_LEASE_STATE_ACTIVE},
		{name: "expired", state: lease.LeaseStateExpired, expected: pb.LeaseState_LEASE_STATE_EXPIRED},
		{name: "revoked", state: lease.LeaseStateRevoked, expected: pb.LeaseState_LEASE_STATE_REVOKED},
		{name: "unknown_default", state: lease.LeaseState(99), expected: pb.LeaseState_LEASE_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := leaseStateToProto(tt.state)
			assert.Equal(t, tt.expected, result)
		})
	}
}
