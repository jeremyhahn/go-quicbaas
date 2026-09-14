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
	"reflect"
	"sort"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestSDLCEmbeddedHandlers_CarryEveryVerb pins that the in-process transport
// routes every verb the SDK declares.
//
// The verbs are read from the interface by reflection rather than listed; see
// the REST copy of this gate for why a hand-maintained list is the bug it
// exists to catch.
func TestSDLCEmbeddedHandlers_CarryEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*sdlc.Client)(nil)).Elem()
	verbs := make([]string, 0, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verbs = append(verbs, iface.Method(i).Name)
	}
	sort.Strings(verbs)

	routed := 0
	for _, verb := range verbs {
		if _, ok := unaryHandlers["quicbaas.SDLC/"+verb]; !ok {
			t.Errorf("the embedded transport does not route quicbaas.SDLC/%s", verb)
			continue
		}
		routed++
	}
	if routed == 0 {
		t.Fatal("no SDLC verbs were checked; the gate would pass vacuously")
	}

	// The other direction: a handler the SDK cannot reach is dead code that
	// looks like surface.
	declared := make(map[string]bool, len(verbs))
	for _, verb := range verbs {
		declared[verb] = true
	}
	for key := range unaryHandlers {
		const prefix = "quicbaas.SDLC/"
		if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		if !declared[key[len(prefix):]] {
			t.Errorf("the embedded transport routes %q, which the SDK interface does not declare", key)
		}
	}
}
