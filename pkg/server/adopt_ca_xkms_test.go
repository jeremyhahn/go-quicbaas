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

package server

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// capturedGRPCServices records the gRPC services a module registers, so a
// test calls a method through the module's own generated handler.
type capturedGRPCServices map[string]struct {
	desc *grpc.ServiceDesc
	impl any
}

// RegisterService records desc and impl.
func (c capturedGRPCServices) RegisterService(desc *grpc.ServiceDesc, impl any) {
	c[desc.ServiceName] = struct {
		desc *grpc.ServiceDesc
		impl any
	}{desc, impl}
}

// call invokes service/method through its registered handler with req.
func (c capturedGRPCServices) call(t *testing.T, service, method string, req proto.Message) (any, error) {
	t.Helper()
	svc, ok := c[service]
	require.True(t, ok, "module registers %s", service)
	for _, m := range svc.desc.Methods {
		if m.MethodName == method {
			return m.Handler(svc.impl, context.Background(), func(v any) error {
				proto.Merge(v.(proto.Message), req)
				return nil
			}, nil)
		}
	}
	t.Fatalf("%s has no method %s", service, method)
	return nil, nil
}

// TestAdoptCA_InstallsTheCAOnTheXKMSService proves the adopted CA reaches the
// global XKMS service: the TCP KeystoreService installs its CA servicer from
// that service, so without it the CA RPCs never reach the server's CA.
func TestAdoptCA_InstallsTheCAOnTheXKMSService(t *testing.T) {
	initOIDCSigningDefault(t)
	t.Cleanup(dbxkms.Reset)

	srv := &Server{config: &config.Config{}}
	srv.adoptCA(newRealSoftwareCA(t))

	svc, err := xkmssdk.Get()
	require.NoError(t, err)
	require.NotNil(t, svc.CA(), "adoptCA must install the CA on the XKMS service")
}

// TestXKMSModuleService_NilUntilCASettled: the xkms service module resolves
// no service until the startup CA step has settled, so it cannot memoize a
// keystore without the CA.
func TestXKMSModuleService_NilUntilCASettled(t *testing.T) {
	initOIDCSigningDefault(t)
	t.Cleanup(dbxkms.Reset)

	srv := &Server{config: &config.Config{}}
	_, err := xkmssdk.Get()
	require.NoError(t, err, "the XKMS service is initialized")
	require.Nil(t, srv.xkmsServiceForModule(), "no service before the CA has settled")

	srv.adoptCA(newRealSoftwareCA(t))
	srv.caSettled.Store(true)

	svc := srv.xkmsServiceForModule()
	require.NotNil(t, svc, "the service resolves once the CA has settled")
	require.NotNil(t, svc.CA())
}

// TestXKMSModule_SignsAfterLateCAAdoption: a KeystoreService call that
// arrives before the CA is adopted is told to retry, and does not leave the
// module serving a CA-less keystore after adoption.
func TestXKMSModule_SignsAfterLateCAAdoption(t *testing.T) {
	initOIDCSigningDefault(t)
	t.Cleanup(dbxkms.Reset)

	srv := &Server{config: &config.Config{}}
	module, err := xkmssdk.NewServiceModule(
		srv.xkmsServiceForModule,
		xkmssdk.WithServiceModuleQRDBClient(srv.GetQRDBNodeClient),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
	)
	require.NoError(t, err)
	services := capturedGRPCServices{}
	module.RegisterGRPC(services)

	_, err = services.call(t, "xkms.v1.KeystoreService", "GetCACertificate", &xkmsv1.GetCACertificateRequest{})
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err), "an early caller is told to retry: %v", err)

	srv.adoptCA(newRealSoftwareCA(t))
	srv.caSettled.Store(true)

	resp, err := services.call(t, "xkms.v1.KeystoreService", "GetCACertificate", &xkmsv1.GetCACertificateRequest{})
	require.NoError(t, err, "the module serves the CA adopted after the early call")
	got, ok := resp.(*xkmsv1.GetCACertificateResponse)
	require.True(t, ok, "got %T", resp)
	require.NotEmpty(t, got.CertificatePem)
	require.True(t, got.IsCa)
}
