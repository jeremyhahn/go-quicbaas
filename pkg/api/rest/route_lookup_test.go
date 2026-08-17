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

import "strings"

// routePermissionForPath looks up the FROZEN Resource/Action for a concrete
// request path + method against the shipped route table (Routes(),
// routes_permissions_generated.go). It replaces the deleted live PEP mapper
// (mapRequestToPermission, authz_pep_mapping.go) as the assertion source for
// tests that pin a specific route's qrdb-canonical permission: ok is true
// only for a RouteAuthPermission route (a RouteAuthPublic or RouteAuthSession
// match reports ok=false, mirroring mapRequestToPermission's "no permission
// to check" contract for those routes).
func routePermissionForPath(path, method string) (resource, action string, ok bool) {
	for _, spec := range Routes() {
		if spec.Method != method {
			continue
		}
		if !routePatternMatches(spec.Pattern, path) {
			continue
		}
		if spec.Auth != RouteAuthPermission {
			return "", "", false
		}
		return spec.Resource, spec.Action, true
	}
	return "", "", false
}

// routeAuthForPath returns the frozen RouteAuth classification for a
// concrete request path + method, or ok=false when no route matches.
func routeAuthForPath(path, method string) (auth RouteAuth, ok bool) {
	for _, spec := range Routes() {
		if spec.Method != method {
			continue
		}
		if !routePatternMatches(spec.Pattern, path) {
			continue
		}
		return spec.Auth, true
	}
	return 0, false
}

// routePatternMatches reports whether concrete path matches a chi route
// pattern, honoring "{param}" segments (match any single non-empty segment)
// and a trailing "*" wildcard (match the remainder of the path).
func routePatternMatches(pattern, path string) bool {
	patternSegs := strings.Split(strings.Trim(pattern, "/"), "/")
	pathSegs := strings.Split(strings.Trim(path, "/"), "/")

	for i, seg := range patternSegs {
		if seg == "*" {
			return true
		}
		if i >= len(pathSegs) {
			return false
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			continue
		}
		if seg != pathSegs[i] {
			return false
		}
	}
	return len(patternSegs) == len(pathSegs)
}
