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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errObj is the sentinel error injected into the object-storage mock to
// exercise the error-propagation branch of the service-typed dispatch path.
var errObj = errors.New("objectstorage: boom")

// objSvcTypeCase pairs a wire method with a service-typed request value so
// the embedded handlers exercise the "accept service type directly" branch
// that the SDK-client-typed tests never reach.
type objSvcTypeCase struct {
	method string
	req    interface{}
}

func objSvcTypeCases() []objSvcTypeCase {
	return []objSvcTypeCase{
		{"PutObject", &service.PutObjectRequest{Key: "k", Value: []byte("v")}},
		{"GetObject", &service.GetObjectRequest{Key: "k"}},
		{"DeleteObject", &service.DeleteObjectRequest{Key: "k"}},
		{"HeadObject", &service.HeadObjectRequest{Key: "k"}},
		{"ListObjects", &service.ListObjectsRequest{}},
		{"ListByTag", &service.ListByTagRequest{}},
		{"ListByTenant", &service.ListByTenantRequest{}},
		{"CopyObject", &service.CopyObjectRequest{}},
		{"ReplicateObject", &service.ReplicateObjectRequest{}},
		{"MigrateObject", &service.MigrateObjectRequest{}},
		{"ArchiveObject", &service.ArchiveObjectRequest{}},
		{"RestoreObject", &service.RestoreObjectRequest{}},
		{"VerifyObject", &service.VerifyObjectRequest{}},
		{"SetObjectPolicy", &service.SetObjectPolicyRequest{}},
		{"GetObjectPolicy", &service.GetObjectPolicyRequest{}},
		{"ListBackends", &service.ListBackendsRequest{}},
		{"GetBackendHealth", &service.GetBackendHealthRequest{}},
		{"GetTenantUsage", &service.GetTenantUsageRequest{}},
		{"GetTenantQuotas", &service.GetTenantQuotasRequest{}},
		{"SetTenantQuotas", &service.SetTenantQuotasRequest{}},
		{"BatchPutObjects", &service.BatchPutObjectsRequest{}},
		{"BatchGetObjects", &service.BatchGetObjectsRequest{}},
		{"BatchDeleteObjects", &service.BatchDeleteObjectsRequest{}},
	}
}

func TestHandleObjectStorage_ServiceTypeDirect_Success(t *testing.T) {
	objMock := &mockObjectStorageService{}
	tr, err := New(&mockQuicBaaS{objectStorageService: objMock})
	require.NoError(t, err)
	ctx := context.Background()

	for _, tc := range objSvcTypeCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", tc.method, tc.req)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

// errObjStorage is an ObjectStorageService whose every method returns errObj,
// driving the error branch of the service-typed dispatch path for every
// object-storage handler.
type errObjStorage struct{ *mockObjectStorageService }

func (errObjStorage) PutObject(context.Context, *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) GetObject(context.Context, *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) DeleteObject(context.Context, *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) HeadObject(context.Context, *service.HeadObjectRequest) (*service.HeadObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) ListObjects(context.Context, *service.ListObjectsRequest) (*service.ListObjectsResponse, error) {
	return nil, errObj
}
func (errObjStorage) ListByTag(context.Context, *service.ListByTagRequest) (*service.ListByTagResponse, error) {
	return nil, errObj
}
func (errObjStorage) ListByTenant(context.Context, *service.ListByTenantRequest) (*service.ListByTenantResponse, error) {
	return nil, errObj
}
func (errObjStorage) CopyObject(context.Context, *service.CopyObjectRequest) (*service.CopyObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) ReplicateObject(context.Context, *service.ReplicateObjectRequest) (*service.ReplicateObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) MigrateObject(context.Context, *service.MigrateObjectRequest) (*service.MigrateObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) ArchiveObject(context.Context, *service.ArchiveObjectRequest) (*service.ArchiveObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) RestoreObject(context.Context, *service.RestoreObjectRequest) (*service.RestoreObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) VerifyObject(context.Context, *service.VerifyObjectRequest) (*service.VerifyObjectResponse, error) {
	return nil, errObj
}
func (errObjStorage) SetObjectPolicy(context.Context, *service.SetObjectPolicyRequest) (*service.SetObjectPolicyResponse, error) {
	return nil, errObj
}
func (errObjStorage) GetObjectPolicy(context.Context, *service.GetObjectPolicyRequest) (*service.GetObjectPolicyResponse, error) {
	return nil, errObj
}
func (errObjStorage) ListBackends(context.Context, *service.ListBackendsRequest) (*service.ListBackendsResponse, error) {
	return nil, errObj
}
func (errObjStorage) GetBackendHealth(context.Context, *service.GetBackendHealthRequest) (*service.GetBackendHealthResponse, error) {
	return nil, errObj
}
func (errObjStorage) GetTenantUsage(context.Context, *service.GetTenantUsageRequest) (*service.GetTenantUsageResponse, error) {
	return nil, errObj
}
func (errObjStorage) GetTenantQuotas(context.Context, *service.GetTenantQuotasRequest) (*service.GetTenantQuotasResponse, error) {
	return nil, errObj
}
func (errObjStorage) SetTenantQuotas(context.Context, *service.SetTenantQuotasRequest) (*service.SetTenantQuotasResponse, error) {
	return nil, errObj
}
func (errObjStorage) BatchPutObjects(context.Context, *service.BatchPutObjectsRequest) (*service.BatchPutObjectsResponse, error) {
	return nil, errObj
}
func (errObjStorage) BatchGetObjects(context.Context, *service.BatchGetObjectsRequest) (*service.BatchGetObjectsResponse, error) {
	return nil, errObj
}
func (errObjStorage) BatchDeleteObjects(context.Context, *service.BatchDeleteObjectsRequest) (*service.BatchDeleteObjectsResponse, error) {
	return nil, errObj
}

func TestHandleObjectStorage_ServiceTypeDirect_Error(t *testing.T) {
	objMock := errObjStorage{mockObjectStorageService: &mockObjectStorageService{}}
	tr, err := New(&mockQuicBaaS{objectStorageService: objMock})
	require.NoError(t, err)
	ctx := context.Background()

	for _, tc := range objSvcTypeCases() {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(ctx, "quicbaas.ObjectStorage", tc.method, tc.req)
			require.ErrorIs(t, err, errObj)
			assert.Nil(t, resp)
		})
	}
}

