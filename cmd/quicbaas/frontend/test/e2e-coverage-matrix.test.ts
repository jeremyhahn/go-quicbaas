/**
 * E2E coverage matrix: test/e2e/README.md holds one row per router name,
 * with the primary flows of the view and the Playwright specs that open it.
 *
 * RouteName is a type with no runtime list, so this test reads the router
 * source text and the README table and compares the two. A new route cannot
 * land without a row, a row cannot name a route the router does not define,
 * and a row cannot cite a spec file that does not exist.
 *
 * This file lives outside test/e2e because Playwright's testDir is
 * ./test/e2e and vitest's include is test/**\/*.test.ts.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const routerPath = resolve(__dirname, '../src/stores/router.svelte.ts');
const readmePath = resolve(__dirname, 'e2e/README.md');
const specDir = resolve(__dirname, 'e2e');

const UNION_MARKER = 'export type RouteName =';
const TABLE_HEADER = '| Route | Hash | Flows | Specs |';

interface MatrixRow {
  route: string;
  specs: string[];
}

/** Every 'name' literal of the RouteName union, in declaration order. */
function routeNames(source: string): string[] {
  const start = source.indexOf(UNION_MARKER);
  if (start < 0) {
    throw new Error(`router source has no "${UNION_MARKER}"`);
  }
  const end = source.indexOf(';', start);
  const union = source.slice(start, end < 0 ? undefined : end);
  return Array.from(union.matchAll(/'([a-z][a-z-]*)'/g), (m) => m[1] ?? '');
}

/** The rows of the coverage table that follows TABLE_HEADER. */
function matrixRows(markdown: string): MatrixRow[] {
  const lines = markdown.split('\n');
  const headerIdx = lines.findIndex((l) => l.trim() === TABLE_HEADER);
  if (headerIdx < 0) {
    throw new Error(`markdown has no "${TABLE_HEADER}" table header`);
  }
  const rows: MatrixRow[] = [];
  // headerIdx + 1 is the separator row.
  for (const raw of lines.slice(headerIdx + 2)) {
    const line = raw.trim();
    if (!line.startsWith('|')) break;
    const cells = line.split('|').slice(1, -1).map((c) => c.trim());
    const route = (cells[0] ?? '').replace(/`/g, '');
    const specCell = cells[3] ?? '';
    const specs =
      specCell === 'none'
        ? []
        : specCell.split(',').map((s) => s.trim()).filter((s) => s !== '');
    rows.push({ route, specs });
  }
  return rows;
}

const names = routeNames(readFileSync(routerPath, 'utf8'));
const rows = matrixRows(readFileSync(readmePath, 'utf8'));
const rowRoutes = rows.map((r) => r.route);

describe('E2E coverage matrix', () => {
  it('has a row for every router name', () => {
    const missing = names.filter((n) => !rowRoutes.includes(n));
    expect(missing, `routes with no row in test/e2e/README.md: ${missing.join(', ')}`).toEqual([]);
  });

  it('has no row for a name the router does not define', () => {
    const unknown = rowRoutes.filter((r) => !names.includes(r));
    expect(unknown, `rows naming no router name: ${unknown.join(', ')}`).toEqual([]);
  });

  it('has exactly one row per router name', () => {
    const repeated = rowRoutes.filter((r, i) => rowRoutes.indexOf(r) !== i);
    expect(repeated, `routes with more than one row: ${repeated.join(', ')}`).toEqual([]);
  });

  it('cites only spec files that exist', () => {
    const bad: string[] = [];
    for (const row of rows) {
      for (const spec of row.specs) {
        if (!spec.endsWith('.spec.ts') || !existsSync(resolve(specDir, spec))) {
          bad.push(`${row.route}: ${spec}`);
        }
      }
    }
    expect(bad, `rows citing a missing spec file: ${bad.join('; ')}`).toEqual([]);
  });

  it('parses the router union', () => {
    expect(routeNames("export type RouteName =\n  | 'a'\n  // comment\n  | 'b-c';\nconst x = 'd';")).toEqual([
      'a',
      'b-c',
    ]);
    expect(() => routeNames('type Other = string;')).toThrow();

    const fixture = [
      '# Title',
      '',
      TABLE_HEADER,
      '| --- | --- | --- | --- |',
      '| `alpha` | #/alpha | list | none |',
      '| `beta` | #/beta/:id | detail | one.spec.ts, two.spec.ts |',
      '',
      '| `gamma` | #/gamma | list | none |',
    ].join('\n');
    expect(matrixRows(fixture)).toEqual([
      { route: 'alpha', specs: [] },
      { route: 'beta', specs: ['one.spec.ts', 'two.spec.ts'] },
    ]);
    expect(() => matrixRows('no table here')).toThrow();

    expect(names.length).toBeGreaterThanOrEqual(40);
    expect(rows.length).toBeGreaterThanOrEqual(40);
  });
});
