/**
 * page-header-consistency.test.ts
 *
 * Drives header-pattern consistency across every top-level view from a
 * single test rather than per-view in N separate test files.
 *
 * Implementation: this test reads each view's `.svelte` source and asserts:
 *   1. The file imports the shared `PageHeader` component.
 *   2. The file uses `<PageHeader …>` with the expected title, a subtitle,
 *      and an `icon=` prop.
 *
 * A static-source check is intentionally simpler than module-rendering for
 * this assertion — every view either uses the shared `PageHeader.svelte`
 * (and therefore inherits the icon + subtitle layout for free) or it
 * doesn't. There is no need to spin up a full Svelte renderer.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

interface ViewEntry {
  /** Friendly name used for the test description. */
  name: string;
  /** Path relative to the frontend root. */
  path: string;
  /** Substring expected to appear in the PageHeader `title=` attribute. */
  title: string;
}

// All top-level views shown in the sidebar. Every entry must use the shared
// PageHeader component so the icon + subtitle layout stays consistent
// across the app.
const views: ViewEntry[] = [
  { name: 'SystemDashboard',       path: 'src/views/SystemDashboard.svelte',                       title: 'System Status' },
  { name: 'OrchestratorDashboard', path: 'src/views/orchestrator/OrchestratorDashboard.svelte',    title: 'Dashboard' },
  { name: 'JobsView',              path: 'src/views/orchestrator/JobsView.svelte',                 title: 'Jobs' },
  { name: 'TasksView',             path: 'src/views/orchestrator/TasksView.svelte',                title: 'Tasks' },
  { name: 'TaskDetailView',        path: 'src/views/orchestrator/TaskDetailView.svelte',           title: 'Task' },
  { name: 'WorkflowsView',         path: 'src/views/orchestrator/WorkflowsView.svelte',            title: 'Workflows' },
  { name: 'EventStreamView',       path: 'src/views/orchestrator/EventStreamView.svelte',          title: 'Live Events' },
  { name: 'CostView',              path: 'src/views/orchestrator/CostView.svelte',                 title: 'Cost' },
  { name: 'InferenceView',         path: 'src/views/orchestrator/InferenceView.svelte',            title: 'Inference' },
  { name: 'ProjectsView',          path: 'src/views/orchestrator/ProjectsView.svelte',             title: 'Projects' },
  { name: 'KVStoreView',           path: 'src/views/data/KVStoreView.svelte',                      title: 'KV' },
  { name: 'VectorsView',           path: 'src/views/data/VectorsView.svelte',                      title: 'Vectors' },
  { name: 'TimeSeriesView',        path: 'src/views/data/TimeSeriesView.svelte',                   title: 'Time' },
  { name: 'ObjectsView',           path: 'src/views/data/ObjectsView.svelte',                      title: 'Objects' },
  { name: 'EmbeddingsView',        path: 'src/views/ai/EmbeddingsView.svelte',                     title: 'Embedding' },
  { name: 'RAGView',               path: 'src/views/ai/RAGView.svelte',                            title: 'RAG' },
  { name: 'MessagingView',         path: 'src/views/async/MessagingView.svelte',                   title: 'Messaging' },
  { name: 'SchedulerView',         path: 'src/views/async/SchedulerView.svelte',                   title: 'Scheduler' },
  { name: 'KMSView',               path: 'src/views/security/KMSView.svelte',                      title: 'KMS' },
  { name: 'SecretsView',           path: 'src/views/security/SecretsView.svelte',                  title: 'Secrets' },
  { name: 'CertificatesView',      path: 'src/views/security/CertificatesView.svelte',             title: 'Certificates' },
  { name: 'TrustView',             path: 'src/views/security/TrustView.svelte',                    title: 'Trust' },
  { name: 'CryptoView',            path: 'src/views/security/CryptoView.svelte',                   title: 'Crypto' },
  { name: 'DEKView',               path: 'src/views/security/DEKView.svelte',                      title: 'DEK' },
  { name: 'DynamicSecretsView',    path: 'src/views/security/DynamicSecretsView.svelte',           title: 'Dynamic Secrets' },
  { name: 'BarrierView',           path: 'src/views/security/BarrierView.svelte',                  title: 'Barrier' },
  { name: 'CeremonyView',          path: 'src/views/security/CeremonyView.svelte',                 title: 'Ceremony' },
  { name: 'BootstrapView',         path: 'src/views/security/BootstrapView.svelte',                title: 'Bootstrap' },
  { name: 'IAMView',               path: 'src/views/security/IAMView.svelte',                      title: 'IAM' },
  { name: 'SSHKeysView',           path: 'src/views/security/SSHKeysView.svelte',                  title: 'SSH' },
  { name: 'ClusterDashboard',      path: 'src/views/cluster/ClusterDashboard.svelte',              title: 'Cluster' },

  // SessionList lives under components/ but is the route component for the
  // /sessions URL — it must satisfy the same header contract.
  { name: 'SessionList',           path: 'src/components/orchestrator/SessionList.svelte',         title: 'Sessions' },
];

function read(viewPath: string): string {
  return readFileSync(resolve(__dirname, '..', viewPath), 'utf8');
}

describe('Page header consistency across all top-level views', () => {
  for (const view of views) {
    describe(view.name, () => {
      it('imports the shared PageHeader component', () => {
        const src = read(view.path);
        expect(src).toMatch(/import\s+PageHeader\s+from\s+['"][^'"]*PageHeader\.svelte['"]/);
      });

      it(`uses <PageHeader …> with a title containing "${view.title}"`, () => {
        const src = read(view.path);
        // Pull the entire `<PageHeader …>` opening tag (may span multiple lines).
        const tagMatch = src.match(/<PageHeader\b[^>]*?>/s);
        expect(tagMatch, `${view.name} must render a <PageHeader …>`).not.toBeNull();
        // The title attribute supports both static strings and template
        // expressions — match the first `title="…"` literal occurrence.
        const titleMatch = tagMatch![0].match(/title\s*=\s*["']([^"']+)["']/);
        expect(titleMatch, `${view.name} <PageHeader> must declare a title`).not.toBeNull();
        expect(titleMatch![1].toLowerCase()).toContain(view.title.toLowerCase());
      });

      it('passes a subtitle prop to PageHeader', () => {
        const src = read(view.path);
        const tagMatch = src.match(/<PageHeader\b[^>]*?>/s);
        expect(tagMatch).not.toBeNull();
        expect(tagMatch![0]).toMatch(/subtitle\s*=/);
      });

      it('passes an icon prop to PageHeader (icon prefix)', () => {
        const src = read(view.path);
        const tagMatch = src.match(/<PageHeader\b[^>]*?>/s);
        expect(tagMatch).not.toBeNull();
        expect(tagMatch![0]).toMatch(/icon\s*=\s*\{/);
      });
    });
  }
});

describe('Shared PageHeader component', () => {
  const headerSrc = read('src/components/core/PageHeader.svelte');

  it('renders an <h1> for the title', () => {
    expect(headerSrc).toMatch(/<h1[^>]*>\s*\{title\}\s*<\/h1>/);
  });

  it('renders the subtitle inside a muted <p>', () => {
    expect(headerSrc).toMatch(/<p[^>]*text-base-content\/60[^>]*>\s*\{subtitle\}\s*<\/p>/);
  });

  it('renders the icon beside the title in the accent colour', () => {
    expect(headerSrc).toMatch(/<Icon\s+[^>]*text-primary/);
  });
});
