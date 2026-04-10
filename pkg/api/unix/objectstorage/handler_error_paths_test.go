//go:build unix

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

package objectstorage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// invalidPayload produces bytes that cannot be decoded as JSON into any struct.
var invalidPayload = []byte(`{invalid-json!!!`)

// TestHandlerCRUDServiceErrors verifies that CRUD handlers propagate service errors.
func TestHandlerCRUDServiceErrors(t *testing.T) {
	t.Parallel()

	serviceErr := &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "test.service",
		Err:  assert.AnError,
	}

	tests := []struct {
		name   string
		opcode byte
		svc    *mockObjectStorageService
		encode func() ([]byte, error)
	}{
		{
			name:   "PutObject service error",
			opcode: OpPutObject,
			svc: &mockObjectStorageService{
				putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.PutObjectRequest{Key: "k"})
			},
		},
		{
			name:   "GetObject service error",
			opcode: OpGetObject,
			svc: &mockObjectStorageService{
				getObjectFunc: func(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.GetObjectRequest{Key: "k"})
			},
		},
		{
			name:   "DeleteObject service error",
			opcode: OpDeleteObject,
			svc: &mockObjectStorageService{
				deleteObjectFunc: func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.DeleteObjectRequest{Key: "k"})
			},
		},
		{
			name:   "HeadObject service error",
			opcode: OpHeadObject,
			svc: &mockObjectStorageService{
				headObjectFunc: func(ctx context.Context, req *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.HeadObjectRequest{Key: "k"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, nil, &testLogger{})
			require.NoError(t, err)

			payload, err := tt.encode()
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, payload)
			require.Error(t, err)

			var dragonErr *dberrors.DragonError
			require.True(t, dberrors.As(err, &dragonErr))
			assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
		})
	}
}

// TestHandlerCRUDDecodeErrors verifies that CRUD handlers return errors for invalid payloads.
func TestHandlerCRUDDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"PutObject decode error", OpPutObject},
		{"GetObject decode error", OpGetObject},
		{"DeleteObject decode error", OpDeleteObject},
		{"HeadObject decode error", OpHeadObject},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)

			var dragonErr *dberrors.DragonError
			require.True(t, dberrors.As(err, &dragonErr))
			assert.Equal(t, dberrors.ErrInternal, dragonErr.Code)
		})
	}
}

// TestHandlerListServiceErrors verifies that list handlers propagate service errors.
func TestHandlerListServiceErrors(t *testing.T) {
	t.Parallel()

	serviceErr := &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "test.service",
		Err:  assert.AnError,
	}

	tests := []struct {
		name   string
		opcode byte
		svc    *mockObjectStorageService
		encode func() ([]byte, error)
	}{
		{
			name:   "ListObjects service error",
			opcode: OpListObjects,
			svc: &mockObjectStorageService{
				listObjectsFunc: func(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.ListObjectsRequest{Prefix: "/"})
			},
		},
		{
			name:   "ListByTag service error",
			opcode: OpListByTag,
			svc: &mockObjectStorageService{
				listByTagFunc: func(ctx context.Context, req *service.ListByTagRequest) (*service.ListByTagResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.ListByTagRequest{TagName: "env"})
			},
		},
		{
			name:   "ListByTenant service error",
			opcode: OpListByTenant,
			svc: &mockObjectStorageService{
				listByTenantFunc: func(ctx context.Context, req *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.ListByTenantRequest{TenantID: "t1"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, nil, &testLogger{})
			require.NoError(t, err)

			payload, err := tt.encode()
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, payload)
			require.Error(t, err)

			var dragonErr *dberrors.DragonError
			require.True(t, dberrors.As(err, &dragonErr))
		})
	}
}

// TestHandlerListDecodeErrors verifies that list handlers return errors for invalid payloads.
func TestHandlerListDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"ListObjects decode error", OpListObjects},
		{"ListByTag decode error", OpListByTag},
		{"ListByTenant decode error", OpListByTenant},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)
		})
	}
}

