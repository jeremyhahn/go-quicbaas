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
/** formatAgentRef renders a reference for a person: "agent:<id>", "pool:<id>" or "". */
export function formatAgentRef(ref) {
    if (ref?.pool_id)
        return `pool:${ref.pool_id}`;
    if (ref?.agent_id)
        return `agent:${ref.agent_id}`;
    return '';
}
/**
 * parseAgentRef reads "agent:<id>" or "pool:<id>"; a bare ID is an Agent and
 * empty is no reference. It throws on any other kind.
 */
export function parseAgentRef(text) {
    const trimmed = text.trim();
    if (trimmed === '')
        return {};
    const colon = trimmed.indexOf(':');
    if (colon < 0)
        return { agent_id: trimmed };
    const kind = trimmed.slice(0, colon).trim();
    const id = trimmed.slice(colon + 1).trim();
    if (kind === 'agent')
        return { agent_id: id };
    if (kind === 'pool')
        return { pool_id: id };
    throw new Error(`a reference is agent:<id> or pool:<id>, got ${trimmed}`);
}
//# sourceMappingURL=agents.js.map