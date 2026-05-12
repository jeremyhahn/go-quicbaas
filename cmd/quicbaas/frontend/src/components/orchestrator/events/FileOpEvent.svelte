<!--
  FileOpEvent shows a file operation (read/write/edit/delete) with icon,
  operation badge, and path. For 'edit' operations the diff is shown inline.
-->
<script lang="ts">
  import InlineDiff from './InlineDiff.svelte';

  let {
    op,
    path,
    diff = '',
  }: {
    op: string;
    path: string;
    diff?: string;
  } = $props();

  const opIcon: Record<string, string> = {
    read:   '&#x1F4C4;',
    write:  '&#x270F;&#xFE0F;',
    edit:   '&#x1F4DD;',
    delete: '&#x1F5D1;&#xFE0F;',
    create: '&#x2795;',
    rename: '&#x2194;&#xFE0F;',
  };

  const opBadgeClass: Record<string, string> = {
    read:   'badge-info',
    write:  'badge-warning',
    edit:   'badge-warning',
    delete: 'badge-error',
    create: 'badge-success',
    rename: 'badge-ghost',
  };

  const icon = $derived(opIcon[op] ?? '&#x1F4C1;');
  const badgeClass = $derived(opBadgeClass[op] ?? 'badge-ghost');
  const showDiff = $derived(op === 'edit' && diff.length > 0);
</script>

<div class="flex flex-col gap-1 my-0.5">
  <div class="flex items-center gap-2 text-sm">
    <span aria-hidden="true">{@html icon}</span>
    <span class="badge badge-xs {badgeClass}">{op}</span>
    <span class="font-mono text-xs text-base-content/70 truncate max-w-sm" title={path}>
      {path}
    </span>
  </div>
  {#if showDiff}
    <InlineDiff {diff} />
  {/if}
</div>