// TestHandlerOpsServiceErrors verifies that ops handlers propagate service errors.
func TestHandlerOpsServiceErrors(t *testing.T) {
	t.Parallel()

	serviceErr := &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "test.service",
		Err:  assert.AnError,
	}

	tests := []struct {
		name   string
		opcode byte
		svc    *mockObjectStorageService
		encode func() ([]byte, error)
	}{
		{
			name:   "CopyObject service error",
			opcode: OpCopyObject,
			svc: &mockObjectStorageService{
				copyObjectFunc: func(ctx context.Context, req *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.CopyObjectRequest{SourceKey: "s", DestinationKey: "d"})
			},
		},
		{
			name:   "ReplicateObject service error",
			opcode: OpReplicateObject,
			svc: &mockObjectStorageService{
				replicateObjectFunc: func(ctx context.Context, req *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.ReplicateObjectRequest{Key: "k"})
			},
		},
		{
			name:   "MigrateObject service error",
			opcode: OpMigrateObject,
			svc: &mockObjectStorageService{
				migrateObjectFunc: func(ctx context.Context, req *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.MigrateObjectRequest{Key: "k"})
			},
		},
		{
			name:   "ArchiveObject service error",
			opcode: OpArchiveObject,
			svc: &mockObjectStorageService{
				archiveObjectFunc: func(ctx context.Context, req *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.ArchiveObjectRequest{Key: "k"})
			},
		},
		{
			name:   "RestoreObject service error",
			opcode: OpRestoreObject,
			svc: &mockObjectStorageService{
				restoreObjectFunc: func(ctx context.Context, req *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.RestoreObjectRequest{Key: "k"})
			},
		},
		{
			name:   "VerifyObject service error",
			opcode: OpVerifyObject,
			svc: &mockObjectStorageService{
				verifyObjectFunc: func(ctx context.Context, req *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.VerifyObjectRequest{Key: "k"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, nil, &testLogger{})
			require.NoError(t, err)

			payload, err := tt.encode()
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, payload)
			require.Error(t, err)

			var dragonErr *dberrors.DragonError
			require.True(t, dberrors.As(err, &dragonErr))
		})
	}
}

// TestHandlerOpsDecodeErrors verifies that ops handlers return errors for invalid payloads.
func TestHandlerOpsDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CopyObject decode error", OpCopyObject},
		{"ReplicateObject decode error", OpReplicateObject},
		{"MigrateObject decode error", OpMigrateObject},
		{"ArchiveObject decode error", OpArchiveObject},
		{"RestoreObject decode error", OpRestoreObject},
		{"VerifyObject decode error", OpVerifyObject},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)
		})
	}
}

// TestHandlerPolicyServiceErrors verifies that policy handlers propagate service errors.
func TestHandlerPolicyServiceErrors(t *testing.T) {
	t.Parallel()

	serviceErr := &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "test.service",
		Err:  assert.AnError,
	}

	tests := []struct {
		name   string
		opcode byte
		svc    *mockObjectStorageService
		encode func() ([]byte, error)
	}{
		{
			name:   "SetObjectPolicy service error",
			opcode: OpSetObjectPolicy,
			svc: &mockObjectStorageService{
				setObjectPolicyFunc: func(ctx context.Context, req *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.SetObjectPolicyRequest{Key: "k"})
			},
		},
		{
			name:   "GetObjectPolicy service error",
			opcode: OpGetObjectPolicy,
			svc: &mockObjectStorageService{
				getObjectPolicyFunc: func(ctx context.Context, req *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.GetObjectPolicyRequest{Key: "k"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, nil, &testLogger{})
			require.NoError(t, err)

			payload, err := tt.encode()
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, payload)
			require.Error(t, err)
		})
	}
}

// TestHandlerPolicyDecodeErrors verifies that policy handlers return errors for invalid payloads.
func TestHandlerPolicyDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"SetObjectPolicy decode error", OpSetObjectPolicy},
		{"GetObjectPolicy decode error", OpGetObjectPolicy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)
		})
	}
}

// TestHandlerBackendServiceErrors verifies that backend handlers propagate service errors.
func TestHandlerBackendServiceErrors(t *testing.T) {
	t.Parallel()

	serviceErr := &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "test.service",
		Err:  assert.AnError,
	}

	tests := []struct {
		name   string
		opcode byte
		svc    *mockObjectStorageService
		encode func() ([]byte, error)
	}{
		{
			name:   "ListBackends service error",
			opcode: OpListBackends,
			svc: &mockObjectStorageService{
				listBackendsFunc: func(ctx context.Context, req *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.ListBackendsRequest{})
			},
		},
		{
			name:   "GetBackendHealth service error",
			opcode: OpGetBackendHealth,
			svc: &mockObjectStorageService{
				getBackendHealthFunc: func(ctx context.Context, req *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.GetBackendHealthRequest{Name: "b"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, nil, &testLogger{})
			require.NoError(t, err)

			payload, err := tt.encode()
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, payload)
			require.Error(t, err)
		})
	}
}

// TestHandlerBackendDecodeErrors verifies that backend handlers return errors for invalid payloads.
func TestHandlerBackendDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"ListBackends decode error", OpListBackends},
		{"GetBackendHealth decode error", OpGetBackendHealth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)
		})
	}
}

