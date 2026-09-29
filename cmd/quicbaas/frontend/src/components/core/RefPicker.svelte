<!--
  Copyright (c) 2026 Jeremy Hahn
  Copyright (c) 2026 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  RefPicker is the one control every field that names another object this
  server controls — a secret path, a KMS key, a project — uses instead of a
  free-text input. It knows nothing about any one kind; everything kind
  -specific (how to list it, search it, create it, resolve its live status)
  comes from the `RefProvider` registered for `kind` (lib/refs/providers.ts).

  A full WAI-ARIA combobox: the input carries role="combobox" with
  aria-expanded/aria-controls/aria-activedescendant, the popover is a
  role="listbox" of role="option" rows, and every key the pattern defines
  (Up/Down/Home/End/Enter/Escape) moves or commits the active option. Rows
  are grouped when a provider's options carry a `group`, each shows the
  kind's glyph, its name, a secondary detail line, and a status dot.

  Creatable kinds (secret today) get a trailing "Create <kind> `<typed>`"
  row. Choosing it opens an inline panel — never a separate page — collects
  whatever extra fields the provider asks for (a secret's value), and on
  success selects the freshly created object.
-->
<script lang="ts">
  import { Check, ChevronDown, KeySquare, LoaderCircle, Plus, TriangleAlert, X } from 'lucide-svelte';
  import '../../lib/refs/providers.ts';
  import { getRefProvider } from '../../lib/refs/registry.ts';
  import { refStatusLabel, refStatusTone } from '../../lib/refs/status.ts';
  import type { RefKind, RefOption, RefProviderContext } from '../../lib/refs/types.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { themeStore } from '../../stores/theme.svelte.ts';
  import { STATUS_TONES } from '../../lib/theme/tokens.ts';

  let {
    kind,
    value = $bindable(''),
    tenantID,
    context,
    id,
    placeholder,
    disabled = false,
    ariaLabel,
    allowClear = true,
  }: {
    kind: RefKind;
    value?: string;
    /** tenantID scopes tenant-owned kinds; defaults to the signed-in tenant. */
    tenantID?: string;
    /** context adds provider-specific narrowing — the SSH key store's scope/owner, for example. */
    context?: Record<string, unknown>;
    id?: string;
    placeholder?: string;
    disabled?: boolean;
    ariaLabel?: string;
    allowClear?: boolean;
  } = $props();

  const provider = $derived(getRefProvider(kind));
  const ctx = $derived<RefProviderContext>({ tenantID: tenantID ?? authStore.tenantID, ...(context ?? {}) });

  const reactId = $derived(id ?? `ref-picker-${kind}`);
  const listboxId = $derived(`${reactId}-listbox`);

  let query = $state(value);
  let selected = $state<RefOption | null>(null);
  let open = $state(false);
  let loading = $state(false);
  let options = $state<RefOption[]>([]);
  let activeIndex = $state(-1);
  let showCreate = $state(false);
  let createValues = $state<Record<string, string>>({});
  let creating = $state(false);
  let createError = $state<string | null>(null);

  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  let requestSeq = 0;
  /**
   * syncedValue is the last value THIS component set — typing, selecting, or
   * clearing. The effect below only resolves a friendly label for a value
   * change it did not cause itself (a form reset, a fresh record loaded), so
   * every keystroke does not race its own resolution.
   */
  let syncedValue = value;

  /** resolveSelected fetches a value's label/status once, so the box can read a name, not a raw ID. */
  async function resolveSelected(forValue: string): Promise<void> {
    if (provider === undefined) return;
    if (forValue === '') {
      selected = null;
      return;
    }
    try {
      const found = (await provider.search(forValue, ctx)).find((o) => o.value === forValue);
      if (forValue !== value) return; // stale — value moved on while this was in flight
      if (found !== undefined) {
        selected = found;
        query = found.label;
        return;
      }
    } catch {
      // fall through to the best-effort fallback below
    }
    if (forValue !== value) return;
    const status = provider.status !== undefined ? await provider.status(forValue, ctx).catch(() => undefined) : undefined;
    if (forValue !== value) return;
    selected = { value: forValue, label: forValue, status };
  }

  // Resolve once for whatever value the field opens with.
  void resolveSelected(value);

  $effect(() => {
    if (value === syncedValue) return;
    syncedValue = value;
    query = value;
    selected = null;
    void resolveSelected(value);
  });

  function scheduleSearch(): void {
    if (searchTimer !== undefined) clearTimeout(searchTimer);
    searchTimer = setTimeout(() => void runSearch(), 150);
  }

  async function runSearch(): Promise<void> {
    if (provider === undefined) return;
    const seq = ++requestSeq;
    loading = true;
    try {
      const results = await provider.search(query, ctx);
      if (seq !== requestSeq) return;
      options = results;
      activeIndex = results.length > 0 ? 0 : -1;
      // A typed value that happens to already exist gets its status dot back
      // without waiting for a separate resolve — the search that lists it IS
      // the answer.
      const exact = results.find((o) => o.value === value);
      if (exact !== undefined) selected = exact;
    } catch {
      if (seq !== requestSeq) return;
      options = [];
      activeIndex = -1;
    } finally {
      if (seq === requestSeq) loading = false;
    }
  }

  function openPicker(): void {
    if (disabled || provider === undefined) return;
    open = true;
    showCreate = false;
    void runSearch();
  }

  function closePicker(): void {
    open = false;
    showCreate = false;
    activeIndex = -1;
  }

  /**
   * onInput mirrors the typed text straight into `value`, the same as a
   * plain text input would — the popover's options are suggestions, not a
   * gate. A kind whose value IS the typed name (a secret path, a KMS key)
   * needs nothing more; a kind addressed by opaque ID is guided to the right
   * one by the suggestions, but a caller who already knows the ID (pasted
   * from elsewhere) is never blocked from typing it directly. The server
   * validates the saved reference either way.
   */
  function onInput(e: Event): void {
    const text = (e.currentTarget as HTMLInputElement).value;
    query = text;
    value = text;
    syncedValue = text;
    selected = null;
    if (!open) openPicker();
    else scheduleSearch();
  }

  function onBlur(): void {
    // Snap the visible text to the resolved label once focus leaves, so a
    // typed ID that matched something reads as its name afterward.
    if (selected !== null) query = selected.label;
  }

  function commit(option: RefOption): void {
    query = option.label;
    value = option.value;
    syncedValue = option.value;
    selected = option;
    closePicker();
  }

  function clearValue(): void {
    query = '';
    value = '';
    syncedValue = '';
    selected = null;
    options = [];
  }

  /** trailingCreate is the "Create <kind> `<typed>`" row, offered when the provider allows it and nothing matches exactly. */
  const trailingCreate = $derived.by(() => {
    if (provider?.create === undefined) return null;
    const typed = query.trim();
    if (typed === '') return null;
    if (options.some((o) => o.value === typed)) return null;
    return typed;
  });

  const rowCount = $derived(options.length + (trailingCreate !== null ? 1 : 0));

  function optionId(index: number): string {
    return `${listboxId}-opt-${index}`;
  }

  function openCreatePanel(): void {
    showCreate = true;
    createError = null;
    createValues = {};
    for (const field of provider?.createFields ?? []) createValues[field.key] = '';
  }

  async function submitCreate(): Promise<void> {
    if (provider?.create === undefined) return;
    const typed = query.trim();
    if (typed === '') return;
    for (const field of provider.createFields ?? []) {
      if (field.required === true && (createValues[field.key] ?? '').trim() === '') {
        createError = `${field.label} is required.`;
        return;
      }
    }
    creating = true;
    createError = null;
    try {
      const result = await provider.create(typed, createValues, ctx);
      commit(result);
    } catch (e) {
      createError = e instanceof Error ? e.message : 'Create failed.';
    } finally {
      creating = false;
    }
  }

  function onKeydown(e: KeyboardEvent): void {
    if (disabled) return;
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        e.preventDefault();
        openPicker();
      }
      return;
    }
    if (showCreate) {
      if (e.key === 'Escape') {
        e.preventDefault();
        showCreate = false;
      }
      return;
    }
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        activeIndex = rowCount === 0 ? -1 : (activeIndex + 1) % rowCount;
        break;
      case 'ArrowUp':
        e.preventDefault();
        activeIndex = rowCount === 0 ? -1 : (activeIndex - 1 + rowCount) % rowCount;
        break;
      case 'Home':
        e.preventDefault();
        activeIndex = rowCount === 0 ? -1 : 0;
        break;
      case 'End':
        e.preventDefault();
        activeIndex = rowCount - 1;
        break;
      case 'Enter':
        e.preventDefault();
        if (activeIndex < 0) return;
        if (activeIndex < options.length) {
          const option = options[activeIndex];
          if (option !== undefined) commit(option);
        } else {
          openCreatePanel();
        }
        break;
      case 'Escape':
        e.preventDefault();
        closePicker();
        break;
      case 'Tab':
        closePicker();
        break;
      default:
        break;
    }
  }

  const tone = $derived(themeStore.theme === 'light' ? STATUS_TONES.light : STATUS_TONES.dark);

  /** grouped folds options into (group, options) buckets, preserving first-seen order. */
  const grouped = $derived.by(() => {
    const order: string[] = [];
    const buckets = new Map<string, RefOption[]>();
    for (const option of options) {
      const key = option.group ?? '';
      if (!buckets.has(key)) {
        buckets.set(key, []);
        order.push(key);
      }
      buckets.get(key)?.push(option);
    }
    return order.map((key) => ({ group: key, items: buckets.get(key) ?? [] }));
  });

  function flatIndexOfGroupItem(groupIndex: number, itemIndex: number): number {
    let base = 0;
    for (let g = 0; g < groupIndex; g++) base += grouped[g]?.items.length ?? 0;
    return base + itemIndex;
  }
