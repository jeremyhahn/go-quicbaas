//go:build rest

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

package rest

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestRouteSurfaceParity pins the complete chi route surface (every
// method+pattern the router serves, including the IAM/OIDC, ACME directory,
// docs, and UI mounts) against testdata/route_surface.golden. The golden file
// was captured from the router IMMEDIATELY BEFORE the table-driven
// conversion (routes.go), so this test is the permanent proof that the
// table-driven registration reproduces the historical surface exactly. Any
// intentional surface change must update the golden file in the same change
// set.
func TestRouteSurfaceParity(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(testutil.NewMockQuicBaaS(), DefaultConfig(), log)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	walkErr := chi.Walk(srv.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, fmt.Sprintf("%s %s", method, route))
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	sort.Strings(got)

	raw, err := os.ReadFile("testdata/route_surface.golden")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSpace(string(raw)), "\n")
	sort.Strings(want)

	// expand canonicalizes chi's two representations of a collection route.
	// The historical registration nested Get("/") inside Route("/parent"),
	// which chi walks as "/parent/" but ALSO matches as the bare "/parent"
	// (the mount dispatches it as "/"); the table-driven registration makes
	// that implied bare alias an explicit registration. Both representations
	// therefore expand to the same matchable set {/parent, /parent/}, and a
	// genuinely lost or added route still fails the comparison.
	expand := func(routes []string) map[string]struct{} {
		set := make(map[string]struct{}, 2*len(routes))
		for _, route := range routes {
			set[route] = struct{}{}
			if bare := strings.TrimSuffix(route, "/"); bare != route {
				if !strings.HasSuffix(bare, " ") { // never alias a mount root like "GET /"
					set[bare] = struct{}{}
				}
			}
		}
		return set
	}

	wantSet := expand(want)
	gotSet := expand(got)
	for g := range gotSet {
		if _, ok := wantSet[g]; !ok {
			t.Errorf("route not in golden surface: %s", g)
		}
	}
	for w := range wantSet {
		if _, ok := gotSet[w]; !ok {
			t.Errorf("golden route missing from router: %s", w)
		}
	}
}
