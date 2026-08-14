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

package orchestration

import "errors"

// IsPolicyDenial reports whether err is a spawn-policy refusal: the request
// was well-formed and the caller was authenticated, but the operator's policy
// forbids what it asked for.
//
// It is the single classifier every transport uses so a denial is shaped the
// same everywhere — 403 on REST, PermissionDenied on gRPC and QUIC,
// "operation not allowed" on MCP — instead of collapsing into an opaque
// Internal error that an operator cannot tell apart from a server fault. The
// four policies are the axes SpawnSession resolves before any side effect:
//
//   - MountNotAllowedError          host bind-mount source outside every root
//   - SandboxModeViolationError     isolation level the policy forbids
//   - TrustLevelViolationError      integrity tier below the operator's floor
//   - SandboxNetworkNotAllowedError network namespace the policy forbids
//
// Each carries an actionable message naming the offending field and the
// reason, which transports must preserve.
func IsPolicyDenial(err error) bool {
	if err == nil {
		return false
	}
	var mount *MountNotAllowedError
	var mode *SandboxModeViolationError
	var trust *TrustLevelViolationError
	var network *SandboxNetworkNotAllowedError
	return errors.As(err, &mount) ||
		errors.As(err, &mode) ||
		errors.As(err, &trust) ||
		errors.As(err, &network)
}