</script>

<div class="relative" data-testid="ref-picker-{kind}">
  <div
    class="input input-bordered input-sm flex w-full items-center gap-2 {disabled ? 'opacity-60' : ''}"
    class:input-primary={open}
  >
    {#if provider !== undefined}
      {@const Glyph = provider.glyph}
      <Glyph class="h-3.5 w-3.5 shrink-0 text-base-content/40" />
    {:else}
      <KeySquare class="h-3.5 w-3.5 shrink-0 text-base-content/40" />
    {/if}
    <input
      id={reactId}
      type="text"
      class="min-w-0 flex-1 bg-transparent font-mono text-sm outline-none"
      role="combobox"
      aria-expanded={open}
      aria-controls={listboxId}
      aria-autocomplete="list"
      aria-activedescendant={open && activeIndex >= 0 ? optionId(activeIndex) : undefined}
      aria-label={ariaLabel ?? (provider !== undefined ? `${provider.label} reference` : 'Reference')}
      autocomplete="off"
      {disabled}
      {placeholder}
      value={query}
      onfocus={openPicker}
      oninput={onInput}
      onkeydown={onKeydown}
      onblur={onBlur}
    />
    {#if selected?.status !== undefined}
      <span
        class="h-1.5 w-1.5 shrink-0 rounded-full"
        style:background-color={tone[refStatusTone(selected.status)].fg}
        title={refStatusLabel(selected.status)}
        aria-hidden="true"
      ></span>
    {/if}
    {#if allowClear && value !== '' && !disabled}
      <button
        type="button"
        class="btn btn-ghost btn-xs btn-circle shrink-0"
        aria-label="Clear {provider?.label ?? 'reference'}"
        onclick={clearValue}
      >
        <X class="h-3 w-3" />
      </button>
    {/if}
    <ChevronDown class="h-3.5 w-3.5 shrink-0 text-base-content/30" aria-hidden="true" />
  </div>

  {#if open}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <div
      class="absolute z-30 mt-1 w-full min-w-[20rem] overflow-hidden rounded-box border border-base-300 bg-base-100 shadow-lg"
      data-testid="ref-picker-{kind}-popover"
    >
      {#if showCreate}
        <div class="flex flex-col gap-2 p-3" data-testid="ref-picker-{kind}-create-panel">
          <p class="text-xs font-medium text-base-content">
            {provider?.createLabel ?? 'Create'} <code class="font-mono text-primary">{query.trim()}</code>
          </p>
          {#each provider?.createFields ?? [] as field (field.key)}
            <div>
              <label class="label py-0.5" for="{reactId}-create-{field.key}">
                <span class="label-text text-xs">{field.label}{field.required === true ? ' *' : ''}</span>
              </label>
              {#if field.type === 'textarea'}
                <textarea
                  id="{reactId}-create-{field.key}"
                  class="textarea textarea-bordered textarea-sm w-full font-mono"
                  placeholder={field.placeholder}
                  bind:value={createValues[field.key]}
                ></textarea>
              {:else}
                <input
                  id="{reactId}-create-{field.key}"
                  type={field.type}
                  class="input input-bordered input-sm w-full font-mono"
                  placeholder={field.placeholder}
                  bind:value={createValues[field.key]}
                />
              {/if}
            </div>
          {/each}
          {#if createError !== null}
            <p class="text-xs text-error" role="alert">{createError}</p>
          {/if}
          <div class="flex items-center justify-end gap-2 pt-1">
            <button type="button" class="btn btn-ghost btn-xs" onclick={() => { showCreate = false; }}>
              Cancel
            </button>
            <button
              type="button"
              class="btn btn-primary btn-xs gap-1"
              disabled={creating}
              onclick={() => void submitCreate()}
            >
              {#if creating}
                <LoaderCircle class="h-3 w-3 animate-spin" />
              {:else}
                <Check class="h-3 w-3" />
              {/if}
              Create
            </button>
          </div>
        </div>
      {:else}
        <ul role="listbox" id={listboxId} class="max-h-72 overflow-y-auto py-1" aria-label={provider?.label ?? 'Options'}>
          {#if loading}
            <li class="flex items-center gap-2 px-3 py-2 text-xs text-base-content/50">
              <LoaderCircle class="h-3.5 w-3.5 animate-spin" />
              Searching…
            </li>
          {:else if grouped.length === 0 && trailingCreate === null}
            <li class="px-3 py-3 text-xs text-base-content/50" data-testid="ref-picker-{kind}-empty">
              {#if query.trim() === ''}
                No {provider?.label.toLowerCase() ?? 'options'} yet.
              {:else}
                Nothing matches “{query.trim()}”.
              {/if}
              {#if provider?.create === undefined}
                Check the spelling, or ask an operator with access to create one.
              {/if}
            </li>
          {:else}
            {#each grouped as bucket, groupIndex (bucket.group || groupIndex)}
              {#if bucket.group !== ''}
                <li class="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-base-content/40" role="presentation">
                  {bucket.group}
                </li>
              {/if}
              {#each bucket.items as option, itemIndex (option.value)}
                {@const flatIndex = flatIndexOfGroupItem(groupIndex, itemIndex)}
                {@const active = flatIndex === activeIndex}
                <!-- Options are not independent tab-stops: the combobox
                     input above owns keyboard navigation per the WAI-ARIA
                     combobox pattern; a click is a mouse-only convenience. -->
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                <li
                  id={optionId(flatIndex)}
                  role="option"
                  aria-selected={option.value === value}
                  class="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm {active ? 'bg-primary/10' : 'hover:bg-base-200'}"
                  data-testid="ref-picker-{kind}-option-{option.value}"
                  onmouseenter={() => { activeIndex = flatIndex; }}
                  onclick={() => commit(option)}
                >
                  {#if provider !== undefined}
                    {@const Glyph = provider.glyph}
                    <Glyph class="h-3.5 w-3.5 shrink-0 text-base-content/40" aria-hidden="true" />
                  {/if}
                  <span class="min-w-0 flex-1 truncate">
                    <span class="font-mono">{option.label}</span>
                    {#if option.detail !== undefined && option.detail !== ''}
                      <span class="ml-1.5 text-xs text-base-content/50">{option.detail}</span>
                    {/if}
                  </span>
                  {#if option.status !== undefined}
                    <span
                      class="h-1.5 w-1.5 shrink-0 rounded-full"
                      style:background-color={tone[refStatusTone(option.status)].fg}
                      title={refStatusLabel(option.status)}
                      aria-hidden="true"
                    ></span>
                  {/if}
                  {#if option.value === value}
                    <Check class="h-3.5 w-3.5 shrink-0 text-primary" aria-hidden="true" />
                  {/if}
                </li>
              {/each}
            {/each}
            {#if trailingCreate !== null}
              {@const active = options.length === activeIndex}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
              <li
                id={optionId(options.length)}
                role="option"
                aria-selected={false}
                class="flex cursor-pointer items-center gap-2 border-t border-base-300 px-3 py-1.5 text-sm text-primary {active ? 'bg-primary/10' : 'hover:bg-base-200'}"
                data-testid="ref-picker-{kind}-create-row"
                onmouseenter={() => { activeIndex = options.length; }}
                onclick={openCreatePanel}
              >
                <Plus class="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                <span>{provider?.createLabel ?? 'Create'} <code class="font-mono">{trailingCreate}</code></span>
              </li>
            {/if}
          {/if}
        </ul>
      {/if}
    </div>
    <!-- A click outside closes the popover without a global listener leak: a
         full-viewport transparent button below the panel, above everything
         else — the same pattern DataTable's row menus use. -->
    <button
      type="button"
      class="fixed inset-0 z-20 cursor-default"
      aria-label="Close {provider?.label ?? 'reference'} picker"
      tabindex="-1"
      onclick={closePicker}
    ></button>
  {/if}
</div>

{#if provider === undefined}
  <p class="mt-1 flex items-center gap-1 text-xs text-warning" role="alert">
    <TriangleAlert class="h-3 w-3" />
    No picker is registered for "{kind}" yet — reference by hand.
  </p>
{/if}
