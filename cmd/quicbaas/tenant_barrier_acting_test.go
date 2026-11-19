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

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// The unseal verb names --tenant on the context it calls the tenant client
// with. On the embedded transport that is the tenant the in-process caller
// acts for; without it the server refuses the unseal.
func TestTenantBarrierUnseal_NamesTheTenantOnTheContext(t *testing.T) {
	defer resetTenantBarrierFlags()

	var named string
	var bound bool
	fc := &fakeTenantBarrierClient{
		unsealFunc: func(ctx context.Context, _ *qrdbtransport.TenantBarrierUnsealRequest) error {
			named, bound = tenantctx.Lookup(ctx)
			return nil
		},
	}
	defer withFakeTenantBarrier(fc)()
	tenantBarrierTenantID = "acme"

	cmd := newTenantBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("tenant-pp\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.True(t, bound, "the unseal context must name a tenant")
	assert.Equal(t, "acme", named)
}

// The unseal-share verb names --tenant on its context the same way.
func TestTenantBarrierUnsealShare_NamesTheTenantOnTheContext(t *testing.T) {
	resetTenantBarrierFlags()
	defer resetTenantBarrierFlags()

	var named string
	var bound bool
	fc := &fakeTenantBarrierClient{
		shareFunc: func(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealShareRequest) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
			named, bound = tenantctx.Lookup(ctx)
			return &qrdbtransport.TenantBarrierUnsealProgress{TenantID: req.TenantID, Required: 3, Submitted: 1}, nil
		},
	}
	defer withFakeTenantBarrier(fc)()
	tenantBarrierTenantID = "acme"
	tenantBarrierShare = "share-secret-value"

	cmd := &cobra.Command{Use: "unseal-share"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierUnsealShareCommand(cmd, nil))
	assert.True(t, bound, "the unseal-share context must name a tenant")
	assert.Equal(t, "acme", named)
}
