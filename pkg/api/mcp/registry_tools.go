//go:build mcp

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

package mcp

// Service registry tool definitions. Lease IDs cross the MCP boundary as
// decimal strings: MCP arguments and results are JSON, and a JSON number
// above 2^53 is silently rounded by every float-based decoder.
var (
	// RegistryRegisterTool lists a service instance under a new lease.
	RegistryRegisterTool = Tool{
		Name:        "quicbaas_registry_register",
		Description: "Register a service instance in the service registry under a lease. The instance stays listed while the lease is renewed and disappears when it lapses. Returns the lease ID",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"service":     {Type: "string", Description: "Service name the instance belongs to (no '/')"},
				"id":          {Type: "string", Description: "Instance ID, unique within the service (no '/')"},
				"address":     {Type: "string", Description: "Instance network address, host:port"},
				"metadata":    {Type: "object", Description: "Optional string key-value metadata"},
				"ttl_seconds": {Type: "integer", Description: "Lease TTL in seconds (1 to 86400)"},
			},
			Required: []string{"service", "id", "address", "ttl_seconds"},
		},
	}

	// RegistryRenewTool renews a registration's lease.
	RegistryRenewTool = Tool{
		Name:        "quicbaas_registry_renew",
		Description: "Renew the lease backing one of your service registrations and report the TTL it was reset to",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"lease_id": {Type: "string", Description: "Lease ID returned by quicbaas_registry_register, as a decimal string"},
			},
			Required: []string{"lease_id"},
		},
	}

	// RegistryDeregisterTool revokes a registration's lease.
	RegistryDeregisterTool = Tool{
		Name:        "quicbaas_registry_deregister",
		Description: "Deregister one of your service instances by revoking the lease that backs it",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"lease_id": {Type: "string", Description: "Lease ID returned by quicbaas_registry_register, as a decimal string"},
			},
			Required: []string{"lease_id"},
		},
	}

	// RegistryResolveTool lists a service's instances.
	RegistryResolveTool = Tool{
		Name:        "quicbaas_registry_resolve",
		Description: "List a service's registered instances and the store revision they were read at. Watch from revision+1 to observe every later change",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"service": {Type: "string", Description: "Service name"},
			},
			Required: []string{"service"},
		},
	}

	// RegistryWatchTool collects a service's registration changes for a
	// bounded time. MCP is request/response, so a watch is a long poll: the
	// result's revision is the cursor, and calling again with
	// from_revision = revision+1 continues the stream with nothing lost.
	RegistryWatchTool = Tool{
		Name: "quicbaas_registry_watch",
		Description: "Wait up to timeout_seconds for registration changes of a service starting at from_revision, and return them. " +
			"Call again with from_revision = revision+1 to continue without gaps. compacted=true means the start revision is " +
			"older than the retained history: resolve again and watch from its revision",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"service":         {Type: "string", Description: "Service name"},
				"from_revision":   {Type: "integer", Description: "First revision to deliver; 0 watches only changes made during this call"},
				"timeout_seconds": {Type: "integer", Description: "Longest wait for a change, 1 to 30 seconds (default 5)"},
			},
			Required: []string{"service"},
		},
	}
)

// getAllRegistryTools returns all service registry tools (5 tools).
func getAllRegistryTools() []Tool {
	return []Tool{
		RegistryRegisterTool,
		RegistryRenewTool,
		RegistryDeregisterTool,
		RegistryResolveTool,
		RegistryWatchTool,
	}
}
