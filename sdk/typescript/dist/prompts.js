// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
// The prompts family: the layered prompt library (platform, tenant, project).
// Each scope may override a prompt of a set; the effective body a job runs
// with is the nearest layer that answers. The wire types mirror the Go SDK's
// sdk/go/client/prompt, and the server serves them over REST
// (/api/v1/prompts/{set}[/{name}[/effective|/diff|/history|/rollback]]) and
// MCP (the prompt_* tools, pkg/api/mcp/prompt_tools.go).
import { QuicBaaSError } from './errors.js';
/** promptScopeQuery carries the scope fields the REST routes read as query. */
export function promptScopeQuery(scope) {
    const query = new URLSearchParams();
    if (scope.tenant_id)
        query.set('tenant_id', scope.tenant_id);
    if (scope.scope)
        query.set('scope', scope.scope);
    if (scope.project_id)
        query.set('project_id', scope.project_id);
    return query;
}
/**
 * promptPath builds /api/v1/prompts/{set}[/{name}]{suffix}[?query]. A verb
 * that addresses one prompt and has no name is refused before any request:
 * the route would silently become the set's listing.
 */
export function promptPath(addr, suffix, query, method) {
    if (!addr.set)
        throw new QuicBaaSError(`prompts.${method}: set is required`);
    let path = `/api/v1/prompts/${encodeURIComponent(addr.set)}`;
    if (method !== 'listPrompts') {
        if (!addr.name)
            throw new QuicBaaSError(`prompts.${method}: name is required`);
        path += `/${encodeURIComponent(addr.name)}`;
    }
    path += suffix;
    const encoded = query.toString();
    return encoded ? `${path}?${encoded}` : path;
}
//# sourceMappingURL=prompts.js.map