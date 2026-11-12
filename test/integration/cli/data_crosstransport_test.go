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

//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// =============================================================================
// Cross-transport data tests
// =============================================================================

// crossTransportDataCommand runs a data verb on whichever transport the runner
// drives, MCP included: over MCP the CLI's typed KV calls reach the
// quicbaas_kv_put/get/delete tools. Output is JSON so the test reads the
// exact bytes and the found flag.
func crossTransportDataCommand(verb string, args ...string) CommandDef {
	full := append([]string{"data", verb}, args...)
	full = append(full, "--output", "json")
	return CommandDef{
		Name:           "data " + verb,
		Args:           full,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
	}
}

// crossTransportRunners returns one authenticated MCP runner and one REST
// runner, failing the test when either transport is not enabled.
func crossTransportRunners(t *testing.T) (mcpRunner, restRunner *TestRunner) {
	t.Helper()
	for _, cfg := range GetTransportConfigs() {
		if !cfg.Enabled {
			continue
		}
		switch cfg.Transport {
		case TransportMCP:
			mcpRunner = NewTestRunner(t, cfg.Transport, cfg.Address)
			if err := mcpRunner.Authenticate(); err != nil {
				t.Fatalf("MCP authentication failed: %v", err)
			}
		case TransportREST:
			restRunner = NewTestRunner(t, cfg.Transport, cfg.Address)
		}
	}
	if mcpRunner == nil || restRunner == nil {
		t.Fatalf("cross-transport test needs MCP and REST enabled (mcp=%v rest=%v)", mcpRunner != nil, restRunner != nil)
	}
	return mcpRunner, restRunner
}

// runDataGet runs `data get` and decodes its JSON answer.
func runDataGet(t *testing.T, runner *TestRunner, entityType, id string) cli.GetResponse {
	t.Helper()
	stdout, stderr, err := runner.Run(crossTransportDataCommand("get", entityType, id))
	if err != nil {
		t.Fatalf("data get %s/%s: %v (stderr=%s)", entityType, id, err, stderr)
	}
	var got cli.GetResponse
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("data get %s/%s answered %q, not JSON: %v", entityType, id, stdout, err)
	}
	return got
}

// TestDataCrossTransport_MCPWriteRESTReadDelete writes an entity over MCP,
// reads and deletes it over REST (GET and DELETE /api/v1/entities/{type}/{id}),
// then reads it over MCP again. Both transports reach the same KV service, so
// they agree on the canonical key: the bytes match and the delete is seen.
func TestDataCrossTransport_MCPWriteRESTReadDelete(t *testing.T) {
	cases := []struct {
		name   string
		tenant bool
	}{
		{name: "SystemTenant"},
		{name: "Tenant", tenant: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mcpRunner, restRunner := crossTransportRunners(t)
			if tc.tenant {
				tenantID := UniqueKey("xtransport")
				mcpRunner.ActAsTenantAdmin(tenantID)
				restRunner.ActAsTenantAdmin(tenantID)
			}

			entityType := "xtransport_entity"
			id := strconv.FormatUint(uint64(time.Now().UnixNano()), 10)
			value := []byte("cross-transport-value")

			mcpRunner.RunAndAssert(crossTransportDataCommand("put", entityType, id, string(value)))

			got := runDataGet(t, restRunner, entityType, id)
			if !got.Found || !bytes.Equal(got.Value, value) {
				t.Fatalf("REST get after MCP put = found %v value %q, want found %q", got.Found, got.Value, value)
			}

			restRunner.RunAndAssert(crossTransportDataCommand("delete", entityType, id, "--yes"))

			miss := runDataGet(t, mcpRunner, entityType, id)
			if miss.Found {
				t.Fatalf("MCP get after REST delete = found value %q, want a miss", miss.Value)
			}
		})
	}
}
