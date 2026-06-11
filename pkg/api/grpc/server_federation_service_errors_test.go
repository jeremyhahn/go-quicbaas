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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestFederationServer_GetProvider_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("GetProvider", mock.Anything, "t-1", "p-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetProvider(context.Background(), &iampb.GetProviderRequest{TenantId: "t-1", ProviderId: "p-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestFederationServer_UpdateProvider_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("GetProvider", mock.Anything, "t-1", "p-1").
		Return(&iam.ExternalProvider{ID: "p-1", TenantID: "t-1", Name: "Original", Type: iam.ProviderTypeOIDC, ClientID: "c1", IssuerURL: "https://issuer.example.com"}, nil)
	mgr.On("UpdateProvider", mock.Anything, mock.Anything).Return(&dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.UpdateProvider(context.Background(), &iampb.UpdateProviderRequest{
		TenantId: "t-1", ProviderId: "p-1", Name: "Google",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestFederationServer_DeleteProvider_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("DeleteProvider", mock.Anything, "t-1", "p-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.DeleteProvider(context.Background(), &iampb.DeleteProviderRequest{TenantId: "t-1", ProviderId: "p-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestFederationServer_ListProviders_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("ListProviders", mock.Anything, "t-1", mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ListProviders(context.Background(), &iampb.ListProvidersRequest{TenantId: "t-1"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestFederationServer_BeginFederatedLogin_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("BeginLogin", mock.Anything, "t-1", "google", "http://cb").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.BeginFederatedLogin(context.Background(), &iampb.BeginFederatedLoginRequest{
		TenantId: "t-1", ProviderName: "google", RedirectUri: "http://cb",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestFederationServer_HandleFederatedCallback_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("HandleCallback", mock.Anything, "state-1", "code-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "test"})

	_, err := srv.HandleFederatedCallback(context.Background(), &iampb.HandleFederatedCallbackRequest{
		State: "state-1", Code: "code-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestFederationServer_LinkAccount_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("LinkAccount", mock.Anything, "u-1", "t-1", "p-1", "token").Return(&dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test"})

	_, err := srv.LinkAccount(context.Background(), &iampb.LinkAccountRequest{
		UserId: "u-1", TenantId: "t-1", ProviderId: "p-1", ExternalToken: "token",
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestFederationServer_UnlinkAccount_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("UnlinkAccount", mock.Anything, "u-1", "t-1", "p-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.UnlinkAccount(context.Background(), &iampb.UnlinkAccountRequest{
		UserId: "u-1", TenantId: "t-1", ProviderId: "p-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestFederationServer_GetUserLinks_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	mgr.On("GetUserLinks", mock.Anything, "u-1", "t-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.GetUserLinks(context.Background(), &iampb.GetUserLinksRequest{
		UserId: "u-1", TenantId: "t-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
