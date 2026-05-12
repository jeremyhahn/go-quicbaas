<!--
  PresenceIndicator shows an avatar stack of currently-attached operators.
  Operator identities are tracked from operator_joined/operator_left events.
-->
<script lang="ts">
  import { sessionStore } from '../../stores/sessionStore.svelte.ts';

  // Derive the current set of present operators from the event log.
  const presentOperators = $derived((): string[] => {
    const present = new Set<string>();
    for (const ev of sessionStore.events) {
      if (ev.kind === 'operator_joined' && ev.operatorId) {
        present.add(ev.operatorId);
      } else if (ev.kind === 'operator_left' && ev.operatorId) {
        present.delete(ev.operatorId);
      }
    }
    return Array.from(present);
  });

  function initials(id: string): string {
    return id.slice(0, 2).toUpperCase();
  }

  // Colour hue is deterministically derived from operator ID so avatars
  // are stable across re-renders.
  function avatarHue(id: string): number {
    let h = 0;
    for (let i = 0; i < id.length; i++) {
      h = (h * 31 + id.charCodeAt(i)) & 0xffff;
    }
    return h % 360;
  }
</script>

{#if presentOperators().length > 0}
  <div
    class="flex items-center gap-1"
    role="status"
    aria-label="{presentOperators().length} operator(s) attached"
  >
    <div class="avatar-group -space-x-3">
      {#each presentOperators().slice(0, 5) as op (op)}
        <div
          class="avatar placeholder"
          title={op}
        >
          <div
            class="w-6 h-6 rounded-full text-white text-xs flex items-center justify-center select-none"
            style="background: oklch(0.55 0.18 {avatarHue(op)}deg)"
            aria-label="Operator {op}"
          >
            {initials(op)}
          </div>
        </div>
      {/each}
      {#if presentOperators().length > 5}
        <div class="avatar placeholder">
          <div class="w-6 h-6 rounded-full bg-base-300 text-xs flex items-center justify-center">
            +{presentOperators().length - 5}
          </div>
        </div>
      {/if}
    </div>
    <span class="text-xs text-base-content/40">
      {presentOperators().length} attached
    </span>
  </div>
{/if}
