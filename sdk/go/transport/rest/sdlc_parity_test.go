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
	"reflect"
	"sort"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// sdlcClientVerbs is every method of the SDLC SDK interface, read by
// reflection rather than written out.
//
// A HAND-MAINTAINED LIST IS THE BUG THIS GATE EXISTS TO CATCH. The whole
// premise of the module — one engine, many repositories, nothing copied — only
// holds if a verb added to the SDK reaches every transport. A list somebody
// has to remember to update is the same failure at a smaller scale: the verb
// ships, four transports carry it, two answer "unknown method", and nothing
// says so until a caller on the wrong transport hits it.
func sdlcClientVerbs() []string {
	iface := reflect.TypeOf((*sdlc.Client)(nil)).Elem()
	verbs := make([]string, 0, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verbs = append(verbs, iface.Method(i).Name)
	}
	sort.Strings(verbs)
	return verbs
}

// TestSDLCRESTHandler_CarriesEveryVerb, in both directions: a verb the
// transport does not route answers a routing miss, and a route the SDK
// cannot reach is dead code that looks like surface.
func TestSDLCRESTHandler_CarriesEveryVerb(t *testing.T) {
	verbs := sdlcClientVerbs()
	declared := make(map[string]bool, len(verbs))
	for _, verb := range verbs {
		declared[verb] = true
		if _, ok := sdlcMethods[verb]; !ok {
			t.Errorf("the REST transport does not route sdlc.%s; the SDK would answer a routing miss", verb)
		}
	}
	for method := range sdlcMethods {
		if !declared[method] {
			t.Errorf("the REST transport routes %q, which the SDK interface does not declare", method)
		}
	}
}
