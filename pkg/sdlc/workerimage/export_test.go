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

package workerimage

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// StaleImage is the managed image the fake engine holds with CLIs behind the
// registry, so every agent pointed at it is refreshed.
const StaleImage = staleImage

// CandidateTag is the local tag a local-mode pass repoints stale agents to.
const CandidateTag = candidateTag

// NewCatalogRefresher builds a refresher over this package's fakes and the
// given agent catalog, for the external tests that drive the real catalog
// service (which imports this package, so cannot be used from inside it).
func NewCatalogRefresher(t *testing.T, tenants []string, bindings map[string][]*sdlc.ProjectBinding, agents Agents) *Refresher {
	t.Helper()
	h := newHarness(t, testConfig())
	h.r.deps.Tenants = fakeTenants(tenants)
	h.r.deps.Bindings = &fakeBindings{byTenant: bindings}
	h.r.deps.Agents = agents
	return h.r
}
