# Orchestrator Integration - Remaining Work

> **Status: REFERENCE** · Last verified: 2026-09-14
> Remaining orchestrator integration work.

## Current State (2026-03-31; Priority 1 re-verified DONE 2026-08-12)
- 8765 PASS, 48 FAIL, 0 SKIP across full integration test suite
- Agent/fleet CLI tests pass on REST transport
- Orchestrator CLI tests partially pass on REST (tenant propagation issue)

## Priority 1: Proto Definitions for Orchestrator gRPC Service — DONE

*(Re-verified 2026-08-12.)* This has shipped since this document was written:
`api/proto/orchestrator.proto` declares `service OrchestratorService` with
generated `orchestrator.pb.go` / `orchestrator_grpc.pb.go`, and orchestrator
handlers exist in `sdk/go/transport/{embedded,grpc,rest,quic,mcp}/`
(`unix` delegates to the gRPC transport over UDS). The earlier statement that
gRPC/QUIC/Unix/MCP silently skip orchestrator commands no longer describes the
tree. Remaining follow-up: confirm the compile-time transport interface
assertions cover the orchestrator client in every transport package (00 L4
conformance).

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

## Related designs

- [prompt-sets.md](prompt-sets.md) — the approved design for making the
  Software Factory's prompts editable, versioned, per-tenant and per-project
  data instead of Go constants, and for consolidating the feature into one
  docs folder (`docs/software-factory/`, shipped) and one package tree
  (`pkg/orchestration/{sdlc,agent,prompts}`, pending). It supersedes nothing
  here; it is the next slice of the same area.

## Related design

The autonomous control loop — what already exists (two cross-project sweepers, priority,
per-project quota, head pinning, webhook ingress, session park/resume) and the nine things
that do not — is specified in [autonomous-control-loop.md](autonomous-control-loop.md).