// TestConvertSvcResponses_NilInput exercises the nil-guard branch of every
// service-to-SDK object-storage converter. The dispatch handlers only invoke
// these on non-nil responses, so the nil path is otherwise unreachable.
func TestConvertSvcResponses_NilInput(t *testing.T) {
	assert.Nil(t, convertCatalogReplicationPolicy(nil))
	assert.Nil(t, convertCatalogArchivePolicy(nil))
	assert.Nil(t, convertCatalogRetentionPolicy(nil))
	assert.Nil(t, convertSvcPutObjectResponse(nil))
	assert.Nil(t, convertSvcGetObjectResponse(nil))
	assert.Nil(t, convertSvcDeleteObjectResponse(nil))
	assert.Nil(t, convertSvcHeadObjectResponse(nil))
	assert.Nil(t, convertSvcObjectSummary(nil))
	assert.Nil(t, convertSvcObjectSummaries(nil))
	assert.Nil(t, convertSvcListObjectsResponse(nil))
	assert.Nil(t, convertSvcListByTagResponse(nil))
	assert.Nil(t, convertSvcListByTenantResponse(nil))
	assert.Nil(t, convertSvcCopyObjectResponse(nil))
	assert.Nil(t, convertSvcReplicateObjectResponse(nil))
	assert.Nil(t, convertSvcMigrateObjectResponse(nil))
	assert.Nil(t, convertSvcArchiveObjectResponse(nil))
	assert.Nil(t, convertSvcRestoreObjectResponse(nil))
	assert.Nil(t, convertSvcVerifyObjectResponse(nil))
	assert.Nil(t, convertSvcSetObjectPolicyResponse(nil))
	assert.Nil(t, convertSvcGetObjectPolicyResponse(nil))
	assert.Nil(t, convertSvcListBackendsResponse(nil))
	assert.Nil(t, convertSvcGetBackendHealthResponse(nil))
	assert.Nil(t, convertSvcGetTenantUsageResponse(nil))
	assert.Nil(t, convertSvcGetTenantQuotasResponse(nil))
	assert.Nil(t, convertSvcSetTenantQuotasResponse(nil))
	assert.Nil(t, convertSvcBatchPutObjectsResponse(nil))
	assert.Nil(t, convertSvcBatchGetObjectsResponse(nil))
	assert.Nil(t, convertSvcBatchDeleteObjectsResponse(nil))
}
