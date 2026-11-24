// Guards the console coverage matrices in docs/desktop-gui against drift
// from the code.
//
// What it guards:
// - every `covered: <file>` cell of the REST matrix (coverage-matrix.md) and
//   the gRPC matrix (coverage-matrix-grpc.md) names files that exist: a
//   console file under src/ or, in the gRPC matrix, a desktop binding
//   cmd/quicbaas/bindings_*.go;
// - a short pinned list of routes a view is known to call is marked covered by
//   a file whose source makes that SDK call;
// - the gRPC matrix has no row left `unaudited`.
//
// What it does not guard: it does not prove that a `not covered` row is truly
// uncovered. Re-deriving the whole matrix from the SDK's path builders would
// duplicate the audit in code; the matrices' own rules decide those rows.
import { describe, it, expect } from 'vitest';
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const docsDir = resolve(__dirname, '../../../../docs/desktop-gui');
const srcDir = resolve(__dirname, '../src');
const quicbaasDir = resolve(__dirname, '../..');

// tableRows returns the cells of every markdown table body row, skipping
// separator rows.
function tableRows(markdown: string): string[][] {
  const rows: string[][] = [];
  for (const line of markdown.split('\n')) {
    if (!line.startsWith('|')) continue;
    const cells = line.split('|').slice(1, -1).map((c) => c.trim());
    if (cells.every((c) => /^-+$/.test(c))) continue;
    rows.push(cells);
  }
  return rows;
}

// coveredFiles returns the files a `covered: a, b` cell names, or null when
// the cell is not a covered cell. A parenthesised note after a file, such as
// "(direct fetch, not the SDK)", is not a file.
function coveredFiles(cell: string): string[] | null {
  const m = /^covered: (.+)$/.exec(cell);
  if (!m) return null;
  return m[1]
    .replace(/\s*\([^)]*\)/g, '')
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '');
}

const bindingRe = /^(?:cmd\/quicbaas\/)?(bindings_[^/]*\.go)$/;

// consolePath resolves a file a covered cell names. Desktop bindings count
// only where allowBindings is set (the gRPC matrix).
function consolePath(file: string, allowBindings: boolean): string {
  const m = bindingRe.exec(file);
  if (m && allowBindings) return resolve(quicbaasDir, m[1]);
  return resolve(srcDir, file);
}

const restRows = tableRows(readFileSync(resolve(docsDir, 'coverage-matrix.md'), 'utf8'));
const grpcRows = tableRows(readFileSync(resolve(docsDir, 'coverage-matrix-grpc.md'), 'utf8'));

function missingFiles(rows: string[][], allowBindings: boolean): string[] {
  const missing: string[] = [];
  for (const cells of rows) {
    const files = coveredFiles(cells[cells.length - 1]);
    if (!files) continue;
    for (const file of files) {
      if (!existsSync(consolePath(file, allowBindings))) {
        missing.push(`${cells[0]} ${cells[1]}: ${file}`);
      }
    }
  }
  return missing;
}

// Routes a view is known to call, and the SDK call its source must contain.
const pinnedCalls: { method: string; path: string; call: string }[] = [
  { method: 'GET', path: '/api/v1/scheduler/status', call: '.getStatus(' },
  { method: 'GET', path: '/api/v1/scheduler/nodes', call: '.listNodes(' },
  { method: 'GET', path: '/api/v1/scheduler/placements', call: '.listPlacements(' },
  { method: 'POST', path: '/api/v1/orchestrator/tasks/decompose', call: '.decomposeTask(' },
];

describe('console coverage matrices', () => {
  it('parses both matrices', () => {
    expect(restRows.length).toBeGreaterThan(100);
    expect(grpcRows.length).toBeGreaterThan(100);
  });

  it('every covered cell in the REST matrix names a file that exists', () => {
    expect(missingFiles(restRows, false)).toEqual([]);
  });

  it('every covered cell in the gRPC matrix names a file that exists', () => {
    expect(missingFiles(grpcRows, true)).toEqual([]);
  });

  it('a route a view calls is not marked not covered', () => {
    const wrong: string[] = [];
    for (const { method, path, call } of pinnedCalls) {
      const row = restRows.find((c) => c[0] === method && c[1] === '`' + path + '`');
      if (!row) {
        wrong.push(`${method} ${path}: no row`);
        continue;
      }
      const files = coveredFiles(row[row.length - 1]);
      if (!files) {
        wrong.push(`${method} ${path}: ${row[row.length - 1]}`);
        continue;
      }
      const calls = files.some((file) => {
        const p = consolePath(file, false);
        return existsSync(p) && readFileSync(p, 'utf8').includes(call);
      });
      if (!calls) wrong.push(`${method} ${path}: ${files.join(', ')} does not call ${call}`);
    }
    expect(wrong).toEqual([]);
  });

  it('no unaudited row is left in the gRPC matrix', () => {
    const unaudited = grpcRows.filter((c) => c[c.length - 1] === 'unaudited').map((c) => c[0]);
    expect(unaudited).toEqual([]);
  });
});
