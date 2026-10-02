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

import "testing"

func routeSet(specs []RouteSpec) map[string]bool {
	out := make(map[string]bool, len(specs))
	for _, spec := range specs {
		out[spec.Method+" "+spec.Pattern] = true
	}
	return out
}

// TestModuleRoutes_LeaveTheQRDBNativeWireRoutesToQRDB pins both halves of the
// split: the sealed-window listener (Routes) still serves every wire route,
// and the unified server's module surface (ModuleRoutes) carries none of the
// ones go-qrdb serves there itself — a duplicate makes go-qrdb's registrar
// panic and the node never boots.
func TestModuleRoutes_LeaveTheQRDBNativeWireRoutesToQRDB(t *testing.T) {
	all, module := routeSet(Routes()), routeSet(ModuleRoutes())
	for route := range qrdbNativeWireRoutes {
		if !all[route] {
			t.Errorf("%s is listed as go-qrdb-native but is not in Routes(); drop it from the list", route)
		}
		if module[route] {
			t.Errorf("%s is registered on the unified server, where go-qrdb already serves it", route)
		}
	}
	if !module["GET /v1/ceremony/officers"] {
		t.Error("GET /v1/ceremony/officers is quicbaas's own and must stay on the module surface")
	}
}
