<!--
  JobRouteHarness follows a `#/jobs/:id` route with the same effect MainApp
  uses: keyed on the job id AND readyScope(), handing the reads to
  followJobRoute. It lets a test drive the real orchestrator and auth stores
  through a scope change mid-read without mounting the whole shell.
-->
<script lang="ts">
  import { followJobRoute, readyScope } from '../../src/stores/orchestrator.svelte.ts';

  let { jobID }: { jobID: string } = $props();

  $effect(() => {
    const id = jobID;
    if (readyScope() === '') return;
    followJobRoute(id);
  });
</script>