// TestHandlerTenantServiceErrors verifies that tenant handlers propagate service errors.
func TestHandlerTenantServiceErrors(t *testing.T) {
	t.Parallel()

	serviceErr := &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "test.service",
		Err:  assert.AnError,
	}

	tests := []struct {
		name   string
		opcode byte
		svc    *mockObjectStorageService
		encode func() ([]byte, error)
	}{
		{
			name:   "GetTenantUsage service error",
			opcode: OpGetTenantUsage,
			svc: &mockObjectStorageService{
				getTenantUsageFunc: func(ctx context.Context, req *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.GetTenantUsageRequest{TenantID: "t"})
			},
		},
		{
			name:   "GetTenantQuotas service error",
			opcode: OpGetTenantQuotas,
			svc: &mockObjectStorageService{
				getTenantQuotasFunc: func(ctx context.Context, req *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.GetTenantQuotasRequest{TenantID: "t"})
			},
		},
		{
			name:   "SetTenantQuotas service error",
			opcode: OpSetTenantQuotas,
			svc: &mockObjectStorageService{
				setTenantQuotasFunc: func(ctx context.Context, req *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.SetTenantQuotasRequest{TenantID: "t"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.svc, nil, &testLogger{})
			require.NoError(t, err)

			payload, err := tt.encode()
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, payload)
			require.Error(t, err)
		})
	}
}

// TestHandlerTenantDecodeErrors verifies that tenant handlers return errors for invalid payloads.
func TestHandlerTenantDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"GetTenantUsage decode error", OpGetTenantUsage},
		{"GetTenantQuotas decode error", OpGetTenantQuotas},
		{"SetTenantQuotas decode error", OpSetTenantQuotas},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)
		})
	}
}

// TestHandlerBatchDecodeErrors verifies that batch handlers return errors for invalid payloads.
func TestHandlerBatchDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"BatchPutObjects decode error", OpBatchPutObjects},
		{"BatchGetObjects decode error", OpBatchGetObjects},
		{"BatchDeleteObjects decode error", OpBatchDeleteObjects},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)
		})
	}
}

// TestHandlerBatchPutServiceErrors tests that batch put propagates individual item errors.
func TestHandlerBatchPutServiceErrors(t *testing.T) {
	t.Parallel()

	svc := &mockObjectStorageService{
		putObjectFunc: func(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			if req.Key == "fail-key" {
				return nil, &dberrors.DragonError{
					Code: dberrors.ErrStorage,
					Op:   "test",
					Err:  assert.AnError,
				}
			}
			return &service.PutObjectResponse{Key: req.Key, ObjectID: 1, Size: int64(len(req.Value))}, nil
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&BatchPutRequest{
		Objects: []*PutObjectItem{
			{Key: "good-key", Value: []byte("val")},
			{Key: "fail-key", Value: []byte("val")},
		},
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpBatchPutObjects, reqPayload)
	require.NoError(t, err)

	var batchResp BatchPutResponse
	err = DecodePayload(resp, &batchResp)
	require.NoError(t, err)
	assert.Equal(t, 1, batchResp.Succeeded)
	assert.Equal(t, 1, batchResp.Failed)
	assert.Len(t, batchResp.Results, 2)
	assert.True(t, batchResp.Results[0].Success)
	assert.False(t, batchResp.Results[1].Success)
	assert.NotEmpty(t, batchResp.Results[1].Error)
}

// TestHandlerBatchDeleteServiceErrors tests that batch delete propagates individual item errors.
func TestHandlerBatchDeleteServiceErrors(t *testing.T) {
	t.Parallel()

	svc := &mockObjectStorageService{
		deleteObjectFunc: func(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
			if req.Key == "fail-key" {
				return nil, &dberrors.DragonError{
					Code: dberrors.ErrNotFound,
					Op:   "test",
					Err:  assert.AnError,
				}
			}
			return &service.DeleteObjectResponse{Key: req.Key, Success: true}, nil
		},
	}

	handler, err := NewHandler(svc, nil, &testLogger{})
	require.NoError(t, err)

	reqPayload, err := EncodePayload(&BatchDeleteRequest{
		Keys: []string{"good-key", "fail-key"},
	})
	require.NoError(t, err)

	resp, err := handler.Handle(context.Background(), OpBatchDeleteObjects, reqPayload)
	require.NoError(t, err)

	var batchResp BatchDeleteResponse
	err = DecodePayload(resp, &batchResp)
	require.NoError(t, err)
	assert.Equal(t, 1, batchResp.Succeeded)
	assert.Equal(t, 1, batchResp.Failed)
	assert.False(t, batchResp.Results[1].Success)
	assert.NotEmpty(t, batchResp.Results[1].Error)
}

// TestHandlerAdminDecodeErrors verifies that admin handlers return errors for invalid payloads.
func TestHandlerAdminDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"AdminCreateBackend decode error", OpAdminCreateBackend},
		{"AdminUpdateBackend decode error", OpAdminUpdateBackend},
		{"AdminDeleteBackend decode error", OpAdminDeleteBackend},
		{"AdminSetBackendStatus decode error", OpAdminSetBackendStatus},
		{"AdminGetStorageStats decode error", OpAdminGetStorageStats},
		{"AdminRunGC decode error", OpAdminRunGC},
		{"AdminGetGCProgress decode error", OpAdminGetGCProgress},
		{"AdminRepairObject decode error", OpAdminRepairObject},
		{"AdminCompactBackend decode error", OpAdminCompactBackend},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, &mockAdminService{}, &testLogger{})
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, invalidPayload)
			require.Error(t, err)
		})
	}
}

