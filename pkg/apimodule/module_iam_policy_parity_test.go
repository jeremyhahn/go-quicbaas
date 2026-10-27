//go:build rest && mcp

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

package apimodule

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// parityPolicySpec is a policy document with one rule and no enabled field.
const parityPolicySpec = `rules:
  - name: "read-docs"
    actions: ["read"]
    resources: ["documents"]
    effect: "allow"`

// parityPolicyComponents serves one in-memory policy engine; the policy
// verbs reach nothing else.
type parityPolicyComponents struct {
	service.IAMComponents
	engine *iam.PolicyEngine
}

func (c *parityPolicyComponents) GetPolicyEngine() *iam.PolicyEngine { return c.engine }

// parityUnusedRaftNode satisfies the builder's raft node requirement; the
// policy verbs never reach it.
type parityUnusedRaftNode struct {
	interfaces.RaftNode
}

// TestPolicyTransportParity_CreateThenReadReportsEnabled covers issue
// #1929: a policy created from its document alone succeeds on every
// transport, and both the create and a later read report the stored
// enabled state. The document sets no enabled field, so it is enabled.
func TestPolicyTransportParity_CreateThenReadReportsEnabled(t *testing.T) {
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&parityUnusedRaftNode{}).
		Build()
	require.NoError(t, err)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: iam.NewMemoryStorage()})
	svc.SetIAMProvider(&parityPolicyComponents{engine: engine})

	transports := parityTransports(t, svc, func(srv grpclib.ServiceRegistrar) {
		adapter := grpcapi.NewIAMServiceAdapter(nil, nil, engine, nil, nil, nil)
		iampb.RegisterIAMServiceServer(srv, grpcapi.NewIAMServer(adapter, testLogger(t)))
	})
	ctx := context.Background()
	for name, tr := range transports {
		t.Run(name, func(t *testing.T) {
			client := sdkiam.NewPolicyClient(tr)
			policyName := "enabled-" + name

			created, err := client.Create(ctx, parityProjectTenant, &sdkiam.CreatePolicyRequest{
				Name:     policyName,
				SpecYAML: []byte(parityPolicySpec),
			})
			require.NoError(t, err, "Create")
			assert.True(t, created.Enabled, "Create reports Enabled")

			got, err := client.Get(ctx, parityProjectTenant, policyName)
			require.NoError(t, err, "Get")
			assert.Equal(t, policyName, got.Name)
			assert.True(t, got.Enabled, "Get reports Enabled")
		})
	}
}
