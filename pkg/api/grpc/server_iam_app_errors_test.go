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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// appStoreFailureDetail is internal text a failing store reports; no status
// may carry it.
const appStoreFailureDetail = "write /var/lib/quicbaas/apps.db: input/output error"

// appStoreFailureError is the error a failing application store returns.
type appStoreFailureError struct{}

func (appStoreFailureError) Error() string { return appStoreFailureDetail }

// failingWriteAppStorage is an application store whose reads work and whose
// every write fails.
type failingWriteAppStorage struct {
	iam.AppStorage
}

func (failingWriteAppStorage) CreateApp(context.Context, *iam.Application) error {
	return appStoreFailureError{}
}

func (failingWriteAppStorage) UpdateApp(context.Context, *iam.Application) error {
	return appStoreFailureError{}
}

// newAppErrorTestServer serves the IAM gRPC verbs over an app manager on storage.
func newAppErrorTestServer(t *testing.T, storage iam.AppStorage) *IAMServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	appManager := iam.NewAppManager(storage, nil)
	return NewIAMServer(NewIAMServiceAdapter(nil, nil, nil, appManager, nil, nil), log)
}

func TestIAMServer_RegisterApp_MetadataErrorIsInvalidArgument(t *testing.T) {
	t.Parallel()
	storage := iam.NewMemoryStorage()
	srv := newAppErrorTestServer(t, storage)

	_, err := srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{
		TenantId: "t1", Name: "MyApp", RedirectUris: []string{"https://app.example/callback"},
		TokenEndpointAuthMethod: "invalid_method",
	})

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	apps, listErr := storage.ListApps(context.Background(), "t1", iam.ListOptions{})
	require.NoError(t, listErr)
	assert.Empty(t, apps)
}

func TestIAMServer_RegisterApp_StorageFailureIsInternal(t *testing.T) {
	t.Parallel()
	srv := newAppErrorTestServer(t, failingWriteAppStorage{iam.NewMemoryStorage()})

	_, err := srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{
		TenantId: "t1", Name: "MyApp", RedirectUris: []string{"https://app.example/callback"},
	})

	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, status.Convert(err).Message(), appStoreFailureDetail)
}

func TestIAMServer_UpdateApp_StorageFailureIsInternal(t *testing.T) {
	t.Parallel()
	storage := iam.NewMemoryStorage()
	app, err := newAppErrorTestServer(t, storage).RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{
		TenantId: "t1", Name: "MyApp", RedirectUris: []string{"https://app.example/callback"},
	})
	require.NoError(t, err)
	srv := newAppErrorTestServer(t, failingWriteAppStorage{storage})

	_, err = srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: app.ClientId, Name: "Renamed",
	})

	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, status.Convert(err).Message(), appStoreFailureDetail)
}

// TestConvertIAMError_AppMetadataError proves the refusal is mapped by its
// type, whatever error it carries.
func TestConvertIAMError_AppMetadataError(t *testing.T) {
	t.Parallel()
	err := convertIAMError(&iam.AppMetadataError{
		Cause: &iam.ResourceURIError{Detail: "default audience must be an absolute URI"},
	})

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "default audience must be an absolute URI")
}
