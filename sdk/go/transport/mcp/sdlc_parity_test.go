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

package mcp

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

// TestSDLCMCPHandler_CarriesEveryVerb covers BOTH halves of the MCP mapping:
// the tool a verb calls and the transform that decodes its result. A verb
// with a tool and no transform reaches the server and then fails on the way
// back, which is a harder failure to read than not being wired at all.
func TestSDLCMCPHandler_CarriesEveryVerb(t *testing.T) {
	for _, verb := range sdlcClientVerbs() {
		tool, ok := sdlcToolName(verb)
		if !ok {
			t.Errorf("the MCP transport maps no tool for sdlc.%s", verb)
			continue
		}
		if tool == "" {
			t.Errorf("sdlc.%s maps to an empty tool name", verb)
		}
		if _, ok := sdlcMCPTransforms[verb]; !ok {
			t.Errorf("sdlc.%s calls tool %q and has no response transform", verb, tool)
		}
	}
	if len(sdlcMethodTools) != len(sdlcClientVerbs()) {
		t.Errorf("the MCP transport maps %d tools, the SDK declares %d verbs",
			len(sdlcMethodTools), len(sdlcClientVerbs()))
	}
}
