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
/**
 * Paging, sorting and search — ONE request type and ONE answer type for every
 * list endpoint.
 *
 * Every orchestrator listing (jobs, tasks, sessions, agents, workflows,
 * projects, accounts) takes the same four parameters and answers with the same
 * envelope; see docs/software-factory/api.md. Seven resource-specific page
 * types would be seven chances for them to drift, so there is one of each here
 * and the resource only decides which member of the envelope holds the records.
 *
 * THE UNPAGED METHODS STILL EXIST AND STILL WORK, and they are not the same
 * question. `listJobs(tenantID)` sends no `limit`, and the server's default
 * page is 25 — so an unpaged call now answers with the FIRST 25 records and no
 * way to tell that from "there are 25". A caller that needs to know how many
 * there are, or needs the second page, asks `listJobsPage`.
 */
import { QuicBaaSError } from './errors.js';
/**
 * MalformedPageError is thrown when a list endpoint answers something that is
 * not a listing at all — a null body, a string, an object with no records
 * under the key the endpoint uses.
 *
 * IT THROWS RATHER THAN REPORTING AN EMPTY PAGE, because "the server sent
 * nothing usable" and "this tenant has no jobs" are different answers and a
 * table rendering "0 of 0" for the first one tells the operator a falsehood.
 */
export class MalformedPageError extends QuicBaaSError {
    /** itemsKey is the envelope member the records were expected under. */
    itemsKey;
    constructor(itemsKey, received) {
        super(`list response carries no "${itemsKey}" array (received ${received}); ` +
            'the endpoint answered something that is not a listing');
        this.name = 'MalformedPageError';
        this.itemsKey = itemsKey;
    }
}
// ---------------------------------------------------------------------------
// Query string
// ---------------------------------------------------------------------------
/**
 * appendPageParams writes the four shared parameters onto a query string.
 *
 * An absent field is NOT written: `limit=0` and an absent limit both mean the
 * default to the server, but sending nothing keeps the URL honest about what
 * the caller actually asked for. An empty search is likewise omitted rather
 * than sent as `q=`.
 */
export function appendPageParams(params, page) {
    if (page === undefined)
        return params;
    if (typeof page.limit === 'number' && page.limit > 0)
        params.set('limit', String(page.limit));
    if (typeof page.offset === 'number' && page.offset > 0)
        params.set('offset', String(page.offset));
    if (page.sort !== undefined && page.sort !== '')
        params.set('sort', page.sort);
    if (page.q !== undefined && page.q !== '')
        params.set('q', page.q);
    return params;
}
/** appendListParam writes one optional scalar filter, skipping empties. */
export function appendListParam(params, name, value) {
    if (value === undefined)
        return params;
    if (typeof value === 'boolean') {
        params.set(name, value ? 'true' : 'false');
        return params;
    }
    if (value !== '')
        params.set(name, value);
    return params;
}
/**
 * appendCSVParam writes a repeated filter the way every transport spells it —
 * comma-separated, as `statuses` and `job_ids` are read server-side. An empty
 * list narrows NOTHING and is omitted rather than sent as an empty value.
 */
export function appendCSVParam(params, name, values) {
    const kept = (values ?? []).filter((value) => value !== '');
    if (kept.length > 0)
        params.set(name, kept.join(','));
    return params;
}
// ---------------------------------------------------------------------------
// Envelope
// ---------------------------------------------------------------------------
/**
 * adoptPage normalises whatever a list endpoint answered into a PageResult.
 *
 * Three shapes are accepted, because three are real:
 *
 *   {jobs: [...], total, limit, offset, next_offset}  a paged answer
 *   {jobs: [...]}                                     a daemon older than
 *                                                     this SDK, which pages
 *                                                     nothing
 *   [...]                                             the same, unwrapped
 *
 * The last two are reported as ONE page holding the whole set — total is its
 * length — which is true of them and is what a table needs to render. Anything
 * else is a MalformedPageError.
 *
 * `map` converts each record, for the listings whose wire shape differs from
 * the SDK's (sessions rename `state` to `status`).
 */
export function adoptPage(raw, itemsKey, map) {
    const convert = (records) => map === undefined ? records : records.map((record) => map(record));
    if (Array.isArray(raw))
        return wholeSetPage(convert(raw));
    if (raw === null || typeof raw !== 'object') {
        throw new MalformedPageError(itemsKey, raw === null ? 'null' : typeof raw);
    }
    const record = raw;
    const items = record[itemsKey];
    if (items === undefined) {
        const pageShaped = Object.keys(record).length === 0 ||
            ['total', 'limit', 'offset', 'next_offset', 'facets'].some((key) => key in record);
        if (!pageShaped) {
            throw new MalformedPageError(itemsKey, 'no such member');
        }
    }
    if (items !== undefined && items !== null && !Array.isArray(items)) {
        throw new MalformedPageError(itemsKey, typeof items);
    }
    const converted = convert(Array.isArray(items) ? items : []);
    const facets = adoptFacets(record['facets']);
    if (typeof record['total'] !== 'number') {
        const whole = wholeSetPage(converted);
        if (facets !== undefined)
            whole.facets = facets;
        return whole;
    }
    const page = {
        items: converted,
        total: record['total'],
        limit: typeof record['limit'] === 'number' ? record['limit'] : converted.length,
        offset: typeof record['offset'] === 'number' ? record['offset'] : 0,
    };
    if (typeof record['next_offset'] === 'number')
        page.next_offset = record['next_offset'];
    if (facets !== undefined)
        page.facets = facets;
    return page;
}
/**
 * adoptFacets reads the counts out of a listing answer, keeping only the
 * numbers. A missing member, or one that is not a map of maps of numbers,
 * reports undefined — which a caller reads as "nobody counted" and answers by
 * offering its declared vocabulary uncounted.
 *
 * IT DOES NOT THROW. A malformed `facets` costs the operator the counts beside
 * the chips; refusing the whole page over it would cost them the records.
 */
export function adoptFacets(raw) {
    if (raw === null || raw === undefined || typeof raw !== 'object')
        return undefined;
    const facets = {};
    for (const [field, counts] of Object.entries(raw)) {
        if (counts === null || typeof counts !== 'object')
            continue;
        const kept = {};
        for (const [value, count] of Object.entries(counts)) {
            if (typeof count === 'number' && Number.isFinite(count))
                kept[value] = count;
        }
        facets[field] = kept;
    }
    return Object.keys(facets).length === 0 ? undefined : facets;
}
/**
 * wholeSetPage reports a complete collection as a single page.
 *
 * It is the honest answer from a transport that cannot page — one whose wire
 * format carries no paging fields, or a daemon older than this SDK. The caller
 * is told it has everything (`total` is the length and there is no
 * `next_offset`) rather than being handed a first page it would ask a second
 * of and never receive.
 */
export function wholeSetPage(items) {
    return { items, total: items.length, limit: items.length, offset: 0 };
}
//# sourceMappingURL=page.js.map