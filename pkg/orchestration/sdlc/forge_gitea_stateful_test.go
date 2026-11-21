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

package sdlc

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// newStatefulGitea starts an in-memory Gitea that REMEMBERS and returns the
// real adapter pointed at it.
//
// The properties onboarding has to hold — run it twice and there is one
// webhook, delete the webhook and the next run puts it back, run it over a
// repository that already has the labels and nothing changes — are properties
// of a SEQUENCE of calls against a server that keeps state. A responder keyed
// by path agrees with whatever the implementation did and proves none of
// them; see pkg/testutil/giteafake for what the instance models and why.
//
// The adapter under test is the real GiteaBackend with its real HTTP client,
// so its transport, status handling, decoding and read-back all run against
// wire shapes restated independently of its own structs.
func newStatefulGitea(t *testing.T) (*GiteaBackend, *giteafake.Server) {
	t.Helper()
	gitea := giteafake.New(t, testRepo.Owner, testRepo.Repo)
	backend, err := NewGiteaBackend(GiteaConfig{BaseURL: gitea.APIURL(), Token: "test-token"})
	if err != nil {
		t.Fatalf("NewGiteaBackend: %v", err)
	}
	return backend, gitea
}