// TestHandlerAdminServiceErrors verifies that admin handlers propagate service errors.
func TestHandlerAdminServiceErrors(t *testing.T) {
	t.Parallel()

	serviceErr := &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "test.admin",
		Err:  assert.AnError,
	}

	tests := []struct {
		name     string
		opcode   byte
		adminSvc *mockAdminService
		encode   func() ([]byte, error)
	}{
		{
			name:   "AdminCreateBackend service error",
			opcode: OpAdminCreateBackend,
			adminSvc: &mockAdminService{
				createBackendFunc: func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.CreateBackendRequest{Name: "b"})
			},
		},
		{
			name:   "AdminUpdateBackend service error",
			opcode: OpAdminUpdateBackend,
			adminSvc: &mockAdminService{
				updateBackendFunc: func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.UpdateBackendRequest{Name: "b"})
			},
		},
		{
			name:   "AdminDeleteBackend service error",
			opcode: OpAdminDeleteBackend,
			adminSvc: &mockAdminService{
				deleteBackendFunc: func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.DeleteBackendRequest{Name: "b"})
			},
		},
		{
			name:   "AdminSetBackendStatus service error",
			opcode: OpAdminSetBackendStatus,
			adminSvc: &mockAdminService{
				setBackendStatusFunc: func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.SetBackendStatusRequest{Name: "b"})
			},
		},
		{
			name:   "AdminGetStorageStats service error",
			opcode: OpAdminGetStorageStats,
			adminSvc: &mockAdminService{
				getStorageStatsFunc: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.GetStorageStatsRequest{})
			},
		},
		{
			name:   "AdminRunGC service error",
			opcode: OpAdminRunGC,
			adminSvc: &mockAdminService{
				runGarbageCollectionFunc: func(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.RunGarbageCollectionRequest{})
			},
		},
		{
			name:   "AdminGetGCProgress service error",
			opcode: OpAdminGetGCProgress,
			adminSvc: &mockAdminService{
				getGarbageCollectionProgressFunc: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.GetGarbageCollectionProgressRequest{OperationID: "gc-1"})
			},
		},
		{
			name:   "AdminRepairObject service error",
			opcode: OpAdminRepairObject,
			adminSvc: &mockAdminService{
				repairObjectFunc: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.RepairObjectRequest{Key: "k"})
			},
		},
		{
			name:   "AdminCompactBackend service error",
			opcode: OpAdminCompactBackend,
			adminSvc: &mockAdminService{
				compactBackendFunc: func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
					return nil, serviceErr
				},
			},
			encode: func() ([]byte, error) {
				return EncodePayload(&service.CompactBackendRequest{Name: "b"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(&mockObjectStorageService{}, tt.adminSvc, &testLogger{})
			require.NoError(t, err)

			payload, err := tt.encode()
			require.NoError(t, err)

			_, err = handler.Handle(context.Background(), tt.opcode, payload)
			require.Error(t, err)

			var dragonErr *dberrors.DragonError
			require.True(t, dberrors.As(err, &dragonErr))
		})
	}
}

// TestErrorToStatusNonDragonError verifies that non-DragonError errors map to internal error.
func TestErrorToStatusNonDragonError(t *testing.T) {
	t.Parallel()

	result := ErrorToStatus(assert.AnError)
	assert.Equal(t, StatusInternalError, result)
}

// TestHandleRequestWithClosedHandler verifies HandleRequest returns error when handler is closed.
func TestHandleRequestWithClosedHandler(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(&mockObjectStorageService{}, nil, &testLogger{})
	require.NoError(t, err)

	err = handler.Close()
	require.NoError(t, err)

	req := &Request{
		Opcode:  OpGetObject,
		Token:   "token",
		Payload: []byte(`{}`),
	}

	resp := handler.HandleRequest(context.Background(), req)
	assert.Equal(t, StatusUnavailable, resp.Status)
}
