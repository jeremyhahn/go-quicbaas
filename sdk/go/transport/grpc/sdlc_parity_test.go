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

package grpc

import (
	"reflect"
	"sort"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// sdlcClientVerbs reads every method of the SDLC SDK interface by
// reflection; see the REST copy of this gate for why it is not a list.
func sdlcClientVerbs() []string {
	iface := reflect.TypeOf((*sdlc.Client)(nil)).Elem()
	verbs := make([]string, 0, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verbs = append(verbs, iface.Method(i).Name)
	}
	sort.Strings(verbs)
	return verbs
}

// TestSDLCGRPCHandler_CarriesEveryVerb.
func TestSDLCGRPCHandler_CarriesEveryVerb(t *testing.T) {
	verbs := sdlcClientVerbs()
	for _, verb := range verbs {
		if _, ok := sdlcMethodMap[verb]; !ok {
			t.Errorf("the gRPC transport does not route sdlc.%s", verb)
		}
	}
	if len(sdlcMethodMap) != len(verbs) {
		t.Errorf("the gRPC transport routes %d methods, the SDK declares %d: %v",
			len(sdlcMethodMap), len(verbs), verbs)
	}
}
