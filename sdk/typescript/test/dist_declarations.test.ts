// The package publishes dist/index.d.ts as its type entry point, so every
// declaration file a dist/*.d.ts imports or re-exports must be in dist/ too,
// and every src module must have its compiled declaration there.
import { describe, expect, it } from 'vitest';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const dist = join(root, 'dist');
const src = join(root, 'src');

const relativeSpecifiers = (text: string): string[] => {
  const out: string[] = [];
  const re = /(?:from|import)\s*\(?\s*['"](\.{1,2}\/[^'"]+)['"]/g;
  for (let m = re.exec(text); m !== null; m = re.exec(text)) out.push(m[1]);
  return out;
};

describe('published dist declarations', () => {
  it('resolve every relative module they reference', () => {
    const missing: string[] = [];
    for (const f of readdirSync(dist).filter((n) => n.endsWith('.d.ts'))) {
      for (const spec of relativeSpecifiers(readFileSync(join(dist, f), 'utf8'))) {
        const target = join(dist, spec.replace(/\.js$/, '.d.ts'));
        if (!existsSync(target)) missing.push(`${f} -> ${spec}`);
      }
    }
    expect(missing).toEqual([]);
  });

  it('include a declaration for every src module', () => {
    const missing = readdirSync(src)
      .filter((n) => n.endsWith('.ts') && !n.endsWith('.d.ts') && !n.endsWith('.test.ts'))
      .map((n) => n.replace(/\.ts$/, '.d.ts'))
      .filter((n) => !existsSync(join(dist, n)));
    expect(missing).toEqual([]);
  });
});
