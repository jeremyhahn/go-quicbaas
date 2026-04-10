# Orchestrator Integration - Remaining Work

## Current State (2026-03-31)
- 8765 PASS, 48 FAIL, 0 SKIP across full integration test suite
- Agent/fleet CLI tests pass on REST transport
- Orchestrator CLI tests partially pass on REST (tenant propagation issue)
- gRPC/QUIC/Unix/MCP transports silently skip orchestrator commands (no proto definitions)

## Priority 1: Proto Definitions for Orchestrator gRPC Service

The orchestrator has REST and MCP API handlers but NO protobuf service definitions. The gRPC server has Go methods (`OrchestratorSpawn`, `OrchestratorHealth`, etc.) but they aren't registered as a gRPC service because there's no `.proto` file.

**What's needed:**
- Add orchestrator RPC methods to `api/proto/quicbaas.proto`
- Run `protoc` to generate Go code
- Register the orchestrator service in the gRPC server
- Update the SDK gRPC handler to use proper proto types instead of JSON payloads
- This enables gRPC, QUIC, Unix, and MCP transports for orchestrator commands

**Files:**
- `api/proto/quicbaas.proto` - add orchestrator service definition
- `api/proto/quicbaas_grpc.pb.go` - regenerate
- `api/proto/quicbaas.pb.go` - regenerate
- `pkg/api/grpc/server.go` - register orchestrator service
- `sdk/go/transport/grpc/orchestrator_handler.go` - use proto types

## Priority 2: Tenant Propagation Through SDK

The SDK `ListBackends()`, `Health()`, `ListSessions()` methods don't pass tenant_id. The server's `validateTenant()` requires tenant context. When the CLI calls these via the SDK REST handler, no tenant_id query parameter is included in the URL.

**Options:**
- A: Remove tenant requirement from non-tenant-scoped operations (ListBackends, Health)
- B: Add tenant to the SDK request types and REST handler URLs
- C: Pass tenant via HTTP header instead of query parameter

## Priority 3: Pre-existing Test Failures

These failures exist independently of the orchestrator work:
- `TestIAMAdminImpersonateStart/rest` - REST transport impersonation bug
- `TestIAMAdminImpersonateEnd/rest` - same
- `TestIAMAdminImpersonateList/rest` - same
- `TestSchedulerJobLifecycle` - scheduler job lifecycle bug
- `TestSchedulerJobHistory` - scheduler history bug
