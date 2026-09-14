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

package embedded

import (
	"context"
	"errors"
	"testing"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// go-quicbaas#606 — ListRunners / ListForges on the in-process transport.
//
// The embedded transport already carried ListRunners; Forges did not exist
// anywhere but the REST route. Both are pinned here so a consumer that embeds
// the daemon sees the same surface as one that dials it.
// =============================================================================

// sdlcRunningFacade is a facade whose SDLC module is up. handleSDLCForges reads
// the process-wide forge registry rather than the service, so the service only
// has to be present — which is exactly the distinction the handler gates on.
type sdlcRunningFacade struct{ service.QuicBaaS }

func (sdlcRunningFacade) SDLC() *service.SDLCService { return &service.SDLCService{} }

// TestRunnersForgesAreDispatchedEmbedded pins both verbs into the transport's
// dispatch table. A verb absent from it is answered "unknown method", which is
// how go-quicbaas#606 presented on the transports that lacked it.
func TestRunnersForgesAreDispatchedEmbedded(t *testing.T) {
	for _, key := range []string{"quicbaas.Orchestrator/ListRunners", "quicbaas.SDLC/ListForges"} {
		if _, ok := unaryHandlers[key]; !ok {
			t.Errorf("%s is not in the embedded dispatch table", key)
		}
	}
}

// TestHandleSDLCForges renders the build's registered forge adapters. The sdlc
// package registers the Gitea factory in its own init, so a build carrying the
// module always carries at least that one.
func TestHandleSDLCForges(t *testing.T) {
	tr, err := New(sdlcRunningFacade{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := tr.Request(context.Background(), "quicbaas.SDLC", "ListForges", &sdlc.ListForgesRequest{})
	if err != nil {
		t.Fatalf("ListForges: %v", err)
	}
	forges, ok := resp.(*sdlc.ListForgesResponse)
	if !ok {
		t.Fatalf("got %T, want *sdlc.ListForgesResponse", resp)
	}

	registered := pkgsdlc.RegisteredForgeTypes()
	if len(forges.Forges) != len(registered) || forges.Count != len(registered) {
		t.Fatalf("got %v (count %d), registry holds %d", forges.Forges, forges.Count, len(registered))
	}
	for i, want := range registered {
		if forges.Forges[i] != string(want) {
			t.Fatalf("forge %d: got %q, want %q (the registry's sort order must be preserved)",
				i, forges.Forges[i], want)
		}
	}
}

// TestHandleSDLCForgesWithoutTheModule refuses typed on a node that does not
// run the module, matching the REST 503 and the gRPC Unavailable: "the module
// is off" and "this build carries no adapters" must stay distinguishable.
func TestHandleSDLCForgesWithoutTheModule(t *testing.T) {
	tr, err := New(sdlcNilFacade{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = tr.Request(context.Background(), "quicbaas.SDLC", "ListForges", &sdlc.ListForgesRequest{})
	if !errors.Is(err, pkgsdlc.ErrUnavailable) {
		t.Fatalf("ListForges without the module: got %v, want ErrUnavailable", err)
	}
}
