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

//go:build integration

package scheduler

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// operatorGRPCAuth returns the client option that makes every RPC of a gRPC
// client carry the tenant operator's bearer. The built-in Operator role holds
// the scheduler job permissions the SchedulerService job RPCs require
// (pkg/api/grpc/authz_mapping.go), and this package's REST clients act as the
// same principal, so a job is visible over either transport.
func operatorGRPCAuth(t *testing.T) grpcclient.ClientOption {
	t.Helper()
	return grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t)))
}

// kmsAdminMethods are the KMS RPCs this suite sends whose permissions,
// keys:create and keys:delete (pkg/api/grpc/authz_mapping.go), go-qrdb's
// built-in Operator role does not hold and its Admin role does.
var kmsAdminMethods = map[string]struct{}{
	pb.KeyManagement_GenerateKey_FullMethodName: {},
	pb.KeyManagement_DeleteKey_FullMethodName:   {},
}

// kmsDutyCredentials sends, on each KMS RPC, the bearer of the one suite
// principal whose role holds that RPC's permission: the tenant principal, the
// tenant's admin, for the RPCs named in kmsAdminMethods and the tenant
// operator for every other RPC this suite sends (keys:list, keys:rotate,
// keys:encrypt and keys:decrypt). No built-in role holds both keys:create and
// keys:encrypt, so no single principal can drive the grace-period tests. Both
// principals are users of one tenant, the tenant of this package's scheduler
// REST client, so a key the admin creates is the key the operator rotates and
// the grace-period job of that rotation is a job the operator can read. It
// implements credentials.PerRPCCredentials.
type kmsDutyCredentials struct {
	admin    credentials.PerRPCCredentials
	operator credentials.PerRPCCredentials
}

// GetRequestMetadata returns the admin's bearer for an RPC named in
// kmsAdminMethods and the operator's bearer for any other, including an RPC
// whose name the transport does not report.
func (c *kmsDutyCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	if info, ok := credentials.RequestInfoFromContext(ctx); ok {
		if _, adminOnly := kmsAdminMethods[info.Method]; adminOnly {
			return c.admin.GetRequestMetadata(ctx, uri...)
		}
	}
	return c.operator.GetRequestMetadata(ctx, uri...)
}

// RequireTransportSecurity reports that a bearer is never sent in the clear.
func (c *kmsDutyCredentials) RequireTransportSecurity() bool {
	return c.admin.RequireTransportSecurity() || c.operator.RequireTransportSecurity()
}

// kmsGRPCAuth returns the client option that makes every RPC of a KMS gRPC
// client carry the bearer kmsDutyCredentials chooses for it.
func kmsGRPCAuth(t *testing.T) grpcclient.ClientOption {
	t.Helper()
	return grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(&kmsDutyCredentials{
		admin:    helpers.TenantGRPCCredentials(t),
		operator: helpers.TenantOperatorGRPCCredentials(t),
	}))
}
