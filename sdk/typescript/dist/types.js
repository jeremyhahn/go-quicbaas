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
// =============================================================================
// Session Spawn Types (Phase 1 additions)
// Mirrors sdk/go/client/orchestrator/client.go SpawnSessionRequest
// =============================================================================
// SandboxMode mirrors orchestration.SandboxMode.
// Use SandboxModeDefault to defer to the server's configured policy.
export const SandboxMode = {
    // Default defers to the server's configured sandbox policy.
    Default: 0,
    // Required forces container-sandboxed execution.
    Required: 1,
    // None forces raw process execution, subject to server policy.
    None: 2,
};
//# sourceMappingURL=types.js.map