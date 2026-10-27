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

// Package policyenabled reports a stored policy's enabled state the way the
// server applies it. A policy document's spec.enabled is presence-tracked:
// absent means enabled. An entry flagged with a load error is never applied,
// so it reports disabled. Every SDK transport decodes the server's policy
// through this one rule.
package policyenabled

import "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/policyloaderror"

// Key is the JSON key of a policy's enabled state, in the server's spec and
// in the SDK's Policy.
const Key = "enabled"

// Resolve returns the enabled state of a stored policy whose document set
// stored (nil when it did not) and which loadFailed or not.
func Resolve(stored *bool, loadFailed bool) bool {
	if loadFailed {
		return false
	}
	return stored == nil || *stored
}

// Copy sets sdkPolicy's enabled from serverPolicy, a policy in the server's
// JSON shape (metadata, spec and an optional load_error).
func Copy(serverPolicy, sdkPolicy map[string]interface{}) {
	var stored *bool
	if spec, ok := serverPolicy["spec"].(map[string]interface{}); ok {
		if enabled, ok := spec[Key].(bool); ok {
			stored = &enabled
		}
	}
	_, loadFailed := serverPolicy[policyloaderror.Key]
	sdkPolicy[Key] = Resolve(stored, loadFailed)
}
