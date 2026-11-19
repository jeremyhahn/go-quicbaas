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

//go:build integration && quic

package kms

import (
	"context"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// principalEnrolTimeout bounds the enrolment and first login of a suite
// principal made on behalf of a client that has no test to fail.
const principalEnrolTimeout = 2 * time.Minute

// cryptoUseMethods are the KMS RPCs that perform a cryptographic primitive
// with a key: their permissions (keys:encrypt, keys:decrypt, keys:sign,
// keys:verify, keys:derive; pkg/api/grpc/authz_mapping.go) are held by
// go-qrdb's built-in Operator role and withheld from its Admin role. Every
// other KMS RPC needs a key-lifecycle or inventory permission (keys:create,
// keys:import, keys:export, keys:wrap, keys:unwrap, keys:rotate, keys:delete,
// keys:get, keys:list, backends:read), all of which the Admin role holds and
// several of which the Operator role does not. No built-in role holds both
// sets, so no single principal can drive this suite.
var cryptoUseMethods = map[string]struct{}{
	pb.Crypto_Encrypt_FullMethodName:         {},
	pb.Crypto_EncryptBatch_FullMethodName:    {},
	pb.Crypto_Decrypt_FullMethodName:         {},
	pb.Crypto_DecryptBatch_FullMethodName:    {},
	pb.Crypto_Sign_FullMethodName:            {},
	pb.Crypto_Verify_FullMethodName:          {},
	pb.Crypto_GenerateDataKey_FullMethodName: {},
	pb.Crypto_GenerateHMAC_FullMethodName:    {},
	pb.Crypto_VerifyHMAC_FullMethodName:      {},
	pb.Crypto_Hash_FullMethodName:            {},
	pb.Crypto_GenerateRandom_FullMethodName:  {},
}

// dutyCredentials sends, on each RPC, the bearer of the one suite principal
// whose role holds that RPC's permission: the tenant operator for the
// cryptographic-use RPCs and the tenant principal, the tenant's admin, for
// everything else. Both are users of the same tenant, so a key the admin
// creates is the key the operator uses. It implements
// credentials.PerRPCCredentials.
type dutyCredentials struct {
	admin    credentials.PerRPCCredentials
	operator credentials.PerRPCCredentials
}

// GetRequestMetadata returns the operator's bearer for an RPC named in
// cryptoUseMethods and the admin's bearer for any other, including an RPC
// whose name the transport does not report.
func (c *dutyCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	if info, ok := credentials.RequestInfoFromContext(ctx); ok {
		if _, cryptoUse := cryptoUseMethods[info.Method]; cryptoUse {
			return c.operator.GetRequestMetadata(ctx, uri...)
		}
	}
	return c.admin.GetRequestMetadata(ctx, uri...)
}

// RequireTransportSecurity reports that a bearer is never sent in the clear.
func (c *dutyCredentials) RequireTransportSecurity() bool {
	return c.admin.RequireTransportSecurity() || c.operator.RequireTransportSecurity()
}

// kmsGRPCAuth returns the client option that makes every RPC of a KMS gRPC
// client, over TCP or the Unix socket, carry the bearer dutyCredentials
// chooses for it.
func kmsGRPCAuth(t *testing.T) grpcclient.ClientOption {
	t.Helper()
	return grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(&dutyCredentials{
		admin:    helpers.TenantGRPCCredentials(t),
		operator: helpers.TenantOperatorGRPCCredentials(t),
	}))
}

// refusingTransport fails every request with the error that kept the client
// from being built, so the cause reaches the test that first uses the client.
type refusingTransport struct {
	err error
}

// RoundTrip closes the request body, as the RoundTripper contract requires,
// and returns the stored error.
func (r refusingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	return nil, r.err
}

// newKMSHTTPClient returns helpers.NewHTTPClient's client with every request
// carrying the tenant principal's bearer. The KMS REST routes need keys:get,
// keys:list, keys:create, keys:rotate or keys:delete
// (pkg/api/rest/routes_permissions_generated.go), all of which the tenant's
// Admin role holds. The REST client constructors of this package take no
// test, so a principal that cannot be enrolled yields a client whose every
// request returns the enrolment error.
func newKMSHTTPClient(timeout time.Duration) *http.Client {
	ctx, cancel := context.WithTimeout(context.Background(), principalEnrolTimeout)
	defer cancel()
	client, err := helpers.TenantHTTPClient(ctx, timeout)
	if err != nil {
		return &http.Client{Timeout: timeout, Transport: refusingTransport{err: err}}
	}
	return client
}
