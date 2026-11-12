<!--
  WorkflowWizard.svelte

  Six-step guided workflow creation wizard. Exposes a `onComplete` callback
  called with the assembled WorkflowDefinition when the operator submits, and
  a `onCancel` callback when they abandon the wizard.

  State is managed purely via Svelte 5 runes so the parent can keep it alive
  across tab switches.
-->
<script lang="ts">
  import {
    Workflow, Users, Network, Gauge, Settings2, CheckCircle,
    Plus, Trash2, ChevronDown, ChevronUp, HelpCircle, AlertCircle,
  } from 'lucide-svelte';
  import { fly } from 'svelte/transition';
  import type {
    WorkflowDefinition, WorkflowMode, AgentRoleSpec,
    CommunicationTopology, WorkspaceIsolation, AgentBackendInfo,
    FleetNode,
  } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let {
    existingNames = [],
    backends = [],
    agents = [],
    initialDef = null,
    onComplete,
    onCancel,
    onEditAdvanced,
  }: {
    existingNames?: string[];
    backends?: AgentBackendInfo[];
    agents?: FleetNode[];
    // When set, pre-populates the wizard for edit or duplicate mode.
    initialDef?: WorkflowDefinition | null;
    onComplete: (def: WorkflowDefinition) => Promise<void>;
    onCancel: () => void;
    onEditAdvanced: (yaml: string) => void;
  } = $props();

  // ---------------------------------------------------------------------------
  // Step metadata
  // ---------------------------------------------------------------------------

  const STEPS = [
    { label: 'Basics',    icon: Workflow    },
    { label: 'Agents',    icon: Users       },
    { label: 'Topology',  icon: Network     },
    { label: 'Limits',    icon: Gauge       },
    { label: 'Advanced',  icon: Settings2   },
    { label: 'Review',    icon: CheckCircle },
  ] as const;

  // ---------------------------------------------------------------------------
  // Wizard state — all module-level so parent can preserve across tab switches
  // ---------------------------------------------------------------------------

  let currentStep = $state(0);

  // Step 1 — Basics
  let wfName        = $state('');
  let wfDescription = $state('');
  let wfMode        = $state<WorkflowMode>('canned');

  // Step 2 — Agents
  interface AgentForm extends Omit<AgentRoleSpec, 'env'> {
    // host_affinity pins the agent to a fleet node by name or host; the
    // Agents step edits it and the assembled definition carries it.
    host_affinity?: string;
    envRows: { key: string; value: string }[];
    showAdvanced: boolean;
  }

  function makeAgent(role: string): AgentForm {
    return {
      role,
      backend: '',
      model: '',
      capabilities: [],
      mcp_scopes: [],
      count: 1,
      workspace_path: '',
      permission_mode: 'default',
      skip_permissions: false,
      sandboxed: false,
      container_image: '',
      host_affinity: '',
      num_ctx: 0,
      envRows: [],
      showAdvanced: false,
    };
  }

  let agentForms = $state<AgentForm[]>([]);

  // Step 3 — Topology
  let topology  = $state<CommunicationTopology>('hub_and_spoke');
  let isolation = $state<WorkspaceIsolation>('shared');

  // Step 4 — Limits
  let maxIterations  = $state(10);
  let maxParallelism = $state(4);
  let pollInterval   = $state('30s');
  let timeoutPerItem = $state('5m');
  let totalTimeout   = $state('1h');

  // CostControls
  let showCostControls    = $state(false);
  let budgetAction        = $state('warn');
  let maxTokensPerJob     = $state(0);
  let maxTokensPerAgent   = $state(0);

  // Step 5 — Advanced
  let showPeerMsg    = $state(false);
  let maxMsgSize     = $state(10240);
  let maxInboxSize   = $state(1000);
  let overflowAction = $state('reject');
  let inboxTTL       = $state('1h');

  let showFileLock       = $state(false);
  let leaseTTL           = $state('5m');
  let heartbeatInterval  = $state('30s');
  let maxRenewals        = $state(60);

  let showVectorDB      = $state(false);
  let vectorEnabled     = $state(false);
  let vectorIndexType   = $state('auto');
  let vectorAutoIndex   = $state(false);
  let vectorGPU         = $state(false);

  let showRecovery    = $state(false);
  let autoRestart     = $state(false);
  let maxRestarts     = $state(3);
  let restartBackoff  = $state('30s');

  let verbosity = $state('info');
  let globalNumCtx = $state(0);

  // Step 6 — Review
  let submitting   = $state(false);
  let submitError  = $state('');
  let showYamlLive = $state(false);
  let showReviewYaml = $state(true);

  // Chip input state per-agent
  let capInputs   = $state<string[]>([]);
  let scopeInputs = $state<string[]>([]);

  // ---------------------------------------------------------------------------
  // Pre-populate state from initialDef (edit / duplicate mode)
  // ---------------------------------------------------------------------------

  $effect(() => {
    const def = initialDef;
    if (!def) return;
    currentStep = 0;
    wfName        = def.name ?? '';
    wfDescription = def.description ?? '';
    wfMode        = (def.mode as WorkflowMode) ?? 'canned';
    // Agents
    agentForms = (def.agents ?? []).map((spec) => {
      const envRows = Object.entries(spec.env ?? {}).map(([key, value]) => ({ key, value }));
      return {
        role:             spec.role ?? '',
        backend:          spec.backend ?? '',
        model:            spec.model ?? '',
        capabilities:     spec.capabilities ?? [],
        mcp_scopes:       spec.mcp_scopes ?? [],
        count:            spec.count ?? 1,
        workspace_path:   spec.workspace_path ?? '',
        permission_mode:  spec.permission_mode ?? 'default',
        skip_permissions: spec.skip_permissions ?? false,
        sandboxed:        spec.sandboxed ?? false,
        container_image:  spec.container_image ?? '',
        num_ctx:          spec.num_ctx ?? 0,
        envRows,
        showAdvanced:     false,
      };
    });
    capInputs   = agentForms.map(() => '');
    scopeInputs = agentForms.map(() => '');
    // Settings
    const s = def.settings;
    if (s) {
      maxIterations  = s.max_iterations  ?? 10;
      maxParallelism = s.max_parallelism ?? 4;
      pollInterval   = s.poll_interval   ?? '30s';
      timeoutPerItem = s.timeout_per_item ?? '5m';
      totalTimeout   = s.total_timeout   ?? '1h';
      topology       = s.topology        ?? 'hub_and_spoke';
      isolation      = s.isolation       ?? 'shared';
      verbosity      = s.verbosity       ?? 'info';
      globalNumCtx   = s.num_ctx         ?? 0;
      if (s.peer_messaging) {
        showPeerMsg    = true;
        maxMsgSize     = s.peer_messaging.max_message_size ?? 10240;
        maxInboxSize   = s.peer_messaging.max_inbox_size   ?? 1000;
        overflowAction = s.peer_messaging.overflow_action  ?? 'reject';
        inboxTTL       = s.peer_messaging.inbox_ttl        ?? '1h';
      }
      if (s.file_locking) {
        showFileLock      = true;
        leaseTTL          = s.file_locking.lease_ttl          ?? '5m';
        heartbeatInterval = s.file_locking.heartbeat_interval ?? '30s';
        maxRenewals       = s.file_locking.max_renewals        ?? 60;
      }
      if (s.vector_db) {
        showVectorDB    = true;
        vectorEnabled   = s.vector_db.enabled       ?? false;
        vectorIndexType = s.vector_db.index_type    ?? 'auto';
        vectorAutoIndex = s.vector_db.auto_index    ?? false;
        vectorGPU       = s.vector_db.gpu_accelerated ?? false;
      }
      if (s.cost_controls) {
        showCostControls    = true;
        maxTokensPerJob     = s.cost_controls.max_tokens_per_job   ?? 0;
        maxTokensPerAgent   = s.cost_controls.max_tokens_per_agent ?? 0;
        budgetAction        = s.cost_controls.budget_action        ?? 'warn';
      }
      if (s.recovery) {
        showRecovery  = true;
        autoRestart   = s.recovery.auto_restart    ?? false;
        maxRestarts   = s.recovery.max_restarts    ?? 3;
        restartBackoff = s.recovery.restart_backoff ?? '30s';
      }
    }
  });

  // ---------------------------------------------------------------------------
  // Derived: assembled WorkflowDefinition
  // ---------------------------------------------------------------------------

  const assembled = $derived<WorkflowDefinition>({
    name: wfName.trim(),
    description: wfDescription.trim(),
    mode: wfMode,
    agents: agentForms.map((af) => {
      const envMap: Record<string, string> = {};
      for (const row of af.envRows) {
        if (row.key.trim()) envMap[row.key.trim()] = row.value;
      }
      const spec: AgentRoleSpec = {
        role: af.role,
        backend: af.backend,
        model: af.model,
        capabilities: af.capabilities,
        mcp_scopes: af.mcp_scopes,
        count: af.count,
        workspace_path: af.workspace_path,
        permission_mode: af.permission_mode,
        skip_permissions: af.skip_permissions,
        env: envMap,
        sandboxed: af.sandboxed,
        container_image: af.container_image,
        host_affinity: af.host_affinity,
        num_ctx: af.num_ctx,
      };
      return spec;
    }),
    settings: {
      max_iterations:  maxIterations,
      max_parallelism: maxParallelism,
      poll_interval:   pollInterval,
      timeout_per_item: timeoutPerItem,
      total_timeout:   totalTimeout,
      review_criteria: [],
      topology,
      isolation,
      peer_messaging: {
        max_message_size: maxMsgSize,
        max_inbox_size:   maxInboxSize,
        overflow_action:  overflowAction,
        inbox_ttl:        inboxTTL,
      },
      file_locking: {
        lease_ttl:          leaseTTL,
        heartbeat_interval: heartbeatInterval,
        max_renewals:       maxRenewals,
      },
      vector_db: {
        enabled:        vectorEnabled,
        index_type:     vectorIndexType,
        auto_index:     vectorAutoIndex,
        triggers:       [],
        gpu_accelerated: vectorGPU,
      },
      cost_controls: {
        max_tokens_per_job:   maxTokensPerJob,
        max_tokens_per_agent: maxTokensPerAgent,
        budget_action:        budgetAction,
      },
      recovery: {
        auto_restart:    autoRestart,
        max_restarts:    maxRestarts,
        restart_backoff: restartBackoff,
      },
      verbosity,
      num_ctx: globalNumCtx,
    },
  });

  // ---------------------------------------------------------------------------
  // YAML serialisation (hand-rolled; covers the well-typed WorkflowDefinition)
  // ---------------------------------------------------------------------------

  function yamlValue(v: unknown, indent: number): string {
    const pad = '  '.repeat(indent);
    if (v === null || v === undefined) return 'null';
    if (typeof v === 'boolean') return v ? 'true' : 'false';
    if (typeof v === 'number') return String(v);
    if (typeof v === 'string') {
      // Quote strings that contain special characters.
      if (/[:#\[\]{},&*?|<>=!%@`\n]/.test(v) || v === '') return JSON.stringify(v);
      return v;
    }
    if (Array.isArray(v)) {
      if (v.length === 0) return '[]';
      return '\n' + v.map((item) => `${pad}- ${yamlValue(item, indent + 1)}`).join('\n');
    }
    if (typeof v === 'object') {
      const entries = Object.entries(v as Record<string, unknown>).filter(([, val]) => val !== undefined);
      if (entries.length === 0) return '{}';
      return '\n' + entries.map(([k, val]) => {
        const serialised = yamlValue(val, indent + 1);
        const isBlock = serialised.startsWith('\n');
        return `${pad}${k}:${isBlock ? serialised : ' ' + serialised}`;
      }).join('\n');
    }
    return String(v);
  }

  function toYaml(def: WorkflowDefinition): string {
    const lines: string[] = [];
    lines.push(`name: ${yamlValue(def.name, 0)}`);
    lines.push(`description: ${yamlValue(def.description, 0)}`);
    lines.push(`mode: ${def.mode}`);
    lines.push(`agents:`);
    for (const ag of def.agents) {
      lines.push(`  - role: ${yamlValue(ag.role, 2)}`);
      lines.push(`    backend: ${yamlValue(ag.backend, 2)}`);
      lines.push(`    model: ${yamlValue(ag.model, 2)}`);
      lines.push(`    capabilities: ${yamlValue(ag.capabilities, 2)}`);
      lines.push(`    mcp_scopes: ${yamlValue(ag.mcp_scopes, 2)}`);
      lines.push(`    count: ${ag.count}`);
      if (ag.workspace_path)   lines.push(`    workspace_path: ${yamlValue(ag.workspace_path, 2)}`);
      if (ag.permission_mode && ag.permission_mode !== 'default')
        lines.push(`    permission_mode: ${ag.permission_mode}`);
      if (ag.skip_permissions) lines.push(`    skip_permissions: true`);
      if (ag.sandboxed)        lines.push(`    sandboxed: true`);
      if (ag.container_image)  lines.push(`    container_image: ${yamlValue(ag.container_image, 2)}`);
      if (ag.host_affinity)    lines.push(`    host_affinity: ${yamlValue(ag.host_affinity, 2)}`);
      if (ag.num_ctx > 0)      lines.push(`    num_ctx: ${ag.num_ctx}`);
      const envEntries = Object.entries(ag.env ?? {});
      if (envEntries.length > 0) {
        lines.push(`    env:`);
        for (const [k, val] of envEntries) lines.push(`      ${k}: ${yamlValue(val, 3)}`);
      }
    }
    lines.push(`settings:`);
    lines.push(`  max_iterations: ${def.settings.max_iterations}`);
    lines.push(`  max_parallelism: ${def.settings.max_parallelism}`);
    lines.push(`  poll_interval: ${def.settings.poll_interval}`);
    lines.push(`  timeout_per_item: ${def.settings.timeout_per_item}`);
    lines.push(`  total_timeout: ${def.settings.total_timeout}`);
    lines.push(`  topology: ${def.settings.topology}`);
    lines.push(`  isolation: ${def.settings.isolation}`);
    lines.push(`  verbosity: ${def.settings.verbosity}`);
    if (def.settings.num_ctx > 0) lines.push(`  num_ctx: ${def.settings.num_ctx}`);
    lines.push(`  peer_messaging:`);
    lines.push(`    max_message_size: ${def.settings.peer_messaging.max_message_size}`);
    lines.push(`    max_inbox_size: ${def.settings.peer_messaging.max_inbox_size}`);
    lines.push(`    overflow_action: ${def.settings.peer_messaging.overflow_action}`);
    lines.push(`    inbox_ttl: ${def.settings.peer_messaging.inbox_ttl}`);
    lines.push(`  file_locking:`);
    lines.push(`    lease_ttl: ${def.settings.file_locking.lease_ttl}`);
    lines.push(`    heartbeat_interval: ${def.settings.file_locking.heartbeat_interval}`);
    lines.push(`    max_renewals: ${def.settings.file_locking.max_renewals}`);
    lines.push(`  vector_db:`);
    lines.push(`    enabled: ${def.settings.vector_db.enabled}`);
    lines.push(`    index_type: ${def.settings.vector_db.index_type}`);
    lines.push(`    auto_index: ${def.settings.vector_db.auto_index}`);
    lines.push(`    gpu_accelerated: ${def.settings.vector_db.gpu_accelerated}`);
    lines.push(`  cost_controls:`);
    lines.push(`    max_tokens_per_job: ${def.settings.cost_controls.max_tokens_per_job}`);
    lines.push(`    max_tokens_per_agent: ${def.settings.cost_controls.max_tokens_per_agent}`);
    lines.push(`    budget_action: ${def.settings.cost_controls.budget_action}`);
    lines.push(`  recovery:`);
    lines.push(`    auto_restart: ${def.settings.recovery.auto_restart}`);
    lines.push(`    max_restarts: ${def.settings.recovery.max_restarts}`);
    lines.push(`    restart_backoff: ${def.settings.recovery.restart_backoff}`);
    return lines.join('\n');
  }

  const generatedYaml = $derived(toYaml(assembled));

  // ---------------------------------------------------------------------------
  // Validation
  // ---------------------------------------------------------------------------

  const DURATION_RE = /^\d+(ms|s|m|h)$/;

  function validDuration(v: string): boolean {
    return DURATION_RE.test(v.trim());
  }

  function step1Valid(): boolean {
    const name = wfName.trim();
    if (!name) return false;
    if (existingNames.includes(name)) return false;
    return true;
  }

  function step1Error(): string {
    const name = wfName.trim();
    if (!name) return 'Workflow name is required.';
    if (existingNames.includes(name)) return `"${name}" is already registered.`;
    return '';
  }

  function step2Valid(): boolean {
    if (agentForms.length === 0) return false;
    if (wfMode === 'canned' || wfMode === 'constrained') {
      return agentForms.some((af) => af.role.trim() === 'lead');
    }
    return true;
  }

  function step2Error(): string {
    if (agentForms.length === 0) return 'Add at least one agent.';
    if ((wfMode === 'canned' || wfMode === 'constrained') && !agentForms.some((af) => af.role.trim() === 'lead')) {
      return 'Canned and constrained modes require exactly one agent with role="lead".';
    }
    return '';
  }

  function step4Valid(): boolean {
    return validDuration(pollInterval) && validDuration(timeoutPerItem) && validDuration(totalTimeout);
  }

  function stepValid(n: number): boolean {
    if (n === 0) return step1Valid();
    if (n === 1) return step2Valid();
    if (n === 3) return step4Valid();
    return true;
  }

  function canAdvance(): boolean {
    return stepValid(currentStep);
  }

  // ---------------------------------------------------------------------------
  // Navigation
  // ---------------------------------------------------------------------------

  function next(): void {
    if (canAdvance() && currentStep < STEPS.length - 1) currentStep++;
  }

  function back(): void {
    if (currentStep > 0) currentStep--;
    submitError = '';
  }

  function goToStep(n: number): void {
    // Allow jumping backwards freely; only allow forward if all prior steps valid.
    if (n < currentStep) {
      currentStep = n;
      submitError = '';
    }
  }

  const progressPct = $derived(Math.round(((currentStep) / (STEPS.length - 1)) * 100));

  // ---------------------------------------------------------------------------
  // Agent helpers
  // ---------------------------------------------------------------------------

  function addAgent(role: string): void {
    agentForms = [...agentForms, makeAgent(role)];
    capInputs  = [...capInputs, ''];
    scopeInputs = [...scopeInputs, ''];
  }

  function removeFleetNode(idx: number): void {
    agentForms  = agentForms.filter((_, i) => i !== idx);
    capInputs   = capInputs.filter((_, i) => i !== idx);
    scopeInputs = scopeInputs.filter((_, i) => i !== idx);
  }

  function addChip(
    idx: number,
    field: 'capabilities' | 'mcp_scopes',
    inputArr: string[],
    setInput: (arr: string[]) => void,
    value: string,
  ): void {
    const v = value.trim();
    if (!v) return;
    const forms = agentForms.map((af, i) => {
      if (i !== idx) return af;
      const existing = af[field] ?? [];
      if (existing.includes(v)) return af;
      return { ...af, [field]: [...existing, v] };
    });
    agentForms = forms;
    const next = [...inputArr];
    next[idx] = '';
    setInput(next);
  }

  function removeChip(idx: number, field: 'capabilities' | 'mcp_scopes', chip: string): void {
    agentForms = agentForms.map((af, i) => {
      if (i !== idx) return af;
      return { ...af, [field]: af[field].filter((c) => c !== chip) };
    });
  }

  function updateAgentField<K extends keyof AgentForm>(idx: number, key: K, value: AgentForm[K]): void {
    agentForms = agentForms.map((af, i) => i === idx ? { ...af, [key]: value } : af);
  }

  function onBackendChange(idx: number, backendName: string): void {
    updateAgentField(idx, 'backend', backendName);
    const info = backends.find((b) => b.name === backendName);
    if (info?.default_model) {
      updateAgentField(idx, 'model', info.default_model);
    }
  }

  function addEnvRow(idx: number): void {
    agentForms = agentForms.map((af, i) =>
      i === idx ? { ...af, envRows: [...af.envRows, { key: '', value: '' }] } : af
    );
  }

  function removeEnvRow(agentIdx: number, rowIdx: number): void {
    agentForms = agentForms.map((af, i) => {
      if (i !== agentIdx) return af;
      return { ...af, envRows: af.envRows.filter((_, ri) => ri !== rowIdx) };
    });
  }

  function updateEnvRow(agentIdx: number, rowIdx: number, field: 'key' | 'value', value: string): void {
    agentForms = agentForms.map((af, i) => {
      if (i !== agentIdx) return af;
      const rows = af.envRows.map((r, ri) => ri === rowIdx ? { ...r, [field]: value } : r);
      return { ...af, envRows: rows };
    });
  }

  // ---------------------------------------------------------------------------
  // Submit
  // ---------------------------------------------------------------------------

  async function submit(): Promise<void> {
    submitting = true;
    submitError = '';
    try {
      await onComplete(assembled);
    } catch (err: unknown) {
      submitError = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Capability / MCP scope suggestions
  // ---------------------------------------------------------------------------

  const CAP_SUGGESTIONS = ['code', 'web_browse', 'file_io', 'shell', 'vector_search', 'rag', 'peer_messaging'];

  // Full IAM scope catalog grouped by domain.
  // Sourced from pkg/iam/ action constants and OIDC scope definitions.
  interface ScopeGroup {
    label: string;
    scopes: string[];
  }
  const MCP_SCOPE_GROUPS: ScopeGroup[] = [
    {
      label: 'Data (KV)',
      scopes: ['kv:read', 'kv:write', 'kv:delete', 'kv:list'],
    },
    {
      label: 'Vector',
      scopes: ['vector:read', 'vector:write', 'vector:delete', 'vector:search', 'vector:namespace', 'vector:stats', 'vector:admin'],
    },
    {
      label: 'Security (KMS)',
      scopes: ['kms:read', 'kms:write', 'kms:encrypt', 'kms:decrypt'],
    },
    {
      label: 'Secrets',
      scopes: ['secrets:read', 'secrets:write', 'secrets:delete'],
    },
    {
      label: 'Orchestration',
      scopes: ['agent:runtime:spawn', 'agent:runtime:prompt', 'agent:runtime:infer', 'agent:runtime:status'],
    },
    {
      label: 'Agent Admin',
      scopes: ['agent:discover', 'agent:register', 'agent:spawn', 'agent:prompt', 'agent:close', 'agent:decompose', 'agent:cache:read', 'agent:cache:write', 'agent:admin'],
    },
    {
      label: 'MCP',
      scopes: ['mcp:tools', 'mcp:resources', 'mcp:admin'],
    },
  ];

  // Flat list for filtering
  const MCP_SUGGESTIONS: string[] = MCP_SCOPE_GROUPS.flatMap((g) => g.scopes);

  const PERM_MODES = ['default', 'bypassPermissions'];
  const VERBOSITIES = ['info', 'verbose', 'debug'];
  const OVERFLOW_ACTIONS = ['reject', 'drop_oldest'];
  const BUDGET_ACTIONS = ['warn', 'pause', 'shutdown'];
  const INDEX_TYPES = ['auto', 'hnsw', 'flat'];

  // Host affinity options
  const hostOptions = $derived<string[]>([
    'any',
    ...agents.map((a) => a.host).filter((h, i, arr) => arr.indexOf(h) === i),
  ]);

  function backendTypeHint(name: string): string {
    const b = backends.find((bk) => bk.name === name);
    if (!b) return '';
    return `${b.name} (${b.type})`;
  }

  // ---------------------------------------------------------------------------
  // Live-filter helpers for chip inputs
  // ---------------------------------------------------------------------------

  function filteredCapSuggestions(idx: number): string[] {
    const query = (capInputs[idx] ?? '').trim().toLowerCase();
    const af = agentForms[idx];
    const existing = af?.capabilities ?? [];
    if (!query) return CAP_SUGGESTIONS.filter((s) => !existing.includes(s));
    return CAP_SUGGESTIONS.filter((s) => !existing.includes(s) && s.toLowerCase().includes(query));
  }

  function filteredScopeSuggestions(idx: number): string[] {
    const query = (scopeInputs[idx] ?? '').trim().toLowerCase();
    const af = agentForms[idx];
    const existing = af?.mcp_scopes ?? [];
    if (!query) return MCP_SUGGESTIONS.filter((s) => !existing.includes(s));
    return MCP_SUGGESTIONS.filter((s) => !existing.includes(s) && s.toLowerCase().includes(query));
  }
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') onCancel(); }} />

<!-- ============================================================
     Wizard root layout — horizontal stepper at top, content below
     ============================================================ -->
<div class="flex flex-col gap-0 min-h-[600px]">

  <!-- ── Horizontal stepper bar ── -->
  <div class="bg-base-200 border-b border-base-300/60 rounded-t-box px-4 pt-4 pb-0">

    <!-- Step pills row -->
    <ol class="flex items-start gap-0 overflow-x-auto pb-0" role="tablist">
      {#each STEPS as step, idx}
        {@const done   = idx < currentStep}
        {@const active = idx === currentStep}
        {@const last   = idx === STEPS.length - 1}
        <li class="flex items-center flex-1 min-w-0" role="presentation">
          <!-- Pill button -->
          <button
            type="button"
            role="tab"
            aria-selected={active}
            aria-current={active ? 'step' : undefined}
            class="group flex flex-col items-center gap-1 pb-3 flex-1 min-w-0 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 rounded-t
              {active  ? 'cursor-default' :
               done    ? 'cursor-pointer hover:opacity-80' :
                         'cursor-not-allowed opacity-40'}"
            onclick={() => goToStep(idx)}
            disabled={idx > currentStep}
          >
            <!-- Icon badge -->
            <span class="relative flex items-center justify-center w-9 h-9 rounded-full border-2 transition-all duration-200
              {active  ? 'bg-primary border-primary text-primary-content shadow-md' :
               done    ? 'bg-success/20 border-success text-success' :
                         'bg-base-300 border-base-300 text-base-content/40'}">
              {#if done}
                <CheckCircle class="h-4 w-4" />
              {:else}
                {@const Icon = step.icon}
                <Icon class="h-4 w-4" />
              {/if}
              <!-- Step number badge -->
              <span class="absolute -top-1 -right-1 flex items-center justify-center w-4 h-4 rounded-full text-[9px] font-bold
                {active ? 'bg-primary-content text-primary' : done ? 'bg-success text-success-content' : 'bg-base-content/20 text-base-content/60'}">
                {idx + 1}
              </span>
            </span>
            <!-- Label -->
            <span class="text-xs font-medium truncate max-w-full
              {active ? 'text-primary font-semibold' : done ? 'text-success' : 'text-base-content/40'}">
              {step.label}
            </span>
          </button>

          <!-- Connector line between pills (not after last) -->
          {#if !last}
            <div class="flex-shrink-0 w-6 h-0.5 mb-5 mx-0.5
              {idx < currentStep ? 'bg-success' : 'bg-base-300'}">
            </div>
          {/if}
        </li>
      {/each}
    </ol>

    <!-- Progress bar below pills -->
    <div class="w-full h-1 bg-base-300 rounded-none overflow-hidden -mx-4 px-0" style="margin-left: -1rem; width: calc(100% + 2rem);">
      <div
        class="h-full bg-primary transition-all duration-500 ease-out"
        style="width: {progressPct}%"
        role="progressbar"
        aria-valuenow={progressPct}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Wizard progress"
      ></div>
    </div>
  </div>

  <!-- ── Step content ── -->
  <main class="flex-1 p-6 flex flex-col gap-6 min-w-0 bg-base-100 rounded-b-box">

    <!-- ── Step 1: Basics ── -->
    {#if currentStep === 0}
      <div>
        <h2 class="text-base font-semibold mb-1">Basics</h2>
        <p class="text-sm text-base-content/60 mb-6">Name your workflow and choose its operating mode.</p>

        <!-- Name -->
        <div class="form-control mb-4">
          <label class="label" for="wz-name">
            <span class="label-text font-medium">
              Workflow Name <span class="text-error">*</span>
            </span>
            <span class="label-text-alt flex items-center gap-1 text-base-content/50">
              <HelpCircle class="h-3 w-3" />
              Slug-style identifier, e.g. dev-sprint-v2
            </span>
          </label>
          <input
            id="wz-name"
            type="text"
            class="input input-bordered {step1Error() && wfName ? 'input-error' : ''}"
            placeholder="dev-sprint-v2"
            bind:value={wfName}
          />
          {#if step1Error() && wfName}
            <div class="label"><span class="label-text-alt text-error">{step1Error()}</span></div>
          {/if}
        </div>

        <!-- Description -->
        <div class="form-control mb-6">
          <label class="label" for="wz-desc">
            <span class="label-text font-medium">Description</span>
          </label>
          <textarea
            id="wz-desc"
            class="textarea textarea-bordered h-20"
            placeholder="What does this workflow do?"
            bind:value={wfDescription}
          ></textarea>
        </div>

        <!-- Mode -->
        <fieldset>
          <legend class="text-sm font-medium mb-3">
            Mode <span class="text-error">*</span>
          </legend>
          <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
            {#each [
              { value: 'canned' as WorkflowMode,
                title: 'Canned',
                desc: 'Runs a fully predetermined workflow with fixed steps. Lead agent required.' },
              { value: 'constrained' as WorkflowMode,
                title: 'Constrained',
                desc: 'Lead agent decomposes tasks within defined roles and capabilities. Lead agent required.' },
              { value: 'autonomous' as WorkflowMode,
                title: 'Autonomous',
                desc: 'Lead agent has full control over task decomposition, agent spawning, and execution.' },
            ] as opt}
              <label
                class="card border-2 cursor-pointer transition-all duration-150 hover:-translate-y-0.5 hover:shadow-md
                  {wfMode === opt.value ? 'border-primary bg-primary/10' : 'border-base-300 bg-base-100'}"
              >
                <input type="radio" class="hidden" name="wz-mode" value={opt.value} bind:group={wfMode} />
                <div class="card-body p-4">
                  <h3 class="font-semibold text-sm">{opt.title}</h3>
                  <p class="text-xs text-base-content/60 mt-1">{opt.desc}</p>
                  {#if opt.value !== 'autonomous'}
                    <div class="badge badge-xs badge-warning mt-2">Lead agent required</div>
                  {/if}
                </div>
              </label>
            {/each}
          </div>
        </fieldset>
      </div>

    <!-- ── Step 2: Agents ── -->
    {:else if currentStep === 1}
      <div>
        <h2 class="text-base font-semibold mb-1">Agents</h2>
        <p class="text-sm text-base-content/60 mb-4">
          Define agent roles for this workflow.
          {#if wfMode !== 'autonomous'}
            <span class="text-warning font-medium">Mode "{wfMode}" requires a role named "lead".</span>
          {/if}
        </p>

        {#if agentForms.length === 0}
          <div class="card bg-base-200 border border-dashed border-base-300 rounded-box p-8 text-center">
            <Users class="h-10 w-10 mx-auto text-base-content/30 mb-3" />
            <p class="text-sm text-base-content/50 mb-4">
              Add your first agent — start with the lead, the role that orchestrates the others.
            </p>
            <button
              type="button"
              class="btn btn-primary btn-sm mx-auto"
              onclick={() => addAgent(wfMode !== 'autonomous' ? 'lead' : 'worker')}
            >
              <Plus class="h-4 w-4" />
              Add Lead Agent
            </button>
          </div>
        {:else}
          <div class="space-y-4">
            {#each agentForms as af, idx (idx)}
              <div class="card bg-base-100 border border-base-300/60 shadow-sm rounded-box">
                <div class="card-body p-4 space-y-3">

                  <!-- Card header -->
                  <div class="flex items-center justify-between">
                    <h3 class="font-semibold text-sm">
                      Agent #{idx + 1}
                      {#if af.role}
                        <span class="badge badge-sm badge-outline ml-1">{af.role}</span>
                      {/if}
                    </h3>
                    <button
                      type="button"
                      class="btn btn-xs btn-ghost text-error"
                      onclick={() => removeFleetNode(idx)}
                      aria-label="Remove agent {idx + 1}"
                    >
                      <Trash2 class="h-3.5 w-3.5" />
                    </button>
                  </div>

                  <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <!-- Role -->
                    <div class="form-control">
                      <label class="label py-0" for="wz-agent-role-{idx}">
                        <span class="label-text text-xs">Role <span class="text-error">*</span></span>
                      </label>
                      <input
                        id="wz-agent-role-{idx}"
                        type="text"
                        class="input input-bordered input-sm"
                        placeholder="lead or worker"
                        value={af.role}
                        oninput={(e) => updateAgentField(idx, 'role', (e.target as HTMLInputElement).value)}
                      />
                    </div>

                    <!-- Backend -->
                    <div class="form-control">
                      <label class="label py-0" for="wz-agent-backend-{idx}">
                        <span class="label-text text-xs">Backend</span>
                      </label>
                      {#if backends.length > 0}
                        <select
                          id="wz-agent-backend-{idx}"
                          class="select select-bordered select-sm"
                          value={af.backend}
                          onchange={(e) => onBackendChange(idx, (e.target as HTMLSelectElement).value)}
                        >
                          <option value="">-- select backend --</option>
                          {#each backends as b}
                            <option value={b.name}>{backendTypeHint(b.name)}</option>
                          {/each}
                        </select>
                      {:else}
                        <input
                          id="wz-agent-backend-{idx}"
                          type="text"
                          class="input input-bordered input-sm"
                          placeholder="ollama, claude, codex…"
                          value={af.backend}
                          oninput={(e) => updateAgentField(idx, 'backend', (e.target as HTMLInputElement).value)}
                        />
                      {/if}
                    </div>

                    <!-- Model -->
                    <div class="form-control">
                      <label class="label py-0" for="wz-agent-model-{idx}">
                        <span class="label-text text-xs">Model</span>
                      </label>
                      <input
                        id="wz-agent-model-{idx}"
                        type="text"
                        class="input input-bordered input-sm"
                        placeholder="e.g. ollama/llama3.3:70b"
                        value={af.model}
                        oninput={(e) => updateAgentField(idx, 'model', (e.target as HTMLInputElement).value)}
                      />
                    </div>

                    <!-- Count -->
                    <div class="form-control">
                      <label class="label py-0" for="wz-agent-count-{idx}">
                        <span class="label-text text-xs">Count</span>
                        <span class="label-text-alt text-xs text-base-content/50">
                          <HelpCircle class="h-3 w-3 inline" /> Instances to spawn
                        </span>
                      </label>
                      <input
                        id="wz-agent-count-{idx}"
                        type="number"
                        min="1"
                        class="input input-bordered input-sm"
                        value={af.count}
                        oninput={(e) => updateAgentField(idx, 'count', parseInt((e.target as HTMLInputElement).value, 10) || 1)}
                      />
                    </div>
                  </div>

                  <!-- Capabilities chips -->
                  <div class="form-control" role="group" aria-labelledby="wz-cap-label-{idx}">
                    <div id="wz-cap-label-{idx}" class="label py-0">
                      <span class="label-text text-xs">Capabilities</span>
                    </div>
                    <!-- Added chips -->
                    {#if af.capabilities.length > 0}
                      <div class="flex flex-wrap gap-1 mb-1">
                        {#each af.capabilities as cap}
                          <span class="badge badge-sm badge-primary gap-1">
                            {cap}
                            <button
                              type="button"
                              class="ml-0.5"
                              onclick={() => removeChip(idx, 'capabilities', cap)}
                              aria-label="Remove {cap}"
                            >×</button>
                          </span>
                        {/each}
                      </div>
                    {/if}
                    <!-- Input row -->
                    <div class="flex gap-1">
                      <input
                        type="text"
                        class="input input-bordered input-xs flex-1"
                        placeholder="Type to filter or add…"
                        value={capInputs[idx] ?? ''}
                        oninput={(e) => {
                          const arr = [...capInputs];
                          arr[idx] = (e.target as HTMLInputElement).value;
                          capInputs = arr;
                        }}
                        onkeydown={(e) => {
                          if (e.key === 'Enter' || e.key === ',') {
                            e.preventDefault();
                            addChip(idx, 'capabilities', capInputs, (a) => { capInputs = a; }, capInputs[idx] ?? '');
                          }
                        }}
                      />
                      <button
                        type="button"
                        class="btn btn-xs btn-ghost"
                        onclick={() => addChip(idx, 'capabilities', capInputs, (a) => { capInputs = a; }, capInputs[idx] ?? '')}
                      >Add</button>
                    </div>
                    <!-- Filtered suggestions -->
                    {#if filteredCapSuggestions(idx).length > 0}
                      <div class="flex flex-wrap gap-1 mt-1">
                        {#each filteredCapSuggestions(idx) as sug}
                          <button
                            type="button"
                            class="badge badge-xs badge-outline cursor-pointer hover:badge-primary transition-colors"
                            onclick={() => addChip(idx, 'capabilities', capInputs, (a) => { capInputs = a; }, sug)}
                          >{sug}</button>
                        {/each}
                      </div>
                    {:else if (capInputs[idx] ?? '').trim() && !af.capabilities.includes((capInputs[idx] ?? '').trim())}
                      <p class="text-xs text-base-content/40 mt-1">
                        No matching suggestions — press <kbd class="kbd kbd-xs">Enter</kbd> to add "{(capInputs[idx] ?? '').trim()}"
                      </p>
                    {/if}
                  </div>

                  <!-- MCP Scopes chips -->
                  <div class="form-control" role="group" aria-labelledby="wz-scope-label-{idx}">
                    <div id="wz-scope-label-{idx}" class="label py-0">
                      <span class="label-text text-xs">MCP Scopes</span>
                    </div>
                    <!-- Added chips -->
                    {#if af.mcp_scopes.length > 0}
                      <div class="flex flex-wrap gap-1 mb-1">
                        {#each af.mcp_scopes as scope}
                          <span class="badge badge-sm badge-secondary gap-1">
                            {scope}
                            <button
                              type="button"
                              class="ml-0.5"
                              onclick={() => removeChip(idx, 'mcp_scopes', scope)}
                              aria-label="Remove {scope}"
                            >×</button>
                          </span>
                        {/each}
                      </div>
                    {/if}
                    <!-- Input row -->
                    <div class="flex gap-1">
                      <input
                        type="text"
                        class="input input-bordered input-xs flex-1"
                        placeholder="Type to filter or add scope…"
                        value={scopeInputs[idx] ?? ''}
                        oninput={(e) => {
                          const arr = [...scopeInputs];
                          arr[idx] = (e.target as HTMLInputElement).value;
                          scopeInputs = arr;
                        }}
                        onkeydown={(e) => {
                          if (e.key === 'Enter' || e.key === ',') {
                            e.preventDefault();
                            addChip(idx, 'mcp_scopes', scopeInputs, (a) => { scopeInputs = a; }, scopeInputs[idx] ?? '');
                          }
                        }}
                      />
                      <button
                        type="button"
                        class="btn btn-xs btn-ghost"
                        onclick={() => addChip(idx, 'mcp_scopes', scopeInputs, (a) => { scopeInputs = a; }, scopeInputs[idx] ?? '')}
                      >Add</button>
                    </div>
                    <!-- Filtered suggestions -->
                    {#if filteredScopeSuggestions(idx).length > 0}
                      <div class="flex flex-wrap gap-1 mt-1" data-testid="scope-suggestions-{idx}">
                        {#each filteredScopeSuggestions(idx) as sug}
                          <button
                            type="button"
                            class="badge badge-xs badge-outline cursor-pointer hover:badge-secondary transition-colors"
                            onclick={() => addChip(idx, 'mcp_scopes', scopeInputs, (a) => { scopeInputs = a; }, sug)}
                          >{sug}</button>
                        {/each}
                      </div>
                    {:else if (scopeInputs[idx] ?? '').trim() && !af.mcp_scopes.includes((scopeInputs[idx] ?? '').trim())}
                      <p class="text-xs text-base-content/40 mt-1" data-testid="scope-no-match-hint-{idx}">
                        No matching suggestions — press <kbd class="kbd kbd-xs">Enter</kbd> to add "{(scopeInputs[idx] ?? '').trim()}"
                      </p>
                    {/if}
                  </div>

                  <!-- Advanced toggle -->
                  <button
                    type="button"
                    class="flex items-center gap-1 text-xs text-base-content/50 hover:text-base-content transition-colors"
                    onclick={() => updateAgentField(idx, 'showAdvanced', !af.showAdvanced)}
                  >
                    {#if af.showAdvanced}
                      <ChevronUp class="h-3 w-3" />
                      Hide sandbox & advanced
                    {:else}
                      <ChevronDown class="h-3 w-3" />
                      Sandbox & advanced
                    {/if}
                  </button>

                  {#if af.showAdvanced}
                    <div class="border border-base-300/40 rounded-box p-3 space-y-3 bg-base-200/50" transition:fly={{ y: -6, duration: 150 }}>

                      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <!-- Workspace path -->
                        <div class="form-control">
                          <label class="label py-0" for="wz-wspath-{idx}">
                            <span class="label-text text-xs">workspace_path</span>
                            <span class="label-text-alt text-xs text-base-content/40">
                              <HelpCircle class="h-3 w-3 inline" /> Host path mounted as /workspace
                            </span>
                          </label>
                          <input
                            id="wz-wspath-{idx}"
                            type="text"
                            class="input input-bordered input-xs"
                            placeholder="/home/user/projects/myapp"
                            value={af.workspace_path}
                            oninput={(e) => updateAgentField(idx, 'workspace_path', (e.target as HTMLInputElement).value)}
                          />
                        </div>

                        <!-- Permission mode -->
                        <div class="form-control">
                          <label class="label py-0" for="wz-permmode-{idx}">
                            <span class="label-text text-xs">permission_mode</span>
                          </label>
                          <select
                            id="wz-permmode-{idx}"
                            class="select select-bordered select-xs"
                            value={af.permission_mode}
                            onchange={(e) => updateAgentField(idx, 'permission_mode', (e.target as HTMLSelectElement).value)}
                          >
                            {#each PERM_MODES as pm}
                              <option value={pm}>{pm}</option>
                            {/each}
                          </select>
                        </div>

                        <!-- Host affinity -->
                        <div class="form-control">
                          <label class="label py-0" for="wz-affinity-{idx}">
                            <span class="label-text text-xs">host_affinity</span>
                            <span class="label-text-alt text-xs text-base-content/40">
                              <HelpCircle class="h-3 w-3 inline" /> Pin role to a specific daemon host
                            </span>
                          </label>
                          <select
                            id="wz-affinity-{idx}"
                            class="select select-bordered select-xs"
                            value={af.host_affinity || 'any'}
                            onchange={(e) => {
                              const v = (e.target as HTMLSelectElement).value;
                              updateAgentField(idx, 'host_affinity', v === 'any' ? '' : v);
                            }}
                          >
                            {#each hostOptions as h}
                              <option value={h}>{h}</option>
                            {/each}
                          </select>
                        </div>

                        <!-- NumCtx -->
                        <div class="form-control">
                          <label class="label py-0" for="wz-numctx-{idx}">
                            <span class="label-text text-xs">num_ctx</span>
                            <span class="label-text-alt text-xs text-base-content/40">
                              <HelpCircle class="h-3 w-3 inline" /> Ollama context window size
                            </span>
                          </label>
                          <input
                            id="wz-numctx-{idx}"
                            type="number"
                            min="0"
                            class="input input-bordered input-xs"
                            placeholder="0 = inherit"
                            value={af.num_ctx}
                            oninput={(e) => updateAgentField(idx, 'num_ctx', parseInt((e.target as HTMLInputElement).value, 10) || 0)}
                          />
                        </div>
                      </div>

                      <!-- Checkboxes -->
                      <div class="flex flex-wrap gap-4">
                        <label class="flex items-center gap-2 cursor-pointer text-xs">
                          <input
                            type="checkbox"
                            class="checkbox checkbox-xs"
                            checked={af.skip_permissions}
                            onchange={(e) => updateAgentField(idx, 'skip_permissions', (e.target as HTMLInputElement).checked)}
                          />
                          skip_permissions
                          <span class="text-base-content/40" title="--dangerously-skip-permissions flag">
                            <HelpCircle class="h-3 w-3 inline" />
                          </span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer text-xs">
                          <input
                            type="checkbox"
                            class="checkbox checkbox-xs"
                            checked={af.sandboxed}
                            onchange={(e) => updateAgentField(idx, 'sandboxed', (e.target as HTMLInputElement).checked)}
                          />
                          sandboxed (OCI container)
                        </label>
                      </div>

                      {#if af.sandboxed}
                        <div class="form-control">
                          <label class="label py-0" for="wz-cimage-{idx}">
                            <span class="label-text text-xs">container_image</span>
                          </label>
                          <input
                            id="wz-cimage-{idx}"
                            type="text"
                            class="input input-bordered input-xs"
                            placeholder="docker.io/myorg/claude-agent:latest"
                            value={af.container_image}
                            oninput={(e) => updateAgentField(idx, 'container_image', (e.target as HTMLInputElement).value)}
                          />
                        </div>
                      {/if}

                      <!-- Env vars -->
                      <div>
                        <div class="text-xs font-medium mb-1">env vars</div>
                        {#each af.envRows as row, rowIdx}
                          <div class="flex gap-1 mb-1">
                            <input
                              type="text"
                              class="input input-bordered input-xs flex-1"
                              placeholder="KEY"
                              value={row.key}
                              oninput={(e) => updateEnvRow(idx, rowIdx, 'key', (e.target as HTMLInputElement).value)}
                            />
                            <input
                              type="text"
                              class="input input-bordered input-xs flex-1"
                              placeholder="value"
                              value={row.value}
                              oninput={(e) => updateEnvRow(idx, rowIdx, 'value', (e.target as HTMLInputElement).value)}
                            />
                            <button
                              type="button"
                              class="btn btn-xs btn-ghost text-error"
                              onclick={() => removeEnvRow(idx, rowIdx)}
                              aria-label="Remove env row"
                            >×</button>
                          </div>
                        {/each}
                        <button
                          type="button"
                          class="btn btn-xs btn-ghost gap-1 mt-1"
                          onclick={() => addEnvRow(idx)}
                        >
                          <Plus class="h-3 w-3" /> Add var
                        </button>
                      </div>
                    </div>
                  {/if}
                </div>
              </div>
            {/each}
          </div>

          <div class="flex gap-2 mt-4">
            <button
              type="button"
              class="btn btn-sm btn-ghost gap-1"
              onclick={() => addAgent(
                (wfMode !== 'autonomous' && !agentForms.some((af) => af.role === 'lead')) ? 'lead' : 'worker'
              )}
            >
              <Plus class="h-4 w-4" />
              Add Agent
            </button>
          </div>
        {/if}

        {#if step2Error() && agentForms.length > 0}
          <div class="alert alert-warning text-sm mt-4 py-2">
            <AlertCircle class="h-4 w-4" />
            <span>{step2Error()}</span>
          </div>
        {/if}
      </div>

    <!-- ── Step 3: Topology ── -->
    {:else if currentStep === 2}
      <div>
        <h2 class="text-base font-semibold mb-1">Topology</h2>
        <p class="text-sm text-base-content/60 mb-6">Choose how agents communicate and how workspaces are shared.</p>

        <fieldset class="mb-6">
          <legend class="text-sm font-medium mb-3">Communication Topology <span class="text-error">*</span></legend>
          <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
            {#each [
              {
                value: 'hub_and_spoke' as CommunicationTopology,
                title: 'Hub-and-Spoke',
                desc: 'All messages route through the lead agent. Highest control, simplest audit trail.',
                svg: `<svg viewBox="0 0 80 60" fill="none" xmlns="http://www.w3.org/2000/svg" class="w-full h-12">
                  <circle cx="40" cy="30" r="8" class="fill-primary/70"/>
                  <circle cx="10" cy="10" r="5" class="fill-base-content/30"/>
                  <circle cx="70" cy="10" r="5" class="fill-base-content/30"/>
                  <circle cx="10" cy="50" r="5" class="fill-base-content/30"/>
                  <circle cx="70" cy="50" r="5" class="fill-base-content/30"/>
                  <line x1="40" y1="30" x2="10" y2="10" class="stroke-primary/50" stroke-width="1.5"/>
                  <line x1="40" y1="30" x2="70" y2="10" class="stroke-primary/50" stroke-width="1.5"/>
                  <line x1="40" y1="30" x2="10" y2="50" class="stroke-primary/50" stroke-width="1.5"/>
                  <line x1="40" y1="30" x2="70" y2="50" class="stroke-primary/50" stroke-width="1.5"/>
                </svg>`,
              },
              {
                value: 'hybrid' as CommunicationTopology,
                title: 'Hybrid',
                desc: 'Workers can send advisory messages to each other; all task assignments still route through the lead.',
                svg: `<svg viewBox="0 0 80 60" fill="none" xmlns="http://www.w3.org/2000/svg" class="w-full h-12">
                  <circle cx="40" cy="30" r="8" class="fill-primary/70"/>
                  <circle cx="10" cy="10" r="5" class="fill-base-content/30"/>
                  <circle cx="70" cy="10" r="5" class="fill-base-content/30"/>
                  <circle cx="10" cy="50" r="5" class="fill-base-content/30"/>
                  <circle cx="70" cy="50" r="5" class="fill-base-content/30"/>
                  <line x1="40" y1="30" x2="10" y2="10" class="stroke-primary/50" stroke-width="1.5"/>
                  <line x1="40" y1="30" x2="70" y2="10" class="stroke-primary/50" stroke-width="1.5"/>
                  <line x1="40" y1="30" x2="10" y2="50" class="stroke-primary/50" stroke-width="1.5"/>
                  <line x1="40" y1="30" x2="70" y2="50" class="stroke-primary/50" stroke-width="1.5"/>
                  <line x1="10" y1="10" x2="70" y2="10" class="stroke-secondary/50" stroke-width="1" stroke-dasharray="3 2"/>
                  <line x1="10" y1="50" x2="70" y2="50" class="stroke-secondary/50" stroke-width="1" stroke-dasharray="3 2"/>
                </svg>`,
              },
              {
                value: 'peer_to_peer' as CommunicationTopology,
                title: 'Peer-to-Peer',
                desc: 'Unrestricted inter-agent messaging. Maximum flexibility; requires careful capability scoping.',
                svg: `<svg viewBox="0 0 80 60" fill="none" xmlns="http://www.w3.org/2000/svg" class="w-full h-12">
                  <circle cx="40" cy="8" r="5" class="fill-primary/70"/>
                  <circle cx="70" cy="40" r="5" class="fill-base-content/30"/>
                  <circle cx="10" cy="40" r="5" class="fill-base-content/30"/>
                  <circle cx="50" cy="55" r="5" class="fill-base-content/30"/>
                  <circle cx="25" cy="55" r="5" class="fill-base-content/30"/>
                  <line x1="40" y1="8" x2="70" y2="40" class="stroke-primary/50" stroke-width="1"/>
                  <line x1="40" y1="8" x2="10" y2="40" class="stroke-primary/50" stroke-width="1"/>
                  <line x1="70" y1="40" x2="10" y2="40" class="stroke-base-content/30" stroke-width="1"/>
                  <line x1="70" y1="40" x2="50" y2="55" class="stroke-base-content/30" stroke-width="1"/>
                  <line x1="10" y1="40" x2="25" y2="55" class="stroke-base-content/30" stroke-width="1"/>
                  <line x1="50" y1="55" x2="25" y2="55" class="stroke-base-content/30" stroke-width="1"/>
                </svg>`,
              },
            ] as opt}
              <label
                class="card border-2 cursor-pointer transition-all duration-150 hover:-translate-y-0.5 hover:shadow-md
                  {topology === opt.value ? 'border-primary bg-primary/10' : 'border-base-300 bg-base-100'}"
              >
                <input type="radio" class="hidden" name="wz-topology" value={opt.value} bind:group={topology} />
                <div class="card-body p-4">
                  {@html opt.svg}
                  <h3 class="font-semibold text-sm mt-2">{opt.title}</h3>
                  <p class="text-xs text-base-content/60 mt-1">{opt.desc}</p>
                </div>
              </label>
            {/each}
          </div>
        </fieldset>

        <fieldset>
          <legend class="text-sm font-medium mb-3">Workspace Isolation <span class="text-error">*</span></legend>
          <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
            {#each [
              { value: 'shared' as WorkspaceIsolation,
                title: 'Shared',
                desc: 'All agents share a single workspace directory. Lowest overhead; suitable for read-heavy workflows.' },
              { value: 'worktree' as WorkspaceIsolation,
                title: 'Worktree',
                desc: 'Each agent gets a separate git worktree. Prevents cross-agent file conflicts during parallel execution.' },
            ] as opt}
              <label
                class="card border-2 cursor-pointer transition-all duration-150 hover:-translate-y-0.5 hover:shadow-md
                  {isolation === opt.value ? 'border-primary bg-primary/10' : 'border-base-300 bg-base-100'}"
              >
                <input type="radio" class="hidden" name="wz-isolation" value={opt.value} bind:group={isolation} />
                <div class="card-body p-4">
                  <h3 class="font-semibold text-sm">{opt.title}</h3>
                  <p class="text-xs text-base-content/60 mt-1">{opt.desc}</p>
                </div>
              </label>
            {/each}
          </div>
        </fieldset>
      </div>

    <!-- ── Step 4: Limits & Budgets ── -->
    {:else if currentStep === 3}
      <div>
        <h2 class="text-base font-semibold mb-1">Limits & Budgets</h2>
        <p class="text-sm text-base-content/60 mb-6">Set execution bounds and optional cost guardrails.</p>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <!-- max_iterations -->
          <div class="form-control">
            <label class="label" for="wz-maxiter">
              <span class="label-text font-medium">max_iterations</span>
              <span class="label-text-alt text-base-content/50">
                <HelpCircle class="h-3 w-3 inline" /> Orchestration loop limit
              </span>
            </label>
            <input id="wz-maxiter" type="number" min="1" class="input input-bordered" bind:value={maxIterations} />
          </div>

          <!-- max_parallelism -->
          <div class="form-control">
            <label class="label" for="wz-maxpar">
              <span class="label-text font-medium">max_parallelism</span>
              <span class="label-text-alt text-base-content/50">
                <HelpCircle class="h-3 w-3 inline" /> Concurrent task slots
              </span>
            </label>
            <input id="wz-maxpar" type="number" min="1" class="input input-bordered" bind:value={maxParallelism} />
          </div>

          <!-- poll_interval -->
          <div class="form-control">
            <label class="label" for="wz-poll">
              <span class="label-text font-medium">poll_interval <span class="text-error">*</span></span>
              <span class="label-text-alt text-base-content/50">e.g. 30s, 5m</span>
            </label>
            <input
              id="wz-poll"
              type="text"
              class="input input-bordered {pollInterval && !validDuration(pollInterval) ? 'input-error' : ''}"
              placeholder="30s"
              bind:value={pollInterval}
            />
            {#if pollInterval && !validDuration(pollInterval)}
              <div class="label"><span class="label-text-alt text-error">Must match /^\d+(ms|s|m|h)$/</span></div>
            {/if}
          </div>

          <!-- timeout_per_item -->
          <div class="form-control">
            <label class="label" for="wz-timeout-item">
              <span class="label-text font-medium">timeout_per_item <span class="text-error">*</span></span>
              <span class="label-text-alt text-base-content/50">e.g. 5m, 1h</span>
            </label>
            <input
              id="wz-timeout-item"
              type="text"
              class="input input-bordered {timeoutPerItem && !validDuration(timeoutPerItem) ? 'input-error' : ''}"
              placeholder="5m"
              bind:value={timeoutPerItem}
            />
            {#if timeoutPerItem && !validDuration(timeoutPerItem)}
              <div class="label"><span class="label-text-alt text-error">Must match /^\d+(ms|s|m|h)$/</span></div>
            {/if}
          </div>

          <!-- total_timeout -->
          <div class="form-control">
            <label class="label" for="wz-timeout-total">
              <span class="label-text font-medium">total_timeout <span class="text-error">*</span></span>
              <span class="label-text-alt text-base-content/50">e.g. 1h, 30m</span>
            </label>
            <input
              id="wz-timeout-total"
              type="text"
              class="input input-bordered {totalTimeout && !validDuration(totalTimeout) ? 'input-error' : ''}"
              placeholder="1h"
              bind:value={totalTimeout}
            />
            {#if totalTimeout && !validDuration(totalTimeout)}
              <div class="label"><span class="label-text-alt text-error">Must match /^\d+(ms|s|m|h)$/</span></div>
            {/if}
          </div>
        </div>

        <!-- Cost Controls collapsible -->
        <div class="mt-6">
          <button
            type="button"
            class="flex items-center gap-2 text-sm font-medium text-base-content/70 hover:text-base-content"
            onclick={() => { showCostControls = !showCostControls; }}
          >
            {#if showCostControls}<ChevronUp class="h-4 w-4" />{:else}<ChevronDown class="h-4 w-4" />{/if}
            Cost Controls
            <span class="badge badge-xs badge-outline">optional</span>
          </button>

          {#if showCostControls}
            <div class="border border-base-300/40 rounded-box p-4 mt-3 space-y-3 bg-base-200/50" transition:fly={{ y: -6, duration: 150 }}>
              <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
                <div class="form-control">
                  <label class="label py-0" for="wz-budget-action">
                    <span class="label-text text-xs">budget_action</span>
                    <span class="label-text-alt text-xs text-base-content/40">
                      <HelpCircle class="h-3 w-3 inline" /> Action when budget exceeded
                    </span>
                  </label>
                  <select id="wz-budget-action" class="select select-bordered select-sm" bind:value={budgetAction}>
                    {#each BUDGET_ACTIONS as a}<option value={a}>{a}</option>{/each}
                  </select>
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-maxtok-job">
                    <span class="label-text text-xs">max_tokens_per_job</span>
                  </label>
                  <input id="wz-maxtok-job" type="number" min="0" class="input input-bordered input-sm" bind:value={maxTokensPerJob} />
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-maxtok-agent">
                    <span class="label-text text-xs">max_tokens_per_agent</span>
                  </label>
                  <input id="wz-maxtok-agent" type="number" min="0" class="input input-bordered input-sm" bind:value={maxTokensPerAgent} />
                </div>
              </div>
            </div>
          {/if}
        </div>
      </div>

    <!-- ── Step 5: Advanced ── -->
    {:else if currentStep === 4}
      <div>
        <h2 class="text-base font-semibold mb-1">Advanced Settings</h2>
        <p class="text-sm text-base-content/60 mb-6">All sections are optional — defaults are production-safe.</p>

        <!-- Verbosity -->
        <div class="form-control mb-4">
          <label class="label" for="wz-verbosity">
            <span class="label-text font-medium">verbosity</span>
            <span class="label-text-alt text-base-content/50">
              <HelpCircle class="h-3 w-3 inline" /> Controls audit event display (full trail always captured)
            </span>
          </label>
          <select id="wz-verbosity" class="select select-bordered" bind:value={verbosity}>
            {#each VERBOSITIES as v}<option value={v}>{v}</option>{/each}
          </select>
        </div>

        <!-- Global NumCtx -->
        <div class="form-control mb-6">
          <label class="label" for="wz-global-numctx">
            <span class="label-text font-medium">num_ctx (global default)</span>
            <span class="label-text-alt text-base-content/50">
              <HelpCircle class="h-3 w-3 inline" /> Ollama context window; individual agents can override
            </span>
          </label>
          <input id="wz-global-numctx" type="number" min="0" class="input input-bordered" placeholder="0 = model default" bind:value={globalNumCtx} />
        </div>

        <!-- Peer Messaging -->
        <div class="mb-3">
          <button
            type="button"
            class="flex items-center gap-2 text-sm font-medium text-base-content/70 hover:text-base-content w-full"
            onclick={() => { showPeerMsg = !showPeerMsg; }}
          >
            {#if showPeerMsg}<ChevronUp class="h-4 w-4" />{:else}<ChevronDown class="h-4 w-4" />{/if}
            Peer Messaging
          </button>
          {#if showPeerMsg}
            <div class="border border-base-300/40 rounded-box p-4 mt-2 space-y-3 bg-base-200/50" transition:fly={{ y: -6, duration: 150 }}>
              <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div class="form-control">
                  <label class="label py-0" for="wz-maxmsg">
                    <span class="label-text text-xs">max_message_size (bytes)</span>
                  </label>
                  <input id="wz-maxmsg" type="number" min="0" class="input input-bordered input-sm" bind:value={maxMsgSize} />
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-maxinbox">
                    <span class="label-text text-xs">max_inbox_size</span>
                  </label>
                  <input id="wz-maxinbox" type="number" min="0" class="input input-bordered input-sm" bind:value={maxInboxSize} />
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-overflow">
                    <span class="label-text text-xs">overflow_action</span>
                  </label>
                  <select id="wz-overflow" class="select select-bordered select-sm" bind:value={overflowAction}>
                    {#each OVERFLOW_ACTIONS as a}<option value={a}>{a}</option>{/each}
                  </select>
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-inbox-ttl">
                    <span class="label-text text-xs">inbox_ttl</span>
                  </label>
                  <input id="wz-inbox-ttl" type="text" class="input input-bordered input-sm" placeholder="1h" bind:value={inboxTTL} />
                </div>
              </div>
            </div>
          {/if}
        </div>

        <!-- File Locking -->
        <div class="mb-3">
          <button
            type="button"
            class="flex items-center gap-2 text-sm font-medium text-base-content/70 hover:text-base-content w-full"
            onclick={() => { showFileLock = !showFileLock; }}
          >
            {#if showFileLock}<ChevronUp class="h-4 w-4" />{:else}<ChevronDown class="h-4 w-4" />{/if}
            File Locking
          </button>
          {#if showFileLock}
            <div class="border border-base-300/40 rounded-box p-4 mt-2 space-y-3 bg-base-200/50" transition:fly={{ y: -6, duration: 150 }}>
              <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
                <div class="form-control">
                  <label class="label py-0" for="wz-lease-ttl">
                    <span class="label-text text-xs">lease_ttl</span>
                  </label>
                  <input id="wz-lease-ttl" type="text" class="input input-bordered input-sm" placeholder="5m" bind:value={leaseTTL} />
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-hb-interval">
                    <span class="label-text text-xs">heartbeat_interval</span>
                  </label>
                  <input id="wz-hb-interval" type="text" class="input input-bordered input-sm" placeholder="30s" bind:value={heartbeatInterval} />
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-max-renewals">
                    <span class="label-text text-xs">max_renewals</span>
                  </label>
                  <input id="wz-max-renewals" type="number" min="0" class="input input-bordered input-sm" bind:value={maxRenewals} />
                </div>
              </div>
            </div>
          {/if}
        </div>

        <!-- Vector DB -->
        <div class="mb-3">
          <button
            type="button"
            class="flex items-center gap-2 text-sm font-medium text-base-content/70 hover:text-base-content w-full"
            onclick={() => { showVectorDB = !showVectorDB; }}
          >
            {#if showVectorDB}<ChevronUp class="h-4 w-4" />{:else}<ChevronDown class="h-4 w-4" />{/if}
            Vector DB
          </button>
          {#if showVectorDB}
            <div class="border border-base-300/40 rounded-box p-4 mt-2 space-y-3 bg-base-200/50" transition:fly={{ y: -6, duration: 150 }}>
              <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div class="form-control">
                  <label class="label py-0" for="wz-vecdb-type">
                    <span class="label-text text-xs">index_type</span>
                  </label>
                  <select id="wz-vecdb-type" class="select select-bordered select-sm" bind:value={vectorIndexType}>
                    {#each INDEX_TYPES as t}<option value={t}>{t}</option>{/each}
                  </select>
                </div>
              </div>
              <div class="flex flex-wrap gap-4">
                <label class="flex items-center gap-2 cursor-pointer text-xs">
                  <input type="checkbox" class="checkbox checkbox-xs" bind:checked={vectorEnabled} />
                  enabled
                </label>
                <label class="flex items-center gap-2 cursor-pointer text-xs">
                  <input type="checkbox" class="checkbox checkbox-xs" bind:checked={vectorAutoIndex} />
                  auto_index
                </label>
                <label class="flex items-center gap-2 cursor-pointer text-xs">
                  <input type="checkbox" class="checkbox checkbox-xs" bind:checked={vectorGPU} />
                  gpu_accelerated
                </label>
              </div>
            </div>
          {/if}
        </div>

        <!-- Recovery -->
        <div class="mb-3">
          <button
            type="button"
            class="flex items-center gap-2 text-sm font-medium text-base-content/70 hover:text-base-content w-full"
            onclick={() => { showRecovery = !showRecovery; }}
          >
            {#if showRecovery}<ChevronUp class="h-4 w-4" />{:else}<ChevronDown class="h-4 w-4" />{/if}
            Recovery
          </button>
          {#if showRecovery}
            <div class="border border-base-300/40 rounded-box p-4 mt-2 space-y-3 bg-base-200/50" transition:fly={{ y: -6, duration: 150 }}>
              <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div class="form-control">
                  <label class="label py-0" for="wz-max-restarts">
                    <span class="label-text text-xs">max_restarts</span>
                  </label>
                  <input id="wz-max-restarts" type="number" min="0" class="input input-bordered input-sm" bind:value={maxRestarts} />
                </div>
                <div class="form-control">
                  <label class="label py-0" for="wz-restart-backoff">
                    <span class="label-text text-xs">restart_backoff</span>
                  </label>
                  <input id="wz-restart-backoff" type="text" class="input input-bordered input-sm" placeholder="30s" bind:value={restartBackoff} />
                </div>
              </div>
              <label class="flex items-center gap-2 cursor-pointer text-xs">
                <input type="checkbox" class="checkbox checkbox-xs" bind:checked={autoRestart} />
                auto_restart
              </label>
            </div>
          {/if}
        </div>
      </div>

    <!-- ── Step 6: Review ── -->
    {:else if currentStep === 5}
      <div>
        <h2 class="text-base font-semibold mb-1">Review</h2>
        <p class="text-sm text-base-content/60 mb-4">Confirm your workflow definition before creating it.</p>

        <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <!-- Human-readable summary -->
          <div class="space-y-3">
            <h3 class="section-title">Summary</h3>

            <div class="card bg-base-200 border border-base-300/50 rounded-box">
              <div class="card-body p-4 space-y-2">
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">name</span>
                  <span class="font-mono font-semibold">{assembled.name}</span>
                </div>
                {#if assembled.description}
                  <div class="flex justify-between text-sm">
                    <span class="text-base-content/60">description</span>
                    <span class="text-right max-w-[60%]">{assembled.description}</span>
                  </div>
                {/if}
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">mode</span>
                  <span class="badge badge-sm badge-primary">{assembled.mode}</span>
                </div>
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">agents</span>
                  <span>{assembled.agents.length}</span>
                </div>
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">topology</span>
                  <span class="font-mono text-xs">{assembled.settings.topology}</span>
                </div>
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">isolation</span>
                  <span class="font-mono text-xs">{assembled.settings.isolation}</span>
                </div>
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">max_iterations</span>
                  <span>{assembled.settings.max_iterations}</span>
                </div>
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">max_parallelism</span>
                  <span>{assembled.settings.max_parallelism}</span>
                </div>
                <div class="flex justify-between text-sm">
                  <span class="text-base-content/60">total_timeout</span>
                  <span class="font-mono text-xs">{assembled.settings.total_timeout}</span>
                </div>
              </div>
            </div>

            <!-- Agent summaries -->
            {#each assembled.agents as ag, i}
              <div class="card bg-base-100 border border-base-300/40 rounded-box">
                <div class="card-body p-3 space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="badge badge-sm badge-outline">{ag.role}</span>
                    <span class="text-xs text-base-content/50">{ag.backend} / {ag.model || '—'}</span>
                    <span class="ml-auto text-xs text-base-content/40">×{ag.count}</span>
                  </div>
                  {#if ag.capabilities.length > 0}
                    <div class="flex flex-wrap gap-1">
                      {#each ag.capabilities as cap}
                        <span class="badge badge-xs badge-primary">{cap}</span>
                      {/each}
                    </div>
                  {/if}
                  {#if ag.mcp_scopes.length > 0}
                    <div class="flex flex-wrap gap-1">
                      {#each ag.mcp_scopes as scope}
                        <span class="badge badge-xs badge-secondary">{scope}</span>
                      {/each}
                    </div>
                  {/if}
                </div>
              </div>
            {/each}

            <button
              type="button"
              class="text-xs text-primary underline"
              onclick={() => onEditAdvanced(generatedYaml)}
            >
              Edit YAML directly in Advanced tab
            </button>
          </div>

          <!-- Generated YAML -->
          <div class="space-y-2">
            <button
              type="button"
              class="flex items-center gap-1 text-sm font-semibold text-base-content/70 hover:text-base-content"
              onclick={() => { showReviewYaml = !showReviewYaml; }}
            >
              {#if showReviewYaml}<ChevronUp class="h-4 w-4" />{:else}<ChevronDown class="h-4 w-4" />{/if}
              Generated YAML
            </button>
            {#if showReviewYaml}
              <pre
                class="bg-base-300 rounded-box p-4 text-xs font-mono overflow-auto max-h-[500px] whitespace-pre leading-relaxed"
                aria-label="generated-yaml"
              >{generatedYaml}</pre>
            {/if}
          </div>
        </div>

        {#if submitError}
          <div class="alert alert-error mt-4 py-2">
            <AlertCircle class="h-4 w-4" />
            <span>{submitError}</span>
          </div>
        {/if}
      </div>
    {/if}

    <!-- ── Live YAML preview (all steps except Review) ── -->
    {#if currentStep < 5}
      <div class="mt-auto pt-4 border-t border-base-300/30">
        <button
          type="button"
          class="flex items-center gap-1 text-xs text-base-content/40 hover:text-base-content/60"
          onclick={() => { showYamlLive = !showYamlLive; }}
        >
          {#if showYamlLive}<ChevronUp class="h-3 w-3" />{:else}<ChevronDown class="h-3 w-3" />{/if}
          Live YAML preview
        </button>
        {#if showYamlLive}
          <pre
            class="bg-base-300 rounded-box p-3 text-xs font-mono overflow-auto max-h-48 mt-2 whitespace-pre leading-relaxed"
            transition:fly={{ y: 6, duration: 150 }}
          >{generatedYaml}</pre>
        {/if}
      </div>
    {/if}

    <!-- ── Navigation buttons ── -->
    <div class="flex justify-between items-center pt-4 border-t border-base-300/30 mt-4">
      <button type="button" class="btn btn-ghost btn-sm" onclick={currentStep === 0 ? onCancel : back}>
        {currentStep === 0 ? 'Cancel' : 'Back'}
      </button>

      <div class="flex gap-2">
        {#if currentStep < STEPS.length - 1}
          <button
            type="button"
            class="btn btn-primary btn-sm"
            onclick={next}
            disabled={!canAdvance()}
          >
            Continue
          </button>
        {:else}
          <button
            type="button"
            class="btn btn-success btn-sm gap-1"
            onclick={() => { void submit(); }}
            disabled={submitting}
          >
            {#if submitting}
              <span class="loading loading-spinner loading-xs"></span>
            {/if}
            Create Workflow
          </button>
        {/if}
      </div>
    </div>
  </main>
</div>
