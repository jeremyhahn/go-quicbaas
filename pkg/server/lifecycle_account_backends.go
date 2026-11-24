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

package server

import (
	"sort"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// accountPooledBackend is an orchestration backend that draws a pooled
// provider account at spawn: the ACP backends and the codex exec backend.
type accountPooledBackend interface {
	SetAccountResolver(orchestration.AccountResolver)
	SetAccountCredentialLeaser(orchestration.AccountCredentialLeaser)
}

// attachAccountResolver gives every pooled backend in backends the resolver
// and the leaser, and returns the names it attached to, sorted.
//
// The leaser is only for the spawn that resolved an account and then failed
// to dispatch: no Session exists to carry the grant, so the backend withdraws
// it itself.
func attachAccountResolver(backends map[string]orchestration.AgentBackend, resolver orchestration.AccountResolver, leases orchestration.AccountCredentialLeaser) []string {
	attached := make([]string, 0, 4)
	// "claude-code" is an alias for the same program and is absent from the
	// registrations, so naming it here is a no-op today — the lookup simply
	// misses. It is named anyway because the SDLC module demands a pooled
	// account for it (orchestration.WorkerSpawnAccountAuto, go-quicbaas#235),
	// and the two lists disagreeing in THIS direction is the expensive one: a
	// backend the module pools and the server does not fails every worker
	// spawn closed, with no configuration an operator could change to fix it.
	for _, name := range []string{"claude", "claude-code", "codex", "opencode"} {
		b, ok := backends[name]
		if !ok {
			continue
		}
		pooled, ok := b.(accountPooledBackend)
		if !ok {
			continue
		}
		pooled.SetAccountResolver(resolver)
		pooled.SetAccountCredentialLeaser(leases)
		attached = append(attached, name)
	}
	sort.Strings(attached)
	return attached
}
