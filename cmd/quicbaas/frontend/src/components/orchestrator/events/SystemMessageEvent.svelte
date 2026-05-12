<!--
  SystemMessageEvent renders a muted informational / warning / error banner.
-->
<script lang="ts">
  let {
    message,
    level = 'info',
  }: {
    message: string;
    level?: 'info' | 'warn' | 'error';
  } = $props();

  const alertClass: Record<string, string> = {
    info:  'alert-info',
    warn:  'alert-warning',
    error: 'alert-error',
  };

  const chipLabel: Record<string, string> = {
    info:  'info',
    warn:  'warn',
    error: 'error',
  };

  const cls = $derived(alertClass[level] ?? 'alert-info');
  const chip = $derived(chipLabel[level] ?? 'info');
</script>

<div
  class="alert {cls} py-1.5 px-3 text-xs flex items-start gap-2 my-0.5"
  role="status"
  aria-label="System message"
>
  <span class="badge badge-xs badge-outline">{chip}</span>
  <span class="whitespace-pre-wrap break-words">{message}</span>
</div>
