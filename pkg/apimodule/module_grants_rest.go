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

package apimodule

import (
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
)

// init registers this build's REST-route permission contribution
// (module_grants.go's restRouteContributor) so registerRoleGrants derives
// from the full REST table whenever the rest tag is compiled in.
func init() {
	restRouteContributor = func(set map[string]struct{}) {
		for _, spec := range restapi.Routes() {
			if spec.Auth == restapi.RouteAuthPermission && spec.Resource != "" && spec.Action != "" {
				set[spec.Resource+":"+spec.Action] = struct{}{}
			}
		}
	}
}
