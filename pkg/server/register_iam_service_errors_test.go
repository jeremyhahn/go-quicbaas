//go:build raft && rest && quic && mcp && iam && vector && !integration

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

// register_iam_service_errors_test.go covers the getter-error early-return
// branches of registerIAMService. The function fetches the core IAM components
// (tenant manager, identity store, policy engine, app manager) from the
// quicBaaSService via the iamComponentsGetter interface and aborts gRPC
// registration the moment any of them errors. Those abort branches are
// unreachable on the happy path (the real service never errors mid-startup),
// so they are driven here with a getter fake whose Nth getter fails.

package server

import (
	"context"
	"errors"
	"testing"

	ggrpc "google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errIAMGetter embeds service.QuicBaaS (nil) so it satisfies the full
// quicBaaSService interface, and implements iamComponentsGetter so
// registerIAMService takes the getter path. failAt selects which getter returns
// an error: 0=tenant, 1=identity, 2=policy, 3=app. Getters before failAt return
// a usable zero component; the failing getter returns sentinel; later getters
// are never reached.
type errIAMGetter struct {
	service.QuicBaaS
	failAt   int
	sentinel error
}

func (g *errIAMGetter) GetTenantManager(context.Context) (iam.TenantManager, error) {
	if g.failAt == 0 {
		return nil, g.sentinel
	}
	return nil, nil
}

func (g *errIAMGetter) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	if g.failAt == 1 {
		return nil, g.sentinel
	}
	return nil, nil
}

func (g *errIAMGetter) GetPolicyEngine(context.Context) (*iam.PolicyEngine, error) {
	if g.failAt == 2 {
		return nil, g.sentinel
	}
	return nil, nil
}

func (g *errIAMGetter) GetAppManager(context.Context) (*iam.AppManager, error) {
	if g.failAt == 3 {
		return nil, g.sentinel
	}
	return nil, nil
}

// The remaining getters are only reached after the four core getters succeed,
// which never happens in these tests; they return zero values to satisfy the
// interface.
func (g *errIAMGetter) GetAdminManager(context.Context) (*iam.AdminManager, error) {
	return nil, nil
}
func (g *errIAMGetter) GetGDPRManager(context.Context) (*iam.GDPRManager, error) { return nil, nil }
func (g *errIAMGetter) GetFederationManager(context.Context) (iam.FederationManager, error) {
	return nil, nil
}
func (g *errIAMGetter) GetMFAManager(context.Context) (service.MFAManager, error) { return nil, nil }
func (g *errIAMGetter) GetAuditLogger(context.Context) (iam.AuditLogger, error)   { return nil, nil }
func (g *errIAMGetter) GetSessionStorage(context.Context) (iam.SessionStorage, error) {
	return nil, nil
}

// TestRegisterIAMService_GetterErrorsAbort drives each of the four core-getter
// error branches: registerIAMService must return early (skip registration)
// without panicking when a getter fails.
func TestRegisterIAMService_GetterErrorsAbort(t *testing.T) {
	saveAndRestoreSeams(t)

	cases := []struct {
		name   string
		failAt int
	}{
		{"tenant-manager", 0},
		{"identity-store", 1},
		{"policy-engine", 2},
		{"app-manager", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createFTConfig(t)
			srv, err := NewServer(cfg)
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}
			srv.quicBaaSService = &errIAMGetter{
				failAt:   tc.failAt,
				sentinel: errors.New("component unavailable"),
			}

			grpcServer := ggrpc.NewServer()
			defer grpcServer.Stop()

			// Must return cleanly (early-return branch) without panicking.
			srv.registerIAMService(grpcServer)
		})
	}
}
